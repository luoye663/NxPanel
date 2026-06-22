// db 包的数据库初始化和迁移测试
package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestOpenMemory 测试内存数据库打开和 PRAGMA 配置
func TestOpenMemory(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	defer db.Close()

	// 注意：:memory: 数据库不支持 WAL，journal_mode 会是 "memory"
	// 这是 SQLite 的限制，生产环境使用文件数据库时 WAL 才会生效
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("查询 journal_mode 失败: %v", err)
	}
	// :memory: 模式下 journal_mode 为 "memory"，文件模式才为 "wal"
	t.Logf("journal_mode = %s（:memory: 模式下预期为 memory）", journalMode)

	// 验证 foreign_keys 已开启
	var fkEnabled int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fkEnabled); err != nil {
		t.Fatalf("查询 foreign_keys 失败: %v", err)
	}
	if fkEnabled != 1 {
		t.Errorf("foreign_keys 期望 1，实际 %d", fkEnabled)
	}

	// 验证 busy_timeout
	var busyTimeout int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("查询 busy_timeout 失败: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout 期望 5000，实际 %d", busyTimeout)
	}
}

// TestOpenFile 测试文件数据库打开
func TestOpenFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("打开文件数据库失败: %v", err)
	}
	defer db.Close()

	// 验证文件已创建
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("数据库文件应已创建")
	}
}

// TestRunMigrations 测试迁移执行
func TestRunMigrations(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	defer db.Close()

	// 执行迁移
	if err := RunMigrations(db); err != nil {
		t.Fatalf("执行迁移失败: %v", err)
	}

	// 验证 schema_migrations 表中有记录
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("查询迁移记录失败: %v", err)
	}
	if count == 0 {
		t.Error("迁移执行后 schema_migrations 应有记录")
	}

	// 验证最新迁移版本
	var version int
	if err := db.QueryRow("SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&version); err != nil {
		t.Fatalf("查询迁移版本失败: %v", err)
	}
	if version != 1 {
		t.Errorf("迁移版本期望 1，实际 %d", version)
	}
}

// TestRunMigrations_Idempotent 测试迁移幂等性（重复执行不应报错）
func TestRunMigrations_Idempotent(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	defer db.Close()

	// 执行两次
	if err := RunMigrations(db); err != nil {
		t.Fatalf("第一次执行迁移失败: %v", err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatalf("第二次执行迁移（幂等）失败: %v", err)
	}

	// 验证只有一条迁移记录
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("查询迁移记录失败: %v", err)
	}
	if count != 1 {
		t.Errorf("幂等迁移后只应有 1 条记录（1个迁移文件），实际 %d", count)
	}
}

// TestMigrationCreatesAllTables 验证迁移创建了所有预期的表
func TestMigrationCreatesAllTables(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("执行迁移失败: %v", err)
	}

	expectedTables := []string{
		"schema_migrations",
		"settings",
		"admin_account",
		"sessions",
		"sites",
		"site_proxy",
		"site_ssl",
		"site_rewrite",
		"operations",
		"backups",
		"site_backups",
		"site_backup_schedules",
		"scheduled_tasks",
		"scheduled_task_runs",
		"certificates",
		"access_analysis_settings",
		"access_analysis_jobs",
		"access_analysis_daily",
		"access_analysis_hourly",
		"access_analysis_paths",
		"access_analysis_ips",
		"access_analysis_entries",
		"access_analysis_anomalies",
		"site_hotlink_rules",
		"rewrite_templates",
	}

	for _, table := range expectedTables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
			table,
		).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("表 %s 未创建", table)
		} else if err != nil {
			t.Fatalf("查询表 %s 失败: %v", table, err)
		}
	}
}

// TestMigrationCreatesIndexes 验证迁移创建了所有预期的索引
func TestMigrationCreatesIndexes(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	defer db.Close()

	if err := RunMigrations(db); err != nil {
		t.Fatalf("执行迁移失败: %v", err)
	}

	expectedIndexes := []string{
		"idx_sessions_expires_at",
		"idx_sites_status",
		"idx_sites_primary_domain",
		"idx_site_ssl_enabled",
		"idx_operations_created_at",
		"idx_operations_target",
		"idx_backups_operation_id",
		"idx_site_hotlink_rules_site_id",
		"idx_site_backups_site_id_created",
		"idx_site_backup_schedules_enabled",
		"idx_scheduled_tasks_enabled_next",
		"idx_scheduled_task_runs_task_created",
		"idx_rewrite_templates_enabled_sort",
	}

	for _, idx := range expectedIndexes {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='index' AND name=?",
			idx,
		).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("索引 %s 未创建", idx)
		} else if err != nil {
			t.Fatalf("查询索引 %s 失败: %v", idx, err)
		}
	}
}

// TestDSNFromPath 测试 DSN 生成
func TestDSNFromPath(t *testing.T) {
	dsn := DSNFromPath("/opt/nxpanel/data/panel.db", 5000)
	if dsn == "" {
		t.Error("DSNFromPath 不应返回空字符串")
	}
	if dsn[:5] != "file:" {
		t.Errorf("DSN 应以 file: 开头，实际: %s", dsn)
	}
}
