// db 包 — SQLite 数据库初始化与迁移
//
// 职责：
//   - 打开 SQLite 连接（modernc.org/sqlite，纯 Go，无 CGO）
//   - 执行 PRAGMA 配置（WAL、busy_timeout、foreign_keys、synchronous）
//   - 运行迁移文件（embed 的 SQL 文件）
//   - 提供 *sql.DB 给各 repository 使用
package db

import (
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ============================================================
// 数据库初始化
// ============================================================

// Open 打开 SQLite 数据库连接并执行初始化配置
//
// 参数：
//   - dsn: SQLite 数据源名称，如 "file:/path/to/panel.db" 或 ":memory:"
//
// 初始化流程：
//  1. 打开连接
//  2. 执行 PRAGMA 配置
//  3. 返回 *sql.DB 供业务使用
func Open(dsn string) (*sql.DB, error) {
	slog.Info("打开 SQLite 数据库", "dsn", dsn)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}

	// 执行 PRAGMA 配置，开启 WAL、外键约束和合理的 busy_timeout。
	if err := configurePragma(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("配置 PRAGMA 失败: %w", err)
	}

	// 测试连接是否可用
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("数据库连接测试失败: %w", err)
	}

	return db, nil
}

// configurePragma 执行 SQLite PRAGMA 配置
//
// 配置项：
//   - journal_mode = WAL        ：写前日志模式，允许读写并发
//   - busy_timeout = 5000       ：写冲突等待 5 秒
//   - foreign_keys = ON         ：启用外键约束
//   - synchronous = NORMAL      ：平衡安全与性能
func configurePragma(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("执行 %q 失败: %w", p, err)
		}
	}
	return nil
}

// ============================================================
// 迁移运行器
// ============================================================

// Migration 表示一个待执行的数据库迁移
type Migration struct {
	Version int    // 迁移版本号，从文件名解析（如 0001 → 1）
	SQL     string // 迁移 SQL 内容
}

// RunMigrations 执行所有未应用的迁移
//
// 工作方式：
//  1. 从 embed.FS 读取 migrations/*.sql 文件
//  2. 查询 schema_migrations 表获取已应用的版本
//  3. 按版本号顺序执行未应用的迁移
//  4. 每个迁移在一个事务内执行，成功后记录版本号
//
// 迁移文件命名规则：{version}_{description}.sql（如 0001_init.sql）
func RunMigrations(db *sql.DB) error {
	// 读取所有嵌入的迁移文件
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("加载迁移文件失败: %w", err)
	}

	if len(migrations) == 0 {
		slog.Info("没有发现迁移文件")
		return nil
	}

	// 获取已应用的版本
	applied, err := getAppliedVersions(db)
	if err != nil {
		return fmt.Errorf("查询已应用迁移版本失败: %w", err)
	}

	// 按版本号排序，依次执行
	for _, m := range migrations {
		if applied[m.Version] {
			slog.Debug("迁移已应用，跳过", "version", m.Version)
			continue
		}

		slog.Info("执行数据库迁移", "version", m.Version)

		// 在事务内执行迁移
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("开启迁移事务失败 (version=%d): %w", m.Version, err)
		}

		if _, err := tx.Exec(m.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("执行迁移 SQL 失败 (version=%d): %w", m.Version, err)
		}

		// 记录已应用的版本
		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version) VALUES (?)",
			m.Version,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("记录迁移版本失败 (version=%d): %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交迁移事务失败 (version=%d): %w", m.Version, err)
		}

		slog.Info("数据库迁移完成", "version", m.Version)
	}

	if err := ensureAuthAccountSchema(db); err != nil {
		return fmt.Errorf("修复访问账户数据库结构失败: %w", err)
	}

	return nil
}

func ensureAuthAccountSchema(db *sql.DB) error {
	if err := ensureAuthAccountTables(db); err != nil {
		return err
	}
	if err := migrateLegacyAuthRules(db); err != nil {
		return err
	}
	_, _ = db.Exec("INSERT OR IGNORE INTO schema_migrations (version) VALUES (2)")
	return nil
}

func ensureAuthAccountTables(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS auth_accounts (
			id TEXT PRIMARY KEY,
			scope TEXT NOT NULL CHECK (scope IN ('global','site')),
			site_id TEXT REFERENCES sites(id) ON DELETE CASCADE,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			CHECK ((scope = 'global' AND site_id IS NULL) OR (scope = 'site' AND site_id IS NOT NULL))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_accounts_scope_site ON auth_accounts(scope, site_id)`,
		`CREATE TABLE IF NOT EXISTS site_auth_rule_accounts (
			rule_id TEXT NOT NULL REFERENCES site_auth_rules(id) ON DELETE CASCADE,
			account_id TEXT NOT NULL REFERENCES auth_accounts(id) ON DELETE RESTRICT,
			PRIMARY KEY (rule_id, account_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_site_auth_rule_accounts_account ON site_auth_rule_accounts(account_id)`,
		`CREATE TABLE IF NOT EXISTS site_proxy_auth_accounts (
			proxy_id TEXT NOT NULL REFERENCES site_proxy(id) ON DELETE CASCADE,
			account_id TEXT NOT NULL REFERENCES auth_accounts(id) ON DELETE RESTRICT,
			PRIMARY KEY (proxy_id, account_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_site_proxy_auth_accounts_account ON site_proxy_auth_accounts(account_id)`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	if ok, err := columnExists(db, "site_proxy", "auth_enabled"); err != nil {
		return err
	} else if !ok {
		if _, err := db.Exec(`ALTER TABLE site_proxy ADD COLUMN auth_enabled INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if ok, err := columnExists(db, "site_proxy", "auth_htpasswd_path"); err != nil {
		return err
	} else if !ok {
		if _, err := db.Exec(`ALTER TABLE site_proxy ADD COLUMN auth_htpasswd_path TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyAuthRules(db *sql.DB) error {
	rows, err := db.Query(`SELECT r.id, r.site_id, r.username, r.password_hash, r.created_at, r.updated_at
		FROM site_auth_rules r
		LEFT JOIN site_auth_rule_accounts ra ON ra.rule_id = r.id
		WHERE ra.rule_id IS NULL AND r.username IS NOT NULL AND r.username != ''`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type legacyRule struct {
		ruleID, siteID, username, passwordHash, createdAt, updatedAt string
	}
	var rules []legacyRule
	for rows.Next() {
		var rule legacyRule
		if err := rows.Scan(&rule.ruleID, &rule.siteID, &rule.username, &rule.passwordHash, &rule.createdAt, &rule.updatedAt); err != nil {
			return err
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, rule := range rules {
		accountID, ok, err := compatibleAccountID(db, rule.username, rule.passwordHash)
		if err != nil {
			return err
		}
		if !ok {
			username, err := uniqueAuthUsername(db, rule.username, rule.ruleID)
			if err != nil {
				return err
			}
			accountID = newMigrationID()
			if _, err := db.Exec(`INSERT INTO auth_accounts (id, scope, site_id, username, password_hash, enabled, created_at, updated_at)
				VALUES (?, 'site', ?, ?, ?, 1, ?, ?)`, accountID, rule.siteID, username, rule.passwordHash, rule.createdAt, rule.updatedAt); err != nil {
				return err
			}
		}
		if _, err := db.Exec(`INSERT OR IGNORE INTO site_auth_rule_accounts (rule_id, account_id) VALUES (?, ?)`, rule.ruleID, accountID); err != nil {
			return err
		}
	}
	return nil
}

func compatibleAccountID(db *sql.DB, username, passwordHash string) (string, bool, error) {
	var id, existingHash string
	err := db.QueryRow(`SELECT id, password_hash FROM auth_accounts WHERE username = ?`, username).Scan(&id, &existingHash)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if existingHash == passwordHash {
		return id, true, nil
	}
	return "", false, nil
}

func uniqueAuthUsername(db *sql.DB, username, ruleID string) (string, error) {
	exists, err := authUsernameExists(db, username)
	if err != nil {
		return "", err
	}
	if !exists {
		return username, nil
	}
	base := username + "_" + ruleID
	for i := 0; ; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s_%d", base, i)
		}
		exists, err := authUsernameExists(db, candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
}

func authUsernameExists(db *sql.DB, username string) (bool, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_accounts WHERE username = ?`, username).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func columnExists(db *sql.DB, tableName, columnName string) (bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + tableName + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}

func newMigrationID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return "aa_" + hex.EncodeToString(b)
}

// loadMigrations 从 embed.FS 加载所有迁移文件并按版本号排序
func loadMigrations() ([]Migration, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		// 从文件名解析版本号：0001_init.sql → 1
		versionStr := strings.SplitN(entry.Name(), "_", 2)[0]
		version, err := strconv.Atoi(versionStr)
		if err != nil {
			return nil, fmt.Errorf("解析迁移文件版本号失败 %q: %w", entry.Name(), err)
		}

		content, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("读取迁移文件失败 %q: %w", entry.Name(), err)
		}

		migrations = append(migrations, Migration{
			Version: version,
			SQL:     string(content),
		})
	}

	// 按版本号升序排列
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// getAppliedVersions 查询已应用的迁移版本
func getAppliedVersions(db *sql.DB) (map[int]bool, error) {
	// schema_migrations 表由 0001_init.sql 创建
	// 首次运行时该表可能还不存在，需要容错
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		// 如果表不存在，说明还没运行过任何迁移
		return make(map[int]bool), nil
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// ============================================================
// 辅助函数
// ============================================================

// DSNFromPath 根据文件路径生成 SQLite 数据源名称
func DSNFromPath(dbPath string, busyTimeout int) string {
	if busyTimeout <= 0 {
		busyTimeout = 5000
	}
	return "file:" + dbPath +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(" + strconv.Itoa(busyTimeout) + ")" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)"
}
