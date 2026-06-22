// agent 包 — 文件事务
//
// 文件事务是 agent 的核心安全机制，确保 Nginx 配置文件修改的原子性：
//  1. 备份：在修改前，将原始文件备份到 backups 目录
//  2. 原子写入：先写临时文件，再 rename，确保断电不损坏
//  3. 测试：执行 nginx -t 验证配置正确性
//  4. Reload：执行 nginx -s reload 使配置生效
//  5. 回滚：如果测试或 reload 失败，从备份恢复原始文件
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// FileChange 表示一个文件变更操作
type FileChange struct {
	Type          string `json:"type"`                     // 操作类型：write, remove, symlink, mkdir, truncate
	Path          string `json:"path"`                     // 目标文件路径
	Target        string `json:"target,omitempty"`         // symlink 的目标路径
	ContentBase64 string `json:"content_base64,omitempty"` // write 的内容（base64 编码）
	Content       []byte `json:"-"`                        // 解码后的内容（内部使用）
	Perm          uint32 `json:"perm,omitempty"`           // 文件权限（如 0644 = 420）
	RequireMarker string `json:"require_marker,omitempty"` // 要求文件中包含的标记
}

// BackupRecord 记录一次文件备份
type BackupRecord struct {
	FilePath   string `json:"file_path"`    // 原始文件路径
	BackupPath string `json:"backup_path"`  // 备份文件路径
	Existed    bool   `json:"file_existed"` // 原始文件是否存在
	Perm       uint32 `json:"perm"`         // 原始文件权限
}

// Transaction 表示一个文件事务
type Transaction struct {
	OperationID string         // 操作 ID（用于备份目录命名）
	BackupDir   string         // 备份目录路径
	Backups     []BackupRecord // 备份记录
	Policy      *PathPolicy    // 路径安全策略
	WebUser     string         // 网站文件所属用户（chown 时使用）
	WebGroup    string         // 网站文件所属组（chown 时使用）
}

// NewTransaction 创建新的文件事务
//
// 参数：
//   - operationID: 操作 ID，用于创建备份子目录
//   - backupBase: 备份根目录（如 /opt/nxpanel/nginx/backups）
//   - policy: 路径安全策略
//   - webUser: 网站文件所属用户（chown 类型时使用）
//   - webGroup: 网站文件所属组（chown 类型时使用）
func NewTransaction(operationID, backupBase string, policy *PathPolicy, webUser, webGroup string) *Transaction {
	return &Transaction{
		OperationID: operationID,
		BackupDir:   filepath.Join(backupBase, operationID),
		Policy:      policy,
		WebUser:     webUser,
		WebGroup:    webGroup,
	}
}

// backup 备份单个文件
//
// 工作方式：
//   - 如果文件不存在，记录 Existed=false，不创建备份
//   - 如果文件存在，读取内容并使用原子写入保存到备份目录
func (tx *Transaction) backup(path string) error {
	b := BackupRecord{
		FilePath:   path,
		BackupPath: filepath.Join(tx.BackupDir, filepath.Base(path)),
		Existed:    true,
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		b.Existed = false
		tx.Backups = append(tx.Backups, b)
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("获取文件信息失败 %s: %w", path, err)
	}
	b.Perm = uint32(info.Mode().Perm())

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取文件失败 %s: %w", path, err)
	}

	// 确保备份目录存在
	if err := os.MkdirAll(tx.BackupDir, 0700); err != nil {
		return fmt.Errorf("创建备份目录失败 %s: %w", tx.BackupDir, err)
	}

	// 使用原子写入保存备份
	if err := writeFileAtomic(b.BackupPath, data, 0600); err != nil {
		return fmt.Errorf("备份文件写入失败 %s: %w", b.BackupPath, err)
	}

	tx.Backups = append(tx.Backups, b)
	slog.Info("文件已备份", "path", path, "backup", b.BackupPath)
	return nil
}

// Apply 执行文件事务
//
// 流程：
//  1. 验证所有变更路径的安全性（path policy）
//  2. 备份所有将被修改的文件
//  3. 依次执行每个变更操作
//  4. 如果某步失败，立即回滚
//
// 此处只做文件操作。如果 handler 在 Apply 后发现 test/reload 失败，
// 可调用 Rollback 回滚。
func (tx *Transaction) Apply(ctx context.Context, changes []FileChange) error {
	// 1. 验证所有路径安全性
	for _, ch := range changes {
		if ch.Path != "" {
			if _, err := tx.Policy.Validate(ch.Path); err != nil {
				return fmt.Errorf("路径安全检查失败 %s: %w", ch.Path, err)
			}
		}
		// symlink 操作还需要验证 target
		if ch.Type == "symlink" && ch.Target != "" {
			if err := tx.Policy.ValidateSymlinkTarget(ch.Path, ch.Target); err != nil {
				return fmt.Errorf("symlink 目标安全检查失败 %s -> %s: %w", ch.Path, ch.Target, err)
			}
		}
	}

	// 2. 备份所有将被修改的文件
	// 跳过 mkdir（幂等操作）和 chown（不修改文件内容）
	for _, ch := range changes {
		if ch.Type == "mkdir" || ch.Type == "chown" {
			continue
		}
		if err := tx.backup(ch.Path); err != nil {
			return fmt.Errorf("备份失败: %w", err)
		}
	}

	// 3. 依次执行变更
	for _, ch := range changes {
		if err := tx.applyOne(ch); err != nil {
			// 执行失败，立即回滚
			slog.Error("文件变更执行失败，开始回滚", "change_type", ch.Type, "path", ch.Path, "error", err)
			_ = tx.Rollback(ctx)
			return fmt.Errorf("变更执行失败 %s %s: %w", ch.Type, ch.Path, err)
		}
		slog.Info("文件变更已执行", "type", ch.Type, "path", ch.Path)
	}

	return nil
}

// applyOne 执行单个文件变更操作
func (tx *Transaction) applyOne(ch FileChange) error {
	switch ch.Type {
	case "write":
		// 确保父目录存在
		dir := filepath.Dir(ch.Path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建父目录失败 %s: %w", dir, err)
		}
		perm := os.FileMode(0644)
		if ch.Perm > 0 {
			perm = os.FileMode(ch.Perm)
		}
		return writeFileAtomic(ch.Path, ch.Content, perm)

	case "remove":
		err := os.Remove(ch.Path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil

	case "symlink":
		// 先删除已有的文件/链接
		_ = os.Remove(ch.Path)
		return os.Symlink(ch.Target, ch.Path)

	case "mkdir":
		perm := os.FileMode(0755)
		if ch.Perm > 0 {
			perm = os.FileMode(ch.Perm)
		}
		return os.MkdirAll(ch.Path, perm)

	case "truncate":
		return os.Truncate(ch.Path, 0)

	case "chown":
		if tx.WebUser == "" {
			slog.Warn("chown 跳过: web_user 未配置")
			return nil
		}
		uid, err := resolveUser(tx.WebUser)
		if err != nil {
			return fmt.Errorf("解析 web_user %s 失败: %w", tx.WebUser, err)
		}
		gid, err := resolveGroup(tx.WebGroup)
		if err != nil {
			return fmt.Errorf("解析 web_group %s 失败: %w", tx.WebGroup, err)
		}
		return os.Chown(ch.Path, uid, gid)

	default:
		return fmt.Errorf("不支持的变更类型: %s", ch.Type)
	}
}

// Rollback 回滚事务：从备份恢复所有文件
//
// 回滚顺序与备份顺序相反（先恢复最后修改的文件）
func (tx *Transaction) Rollback(ctx context.Context) error {
	slog.Info("开始回滚事务", "operation_id", tx.OperationID)

	for i := len(tx.Backups) - 1; i >= 0; i-- {
		b := tx.Backups[i]
		if !b.Existed {
			// 原始文件不存在，删除新创建的文件
			_ = os.Remove(b.FilePath)
			continue
		}

		// 从备份恢复
		data, err := os.ReadFile(b.BackupPath)
		if err != nil {
			slog.Error("读取备份文件失败", "backup_path", b.BackupPath, "error", err)
			return fmt.Errorf("读取备份失败 %s: %w", b.BackupPath, err)
		}
		perm := b.Perm
		if perm == 0 {
			perm = 0644
		}
		if err := writeFileAtomic(b.FilePath, data, os.FileMode(perm)); err != nil {
			slog.Error("恢复文件失败", "path", b.FilePath, "error", err)
			return fmt.Errorf("恢复文件失败 %s: %w", b.FilePath, err)
		}
		slog.Info("文件已恢复", "path", b.FilePath, "from", b.BackupPath)
	}

	slog.Info("事务回滚完成", "operation_id", tx.OperationID)
	return nil
}
