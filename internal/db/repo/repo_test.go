// repo 包的 repository 集成测试
//
// 使用内存 SQLite 数据库测试各 repository 的基础 CRUD。
// 每个测试独立创建数据库和迁移。
package repo

import (
	"database/sql"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/db"
)

// setupTestDB 创建临时数据库并运行迁移
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("运行测试迁移失败: %v", err)
	}
	return database
}

// ============================================================
// Settings Repository 测试
// ============================================================

func TestSettingsRepo_SetAndGet(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSettingsRepo(database)

	// Set 写入
	if err := repo.Set("nginx_bin", "/usr/sbin/nginx"); err != nil {
		t.Fatalf("写入 setting 失败: %v", err)
	}

	// Get 读取
	val, err := repo.Get("nginx_bin")
	if err != nil {
		t.Fatalf("读取 setting 失败: %v", err)
	}
	if val != "/usr/sbin/nginx" {
		t.Errorf("期望 /usr/sbin/nginx，实际 %s", val)
	}
}

func TestSettingsRepo_GetNonExistent(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSettingsRepo(database)

	val, err := repo.Get("nonexistent")
	if err != nil {
		t.Fatalf("查询不存在的 key 不应报错: %v", err)
	}
	if val != "" {
		t.Errorf("不存在的 key 应返回空字符串，实际 %s", val)
	}
}

func TestSettingsRepo_Update(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSettingsRepo(database)

	// 先写入
	repo.Set("key1", "value1")
	// 再更新
	repo.Set("key1", "value2")

	val, _ := repo.Get("key1")
	if val != "value2" {
		t.Errorf("更新后期望 value2，实际 %s", val)
	}
}

func TestSettingsRepo_GetAll(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSettingsRepo(database)

	repo.Set("k1", "v1")
	repo.Set("k2", "v2")

	all, err := repo.GetAll()
	if err != nil {
		t.Fatalf("GetAll 失败: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("期望 2 个 settings，实际 %d", len(all))
	}
	if all["k1"] != "v1" || all["k2"] != "v2" {
		t.Errorf("GetAll 值不正确: %v", all)
	}
}

// ============================================================
// Admin Repository 测试
// ============================================================

func TestAdminRepo_NotExists(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewAdminRepo(database)

	exists, err := repo.Exists()
	if err != nil {
		t.Fatalf("查询管理员存在性失败: %v", err)
	}
	if exists {
		t.Error("初始状态管理员不应存在")
	}
}

func TestAdminRepo_CreateAndGet(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewAdminRepo(database)

	// 创建管理员
	if err := repo.Create("admin", "$2a$10$hashedpassword", "bcrypt"); err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}

	// 验证存在
	exists, _ := repo.Exists()
	if !exists {
		t.Error("创建后管理员应存在")
	}

	// 通过用户名查找
	admin, err := repo.GetByUsername("admin")
	if err != nil {
		t.Fatalf("查询管理员失败: %v", err)
	}
	if admin == nil {
		t.Fatal("管理员不应为 nil")
	}
	if admin.Username != "admin" {
		t.Errorf("用户名期望 admin，实际 %s", admin.Username)
	}
	if admin.PasswordHash != "$2a$10$hashedpassword" {
		t.Error("密码 hash 不匹配")
	}
	if admin.PasswordAlgo != "bcrypt" {
		t.Errorf("密码算法期望 bcrypt，实际 %s", admin.PasswordAlgo)
	}
}

func TestAdminRepo_CreateOnlyOnce(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewAdminRepo(database)

	// 第一次创建应成功
	if err := repo.Create("admin", "hash1", "bcrypt"); err != nil {
		t.Fatalf("第一次创建管理员失败: %v", err)
	}

	// 第二次创建应失败（CHECK(id=1) 约束）
	if err := repo.Create("admin2", "hash2", "bcrypt"); err == nil {
		t.Error("重复创建管理员应失败")
	}
}

func TestAdminRepo_Get(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewAdminRepo(database)

	// 未创建时应返回 nil
	admin, err := repo.Get()
	if err != nil {
		t.Fatalf("查询管理员失败: %v", err)
	}
	if admin != nil {
		t.Error("未创建时 Get 应返回 nil")
	}

	// 创建后查询
	repo.Create("admin", "hash", "bcrypt")
	admin, err = repo.Get()
	if err != nil {
		t.Fatalf("查询管理员失败: %v", err)
	}
	if admin == nil || admin.Username != "admin" {
		t.Error("创建后 Get 应返回管理员信息")
	}
}

func TestAdminRepo_UpdatePassword(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewAdminRepo(database)
	repo.Create("admin", "old_hash", "bcrypt")

	if err := repo.UpdatePassword("new_hash", "argon2id"); err != nil {
		t.Fatalf("更新密码失败: %v", err)
	}

	admin, _ := repo.Get()
	if admin.PasswordHash != "new_hash" {
		t.Errorf("密码 hash 期望 new_hash，实际 %s", admin.PasswordHash)
	}
	if admin.PasswordAlgo != "argon2id" {
		t.Errorf("密码算法期望 argon2id，实际 %s", admin.PasswordAlgo)
	}
}

func TestAdminRepo_ConsumeTOTPCodeCAS(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewAdminRepo(database)
	if err := repo.Create("admin", "hash", "bcrypt"); err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}

	usedAt := time.Now().UTC()
	ok, err := repo.ConsumeTOTPCode(1, "123456", usedAt, 90*time.Second)
	if err != nil {
		t.Fatalf("首次消费 TOTP 失败: %v", err)
	}
	if !ok {
		t.Fatal("首次消费 TOTP 应成功")
	}

	// 同一验证码在重放窗口内再次消费应被 CAS 条件拒绝。
	ok, err = repo.ConsumeTOTPCode(1, "123456", usedAt.Add(time.Second), 90*time.Second)
	if err != nil {
		t.Fatalf("重复消费 TOTP 查询失败: %v", err)
	}
	if ok {
		t.Fatal("同一 TOTP 在重放窗口内不应重复消费成功")
	}

	ok, err = repo.ConsumeTOTPCode(1, "123456", usedAt.Add(91*time.Second), 90*time.Second)
	if err != nil {
		t.Fatalf("窗口外消费 TOTP 查询失败: %v", err)
	}
	if !ok {
		t.Fatal("同一 TOTP 超出重放窗口后应允许消费")
	}
}

// ============================================================
// Site Repository 测试
// ============================================================

func TestSiteRepo_CreateAndGet(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSiteRepo(database)

	site := &Site{
		ID:               "site_test1",
		PrimaryDomain:    "example.com",
		DomainsJSON:      `["example.com","www.example.com"]`,
		Status:           "enabled",
		HTTPPort:         80,
		HTTPSPort:        443,
		RootPath:         "/www/wwwroot/example.com",
		IndexFiles:       "index.html index.htm",
		AccessLogEnabled: true,
		AccessLogPath:    "/www/wwwlogs/example.com.access.log",
		ErrorLogPath:     "/www/wwwlogs/example.com.error.log",
		ConfigPath:       "/opt/nxpanel/nginx/sites-available/example.com.conf",
		EnabledPath:      "/opt/nxpanel/nginx/sites-enabled/example.com.conf",
		RewritePath:      "/opt/nxpanel/nginx/rewrite/example.com.conf",
	}

	if err := repo.Create(site); err != nil {
		t.Fatalf("创建站点失败: %v", err)
	}

	// 按 ID 查询
	found, err := repo.GetByID("site_test1")
	if err != nil {
		t.Fatalf("按 ID 查询站点失败: %v", err)
	}
	if found == nil {
		t.Fatal("站点不应为 nil")
	}
	if found.PrimaryDomain != "example.com" {
		t.Errorf("主域名期望 example.com，实际 %s", found.PrimaryDomain)
	}
	if !found.AccessLogEnabled {
		t.Error("AccessLogEnabled 应为 true")
	}
	if found.HTTPPort != 80 {
		t.Errorf("HTTPPort 期望 80，实际 %d", found.HTTPPort)
	}
}

func TestSiteRepo_GetByPrimaryDomain(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSiteRepo(database)

	repo.Create(&Site{
		ID:            "site_1",
		PrimaryDomain: "test.com",
		DomainsJSON:   `["test.com"]`,
		Status:        "disabled",
		HTTPPort:      80,
		RootPath:      "/www/test",
		IndexFiles:    "index.html",
		AccessLogPath: "/www/logs/test.access.log",
		ErrorLogPath:  "/www/logs/test.error.log",
		ConfigPath:    "/opt/panel/nginx/sites-available/test.com.conf",
		EnabledPath:   "/opt/panel/nginx/sites-enabled/test.com.conf",
		RewritePath:   "/opt/panel/nginx/rewrite/test.com.conf",
	})

	found, err := repo.GetByPrimaryDomain("test.com")
	if err != nil {
		t.Fatalf("按域名查询失败: %v", err)
	}
	if found == nil {
		t.Fatal("应能按域名找到站点")
	}
}

func TestSiteRepo_List(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSiteRepo(database)

	// 创建多个站点
	for i := 0; i < 5; i++ {
		repo.Create(&Site{
			ID:            "site_" + string(rune('a'+i)),
			PrimaryDomain: string(rune('a'+i)) + ".com",
			DomainsJSON:   `["` + string(rune('a'+i)) + `.com"]`,
			Status:        "enabled",
			HTTPPort:      80,
			RootPath:      "/www/" + string(rune('a'+i)),
			IndexFiles:    "index.html",
			AccessLogPath: "/www/logs/" + string(rune('a'+i)) + ".log",
			ErrorLogPath:  "/www/logs/" + string(rune('a'+i)) + ".err",
			ConfigPath:    "/opt/panel/sites/" + string(rune('a'+i)) + ".conf",
			EnabledPath:   "/opt/panel/enabled/" + string(rune('a'+i)) + ".conf",
			RewritePath:   "/opt/panel/rewrite/" + string(rune('a'+i)) + ".conf",
		})
	}

	// 分页查询第 1 页，每页 3 条
	sites, total, err := repo.List(1, 3, "", "")
	if err != nil {
		t.Fatalf("查询站点列表失败: %v", err)
	}
	if total != 5 {
		t.Errorf("总数期望 5，实际 %d", total)
	}
	if len(sites) != 3 {
		t.Errorf("第 1 页期望 3 条，实际 %d", len(sites))
	}
}

func TestSiteRepo_ListWithFilter(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSiteRepo(database)

	repo.Create(&Site{
		ID: "site_1", PrimaryDomain: "enabled.com", DomainsJSON: `["enabled.com"]`,
		Status: "enabled", HTTPPort: 80,
		RootPath: "/www/1", IndexFiles: "index.html",
		AccessLogPath: "/logs/1.a", ErrorLogPath: "/logs/1.e",
		ConfigPath: "/c/1", EnabledPath: "/e/1", RewritePath: "/r/1",
	})
	repo.Create(&Site{
		ID: "site_2", PrimaryDomain: "disabled.com", DomainsJSON: `["disabled.com"]`,
		Status: "disabled", HTTPPort: 80,
		RootPath: "/www/2", IndexFiles: "index.html",
		AccessLogPath: "/logs/2.a", ErrorLogPath: "/logs/2.e",
		ConfigPath: "/c/2", EnabledPath: "/e/2", RewritePath: "/r/2",
	})

	// 按状态筛选
	sites, total, _ := repo.List(1, 10, "", "enabled")
	if total != 1 {
		t.Errorf("enabled 站点期望 1，实际 %d", total)
	}
	if len(sites) != 1 || sites[0].PrimaryDomain != "enabled.com" {
		t.Error("状态筛选结果不正确")
	}

	// 按关键字筛选
	sites, total, _ = repo.List(1, 10, "disabled", "")
	if total != 1 {
		t.Errorf("关键字筛选期望 1，实际 %d", total)
	}
}

func TestSiteRepo_UpdateStatus(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSiteRepo(database)

	repo.Create(&Site{
		ID: "site_1", PrimaryDomain: "test.com", DomainsJSON: `["test.com"]`,
		Status: "enabled", HTTPPort: 80,
		RootPath: "/www/test", IndexFiles: "index.html",
		AccessLogPath: "/logs/test.a", ErrorLogPath: "/logs/test.e",
		ConfigPath: "/c/1", EnabledPath: "/e/1", RewritePath: "/r/1",
	})

	if err := repo.UpdateStatus("site_1", "disabled"); err != nil {
		t.Fatalf("更新状态失败: %v", err)
	}

	site, _ := repo.GetByID("site_1")
	if site.Status != "disabled" {
		t.Errorf("状态期望 disabled，实际 %s", site.Status)
	}
}

func TestSiteRepo_Delete(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSiteRepo(database)

	repo.Create(&Site{
		ID: "site_1", PrimaryDomain: "del.com", DomainsJSON: `["del.com"]`,
		Status: "enabled", HTTPPort: 80,
		RootPath: "/www/del", IndexFiles: "index.html",
		AccessLogPath: "/logs/del.a", ErrorLogPath: "/logs/del.e",
		ConfigPath: "/c/1", EnabledPath: "/e/1", RewritePath: "/r/1",
	})

	if err := repo.Delete("site_1"); err != nil {
		t.Fatalf("删除站点失败: %v", err)
	}

	site, _ := repo.GetByID("site_1")
	if site != nil {
		t.Error("删除后查询应返回 nil")
	}
}

func TestSiteRepo_DuplicateDomain(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSiteRepo(database)

	site := &Site{
		ID: "site_1", PrimaryDomain: "dup.com", DomainsJSON: `["dup.com"]`,
		Status: "enabled", HTTPPort: 80,
		RootPath: "/www/dup", IndexFiles: "index.html",
		AccessLogPath: "/logs/dup.a", ErrorLogPath: "/logs/dup.e",
		ConfigPath: "/c/1", EnabledPath: "/e/1", RewritePath: "/r/1",
	}
	repo.Create(site)

	// 重复域名应失败
	dup := *site
	dup.ID = "site_2"
	if err := repo.Create(&dup); err == nil {
		t.Error("重复域名创建应失败")
	}
}

// ============================================================
// Session Repository 测试
// ============================================================

func TestSessionRepo_CreateAndGet(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSessionRepo(database)

	now := time.Now().UTC().Format(time.RFC3339)
	session := &Session{
		ID:            "sess_abc123",
		CSRFTokenHash: "hash_of_csrf_token",
		UserAgent:     "Mozilla/5.0",
		IP:            "127.0.0.1",
		ExpiresAt:     now,
		CreatedAt:     now,
		LastSeenAt:    now,
	}

	if err := repo.Create(session); err != nil {
		t.Fatalf("创建 session 失败: %v", err)
	}

	found, err := repo.GetByID("sess_abc123")
	if err != nil {
		t.Fatalf("查询 session 失败: %v", err)
	}
	if found == nil {
		t.Fatal("session 不应为 nil")
	}
	if found.CSRFTokenHash != "hash_of_csrf_token" {
		t.Error("CSRF token hash 不匹配")
	}
}

func TestSessionRepo_Delete(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewSessionRepo(database)
	now := time.Now().UTC().Format(time.RFC3339)

	repo.Create(&Session{
		ID: "sess_del", CSRFTokenHash: "hash",
		ExpiresAt: now, CreatedAt: now, LastSeenAt: now,
	})

	if err := repo.Delete("sess_del"); err != nil {
		t.Fatalf("删除 session 失败: %v", err)
	}

	found, _ := repo.GetByID("sess_del")
	if found != nil {
		t.Error("删除后查询应返回 nil")
	}
}

// ============================================================
// Operation Repository 测试
// ============================================================

func TestOperationRepo_CreateAndGet(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewOperationRepo(database)
	now := time.Now().UTC().Format(time.RFC3339)

	op := &Operation{
		ID:         "op_test1",
		Action:     "site.create",
		TargetType: "site",
		TargetID:   "site_1",
		Status:     "success",
		RequestID:  "req_123",
		Actor:      "admin",
		IP:         "127.0.0.1",
		Message:    "创建站点成功",
		CreatedAt:  now,
	}

	if err := repo.Create(op); err != nil {
		t.Fatalf("创建操作记录失败: %v", err)
	}

	found, err := repo.GetByID("op_test1")
	if err != nil {
		t.Fatalf("查询操作记录失败: %v", err)
	}
	if found == nil {
		t.Fatal("操作记录不应为 nil")
	}
	if found.Action != "site.create" {
		t.Errorf("action 期望 site.create，实际 %s", found.Action)
	}
	if found.Status != "success" {
		t.Errorf("status 期望 success，实际 %s", found.Status)
	}
}

func TestOperationRepo_UpdateStatus(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewOperationRepo(database)

	repo.Create(&Operation{
		ID: "op_1", Action: "nginx.test", TargetType: "nginx",
		Status: "pending", Actor: "admin", CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	if err := repo.UpdateStatus("op_1", "success"); err != nil {
		t.Fatalf("更新操作状态失败: %v", err)
	}

	op, _ := repo.GetByID("op_1")
	if op.Status != "success" {
		t.Errorf("状态期望 success，实际 %s", op.Status)
	}
}

func TestOperationRepo_UpdateError(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewOperationRepo(database)

	repo.Create(&Operation{
		ID: "op_2", Action: "nginx.test", TargetType: "nginx",
		Status: "pending", Actor: "admin", CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	repo.UpdateError("op_2", "failed", "NGINX_TEST_FAILED", "配置测试失败",
		"nginx: [emerg] duplicate location")

	op, _ := repo.GetByID("op_2")
	if op.Status != "failed" {
		t.Errorf("状态期望 failed，实际 %s", op.Status)
	}
	if op.ErrorCode != "NGINX_TEST_FAILED" {
		t.Errorf("错误码期望 NGINX_TEST_FAILED，实际 %s", op.ErrorCode)
	}
	if op.Stderr != "nginx: [emerg] duplicate location" {
		t.Errorf("stderr 不正确: %s", op.Stderr)
	}
}

func TestOperationRepo_List(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	repo := NewOperationRepo(database)
	now := time.Now().UTC().Format(time.RFC3339)

	for i := 0; i < 5; i++ {
		repo.Create(&Operation{
			ID:     "op_" + string(rune('a'+i)),
			Action: "site.create", TargetType: "site",
			TargetID: "site_1", Status: "success",
			Actor: "admin", CreatedAt: now,
		})
	}

	ops, total, err := repo.List(1, 3, "", "")
	if err != nil {
		t.Fatalf("查询操作列表失败: %v", err)
	}
	if total != 5 {
		t.Errorf("总数期望 5，实际 %d", total)
	}
	if len(ops) != 3 {
		t.Errorf("第 1 页期望 3 条，实际 %d", len(ops))
	}
}

// ============================================================
// Backup Repository 测试
// ============================================================

func TestBackupRepo_CreateAndList(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	opRepo := NewOperationRepo(database)
	backupRepo := NewBackupRepo(database)
	now := time.Now().UTC().Format(time.RFC3339)

	// 先创建一个 operation（backups 有外键依赖）
	opRepo.Create(&Operation{
		ID: "op_bak", Action: "site.update", TargetType: "site",
		TargetID: "site_1", Status: "success", Actor: "admin", CreatedAt: now,
	})

	backup := &Backup{
		ID:             "bak_1",
		OperationID:    "op_bak",
		FilePath:       "/opt/panel/nginx/sites-available/test.conf",
		BackupPath:     "/opt/panel/nginx/backups/op_bak/test.conf",
		OriginalSHA256: "abc123",
		BackupSHA256:   "def456",
		FileExisted:    true,
	}

	if err := backupRepo.Create(backup); err != nil {
		t.Fatalf("创建备份记录失败: %v", err)
	}

	backups, err := backupRepo.ListByOperationID("op_bak")
	if err != nil {
		t.Fatalf("查询备份列表失败: %v", err)
	}
	if len(backups) != 1 {
		t.Errorf("期望 1 条备份记录，实际 %d", len(backups))
	}
	if backups[0].FilePath != "/opt/panel/nginx/sites-available/test.conf" {
		t.Errorf("FilePath 不匹配: %s", backups[0].FilePath)
	}
}

// ============================================================
// Foreign Key Cascade 测试
// ============================================================

func TestSiteDeleteCascades(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	siteRepo := NewSiteRepo(database)
	proxyRepo := NewProxyRepo(database)

	// 创建站点
	siteRepo.Create(&Site{
		ID: "site_cascade", PrimaryDomain: "cascade.com", DomainsJSON: `["cascade.com"]`,
		Status: "enabled", HTTPPort: 80,
		RootPath: "/www/cascade", IndexFiles: "index.html",
		AccessLogPath: "/logs/c.a", ErrorLogPath: "/logs/c.e",
		ConfigPath: "/c/c", EnabledPath: "/e/c", RewritePath: "/r/c",
	})

	// 创建代理配置
	proxyRepo.Create(&SiteProxy{
		ID:          "proxy_cascade_1",
		SiteID:      "site_cascade",
		Name:        "默认代理",
		Enabled:     true,
		UpstreamURL: "http://127.0.0.1:3000",
	})

	// 删除站点
	siteRepo.Delete("site_cascade")

	// 代理配置应被级联删除
	proxy, _ := proxyRepo.GetBySiteID("site_cascade")
	if proxy != nil {
		t.Error("站点删除后代理配置应被级联删除")
	}
}
