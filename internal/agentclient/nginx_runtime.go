package agentclient

import (
	"context"
	"fmt"
	"github.com/luoye663/nxpanel/internal/nginx"
)

func (c *Client) NginxStatus(ctx context.Context) (*nginx.RuntimeStatus, error) {
	var resp struct {
		OK    bool                `json:"ok"`
		Error string              `json:"error"`
		Data  nginx.RuntimeStatus `json:"data"`
	}
	if err := c.get(ctx, "/internal/v1/nginx/status", &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("Nginx 状态读取失败: %s", resp.Error)
	}
	return &resp.Data, nil
}

func (c *Client) ControlNginx(ctx context.Context, action string) error {
	if action != "start" && action != "stop" && action != "restart" {
		return fmt.Errorf("invalid nginx action")
	}
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/nginx/"+action, struct{}{}, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("Nginx %s 失败: %s", action, resp.Error)
	}
	return nil
}
