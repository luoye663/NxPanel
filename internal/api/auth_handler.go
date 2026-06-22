package api

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/auth"
	"github.com/luoye663/nxpanel/internal/captcha"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

type loginRequest struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	CaptchaToken string `json:"captcha_token,omitempty"`
}

type loginResponse struct {
	Username    string `json:"username"`
	Requires2FA bool   `json:"requires_2fa"`
	TempToken   string `json:"temp_token,omitempty"`
}

type login2FARequest struct {
	TempToken string `json:"temp_token"`
	Code      string `json:"code"`
}

type loginRecoverRequest struct {
	TempToken    string `json:"temp_token"`
	RecoveryCode string `json:"recovery_code"`
}

func getIP(r *http.Request) string {
	ip := middleware.GetRealIP(r.Context())
	if ip == "" {
		return r.RemoteAddr
	}
	return ip
}

func (s *Server) auditLogin(username, ip, ua string, success bool, reason string, captchaVerified, totpUsed bool) {
	if s.loginAuditRepo != nil {
		_ = s.loginAuditRepo.Record(&repo.LoginAudit{
			Username:        username,
			IP:              ip,
			UserAgent:       ua,
			Success:         success,
			FailureReason:   reason,
			CaptchaVerified: captchaVerified,
			TOTPUsed:        totpUsed,
		})
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)

	ip := getIP(r)
	ua := r.UserAgent()

	if s.limiter != nil && !s.limiter.Check(ip) {
		WriteError(w, r, http.StatusTooManyRequests, "TOO_MANY_REQUESTS",
			"登录失败次数过多，请稍后再试", nil)
		return
	}

	var req loginRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	if req.Username == "" || req.Password == "" {
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"用户名和密码不能为空", nil)
		return
	}

	captchaVerified := false
	if s.captchaSvc != nil && s.captchaSvc.Enabled() {
		failCount := 0
		if s.limiter != nil {
			failCount = s.limiter.FailCount(ip)
		}
		if s.captchaSvc.ShouldTrigger(failCount) {
			if err := s.captchaSvc.VerifyToken(req.CaptchaToken, ip); err != nil {
				slog.Warn("CAPTCHA 验证失败",
					"request_id", middleware.GetRequestID(r.Context()),
					"username", req.Username,
					"ip", ip,
					"provider", s.captchaProviderName(),
					"reason", err.Error(),
				)
				s.auditLogin(req.Username, ip, ua, false, "CAPTCHA验证失败", false, false)
				WriteError(w, r, http.StatusBadRequest, "CAPTCHA_FAILED",
					"CAPTCHA验证失败", nil)
				return
			}
			captchaVerified = true
		}
	}

	result, err := s.authSvc.Login(req.Username, req.Password, ua, ip)
	if err != nil {
		if s.limiter != nil && errors.Is(err, auth.ErrInvalidCredentials) {
			s.limiter.RecordFail(ip)
		}
		s.auditLogin(req.Username, ip, ua, false, "用户名或密码错误", captchaVerified, false)
		handleAuthError(w, r, err)
		return
	}

	if s.limiter != nil {
		s.limiter.Reset(ip)
	}

	if result.TOTPEnabled && s.twofaSvc != nil {
		// temp_token 绑定当前 IP 与 User-Agent，只允许原登录上下文继续完成 2FA。
		tempToken := s.twofaSvc.GetTempStore().Create(result.AdminID, result.Username, ip, ua)
		s.auditLogin(req.Username, ip, ua, false, "password_ok_waiting_2fa", captchaVerified, false)
		WriteOK(w, r, loginResponse{
			Username:    result.Username,
			Requires2FA: true,
			TempToken:   tempToken,
		})
		return
	}

	s.setSessionCookies(w, r, result.SessionID, result.CSRFToken)
	s.auditLogin(req.Username, ip, ua, true, "", captchaVerified, false)
	WriteOK(w, r, loginResponse{
		Username:    result.Username,
		Requires2FA: false,
	})
}

func (s *Server) handleLogin2FA(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	ip := getIP(r)
	ua := r.UserAgent()

	var req login2FARequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.TempToken == "" || req.Code == "" {
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"临时令牌和验证码不能为空", nil)
		return
	}

	entry, valid := s.twofaSvc.GetTempStore().ValidateContext(req.TempToken, ip, ua)
	if !valid {
		s.twofaSvc.GetTempStore().RecordFailure(req.TempToken)
		if s.limiter != nil {
			s.limiter.RecordFail(ip)
		}
		WriteError(w, r, http.StatusUnauthorized, app.ErrUnauthorized,
			"临时令牌无效或已过期", nil)
		return
	}

	admin, err := s.authSvc.GetAdminInfo()
	if err != nil || admin == nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError,
			"内部错误", nil)
		return
	}

	if err := s.twofaSvc.VerifyAndConsumeCode(admin, req.Code); err != nil {
		s.twofaSvc.GetTempStore().RecordFailure(req.TempToken)
		if s.limiter != nil {
			s.limiter.RecordFail(ip)
		}
		s.auditLogin(entry.Username, ip, ua, false, "验证码错误", false, true)
		handleAuthError(w, r, err)
		return
	}
	entry, valid = s.twofaSvc.GetTempStore().Consume(req.TempToken, ip, ua)
	if !valid {
		s.twofaSvc.GetTempStore().RecordFailure(req.TempToken)
		if s.limiter != nil {
			s.limiter.RecordFail(ip)
		}
		WriteError(w, r, http.StatusUnauthorized, app.ErrUnauthorized,
			"临时令牌无效或已过期", nil)
		return
	}
	if s.limiter != nil {
		s.limiter.Reset(ip)
	}

	sessionID, csrfToken, serr := s.authSvc.CreateSession(ua, ip)
	if serr != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError,
			"创建会话失败", nil)
		return
	}

	s.setSessionCookies(w, r, sessionID, csrfToken)
	s.auditLogin(entry.Username, ip, ua, true, "", false, true)
	WriteOK(w, r, loginResponse{
		Username:    entry.Username,
		Requires2FA: false,
	})
}

func (s *Server) handleLoginRecover(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	ip := getIP(r)
	ua := r.UserAgent()

	var req loginRecoverRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.TempToken == "" || req.RecoveryCode == "" {
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"临时令牌和恢复码不能为空", nil)
		return
	}

	entry, valid := s.twofaSvc.GetTempStore().ValidateContext(req.TempToken, ip, ua)
	if !valid {
		s.twofaSvc.GetTempStore().RecordFailure(req.TempToken)
		if s.limiter != nil {
			s.limiter.RecordFail(ip)
		}
		WriteError(w, r, http.StatusUnauthorized, app.ErrUnauthorized,
			"临时令牌无效或已过期", nil)
		return
	}

	ok, err := s.twofaSvc.VerifyRecoveryCode(entry.AdminID, req.RecoveryCode)
	if err != nil || !ok {
		s.twofaSvc.GetTempStore().RecordFailure(req.TempToken)
		if s.limiter != nil {
			s.limiter.RecordFail(ip)
		}
		s.auditLogin(entry.Username, ip, ua, false, "恢复码错误", false, true)
		WriteError(w, r, http.StatusUnauthorized, app.ErrUnauthorized,
			"恢复码错误或已使用", nil)
		return
	}
	entry, valid = s.twofaSvc.GetTempStore().Consume(req.TempToken, ip, ua)
	if !valid {
		s.twofaSvc.GetTempStore().RecordFailure(req.TempToken)
		if s.limiter != nil {
			s.limiter.RecordFail(ip)
		}
		WriteError(w, r, http.StatusUnauthorized, app.ErrUnauthorized,
			"临时令牌无效或已过期", nil)
		return
	}
	if s.limiter != nil {
		s.limiter.Reset(ip)
	}

	sessionID, csrfToken, serr := s.authSvc.CreateSession(ua, ip)
	if serr != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError,
			"创建会话失败", nil)
		return
	}

	s.setSessionCookies(w, r, sessionID, csrfToken)
	s.auditLogin(entry.Username, ip, ua, true, "", false, true)
	WriteOK(w, r, loginResponse{
		Username:    entry.Username,
		Requires2FA: false,
	})
}

func (s *Server) CreateSession(ua, ip string) (string, string, error) {
	return s.authSvc.CreateSession(ua, ip)
}

func (s *Server) setSessionCookies(w http.ResponseWriter, r *http.Request, sessionID, csrfToken string) {
	secure := isHTTPS(r)
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		Expires:  time.Now().UTC().Add(s.authSvc.GetSessionDuration()),
	})

	http.SetCookie(w, &http.Cookie{
		Name:     auth.CSRFCookieName,
		Value:    csrfToken,
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
		Expires:  time.Now().UTC().Add(s.authSvc.GetSessionDuration()),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r.Context())
	if session != nil {
		_ = s.authSvc.Logout(session.ID)
	}

	secure := isHTTPS(r)

	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   -1,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     auth.CSRFCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
		MaxAge:   -1,
	})

	WriteOK(w, r, nil)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	adminExists, _ := s.authSvc.AdminExists()

	session := middleware.GetSession(r.Context())
	if session == nil {
		WriteOK(w, r, map[string]any{
			"authenticated": false,
			"needs_setup":   !adminExists,
		})
		return
	}

	admin, err := s.authSvc.GetAdminInfo()
	if err != nil || admin == nil {
		WriteOK(w, r, map[string]any{
			"authenticated": false,
			"needs_setup":   !adminExists,
		})
		return
	}

	WriteOK(w, r, map[string]any{
		"authenticated": true,
		"username":      admin.Username,
		"needs_setup":   false,
		"totp_enabled":  admin.TOTPEnabled,
	})
}

type captchaConfigResponse struct {
	Required bool   `json:"required"`
	Provider string `json:"provider,omitempty"`
	SiteKey  string `json:"site_key,omitempty"`
}

func (s *Server) handleCaptchaConfig(w http.ResponseWriter, r *http.Request) {
	if s.captchaSvc == nil || !s.captchaSvc.Enabled() {
		WriteOK(w, r, captchaConfigResponse{
			Required: false,
		})
		return
	}

	ip := getIP(r)
	failCount := 0
	if s.limiter != nil {
		failCount = s.limiter.FailCount(ip)
	}
	if !s.captchaSvc.ShouldTrigger(failCount) {
		WriteOK(w, r, captchaConfigResponse{Required: false})
		return
	}

	WriteOK(w, r, captchaConfigResponse{
		Required: true,
		Provider: s.captchaProviderName(),
		SiteKey:  s.cfg.API.Captcha.SiteKey,
	})
}

func (s *Server) captchaProviderName() string {
	if s == nil || s.captchaSvc == nil || !s.captchaSvc.Enabled() {
		return string(captcha.ProviderNone)
	}
	if s.cfg != nil && s.cfg.API.Captcha.Provider != "" {
		return s.cfg.API.Captcha.Provider
	}
	return string(captcha.ProviderNone)
}

func isHTTPS(r *http.Request) bool {
	if r.URL.Scheme == "https" {
		return true
	}
	if r.TLS != nil {
		return true
	}
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		if middleware.IsFromTrustedProxy(r) {
			return true
		}
	}
	return false
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*1024*1024)
	sessionID, ok := s.checkSensitiveActionLimit(w, r)
	if !ok {
		return
	}

	var req changePasswordRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	if req.CurrentPassword == "" || req.NewPassword == "" {
		WriteError(w, r, http.StatusUnprocessableEntity, app.ErrValidationFailed,
			"原密码和新密码不能为空", nil)
		return
	}

	if err := s.authSvc.ChangePassword(req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			// 修改密码失败同样记录到 session 级限流，防止已登录会话暴力枚举当前密码。
			if s.sensitiveActionLimiter != nil {
				s.sensitiveActionLimiter.RecordFail(sessionID)
			}
			WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest,
				"原密码错误", nil)
			return
		}
		handleAuthError(w, r, err)
		return
	}
	if s.sensitiveActionLimiter != nil {
		s.sensitiveActionLimiter.Reset(sessionID)
	}

	secure := isHTTPS(r)
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CSRFCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
		MaxAge:   -1,
	})

	WriteOK(w, r, map[string]any{"changed": true})
}
