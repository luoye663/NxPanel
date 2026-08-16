package middleware

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestTokenBucketBoundedCardinality(t *testing.T) {
	limiter := NewRequestTokenBucket(10, 40, 32)
	for i := 0; i < 1000; i++ {
		if !limiter.Allow(fmt.Sprintf("192.0.2.%d", i)) {
			t.Fatal("new bucket should receive its initial burst")
		}
	}
	if got := limiter.Len(); got != 32 {
		t.Fatalf("tracked IPs = %d, want 32", got)
	}
}

func TestRequestTokenBucketConcurrentBurst(t *testing.T) {
	limiter := NewRequestTokenBucket(0.0001, 40, 64)
	limiter.now = func() time.Time { return time.Unix(100, 0) }
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Allow("203.0.113.9") {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 40 {
		t.Fatalf("allowed = %d, want 40", got)
	}
}
