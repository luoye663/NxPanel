// api 包的基础 handler。
package api

import (
	"net/http"

	"github.com/luoye663/nxpanel/internal/app"
)

// handleHealth 健康检查接口
// 用于负载均衡健康探针和容器 readiness/liveness 检测
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	WriteOK(w, r, map[string]any{
		"status":  "ok",
		"service": "nxpanel-api",
		"version": app.Version,
	})
}
