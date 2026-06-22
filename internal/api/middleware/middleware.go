// middleware 包提供 API 级公共中间件。
package middleware

import (
	"net/http"
	"strings"
)

func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return MaxBodySizeExcept(maxBytes)
}

func MaxBodySizeExcept(maxBytes int64, skipPaths ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, skipPath := range skipPaths {
				if skipPath == "" {
					continue
				}
				if r.URL.Path == skipPath || strings.HasSuffix(r.URL.Path, skipPath) {
					next.ServeHTTP(w, r)
					return
				}
			}
			if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}
