package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/sse"
)

type sseResponse struct {
	w          http.ResponseWriter
	controller *http.ResponseController
	timeout    time.Duration
	release    func()
}

func (s *Server) openSSEResponse(w http.ResponseWriter, r *http.Request) (*sseResponse, bool) {
	select {
	case s.sseSlots <- struct{}{}:
	default:
		w.Header().Set("Retry-After", "5")
		WriteError(w, r, http.StatusServiceUnavailable, "SSE_CAPACITY_REACHED", "实时连接数已达上限，请稍后重试", nil)
		return nil, false
	}

	release := func() { <-s.sseSlots }
	if _, ok := w.(http.Flusher); !ok {
		release()
		WriteError(w, r, http.StatusInternalServerError, "STREAM_UNSUPPORTED", "当前连接不支持实时推送", nil)
		return nil, false
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	resp := &sseResponse{
		w:          w,
		controller: http.NewResponseController(w),
		timeout:    app.ParseDurationOrDefault(s.cfg.API.SSEWriteTimeout, 10*time.Second),
		release:    release,
	}
	if err := resp.flushHeaders(); err != nil {
		resp.Close()
		return nil, false
	}
	return resp, true
}

func (w *sseResponse) flushHeaders() error {
	if err := w.setDeadline(); err != nil {
		return err
	}
	w.w.WriteHeader(http.StatusOK)
	if err := w.controller.Flush(); err != nil {
		return err
	}
	return w.clearDeadline()
}

func (w *sseResponse) WriteFrame(frame []byte) error {
	if err := w.setDeadline(); err != nil {
		return err
	}
	n, err := w.w.Write(frame)
	if err != nil {
		return err
	}
	if n != len(frame) {
		return io.ErrShortWrite
	}
	if err := w.controller.Flush(); err != nil {
		return err
	}
	return w.clearDeadline()
}

func (w *sseResponse) Close() {
	_ = w.clearDeadline()
	if w.release != nil {
		w.release()
		w.release = nil
	}
}

func (w *sseResponse) setDeadline() error {
	err := w.controller.SetWriteDeadline(time.Now().Add(w.timeout))
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

func (w *sseResponse) clearDeadline() error {
	err := w.controller.SetWriteDeadline(time.Time{})
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

func sseDataFrame(data string) []byte {
	return []byte(fmt.Sprintf("data: %s\n\n", data))
}

func sseEventFrame(event, data string) []byte {
	encoded, _ := json.Marshal(map[string]string{"line": data})
	return []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, encoded))
}

var sseHeartbeatFrame = []byte(": keep-alive\n\n")

func (s *Server) serveSSE(w http.ResponseWriter, r *http.Request, stream *sse.Stream, heartbeatInterval time.Duration) {
	resp, ok := s.openSSEResponse(w, r)
	if !ok {
		return
	}
	defer resp.Close()

	sub := stream.SubscribeSnapshot()
	defer sub.Unsubscribe()
	for _, evt := range sub.History {
		if err := resp.WriteFrame(sseDataFrame(evt.Data)); err != nil {
			return
		}
		if evt.Data == "[DONE]" {
			return
		}
	}
	if sub.Closed {
		return
	}

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case evt, open := <-sub.Events:
			if !open || resp.WriteFrame(sseDataFrame(evt.Data)) != nil {
				return
			}
			if evt.Data == "[DONE]" {
				return
			}
		case <-heartbeat.C:
			if resp.WriteFrame(sseHeartbeatFrame) != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}
