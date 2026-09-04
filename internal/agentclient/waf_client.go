package agentclient

import (
	"context"
	"encoding/json"
	"fmt"
)

func (c *Client) DetectWAFRuntime(ctx context.Context) (*WAFRuntimeDetectResponse, error) {
	var result WAFRuntimeDetectResponse
	if err := c.callWAF(ctx, "/internal/v1/waf/runtime/detect", struct{}{}, &result); err != nil {
		return nil, fmt.Errorf("detect WAF runtime: %w", err)
	}
	return &result, nil
}

func (c *Client) InstallWAFProvider(ctx context.Context, req *WAFProviderInstallRequest) (*WAFProviderInstallResponse, error) {
	var result WAFProviderInstallResponse
	if err := c.callWAF(ctx, "/internal/v1/waf/provider/install", req, &result); err != nil {
		return nil, fmt.Errorf("install WAF provider: %w", err)
	}
	return &result, nil
}

func (c *Client) ActivateWAFProvider(ctx context.Context, req *WAFProviderActivateRequest) (*WAFProviderActivateResponse, error) {
	var result WAFProviderActivateResponse
	if err := c.callWAF(ctx, "/internal/v1/waf/provider/activate", req, &result); err != nil {
		return nil, fmt.Errorf("activate WAF provider: %w", err)
	}
	return &result, nil
}

func (c *Client) ApplyWAFSite(ctx context.Context, req *WAFSiteApplyRequest) (*WAFSiteApplyResponse, error) {
	var result WAFSiteApplyResponse
	if err := c.callWAF(ctx, "/internal/v1/waf/sites/apply", req, &result); err != nil {
		return nil, fmt.Errorf("apply WAF site policy: %w", err)
	}
	return &result, nil
}

func (c *Client) ListWAFAuditEvents(ctx context.Context, req *WAFAuditListRequest) (*WAFAuditListResponse, error) {
	var result WAFAuditListResponse
	if err := c.callWAF(ctx, "/internal/v1/waf/audit/list", req, &result); err != nil {
		return nil, fmt.Errorf("list WAF audit events: %w", err)
	}
	return &result, nil
}

func (c *Client) ReadWAFAuditEvent(ctx context.Context, req *WAFAuditReadRequest) (*WAFAuditReadResponse, error) {
	var result WAFAuditReadResponse
	if err := c.callWAF(ctx, "/internal/v1/waf/audit/read", req, &result); err != nil {
		return nil, fmt.Errorf("read WAF audit event: %w", err)
	}
	return &result, nil
}

func (c *Client) CleanupWAFAuditEvents(ctx context.Context, req *WAFAuditCleanupRequest) (*WAFAuditCleanupResponse, error) {
	var result WAFAuditCleanupResponse
	if err := c.callWAF(ctx, "/internal/v1/waf/audit/cleanup", req, &result); err != nil {
		return nil, fmt.Errorf("cleanup WAF audit events: %w", err)
	}
	return &result, nil
}

func (c *Client) callWAF(ctx context.Context, path string, req, result any) error {
	var response AgentResponse
	if err := c.postJSON(ctx, path, req, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("agent rejected request: %s", response.Error)
	}
	data, err := json.Marshal(response.Data)
	if err != nil {
		return fmt.Errorf("encode agent response: %w", err)
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("decode agent response: %w", err)
	}
	return nil
}
