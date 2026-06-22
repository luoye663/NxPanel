package api

import (
	"net/http"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
)

func (s *Server) checkSensitiveActionLimit(w http.ResponseWriter, r *http.Request) (string, bool) {
	session := middleware.GetSession(r.Context())
	if session == nil {
		WriteError(w, r, http.StatusUnauthorized, app.ErrUnauthorized,
			"未登录", nil)
		return "", false
	}
	if s.sensitiveActionLimiter != nil && !s.sensitiveActionLimiter.Check(session.ID) {
		WriteError(w, r, http.StatusTooManyRequests, "TOO_MANY_REQUESTS",
			"敏感操作失败次数过多，请稍后再试", nil)
		return "", false
	}
	return session.ID, true
}

func (s *Server) handle2FAStatus(w http.ResponseWriter, r *http.Request) {
	admin, err := s.authSvc.GetAdminInfo()
	if err != nil || admin == nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError,
			"查询管理员信息失败", nil)
		return
	}
	WriteOK(w, r, map[string]any{
		"enabled":      admin.TOTPEnabled,
		"has_recovery": admin.RecoveryCodes != "" && admin.RecoveryCodes != "[]",
	})
}

type twoFASetupResponse struct {
	Secret string `json:"secret"`
	URL    string `json:"url"`
}

func (s *Server) handle2FASetup(w http.ResponseWriter, r *http.Request) {
	admin, err := s.authSvc.GetAdminInfo()
	if err != nil || admin == nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError,
			"查询管理员信息失败", nil)
		return
	}
	if admin.TOTPEnabled {
		WriteError(w, r, http.StatusConflict, app.ErrConflict,
			"两步验证已启用", nil)
		return
	}

	secret, url, err := s.twofaSvc.GenerateSecret(admin.Username)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError,
			"生成 TOTP 密钥失败", nil)
		return
	}

	session := middleware.GetSession(r.Context())
	if session != nil {
		s.twofaSvc.StorePendingSecret(session.ID, secret)
	}

	WriteOK(w, r, twoFASetupResponse{
		Secret: secret,
		URL:    url,
	})
}

type twoFAEnableRequest struct {
	Code string `json:"code"`
}

type twoFAEnableResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

func (s *Server) handle2FAEnable(w http.ResponseWriter, r *http.Request) {
	var req twoFAEnableRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" {
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"验证码不能为空", nil)
		return
	}

	admin, err := s.authSvc.GetAdminInfo()
	if err != nil || admin == nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError,
			"查询管理员信息失败", nil)
		return
	}
	if admin.TOTPEnabled {
		WriteError(w, r, http.StatusConflict, app.ErrConflict,
			"两步验证已启用", nil)
		return
	}
	sessionID, ok := s.checkSensitiveActionLimit(w, r)
	if !ok {
		return
	}

	session := middleware.GetSession(r.Context())
	if session == nil {
		WriteError(w, r, http.StatusUnauthorized, app.ErrUnauthorized,
			"未登录", nil)
		return
	}

	secret, ok := s.twofaSvc.PopPendingSecret(session.ID)
	if !ok {
		WriteError(w, r, http.StatusBadRequest, "NO_PENDING_SECRET",
			"没有待确认的密钥，请重新执行设置步骤", nil)
		return
	}

	recoveryCodes, err := s.twofaSvc.Enable(admin.ID, secret, req.Code)
	if err != nil {
		// 启用确认同样是 TOTP 校验，失败后纳入统一的已登录敏感操作限流。
		if s.sensitiveActionLimiter != nil {
			s.sensitiveActionLimiter.RecordFail(sessionID)
		}
		WriteError(w, r, http.StatusBadRequest, "INVALID_TOTP_CODE",
			"验证码错误", nil)
		return
	}
	if s.sensitiveActionLimiter != nil {
		s.sensitiveActionLimiter.Reset(sessionID)
	}

	WriteOK(w, r, twoFAEnableResponse{RecoveryCodes: recoveryCodes})
}

type twoFADisableRequest struct {
	Code string `json:"code"`
}

func (s *Server) handle2FADisable(w http.ResponseWriter, r *http.Request) {
	var req twoFADisableRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" {
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"验证码不能为空", nil)
		return
	}

	admin, err := s.authSvc.GetAdminInfo()
	if err != nil || admin == nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError,
			"查询管理员信息失败", nil)
		return
	}
	if !admin.TOTPEnabled {
		WriteError(w, r, http.StatusBadRequest, "2FA_NOT_ENABLED",
			"两步验证未启用", nil)
		return
	}
	sessionID, ok := s.checkSensitiveActionLimit(w, r)
	if !ok {
		return
	}

	if err := s.twofaSvc.VerifyAndConsumeCode(admin, req.Code); err != nil {
		// 只在 TOTP 校验失败后记数，防止已登录会话暴力枚举 6 位验证码。
		if s.sensitiveActionLimiter != nil {
			s.sensitiveActionLimiter.RecordFail(sessionID)
		}
		handleAuthError(w, r, err)
		return
	}
	if s.sensitiveActionLimiter != nil {
		s.sensitiveActionLimiter.Reset(sessionID)
	}

	if err := s.twofaSvc.Disable(admin.ID); err != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError,
			"禁用两步验证失败", nil)
		return
	}

	session := middleware.GetSession(r.Context())
	if session != nil {
		sessions := []string{session.ID}
		for _, sid := range sessions {
			_ = s.authSvc.Logout(sid)
		}
	}

	WriteOK(w, r, map[string]any{"disabled": true})
}

type twoFARegenerateRequest struct {
	Code string `json:"code"`
}

type twoFARegenerateResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

func (s *Server) handle2FARegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	var req twoFARegenerateRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.Code == "" {
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"验证码不能为空", nil)
		return
	}

	admin, err := s.authSvc.GetAdminInfo()
	if err != nil || admin == nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError,
			"查询管理员信息失败", nil)
		return
	}
	if !admin.TOTPEnabled {
		WriteError(w, r, http.StatusBadRequest, "2FA_NOT_ENABLED",
			"两步验证未启用", nil)
		return
	}
	sessionID, ok := s.checkSensitiveActionLimit(w, r)
	if !ok {
		return
	}

	if err := s.twofaSvc.VerifyAndConsumeCode(admin, req.Code); err != nil {
		// 恢复码重生成会暴露新的备用凭据，失败同样纳入 session 级限流。
		if s.sensitiveActionLimiter != nil {
			s.sensitiveActionLimiter.RecordFail(sessionID)
		}
		handleAuthError(w, r, err)
		return
	}
	if s.sensitiveActionLimiter != nil {
		s.sensitiveActionLimiter.Reset(sessionID)
	}

	codes, err := s.twofaSvc.RegenerateRecoveryCodes(admin.ID)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError,
			"重新生成恢复码失败", nil)
		return
	}

	WriteOK(w, r, twoFARegenerateResponse{RecoveryCodes: codes})
}
