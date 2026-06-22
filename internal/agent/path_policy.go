// agent 包 — 路径安全策略
//
// 定义 agent 允许操作的目录白名单。
// 所有文件写操作的目标路径必须在这些目录内。
//
// 白名单由三部分合并组成：
//  1. 配置驱动路径（自动跟随用户安装位置）：PanelDir、DataDir、ConfPath 父目录、LogDir
//  2. 配置中的站点根目录和日志目录前缀
//  3. 用户追加路径（config.yaml 中的 agent.allowed_roots）
package agent

import (
	"path/filepath"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/security"
)

// hardcodedSystemRoots 返回不依赖面板安装位置的最小通用网站根目录。
func hardcodedSystemRoots() []string {
	return []string{
		"/www/wwwroot",
		"/var/www",
	}
}

// DefaultAllowedRoots 返回默认的允许目录列表
// 保留此函数以向后兼容现有测试
func DefaultAllowedRoots() []string {
	return append(
		[]string{
			"/opt/nxpanel/nginx",
		},
		hardcodedSystemRoots()...,
	)
}

// NewPathPolicyFromConfig 从配置构建路径策略
//
// 白名单由四部分合并组成：
//  1. 配置驱动路径：PanelDir、DataDir、ConfPath 父目录、LogDir（自动跟随用户安装位置）
//  2. 配置中的站点根目录和日志目录前缀
//  3. 最小通用网站根目录：/www/wwwroot、/var/www
//  4. 用户追加路径：cfg.Agent.AllowedRoots（可选）
func NewPathPolicyFromConfig(cfg *app.Config) *PathPolicy {
	return NewPathPolicy(app.BuildAllowedPathRoots(cfg))
}

// PathPolicy 路径安全策略
type PathPolicy struct {
	roots []string
}

// Roots 返回当前生效的允许目录列表（只读副本）
func (p *PathPolicy) Roots() []string {
	out := make([]string, len(p.roots))
	copy(out, p.roots)
	return out
}

// NewPathPolicy 创建路径安全策略
func NewPathPolicy(roots []string) *PathPolicy {
	return &PathPolicy{roots: roots}
}

// NewDefaultPathPolicy 创建使用默认允许目录的策略
func NewDefaultPathPolicy() *PathPolicy {
	return NewPathPolicy(DefaultAllowedRoots())
}

// Validate 检查路径是否在允许的目录内
// 返回清理后的路径和错误
func (p *PathPolicy) Validate(path string) (string, error) {
	return security.CleanAbsWithin(path, p.roots)
}

// ValidateSymlinkTarget 检查符号链接目标是否安全
// 相对目标路径基于链接所在目录解析后，也必须在允许范围内
func (p *PathPolicy) ValidateSymlinkTarget(linkPath, target string) error {
	// 如果是绝对路径，直接验证
	if filepath.IsAbs(target) {
		_, err := security.CleanAbsWithin(target, p.roots)
		return err
	}

	// 相对路径：基于链接所在目录解析后验证
	linkDir := filepath.Dir(linkPath)
	resolved := filepath.Clean(filepath.Join(linkDir, target))

	// 检查解析后的路径是否在允许范围内
	_, err := security.CleanAbsWithin(resolved, p.roots)
	return err
}
