package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// GateSecretValidator 校验隐藏入口段；失败统一返回 404，避免泄露“这里有 API”。
func GateSecretValidator(tokenProvider func() string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			expected := tokenProvider()
			actual := chi.URLParam(r, "gateSecret")
			if expected == "" || actual == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) != 1 {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
