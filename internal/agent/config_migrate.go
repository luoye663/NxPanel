// config_migrate.go — 配置文件迁移逻辑
//
// 功能：
//  1. 读取当前 config.yaml
//  2. 与 defaultConfig() 对比，添加缺失字段（带注释说明）
//  3. 标记废弃字段（带注释警告）
//  4. 写回文件，保留用户已修改的值
//
// 使用方式：nxpanel-agent --migrate-config --config /path/to/config.yaml
package agent

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/luoye663/nxpanel/internal/app"
	"gopkg.in/yaml.v3"
)

// deprecatedFields 定义已废弃的配置字段
// key: YAML 路径（如 "nginx.static_mode"）
// value: 废弃说明（如 "v0.2.0 废弃，请使用标准模式"）
var deprecatedFields = map[string]string{
	// 示例（取消注释以启用）：
	// "nginx.static_mode":     "v0.2.0 废弃，请使用标准模式",
	// "api.old_rate_limit":    "v0.3.0 废弃，请使用 api.rate_limit",
}

// MigrateConfig 迁移配置文件
// 1. 读取当前 config.yaml
// 2. 与 defaultConfig() 对比，添加缺失字段
// 3. 标记废弃字段
// 4. 写回文件
func MigrateConfig(configPath string) error {
	// 读取当前配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 解析为 YAML 节点树（保留注释和格式）
	var userDoc yaml.Node
	if err := yaml.Unmarshal(data, &userDoc); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 确保是文档节点
	if userDoc.Kind != yaml.DocumentNode || len(userDoc.Content) == 0 {
		return fmt.Errorf("配置文件格式错误：不是有效的 YAML 文档")
	}

	// 获取根映射节点
	userRoot := userDoc.Content[0]
	if userRoot.Kind != yaml.MappingNode {
		return fmt.Errorf("配置文件格式错误：根节点不是映射")
	}

	// 获取默认配置
	defaultCfg := app.DefaultConfig()
	if defaultCfg == nil {
		return fmt.Errorf("无法获取默认配置")
	}

	// 将默认配置转换为 YAML 节点树
	defaultData, err := yaml.Marshal(defaultCfg)
	if err != nil {
		return fmt.Errorf("序列化默认配置失败: %w", err)
	}

	var defaultDoc yaml.Node
	if err := yaml.Unmarshal(defaultData, &defaultDoc); err != nil {
		return fmt.Errorf("解析默认配置失败: %w", err)
	}

	defaultRoot := defaultDoc.Content[0]

	// 执行迁移：添加缺失字段
	added := mergeNodes(userRoot, defaultRoot, "")

	// 标记废弃字段
	deprecated := markDeprecatedFields(userRoot, "")

	// 写回文件
	if added > 0 || deprecated > 0 {
		if err := writeYAMLToFile(configPath, &userDoc); err != nil {
			return fmt.Errorf("写入配置文件失败: %w", err)
		}

		if added > 0 {
			slog.Info("配置迁移：添加了缺失字段", "count", added)
		}
		if deprecated > 0 {
			slog.Info("配置迁移：标记了废弃字段", "count", deprecated)
		}
	} else {
		slog.Info("配置文件无需迁移")
	}

	return nil
}

// mergeNodes 递归合并 YAML 节点，将 defaultNode 中存在但 userNode 中缺失的字段添加到 userNode
// 返回添加的字段数量
func mergeNodes(userNode, defaultNode *yaml.Node, path string) int {
	if userNode == nil || defaultNode == nil {
		return 0
	}

	added := 0

	// 只处理映射节点
	if userNode.Kind != yaml.MappingNode || defaultNode.Kind != yaml.MappingNode {
		return 0
	}

	// 构建用户节点的键索引
	userKeys := make(map[string]int)
	for i := 0; i < len(userNode.Content); i += 2 {
		if i+1 < len(userNode.Content) {
			key := userNode.Content[i].Value
			userKeys[key] = i
		}
	}

	// 遍历默认节点的键值对
	for i := 0; i < len(defaultNode.Content); i += 2 {
		if i+1 >= len(defaultNode.Content) {
			break
		}

		defaultKey := defaultNode.Content[i]
		defaultValue := defaultNode.Content[i+1]

		fieldPath := path
		if fieldPath == "" {
			fieldPath = defaultKey.Value
		} else {
			fieldPath = path + "." + defaultKey.Value
		}

		// 检查用户配置中是否存在该键
		userIdx, exists := userKeys[defaultKey.Value]
		if !exists {
			// 键不存在，添加到用户节点
			// 创建新的键节点
			newKey := &yaml.Node{
				Kind:  yaml.ScalarNode,
				Tag:   defaultKey.Tag,
				Value: defaultKey.Value,
				LineComment: fmt.Sprintf("added by migration"),
			}

			// 创建新的值节点（深拷贝）
			newValue := deepCopyNode(defaultValue)

			// 添加注释说明
			if isLeafNode(defaultValue) {
				newValue.HeadComment = fmt.Sprintf("新增字段，默认值: %v", getNodeValue(defaultValue))
			}

			// 添加到用户节点
			userNode.Content = append(userNode.Content, newKey, newValue)
			added++

			slog.Debug("添加缺失字段", "field", fieldPath, "default", getNodeValue(defaultValue))
		} else {
			// 键存在，递归处理子节点
			if userIdx+1 < len(userNode.Content) {
				added += mergeNodes(userNode.Content[userIdx+1], defaultValue, fieldPath)
			}
		}
	}

	return added
}

// markDeprecatedFields 标记废弃字段
// 返回标记的字段数量
func markDeprecatedFields(node *yaml.Node, path string) int {
	if node == nil || node.Kind != yaml.MappingNode {
		return 0
	}

	marked := 0

	for i := 0; i < len(node.Content); i += 2 {
		if i+1 >= len(node.Content) {
			break
		}

		key := node.Content[i]
		value := node.Content[i+1]

		fieldPath := path
		if fieldPath == "" {
			fieldPath = key.Value
		} else {
			fieldPath = path + "." + key.Value
		}

		// 检查是否是废弃字段
		if reason, isDeprecated := deprecatedFields[fieldPath]; isDeprecated {
			// 添加废弃警告注释
			warning := fmt.Sprintf("⚠️ [废弃] %s", reason)
			if key.HeadComment == "" {
				key.HeadComment = warning
			} else {
				key.HeadComment = key.HeadComment + "\n" + warning
			}
			marked++

			slog.Debug("标记废弃字段", "field", fieldPath, "reason", reason)
		}

		// 递归处理子节点
		marked += markDeprecatedFields(value, fieldPath)
	}

	return marked
}

// deepCopyNode 深拷贝 YAML 节点
func deepCopyNode(src *yaml.Node) *yaml.Node {
	if src == nil {
		return nil
	}

	dst := &yaml.Node{
		Kind:        src.Kind,
		Style:       src.Style,
		Tag:         src.Tag,
		Value:       src.Value,
		Anchor:      src.Anchor,
		Alias:       src.Alias,
		HeadComment: src.HeadComment,
		LineComment: src.LineComment,
		FootComment: src.FootComment,
		Line:        src.Line,
		Column:      src.Column,
	}

	if len(src.Content) > 0 {
		dst.Content = make([]*yaml.Node, len(src.Content))
		for i, child := range src.Content {
			dst.Content[i] = deepCopyNode(child)
		}
	}

	return dst
}

// isLeafNode 判断是否是叶子节点（标量）
func isLeafNode(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	return node.Kind == yaml.ScalarNode
}

// getNodeValue 获取节点的字符串表示
func getNodeValue(node *yaml.Node) interface{} {
	if node == nil {
		return nil
	}

	switch node.Kind {
	case yaml.ScalarNode:
		return node.Value
	case yaml.MappingNode:
		return "{...}"
	case yaml.SequenceNode:
		return "[...]"
	default:
		return nil
	}
}

// writeYAMLToFile 将 YAML 节点树写入文件
func writeYAMLToFile(path string, doc *yaml.Node) error {
	// 创建备份
	backupPath := path + ".bak"
	data, err := os.ReadFile(path)
	if err == nil {
		_ = os.WriteFile(backupPath, data, 0644)
	}

	// 序列化
	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return fmt.Errorf("序列化 YAML 失败: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("关闭 YAML 编码器失败: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(path, []byte(buf.String()), 0644); err != nil {
		// 写入失败，尝试恢复备份
		if backupData, readErr := os.ReadFile(backupPath); readErr == nil {
			_ = os.WriteFile(path, backupData, 0644)
		}
		return fmt.Errorf("写入文件失败: %w", err)
	}

	// 删除备份（成功写入后）
	_ = os.Remove(backupPath)

	return nil
}
