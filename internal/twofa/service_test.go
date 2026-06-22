package twofa

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/luoye663/nxpanel/internal/auth"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

func TestTempTokenStoreContextBinding(t *testing.T) {
	store := NewTempTokenStore(5 * time.Minute)
	t.Cleanup(store.Stop)

	token := store.Create(1, "admin", "203.0.113.10:1234", "ua-a")

	if _, ok := store.ValidateContext(token, "203.0.113.10:1234", "ua-b"); ok {
		t.Fatal("不同 User-Agent 不应通过临时令牌校验")
	}
	if _, ok := store.ValidateContext(token, "203.0.113.11:1234", "ua-a"); ok {
		t.Fatal("不同 IP 不应通过临时令牌校验")
	}
	entry, ok := store.ValidateContext(token, "203.0.113.10:1234", "ua-a")
	if !ok {
		t.Fatal("相同登录上下文应通过临时令牌校验")
	}
	if entry.AdminID != 1 || entry.Username != "admin" {
		t.Fatalf("临时令牌信息不正确: %+v", entry)
	}
}

func TestTempTokenStoreFailureLimit(t *testing.T) {
	store := NewTempTokenStore(5 * time.Minute)
	t.Cleanup(store.Stop)

	token := store.Create(1, "admin", "203.0.113.10:1234", "ua-a")
	for i := 0; i < 5; i++ {
		store.RecordFailure(token)
	}

	if _, ok := store.ValidateContext(token, "203.0.113.10:1234", "ua-a"); ok {
		t.Fatal("失败次数达到上限后临时令牌应失效")
	}
}

func TestTempTokenStoreExpiredAndConsumed(t *testing.T) {
	expiredStore := NewTempTokenStore(-time.Millisecond)
	t.Cleanup(expiredStore.Stop)
	expiredToken := expiredStore.Create(1, "admin", "203.0.113.10:1234", "ua-a")
	if _, ok := expiredStore.ValidateContext(expiredToken, "203.0.113.10:1234", "ua-a"); ok {
		t.Fatal("过期临时令牌不应通过校验")
	}

	store := NewTempTokenStore(5 * time.Minute)
	t.Cleanup(store.Stop)
	token := store.Create(1, "admin", "203.0.113.10:1234", "ua-a")
	if _, ok := store.Consume(token, "203.0.113.10:1234", "ua-a"); !ok {
		t.Fatal("首次消费临时令牌应成功")
	}
	if _, ok := store.Consume(token, "203.0.113.10:1234", "ua-a"); ok {
		t.Fatal("临时令牌消费后不应重复使用")
	}
}

func TestVerifyAndConsumeCodeRejectsReplay(t *testing.T) {
	service, admin := setupTOTPService(t)
	code := currentTOTPCode(t, admin.TOTPSecret)

	if err := service.VerifyAndConsumeCode(admin, code); err != nil {
		t.Fatalf("首次消费 TOTP 应成功: %v", err)
	}
	if err := service.VerifyAndConsumeCode(admin, code); !errors.Is(err, auth.ErrTOTPCodeReplayed) {
		t.Fatalf("重复消费 TOTP 应返回重放错误，实际: %v", err)
	}
}

func TestVerifyAndConsumeCodeConcurrentReplay(t *testing.T) {
	service, admin := setupTOTPService(t)
	code := currentTOTPCode(t, admin.TOTPSecret)

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- service.VerifyAndConsumeCode(admin, code)
		}()
	}
	wg.Wait()
	close(errs)

	successes := 0
	replays := 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		if errors.Is(err, auth.ErrTOTPCodeReplayed) {
			replays++
			continue
		}
		t.Fatalf("并发消费返回非预期错误: %v", err)
	}
	if successes != 1 {
		t.Fatalf("并发消费同一 TOTP 应最多且仅有一次成功，实际成功 %d 次", successes)
	}
	if replays != 15 {
		t.Fatalf("其余并发请求应返回重放错误，实际 %d 次", replays)
	}
}

func setupTOTPService(t *testing.T) (*Service, *repo.Admin) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	// 内存 SQLite 每个连接是独立数据库；限制单连接避免并发测试拿到空库。
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("运行测试迁移失败: %v", err)
	}

	adminRepo := repo.NewAdminRepo(database)
	if err := adminRepo.Create("admin", "hash", "bcrypt"); err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}
	if err := adminRepo.UpdateTOTP("JBSWY3DPEHPK3PXP", true, "[]"); err != nil {
		t.Fatalf("启用测试 TOTP 失败: %v", err)
	}
	admin, err := adminRepo.Get()
	if err != nil {
		t.Fatalf("查询管理员失败: %v", err)
	}
	service := NewService(adminRepo)
	t.Cleanup(service.Stop)
	return service, admin
}

func currentTOTPCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("生成测试 TOTP 失败: %v", err)
	}
	return code
}
