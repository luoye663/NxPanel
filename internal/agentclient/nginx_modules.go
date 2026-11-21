package agentclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
)

func (c *Client) GeoIP2ModuleStatus(ctx context.Context) (nginxmodule.Status, error) {
	var resp struct {
		OK   bool                 `json:"ok"`
		Data []nginxmodule.Status `json:"data"`
	}
	if err := c.get(ctx, "/internal/v1/nginx/modules/local", &resp); err != nil {
		return nginxmodule.Status{}, err
	}
	if !resp.OK || len(resp.Data) != 1 || resp.Data[0].ID != nginxmodule.GeoIP2 {
		return nginxmodule.Status{}, fmt.Errorf("Agent 模块状态响应无效")
	}
	return resp.Data[0], nil
}

func (c *Client) NginxModuleList(ctx context.Context, refresh bool) (nginxmodule.Listing, error) {
	path := "/internal/v1/nginx/modules"
	if refresh {
		path += "?refresh=1"
	}
	var resp struct {
		OK   bool                `json:"ok"`
		Data nginxmodule.Listing `json:"data"`
	}
	if err := c.get(ctx, path, &resp); err != nil {
		return resp.Data, err
	}
	if !resp.OK || resp.Data.Modules == nil || resp.Data.FeedState == "" {
		return resp.Data, fmt.Errorf("Agent 模块目录响应无效")
	}
	return resp.Data, nil
}

func (c *Client) InstallNginxModule(ctx context.Context, request nginxmodule.InstallRequest) (nginxmodule.Result, error) {
	return c.nginxModuleAction(ctx, "install", request)
}

func (c *Client) UninstallNginxModule(ctx context.Context, request nginxmodule.InstallRequest) (nginxmodule.Result, error) {
	return c.nginxModuleAction(ctx, "uninstall", request)
}

func (c *Client) nginxModuleAction(ctx context.Context, action string, request nginxmodule.InstallRequest) (nginxmodule.Result, error) {
	var result nginxmodule.Result
	var resp struct {
		OK   bool               `json:"ok"`
		Data nginxmodule.Result `json:"data"`
	}
	client := Client{token: c.token, httpClient: &http.Client{Transport: c.httpClient.Transport, Timeout: nginxmodule.ActionTimeout + 10*time.Second}}
	if err := client.postJSON(ctx, "/internal/v1/nginx/modules/"+action, request, &resp); err != nil {
		return result, err
	}
	if !resp.OK || resp.Data.State != "success" && resp.Data.State != "failed" && resp.Data.State != "needs_attention" {
		return result, fmt.Errorf("Agent 模块操作结果无效")
	}
	return resp.Data, nil
}
