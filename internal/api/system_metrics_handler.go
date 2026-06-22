package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/systemmetrics"
)

func (s *Server) handleSystemMetricsStream(w http.ResponseWriter, r *http.Request) {
	if s.metricsSvc == nil {
		WriteError(w, r, http.StatusServiceUnavailable, "METRICS_UNAVAILABLE", "系统指标服务不可用", nil)
		return
	}

	// SSE 必须使用 text/event-stream，并关闭代理缓冲，否则浏览器可能收不到实时数据。
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)
	// http.Server.WriteTimeout 默认 30s；SSE 是长连接，需要清除本响应的写超时。
	// 只影响当前 SSE 请求，不会关闭普通 HTTP 请求的超时保护。
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetWriteDeadline(time.Time{})
	}
	if canFlush {
		flusher.Flush()
	}

	// scope 用于按需下发：常驻仪表盘只拿轻量数据，打开弹窗时才订阅详情数据。
	ch, unsub := s.metricsSvc.Subscribe(r.Context(), r.URL.Query().Get("scope"))
	defer unsub()

	heartbeatInterval := app.ParseDurationOrDefault(s.cfg.API.SSEHeartbeat, 15*time.Second)
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case snapshot, ok := <-ch:
			if !ok {
				return
			}
			data, err := systemmetrics.MarshalSnapshot(snapshot)
			if err != nil {
				slog.Debug("序列化系统指标失败", "error", err)
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		case <-heartbeat.C:
			// 心跳是 SSE 注释行，不会触发前端 message，但可以保持连接活跃。
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		case <-r.Context().Done():
			slog.Debug("系统指标 SSE 客户端断开")
			return
		}
	}
}
