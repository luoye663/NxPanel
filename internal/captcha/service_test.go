package captcha

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestVerifyTokenFailsClosed(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		secret   string
		siteKey  string
		result   *http.Response
		err      error
	}{
		{name: "unknown provider", provider: "other", secret: "secret", siteKey: "site"},
		{name: "missing secret", provider: "turnstile", siteKey: "site"},
		{name: "missing site key", provider: "turnstile", secret: "secret"},
		{name: "non 2xx", provider: "turnstile", secret: "secret", siteKey: "site", result: response(503, `{}`)},
		{name: "malformed", provider: "turnstile", secret: "secret", siteKey: "site", result: response(200, `{`)},
		{name: "upstream error", provider: "turnstile", secret: "secret", siteKey: "site", err: errors.New("offline")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService(tt.provider, tt.secret, tt.siteKey, 0, 1)
			s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return tt.result, tt.err })
			if err := s.VerifyToken(context.Background(), "token", "192.0.2.1"); err == nil {
				t.Fatal("失败场景必须 fail closed")
			}
		})
	}
}

func TestVerifyTokenConcurrencyLimitIsNonblocking(t *testing.T) {
	s := NewService("turnstile", "secret", "site", 0, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return response(200, `{"success":true}`), nil
	})
	done := make(chan error, 1)
	go func() { done <- s.VerifyToken(context.Background(), "first", "192.0.2.1") }()
	<-started
	start := time.Now()
	if err := s.VerifyToken(context.Background(), "second", "192.0.2.2"); err == nil {
		t.Fatal("并发槽满时应立即失败")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("并发槽满时不应阻塞")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("首个验证失败: %v", err)
	}
}

func TestReloadAndReadsAreRaceSafe(t *testing.T) {
	s := NewService("turnstile", "secret", "site", 0, 4)
	s.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `{"success":true}`), nil
	})
	var wg sync.WaitGroup
	var failures atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = s.Enabled()
				_ = s.ShouldTrigger(j)
				_, _, _ = s.PublicConfig()
				if err := s.VerifyToken(context.Background(), "token", "192.0.2.1"); err != nil && !strings.Contains(err.Error(), "繁忙") {
					failures.Add(1)
				}
			}
		}()
	}
	for i := 0; i < 200; i++ {
		s.ReloadConfig("turnstile", "secret", "site", i%3, 4)
	}
	wg.Wait()
	if failures.Load() != 0 {
		t.Fatalf("热重载期间出现非并发限额错误: %d", failures.Load())
	}
}
