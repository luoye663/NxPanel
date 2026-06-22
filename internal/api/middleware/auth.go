package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/luoye663/nxpanel/internal/auth"
)

const (
	SessionKey   contextKey = "session"
	SessionIDKey contextKey = "session_id"
)

func GetSession(ctx context.Context) *SessionInfo {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(SessionKey).(*SessionInfo); ok {
		return v
	}
	return nil
}

type SessionInfo struct {
	ID            string
	CSRFTokenHash string
}

func Authenticate(authSvc *auth.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(auth.SessionCookieName)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			sessionID := cookie.Value
			if sessionID == "" {
				next.ServeHTTP(w, r)
				return
			}

			ip := GetRealIP(r.Context())
			if ip == "" {
				ip = r.RemoteAddr
			}
			ua := r.UserAgent()

			session, err := authSvc.ValidateSession(sessionID, ip, ua)
			if err != nil || session == nil {
				if err != nil {
					clearSessionCookie(w)
				}
				next.ServeHTTP(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), SessionIDKey, sessionID)
			ctx = context.WithValue(ctx, SessionKey, &SessionInfo{
				ID:            sessionID,
				CSRFTokenHash: session.CSRFTokenHash,
			})

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session := GetSession(r.Context())
		if session == nil {
			rid := GetRequestID(r.Context())
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Request-ID", rid)
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"request_id": rid,
				"success":    false,
				"data":       nil,
				"error": map[string]any{
					"code":    "UNAUTHORIZED",
					"message": "未登录或会话已过期",
				},
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
		MaxAge:   -1,
	})
}
