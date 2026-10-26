package agentclient

import (
	"context"
	"fmt"

	"github.com/luoye663/nxpanel/internal/configpreflight"
)

type ConfigPreflightRequest = configpreflight.Request

func (c *Client) CheckAccessConfig(ctx context.Context, req *ConfigPreflightRequest) error {
	var resp AgentResponse
	if err := c.postJSON(ctx, "/internal/v1/nginx/access-preflight", req, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("访问策略配置预检失败: %s", resp.Error)
	}
	return nil
}
