package agentclient

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/luoye663/nxpanel/internal/configpreflight"
)

type ConfigPreflightRequest = configpreflight.Request

func (c *Client) DiagnoseAccessConfig(ctx context.Context, req *ConfigPreflightRequest) (configpreflight.Report, error) {
	var resp AgentResponse
	var report configpreflight.Report
	if err := c.postJSON(ctx, "/internal/v1/nginx/access-preflight", req, &resp); err != nil {
		return report, err
	}
	if !resp.OK {
		return report, fmt.Errorf("配置诊断请求失败: %s", resp.Error)
	}
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return report, err
	}
	var wire struct {
		Warnings  *[]configpreflight.Warning `json:"warnings"`
		Complete  *bool                      `json:"complete"`
		Truncated *bool                      `json:"truncated"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return report, err
	}
	if wire.Warnings == nil || wire.Complete == nil || wire.Truncated == nil {
		report.Incomplete(fmt.Errorf("Agent 未提供完整诊断结果，请确认 API 与 Agent 版本一致"))
		return report, nil
	}
	report = configpreflight.Report{Warnings: *wire.Warnings, Complete: *wire.Complete, Truncated: *wire.Truncated}
	if len(report.Warnings) > configpreflight.MaxWarnings {
		report.Warnings = report.Warnings[:configpreflight.MaxWarnings]
		report.Truncated = true
	}
	if !report.Complete && len(report.Warnings) == 0 {
		report.Incomplete(fmt.Errorf("Agent 未能完成配置诊断"))
	}
	return report, nil
}
