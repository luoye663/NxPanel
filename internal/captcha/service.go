package captcha

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	provider          Provider
	secretKey         string
	siteKey           string
	triggerAfterFails int
	httpClient        *http.Client
}

func NewService(provider, secretKey, siteKey string, triggerAfterFails int) *Service {
	if provider == "" {
		provider = string(ProviderNone)
	}
	if triggerAfterFails < 0 {
		triggerAfterFails = 3
	}
	return &Service{
		provider:          Provider(provider),
		secretKey:         secretKey,
		siteKey:           siteKey,
		triggerAfterFails: triggerAfterFails,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (s *Service) Enabled() bool {
	return s.provider != ProviderNone && s.secretKey != ""
}

func (s *Service) ReloadConfig(provider, secretKey, siteKey string, triggerAfterFails int) {
	if provider == "" {
		provider = string(ProviderNone)
	}
	if triggerAfterFails < 0 {
		triggerAfterFails = 3
	}
	s.provider = Provider(provider)
	s.secretKey = secretKey
	s.siteKey = siteKey
	s.triggerAfterFails = triggerAfterFails
}

func (s *Service) ShouldTrigger(failCount int) bool {
	return s.Enabled() && failCount >= s.triggerAfterFails
}

func (s *Service) VerifyToken(token, remoteIP string) error {
	if !s.Enabled() {
		return nil
	}
	if token == "" {
		return fmt.Errorf("验证码不能为空")
	}

	switch s.provider {
	case ProviderTurnstile:
		return s.verifyTurnstile(token, remoteIP)
	case ProviderHCaptcha:
		return s.verifyHCaptcha(token, remoteIP)
	default:
		return nil
	}
}

func (s *Service) verifyTurnstile(token, remoteIP string) error {
	data := url.Values{}
	data.Set("secret", s.secretKey)
	data.Set("response", token)
	data.Set("remoteip", remoteIP)
	if s.siteKey != "" {
		data.Set("sitekey", s.siteKey)
	}

	resp, err := s.httpClient.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", data)
	if err != nil {
		return fmt.Errorf("Turnstile 验证请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("Turnstile 读取响应失败: %w", err)
	}

	var result struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("Turnstile 解析响应失败: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("Turnstile 验证失败: %v", result.ErrorCodes)
	}
	return nil
}

func (s *Service) verifyHCaptcha(token, remoteIP string) error {
	data := url.Values{}
	data.Set("secret", s.secretKey)
	data.Set("response", token)
	data.Set("remoteip", remoteIP)
	if s.siteKey != "" {
		data.Set("sitekey", s.siteKey)
	}

	resp, err := s.httpClient.PostForm("https://api.hcaptcha.com/siteverify", data)
	if err != nil {
		return fmt.Errorf("hCaptcha 验证请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("hCaptcha 读取响应失败: %w", err)
	}

	var result struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("hCaptcha 解析响应失败: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("hCaptcha 验证失败: %v", result.ErrorCodes)
	}
	return nil
}
