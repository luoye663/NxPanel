package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type failRecord struct {
	count       int
	windowStart time.Time
}

type LoginRateLimiter struct {
	maxFailures int
	window      time.Duration
	mu          sync.Mutex
	store       map[string]*failRecord
	cancel      context.CancelFunc
}

func NewLoginRateLimiter(maxFailures int, window time.Duration) *LoginRateLimiter {
	ctx, cancel := context.WithCancel(context.Background())
	limiter := &LoginRateLimiter{
		maxFailures: maxFailures,
		window:      window,
		store:       make(map[string]*failRecord),
		cancel:      cancel,
	}
	go limiter.cleanup(ctx)
	return limiter
}

func (l *LoginRateLimiter) Stop() {
	if l.cancel != nil {
		l.cancel()
	}
}

func (l *LoginRateLimiter) ReloadConfig(maxFailures int, window time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maxFailures = maxFailures
	l.window = window
}

func (l *LoginRateLimiter) Check(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.checkLocked(ip)
}

func (l *LoginRateLimiter) checkLocked(ip string) bool {
	rec, ok := l.store[ip]
	if !ok {
		return true
	}
	if time.Since(rec.windowStart) >= l.window {
		return true
	}
	return rec.count < l.maxFailures
}

func (l *LoginRateLimiter) RecordFail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	rec, ok := l.store[ip]
	if !ok {
		l.store[ip] = &failRecord{count: 1, windowStart: now}
		return
	}
	if now.Sub(rec.windowStart) >= l.window {
		l.store[ip] = &failRecord{count: 1, windowStart: now}
		return
	}
	rec.count++
}

func (l *LoginRateLimiter) CheckAndRecord(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.checkLocked(ip) {
		return false
	}
	return true
}

func (l *LoginRateLimiter) FailCount(ip string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.store[ip]
	if !ok {
		return 0
	}
	if time.Since(rec.windowStart) >= l.window {
		return 0
	}
	return rec.count
}

func (l *LoginRateLimiter) Reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.store, ip)
}

func (l *LoginRateLimiter) cleanup(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.mu.Lock()
			now := time.Now()
			for ip, rec := range l.store {
				if now.Sub(rec.windowStart) >= l.window {
					delete(l.store, ip)
				}
			}
			l.mu.Unlock()
		}
	}
}

func LoginRateLimitMiddleware(limiter *LoginRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := GetRealIP(r.Context())
			if ip == "" {
				ip = r.RemoteAddr
			}
			if !limiter.Check(ip) {
				writeRateLimitError(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeRateLimitError(w http.ResponseWriter, r *http.Request) {
	rid := GetRequestID(r.Context())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Request-ID", rid)
	w.WriteHeader(http.StatusTooManyRequests)

	resp := map[string]any{
		"request_id": rid,
		"success":    false,
		"data":       nil,
		"error": map[string]any{
			"code":    "TOO_MANY_REQUESTS",
			"message": "登录失败次数过多，请稍后再试",
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}
