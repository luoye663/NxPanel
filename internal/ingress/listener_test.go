package ingress

import (
	"sync"
	"testing"
)

func TestConnectionLimiterAcquireRelease(t *testing.T) {
	limiter := NewConnectionLimiter(3, 2)
	if !limiter.Acquire("192.0.2.1") || !limiter.Acquire("192.0.2.1") {
		t.Fatal("first two per-IP connections should be accepted")
	}
	if limiter.Acquire("192.0.2.1") {
		t.Fatal("third per-IP connection should be rejected")
	}
	if !limiter.Acquire("192.0.2.2") || limiter.Acquire("192.0.2.3") {
		t.Fatal("global limit was not enforced")
	}
	limiter.Release("192.0.2.1")
	if !limiter.Acquire("192.0.2.3") {
		t.Fatal("released capacity should be reusable")
	}
	limiter.Release("192.0.2.1")
	limiter.Release("192.0.2.2")
	limiter.Release("192.0.2.3")
	if total, ips := limiter.Counts(); total != 0 || ips != 0 {
		t.Fatalf("counts after release = %d/%d", total, ips)
	}
}

func TestConnectionLimiterConcurrentLimits(t *testing.T) {
	limiter := NewConnectionLimiter(64, 8)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Acquire("198.51.100.5") {
				defer limiter.Release("198.51.100.5")
			}
		}()
	}
	wg.Wait()
	if total, ips := limiter.Counts(); total != 0 || ips != 0 {
		t.Fatalf("concurrent releases leaked counts: %d/%d", total, ips)
	}
}
