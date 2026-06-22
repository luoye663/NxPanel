// agent 包 — 原子文件写入
//
// 原子写入确保文件操作不会因断电、进程崩溃等原因产生损坏的半写文件。
//
// 原理：
//  1. 在目标目录创建临时文件（.openrest-*.tmp）
//  2. 将内容写入临时文件
//  3. 设置文件权限
//  4. 调用 Sync 强制刷盘
//  5. 关闭临时文件
//  6. 使用 Rename 将临时文件原子重命名为目标文件
//
// rename 在大多数文件系统上是原子操作，确保要么看到旧文件，要么看到新文件，
// 永远不会看到半写状态。
package agent

import (
	"os"
	"path/filepath"
)

// writeFileAtomic 原子写入文件
//
// 参数：
//   - path: 目标文件路径
//   - content: 文件内容
//   - perm: 文件权限
func writeFileAtomic(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	// 在同一目录创建临时文件（确保同一文件系统，rename 才是原子操作）
	tmp, err := os.CreateTemp(dir, ".openrest-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	// 无论成功失败，都尝试清理临时文件
	defer func() { _ = os.Remove(tmpName) }()

	// 写入内容
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}

	// 设置权限
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}

	// 强制刷盘（确保持久性）
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}

	// 关闭临时文件
	if err := tmp.Close(); err != nil {
		return err
	}

	// 原子重命名（这是整个操作的关键）
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}

	// 同步目录，确保 rename 操作持久化
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}

	return nil
}
