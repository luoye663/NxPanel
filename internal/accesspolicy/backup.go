package accesspolicy

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

type backupBundle struct {
	Policy    Policy              `json:"policy"`
	AuthFiles map[string]string   `json:"auth_files"`
	Accounts  []*repo.AuthAccount `json:"accounts"`
}

func (s *Service) backupAccounts(p Policy) ([]*repo.AuthAccount, error) {
	ids := referencedAccounts(p)
	result := []*repo.AuthAccount{}
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		rows, err := s.accounts.ListByIDs(ids[start:end])
		if err != nil {
			return nil, err
		}
		result = append(result, rows...)
	}
	if len(result) != len(ids) {
		return nil, fmt.Errorf("备份引用的访问账户不存在")
	}
	return result, nil
}
func referencedAccounts(p Policy) []string {
	seen := map[string]bool{}
	for _, r := range p.Rules {
		for _, id := range r.Action.AccountIDs {
			seen[id] = true
		}
	}
	for _, id := range p.DefaultAction.AccountIDs {
		seen[id] = true
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func (s *Service) backupPaths(siteID string) []string {
	return append([]string{s.bundlePath(siteID), s.globalPath(siteID)}, AuthSlotPaths(siteID, s.panelDir)...)
}
func (s *Service) withBackupLock(ctx context.Context, siteID string, fn func() error) error {
	release := s.acquireSite(siteID)
	defer release()
	locked := func(_ []byte) error { s.mu.Lock(); defer s.mu.Unlock(); return fn() }
	if s.proxy != nil {
		return s.proxy.WithAccessPolicyConfig(ctx, siteID, locked)
	}
	return locked(nil)
}

// WithBackup holds the same writer locks as policy application. Empty legacy
// slots prevent abandoned policy files from entering newly created backups.
func (s *Service) WithBackup(ctx context.Context, siteID string, run func([]string) error) error {
	return s.withBackupLock(ctx, siteID, func() error {
		paths := s.backupPaths(siteID)
		if !s.Managed(siteID) {
			paths = make([]string, len(paths))
		}
		return run(paths)
	})
}

// RestoreBackup reads only the bounded config prefix, preflights account
// conflicts, and changes metadata only after the agent successfully restores.
func (s *Service) RestoreBackup(ctx context.Context, siteID string, archive io.Reader, run func([]string) error) error {
	bundle, err := readBackupBundle(archive)
	if err != nil {
		return err
	}
	return s.withBackupLock(ctx, siteID, func() error {
		var additions []*repo.AuthAccount
		if bundle != nil {
			var err error
			additions, err = s.prepareBackupAccounts(siteID, bundle)
			if err != nil {
				return err
			}
		}
		if err := run(s.backupPaths(siteID)); err != nil {
			return err
		}
		if err := s.commitBackupPolicy(ctx, siteID, bundle, additions); err != nil {
			return fmt.Errorf("配置已恢复，但访问策略元数据恢复失败，请检查后同步：%w", err)
		}
		return nil
	})
}
func readBackupBundle(reader io.Reader) (*backupBundle, error) {
	compressed := &io.LimitedReader{R: reader, N: 128 * 1024 * 1024}
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("读取访问策略备份: %w", err)
	}
	defer gz.Close()
	expanded := &io.LimitedReader{R: gz, N: 256 * 1024 * 1024}
	tr := tar.NewReader(expanded)
	for count := 0; count < MaxRules+16; count++ {
		header, err := tr.Next()
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("读取访问策略备份目录: %w", err)
		}
		if header.Name == "metadata.json" || !strings.HasPrefix(header.Name, "config/") {
			return nil, nil
		}
		if header.Size < 0 || header.Size > MaxRenderedBytes {
			return nil, fmt.Errorf("备份配置超过 64 MiB")
		}
		if header.Name != "config/extra-4.conf" {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("策略备份必须是普通文件")
		}
		raw, err := io.ReadAll(io.LimitReader(tr, MaxRenderedBytes+1))
		if err != nil {
			return nil, err
		}
		if len(raw) > MaxRenderedBytes {
			return nil, fmt.Errorf("策略备份超过 64 MiB")
		}
		var bundle backupBundle
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&bundle); err != nil {
			return nil, fmt.Errorf("策略备份格式错误: %w", err)
		}
		if decoder.Decode(new(any)) != io.EOF {
			return nil, fmt.Errorf("策略备份包含多余内容")
		}
		if err := Validate(bundle.Policy); err != nil {
			return nil, fmt.Errorf("备份策略无效: %w", err)
		}
		return &bundle, nil
	}
	return nil, fmt.Errorf("备份配置条目超出限制")
}
func (s *Service) prepareBackupAccounts(siteID string, bundle *backupBundle) ([]*repo.AuthAccount, error) {
	saved := map[string]*repo.AuthAccount{}
	for _, a := range bundle.Accounts {
		if a == nil || !identifier.MatchString(a.ID) || a.Username == "" || len(a.Username) > 256 || len(a.PasswordHash) > 4096 || strings.ContainsAny(a.Username, ":\r\n\x00") || strings.ContainsAny(a.PasswordHash, "\r\n\x00") || !strings.HasPrefix(a.PasswordHash, a.Username+":") {
			return nil, fmt.Errorf("备份访问账户无效")
		}
		if saved[a.ID] != nil {
			return nil, fmt.Errorf("备份账户 ID 重复")
		}
		saved[a.ID] = a
	}
	existing, err := s.accounts.ListForSite(siteID)
	if err != nil {
		return nil, err
	}
	byName := map[string]*repo.AuthAccount{}
	for _, a := range existing {
		byName[a.Username] = a
	}
	remap := map[string]string{}
	additions := []*repo.AuthAccount{}
	for _, id := range referencedAccounts(bundle.Policy) {
		a := saved[id]
		if a == nil {
			return nil, fmt.Errorf("策略备份缺少账户 %s", id)
		}
		if current := byName[a.Username]; current != nil {
			if current.PasswordHash != a.PasswordHash || current.Enabled != a.Enabled {
				return nil, fmt.Errorf("访问账户 %s 与备份密码或启用状态冲突，请先处理账户冲突再恢复", a.Username)
			}
			remap[id] = current.ID
			continue
		}
		exists, err := s.accounts.UsernameExists(a.Username, "")
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, fmt.Errorf("访问账户 %s 已属于其他站点，无法恢复", a.Username)
		}
		clone := *a
		clone.ID = app.NewID("auth")
		clone.Scope = "site"
		clone.SiteID = siteID
		additions = append(additions, &clone)
		byName[a.Username] = &clone
		remap[id] = clone.ID
	}
	rewrite := func(a *Action) {
		for i, id := range a.AccountIDs {
			a.AccountIDs[i] = remap[id]
		}
	}
	for i := range bundle.Policy.Rules {
		rewrite(&bundle.Policy.Rules[i].Action)
	}
	rewrite(&bundle.Policy.DefaultAction)
	return additions, nil
}
func (s *Service) commitBackupPolicy(ctx context.Context, siteID string, bundle *backupBundle, additions []*repo.AuthAccount) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if bundle == nil {
		if _, err := tx.ExecContext(ctx, "DELETE FROM site_access_policies WHERE site_id=?", siteID); err != nil {
			return err
		}
		return tx.Commit()
	}
	for _, a := range additions {
		if _, err := tx.ExecContext(ctx, `INSERT INTO auth_accounts(id,scope,site_id,username,password_hash,enabled) VALUES(?,?,?,?,?,?)`, a.ID, a.Scope, a.SiteID, a.Username, a.PasswordHash, a.Enabled); err != nil {
			return err
		}
	}
	var version int64
	err = tx.QueryRowContext(ctx, "SELECT version FROM site_access_policies WHERE site_id=?", siteID).Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	p := bundle.Policy
	p.Version = version + 1
	p.Mode = "unified"
	p.ApplyStatus = "applied"
	p.LastError = ""
	p.PendingSource = ""
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO site_access_policies(site_id,mode,version,desired_json,applied_json,applied_version,apply_status) VALUES(?,'unified',?,?,?,?, 'applied') ON CONFLICT(site_id) DO UPDATE SET mode='unified',version=excluded.version,desired_json=excluded.desired_json,applied_json=excluded.applied_json,applied_version=excluded.applied_version,apply_status='applied',last_error='',updated_at=CURRENT_TIMESTAMP`, siteID, p.Version, string(raw), string(raw), p.Version)
	if err != nil {
		return err
	}
	return tx.Commit()
}
