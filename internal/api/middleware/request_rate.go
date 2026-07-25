package middleware

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type requestBucket struct {
	tokens   float64
	updated  time.Time
	lastSeen time.Time
}

// RequestTokenBucket is a bounded per-key token bucket.
type RequestTokenBucket struct {
	mu         sync.Mutex
	rate       float64
	burst      float64
	maxEntries int
	buckets    map[string]*requestBucket
	now        func() time.Time
}

func NewRequestTokenBucket(rate float64, burst, maxEntries int) *RequestTokenBucket {
	if rate <= 0 {
		rate = 10
	}
	if burst <= 0 {
		burst = 40
	}
	if maxEntries <= 0 {
		maxEntries = 4096
	}
	return &RequestTokenBucket{
		rate:       rate,
		burst:      float64(burst),
		maxEntries: maxEntries,
		buckets:    make(map[string]*requestBucket),
		now:        time.Now,
	}
}

func (l *RequestTokenBucket) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= l.maxEntries {
			l.evictOldestLocked()
		}
		b = &requestBucket{tokens: l.burst, updated: now}
		l.buckets[key] = b
	}
	elapsed := now.Sub(b.updated).Seconds()
	if elapsed > 0 {
		b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
		b.updated = now
	}
	b.lastSeen = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *RequestTokenBucket) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for key, bucket := range l.buckets {
		if oldestKey == "" || bucket.lastSeen.Before(oldest) {
			oldestKey, oldest = key, bucket.lastSeen
		}
	}
	delete(l.buckets, oldestKey)
}

func (l *RequestTokenBucket) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

func RequestRateLimit(limiter *RequestTokenBucket) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := GetRealIP(r.Context())
			if ip == "" {
				ip = r.RemoteAddr
			}
			if limiter.Allow(ip) {
				next.ServeHTTP(w, r)
				return
			}
			rid := GetRequestID(r.Context())
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Request-ID", rid)
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"request_id": rid, "success": false, "data": nil,
				"error": map[string]any{"code": "TOO_MANY_REQUESTS", "message": "请求过于频繁，请稍后再试"},
			})
		})
	}
}
