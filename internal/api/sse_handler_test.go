package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/sse"
)

func newSSETestServer(maxConnections int, writeTimeout time.Duration) *Server {
	return &Server{
		cfg: &app.Config{API: app.APIConfig{
			SSEWriteTimeout:   writeTimeout.String(),
			SSEMaxConnections: maxConnections,
		}},
		sseSlots: make(chan struct{}, maxConnections),
	}
}

func waitForSubscribers(t *testing.T, stream *sse.Stream, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if stream.SubscriberCount() == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("subscriber count = %d, want %d", stream.SubscriberCount(), count)
}

func TestServeSSEConnectionCapAndRelease(t *testing.T) {
	server := newSSETestServer(1, time.Second)
	stream := sse.NewHub().CreateStream("cap")
	ctx, cancel := contextWithCancel(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.serveSSE(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx), stream, time.Hour)
	}()
	waitForSubscribers(t, stream, 1)

	rejected := httptest.NewRecorder()
	server.serveSSE(rejected, httptest.NewRequest(http.MethodGet, "/stream", nil), stream, time.Hour)
	if rejected.Code != http.StatusServiceUnavailable || rejected.Header().Get("Retry-After") == "" {
		t.Fatalf("saturated response = %d, Retry-After %q", rejected.Code, rejected.Header().Get("Retry-After"))
	}

	cancel()
	<-done
	waitForSubscribers(t, stream, 0)
	if len(server.sseSlots) != 0 {
		t.Fatalf("connection slot leaked: %d", len(server.sseSlots))
	}
}

func TestServeSSEReplaysDoneOnce(t *testing.T) {
	server := newSSETestServer(1, time.Second)
	stream := sse.NewHub().CreateStream("done")
	stream.PublishDone("terminal")
	recorder := httptest.NewRecorder()
	server.serveSSE(recorder, httptest.NewRequest(http.MethodGet, "/stream", nil), stream, time.Hour)
	if got := strings.Count(recorder.Body.String(), "data: [DONE]\n\n"); got != 1 {
		t.Fatalf("DONE frame count = %d, body = %q", got, recorder.Body.String())
	}
}

type overlapDetectWriter struct {
	header  http.Header
	active  atomic.Int32
	overlap atomic.Bool
	mu      sync.Mutex
	body    strings.Builder
}

func (w *overlapDetectWriter) Header() http.Header              { return w.header }
func (w *overlapDetectWriter) WriteHeader(int)                  {}
func (w *overlapDetectWriter) SetWriteDeadline(time.Time) error { return nil }
func (w *overlapDetectWriter) enter() func() {
	if w.active.Add(1) != 1 {
		w.overlap.Store(true)
	}
	return func() { w.active.Add(-1) }
}
func (w *overlapDetectWriter) Flush() {
	defer w.enter()()
	time.Sleep(2 * time.Millisecond)
}
func (w *overlapDetectWriter) Write(p []byte) (int, error) {
	defer w.enter()()
	time.Sleep(2 * time.Millisecond)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(p)
}

func TestServeSSENeverOverlapsHeartbeatAndEventWrites(t *testing.T) {
	server := newSSETestServer(1, time.Second)
	stream := sse.NewHub().CreateStream("serialized")
	writer := &overlapDetectWriter{header: make(http.Header)}
	ctx, cancel := contextWithCancel(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.serveSSE(writer, httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx), stream, time.Millisecond)
	}()
	waitForSubscribers(t, stream, 1)
	for i := 0; i < 20; i++ {
		stream.PublishData("event")
	}
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done
	if writer.overlap.Load() {
		t.Fatal("ResponseWriter was written concurrently")
	}
}

type timeoutWriter struct {
	header   http.Header
	mu       sync.Mutex
	deadline time.Time
}

func (w *timeoutWriter) Header() http.Header { return w.header }
func (w *timeoutWriter) WriteHeader(int)     {}
func (w *timeoutWriter) Flush()              {}
func (w *timeoutWriter) SetWriteDeadline(deadline time.Time) error {
	w.mu.Lock()
	w.deadline = deadline
	w.mu.Unlock()
	return nil
}
func (w *timeoutWriter) Write([]byte) (int, error) {
	w.mu.Lock()
	deadline := w.deadline
	w.mu.Unlock()
	if deadline.IsZero() {
		return 0, errors.New("write started without deadline")
	}
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	<-timer.C
	return 0, os.ErrDeadlineExceeded
}

func TestServeSSEWriteTimeoutCleansUp(t *testing.T) {
	server := newSSETestServer(1, 20*time.Millisecond)
	stream := sse.NewHub().CreateStream("timeout")
	writer := &timeoutWriter{header: make(http.Header)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.serveSSE(writer, httptest.NewRequest(http.MethodGet, "/stream", nil), stream, time.Hour)
	}()
	waitForSubscribers(t, stream, 1)
	stream.PublishData("blocked")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out writer did not release handler")
	}
	waitForSubscribers(t, stream, 0)
	if len(server.sseSlots) != 0 {
		t.Fatalf("connection slot leaked: %d", len(server.sseSlots))
	}
}

func contextWithCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx, cancel
}
