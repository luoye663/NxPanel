package api

import (
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

	resp, ok := s.openSSEResponse(w, r)
	if !ok {
		return
	}
	defer resp.Close()

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
			if resp.WriteFrame(sseDataFrame(string(data))) != nil {
				return
			}
		case <-heartbeat.C:
			if resp.WriteFrame(sseHeartbeatFrame) != nil {
				return
			}
		case <-r.Context().Done():
			slog.Debug("系统指标 SSE 客户端断开")
			return
		}
	}
}
