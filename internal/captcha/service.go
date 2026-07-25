package captcha

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const maxResponseSize = 4096

type Provider string

const (
	ProviderNone      Provider = "none"
	ProviderTurnstile Provider = "turnstile"
	ProviderHCaptcha  Provider = "hcaptcha"
)

type Service struct {
	config     atomic.Pointer[serviceConfig]
	active     atomic.Int64
	httpClient *http.Client
}

type serviceConfig struct {
	provider          Provider
	secretKey         string
	siteKey           string
	triggerAfterFails int
	maxConcurrent     int64
}

func NewService(provider, secretKey, siteKey string, triggerAfterFails int, maxConcurrent ...int) *Service {
	s := &Service{httpClient: &http.Client{Timeout: 10 * time.Second}}
	s.ReloadConfig(provider, secretKey, siteKey, triggerAfterFails, maxConcurrent...)
	return s
}

func newServiceConfig(provider, secretKey, siteKey string, triggerAfterFails int, maxConcurrent int) *serviceConfig {
	if provider == "" {
		provider = string(ProviderNone)
	}
	if triggerAfterFails < 0 {
		triggerAfterFails = 3
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 8
	}
	return &serviceConfig{
		provider:          Provider(provider),
		secretKey:         secretKey,
		siteKey:           siteKey,
		triggerAfterFails: triggerAfterFails,
		maxConcurrent:     int64(maxConcurrent),
	}
}

func (s *Service) Enabled() bool {
	cfg := s.config.Load()
	return cfg != nil && cfg.provider != ProviderNone
}

func (s *Service) ReloadConfig(provider, secretKey, siteKey string, triggerAfterFails int, maxConcurrent ...int) {
	limit := 8
	if len(maxConcurrent) > 0 {
		limit = maxConcurrent[0]
	}
	s.config.Store(newServiceConfig(provider, secretKey, siteKey, triggerAfterFails, limit))
}

func (s *Service) ShouldTrigger(failCount int) bool {
	cfg := s.config.Load()
	return cfg != nil && cfg.provider != ProviderNone && failCount >= cfg.triggerAfterFails
}

func (s *Service) PublicConfig() (string, string, bool) {
	cfg := s.config.Load()
	if cfg == nil || cfg.provider == ProviderNone {
		return string(ProviderNone), "", false
	}
	return string(cfg.provider), cfg.siteKey, true
}

func (s *Service) VerifyToken(ctx context.Context, token, remoteIP string) error {
	cfg := s.config.Load()
	if cfg == nil || cfg.provider == ProviderNone {
		return nil
	}
	if strings.TrimSpace(cfg.secretKey) == "" || strings.TrimSpace(cfg.siteKey) == "" {
		return fmt.Errorf("CAPTCHA 配置不完整")
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("验证码不能为空")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.tryAcquire(cfg.maxConcurrent) {
		return fmt.Errorf("CAPTCHA 验证繁忙")
	}
	defer s.active.Add(-1)
	if err := ctx.Err(); err != nil {
		return ctx.Err()
	}

	switch cfg.provider {
	case ProviderTurnstile:
		return s.verify(ctx, cfg, "https://challenges.cloudflare.com/turnstile/v0/siteverify", token, remoteIP)
	case ProviderHCaptcha:
		return s.verify(ctx, cfg, "https://api.hcaptcha.com/siteverify", token, remoteIP)
	default:
		return fmt.Errorf("未知 CAPTCHA provider")
	}
}

func (s *Service) tryAcquire(limit int64) bool {
	for {
		active := s.active.Load()
		if active >= limit {
			return false
		}
		if s.active.CompareAndSwap(active, active+1) {
			return true
		}
	}
}

func (s *Service) verify(ctx context.Context, cfg *serviceConfig, endpoint, token, remoteIP string) error {
	data := url.Values{}
	data.Set("secret", cfg.secretKey)
	data.Set("response", token)
	data.Set("remoteip", remoteIP)
	data.Set("sitekey", cfg.siteKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("创建 CAPTCHA 验证请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("CAPTCHA 验证请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("CAPTCHA 验证服务返回状态 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return fmt.Errorf("读取 CAPTCHA 响应失败: %w", err)
	}
	if len(body) > maxResponseSize {
		return fmt.Errorf("CAPTCHA 响应过大")
	}
	var result struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析 CAPTCHA 响应失败: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("CAPTCHA 验证失败: %v", result.ErrorCodes)
	}
	return nil
}
