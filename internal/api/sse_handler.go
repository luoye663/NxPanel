package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/luoye663/nxpanel/internal/sse"
)

func ServeSSE(w http.ResponseWriter, r *http.Request, stream *sse.Stream, heartbeatInterval time.Duration) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, canFlush := w.(http.Flusher)
	// 通用 SSE 也可能是长连接，清除当前响应写超时，避免被 30s WriteTimeout 截断。
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetWriteDeadline(time.Time{})
	}
	if canFlush {
		flusher.Flush()
	}

	ch, unsub := stream.Subscribe()
	defer unsub()

	history := stream.History()
	for _, evt := range history {
		if evt.Data == "[DONE]" {
			writeSSEData(w, evt.Data)
			if canFlush {
				flusher.Flush()
			}
			return
		}
		writeSSEData(w, evt.Data)
	}
	if canFlush {
		flusher.Flush()
	}

	if stream.IsClosed() {
		return
	}

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	go func() {
		for {
			select {
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
					return
				}
				if canFlush {
					flusher.Flush()
				}
			case <-r.Context().Done():
				return
			}
		}
	}()

	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return
			}
			writeSSEData(w, evt.Data)
			if canFlush {
				flusher.Flush()
			}
			if evt.Data == "[DONE]" {
				return
			}
		case <-r.Context().Done():
			slog.Debug("SSE client disconnected", "stream", stream.String())
			return
		}
	}
}

func writeSSEData(w http.ResponseWriter, data string) {
	fmt.Fprintf(w, "data: %s\n\n", data)
}
