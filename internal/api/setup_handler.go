// api 包 — setup handler
//
// 处理 POST /api/v1/setup/admin 请求
// 初始化管理员账户，只能执行一次
package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/auth"
	"github.com/luoye663/nxpanel/internal/settings"
)

type setupAdminRequest struct {
	Username            string `json:"username"`
	Password            string `json:"password"`
	CaptchaProvider     string `json:"captcha_provider,omitempty"`
	CaptchaSiteKey      string `json:"captcha_site_key,omitempty"`
	CaptchaSecretKey    string `json:"captcha_secret_key,omitempty"`
	CaptchaTriggerAfter *int   `json:"captcha_trigger_after_failures,omitempty"`
	LoginPath           string `json:"login_path,omitempty"`
}

// handleSetupAdmin 处理 POST /api/v1/setup/admin
//
// 请求体：
//
//	{"username":"admin","password":"your-password"}
//
// 成功响应（201）：
//
//	{"request_id":"xxx","success":true,"data":{"username":"admin"},"error":null}
//
// 错误响应：
//   - 409 CONFLICT: 管理员已初始化
//   - 422 VALIDATION_FAILED: 参数校验失败
func (s *Server) handleSetupAdmin(w http.ResponseWriter, r *http.Request) {
	// 限制请求体大小为 2MB，避免初始化接口接受异常大请求。
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)

	var req setupAdminRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	if req.CaptchaProvider != "" && req.CaptchaProvider != "none" {
		if req.CaptchaSiteKey == "" || req.CaptchaSecretKey == "" {
			WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
				"启用验证码时 site_key 和 secret_key 不能为空", nil)
			return
		}
	}
	finalLoginPath := s.CurrentLoginPath()
	if req.LoginPath != "" {
		if err := app.ValidateLoginPath(req.LoginPath); err != nil {
			WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
				"登录路径格式无效: "+err.Error(), nil)
			return
		}
		finalLoginPath = req.LoginPath
	}

	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable,
			"Agent 未配置或未启动，无法初始化管理员", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if _, err := s.agentClient.Health(ctx); err != nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable,
			"Agent 未启动或不可用，无法初始化管理员", nil)
		return
	}

	if err := s.authSvc.SetupAdmin(req.Username, req.Password); err != nil {
		handleAuthError(w, r, err)
		return
	}
	s.SetNeedsSetup(false)

	if s.settingsSvc != nil && finalLoginPath != s.CurrentLoginPath() {
		_, err := s.settingsSvc.UpdateSecuritySettings(r.Context(), &settings.UpdateSecuritySettingsRequest{
			LoginPath: &finalLoginPath,
		})
		if err != nil {
			WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable,
				"管理员已创建，但登录路径回写失败；请使用当前入口继续登录并在安全设置中重试", map[string]any{"login_path": s.CurrentLoginPath()})
			return
		}
	}

	if req.CaptchaProvider != "" && req.CaptchaProvider != "none" {
		triggerAfter := 3
		if req.CaptchaTriggerAfter != nil && *req.CaptchaTriggerAfter >= 0 {
			triggerAfter = *req.CaptchaTriggerAfter
		}
		if s.settingsSvc != nil {
			_, _ = s.settingsSvc.UpdateSecuritySettings(r.Context(), &settings.UpdateSecuritySettingsRequest{
				CaptchaProvider:     &req.CaptchaProvider,
				CaptchaSiteKey:      &req.CaptchaSiteKey,
				CaptchaSecretKey:    &req.CaptchaSecretKey,
				CaptchaTriggerAfter: &triggerAfter,
			})
		}
	}

	WriteCreated(w, r, map[string]any{
		"username":   req.Username,
		"login_path": s.CurrentLoginPath(),
	})
}

// handleAuthError 统一处理认证错误，转换为对应的 HTTP 响应
func handleAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrAdminAlreadyExists):
		WriteError(w, r, http.StatusConflict, app.ErrConflict,
			"管理员已初始化，不能重复执行", nil)
	case errors.Is(err, auth.ErrInvalidCredentials):
		WriteError(w, r, http.StatusUnauthorized, app.ErrUnauthorized,
			"用户名或密码错误", nil)
	case errors.Is(err, auth.ErrUsernameRequired):
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"用户名不能为空", nil)
	case errors.Is(err, auth.ErrUsernameTooLong):
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"用户名不能超过 50 个字符", nil)
	case errors.Is(err, auth.ErrPasswordRequired):
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"密码不能为空", nil)
	case errors.Is(err, auth.ErrPasswordTooShort):
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"密码长度不能少于 8 个字符", nil)
	case errors.Is(err, auth.ErrPasswordTooLong):
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"密码长度不能超过 72 个字符", nil)
	case errors.Is(err, auth.ErrPasswordTooWeak):
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"密码强度不足，需包含大小写字母、数字、特殊字符中的至少 3 种", nil)
	case errors.Is(err, auth.ErrInvalidTOTP):
		WriteError(w, r, http.StatusBadRequest, "INVALID_TOTP_CODE",
			"验证码错误", nil)
	case errors.Is(err, auth.ErrTOTPCodeReplayed):
		WriteError(w, r, http.StatusTooManyRequests, "TOTP_CODE_REPLAYED",
			"该验证码已被使用，请等待新验证码", nil)
	case errors.Is(err, auth.ErrInvalidRecoveryCode):
		WriteError(w, r, http.StatusBadRequest, "INVALID_RECOVERY_CODE",
			"恢复码错误或已使用", nil)
	default:
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError,
			"内部错误", nil)
	}
}
