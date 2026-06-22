// security 包 — 路径安全检查
//
// 所有 agent 操作的文件路径必须通过安全检查：
//   - 必须是绝对路径
//   - 不允许包含 ".."（路径穿越）
//   - 不允许包含空字节 \x00
//   - 不允许包含换行 \n \r
//   - 路径必须在 allowlist 指定的根目录内
//
// 这是 agent 安全的第一道防线，防止路径穿越攻击。
package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// CleanAbsWithin 检查路径是否在允许的根目录内
//
// 工作流程：
//  1. 检查路径是否为空、包含空字节或换行
//  2. 检查路径是否为绝对路径
//  3. 使用 filepath.Clean 清理路径（处理 ..、// 等）
//  4. 解析最近存在父目录的真实路径，避免新文件路径经过父目录 symlink 绕出白名单
//  5. 逐一检查真实目标路径是否在某个允许的真实根目录下
//
// 返回清理后的安全路径，或错误。
func CleanAbsWithin(path string, allowedRoots []string) (string, error) {
	// 基本合法性检查
	if err := validatePathBasics(path); err != nil {
		return "", err
	}

	// 必须是绝对路径
	if !filepath.IsAbs(path) {
		return "", errors.New("路径必须是绝对路径")
	}

	// 清理路径（去除多余的 /、处理 .. 等）
	clean := filepath.Clean(path)

	// 额外检查：清理后的路径不应包含 ".."
	// filepath.Clean 会处理 ".."，但我们在清理后再次确认
	if strings.Contains(clean, "..") {
		return "", errors.New("路径不允许包含 \"..\"")
	}

	// 解析目标真实路径：目标不存在时也要解析最近存在父目录，防止 /allowed/link/new
	// 这类路径中的 link 指向白名单外，最终写入落到非授权目录。
	realPath, err := resolveExistingParentRealPath(clean)
	if err != nil {
		return "", err
	}

	// 检查路径是否在任一允许的根目录下
	for _, root := range allowedRoots {
		rootReal, err := cleanAllowedRoot(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(rootReal, realPath)
		if err != nil {
			continue
		}
		// 如果相对路径不以 ".." 开头，说明路径在根目录内
		if rel != ".." && !strings.HasPrefix(rel, "../") {
			return clean, nil
		}
	}

	return "", errors.New("路径不在允许的目录范围内")
}

// cleanAllowedRoot 清理并尽量解析白名单根目录。
// 根目录本身如果是 symlink，应按真实目标参与比较；根目录尚不存在时保留 clean 后的路径，
// 兼容配置预先声明目录、后续再创建的场景。
func cleanAllowedRoot(root string) (string, error) {
	if err := validatePathBasics(root); err != nil {
		return "", err
	}
	if !filepath.IsAbs(root) {
		return "", errors.New("允许目录必须是绝对路径")
	}

	clean := filepath.Clean(root)
	realRoot, err := resolveExistingParentRealPath(clean)
	if err != nil {
		return "", err
	}
	return realRoot, nil
}

// resolveExistingParentRealPath 解析路径中“最近存在的父目录”的真实路径。
// 对新文件或 mkdir 多级目录，完整目标可能还不存在；此时不能直接回退 clean 路径，
// 必须先解析已存在父目录中的 symlink，再拼回剩余不存在的路径片段。
func resolveExistingParentRealPath(clean string) (string, error) {
	if realPath, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(realPath), nil
	}

	current := clean
	missingParts := []string{}
	for {
		if current == string(filepath.Separator) || current == "." || current == "" {
			return "", errors.New("路径不存在且没有可解析的父目录")
		}

		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return "", errors.New("最近存在的父路径不是目录")
			}
			realParent, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			parts := append([]string{filepath.Clean(realParent)}, missingParts...)
			return filepath.Clean(filepath.Join(parts...)), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}

		missingParts = append([]string{filepath.Base(current)}, missingParts...)
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("路径不存在且没有可解析的父目录")
		}
		current = parent
	}
}

// validatePathBasics 检查路径的基本合法性
func validatePathBasics(path string) error {
	if path == "" {
		return errors.New("路径不能为空")
	}
	// 检查空字节（防止注入攻击）
	if strings.Contains(path, "\x00") {
		return errors.New("路径不允许包含空字节")
	}
	// 检查换行符（防止日志注入和路径注入）
	if strings.ContainsAny(path, "\n\r") {
		return errors.New("路径不允许包含换行符")
	}
	return nil
}

// IsPathSafe 快速检查路径是否包含危险字符（不做 allowlist 检查）
// 用于早期的快速拒绝
func IsPathSafe(path string) bool {
	return validatePathBasics(path) == nil
}
