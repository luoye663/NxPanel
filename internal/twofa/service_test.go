package twofa

import (
	"errors"
	"fmt"
	"io"
	"strings"
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

	token := mustCreateTempToken(t, store, 1, "admin", "203.0.113.10:1234", "ua-a")

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

func TestTempTokenStoreNormalizesLongContextConsistently(t *testing.T) {
	store := NewTempTokenStore(5 * time.Minute)
	t.Cleanup(store.Stop)
	longUA := strings.Repeat("a", auth.SessionUserAgentMaxBytes+80)
	longIP := strings.Repeat("1", auth.SessionIPMaxBytes+20)
	token := mustCreateTempToken(t, store, 1, "admin", longIP, longUA)
	entry, ok := store.ValidateContext(token, longIP, longUA)
	if !ok {
		t.Fatal("相同长上下文应在归一化后通过")
	}
	if len(entry.UserAgent) > auth.SessionUserAgentMaxBytes || entry.UserAgent != auth.NormalizeSessionUserAgent(longUA) || len(entry.IP) != auth.SessionIPMaxBytes {
		t.Fatalf("临时令牌上下文未受限: ua=%d ip=%d", len(entry.UserAgent), len(entry.IP))
	}
	if _, ok := store.ValidateContext(token, longIP, "b"+longUA[1:]); ok {
		t.Fatal("有效前缀变化后不应通过临时令牌校验")
	}
	if _, ok := store.ValidateContext(token, longIP, longUA[:len(longUA)-1]+"b"); ok {
		t.Fatal("哈希覆盖范围内的后缀变化后不应通过临时令牌校验")
	}
}

func TestTempTokenStoreFailureLimit(t *testing.T) {
	store := NewTempTokenStore(5 * time.Minute)
	t.Cleanup(store.Stop)

	token := mustCreateTempToken(t, store, 1, "admin", "203.0.113.10:1234", "ua-a")
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
	expiredToken := mustCreateTempToken(t, expiredStore, 1, "admin", "203.0.113.10:1234", "ua-a")
	if _, ok := expiredStore.ValidateContext(expiredToken, "203.0.113.10:1234", "ua-a"); ok {
		t.Fatal("过期临时令牌不应通过校验")
	}

	store := NewTempTokenStore(5 * time.Minute)
	t.Cleanup(store.Stop)
	token := mustCreateTempToken(t, store, 1, "admin", "203.0.113.10:1234", "ua-a")
	if _, ok := store.Consume(token, "203.0.113.10:1234", "ua-a"); !ok {
		t.Fatal("首次消费临时令牌应成功")
	}
	if _, ok := store.Consume(token, "203.0.113.10:1234", "ua-a"); ok {
		t.Fatal("临时令牌消费后不应重复使用")
	}
}

func TestTempTokenStoreCapacityAndExpiredAdmission(t *testing.T) {
	store := NewTempTokenStore(5*time.Minute, 1, 2)
	t.Cleanup(store.Stop)
	_ = mustCreateTempToken(t, store, 1, "admin", "ip-a", "ua")
	if _, err := store.Create(1, "admin", "ip-b", "ua"); err == nil {
		t.Fatal("同一账号超过临时令牌上限应失败")
	}
	_ = mustCreateTempToken(t, store, 2, "other", "ip-b", "ua")
	if _, err := store.Create(3, "third", "ip-c", "ua"); err == nil {
		t.Fatal("超过全局临时令牌上限应失败")
	}

	expiring := NewTempTokenStore(-time.Millisecond, 1, 1)
	t.Cleanup(expiring.Stop)
	_ = mustCreateTempToken(t, expiring, 1, "admin", "ip-a", "ua")
	if _, err := expiring.Create(2, "other", "ip-b", "ua"); err != nil {
		t.Fatalf("准入前应清理过期令牌: %v", err)
	}
}

func TestTempTokenStoreConcurrentCapacity(t *testing.T) {
	store := NewTempTokenStore(5*time.Minute, 5, 5)
	t.Cleanup(store.Stop)
	var wg sync.WaitGroup
	results := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.Create(1, "admin", fmt.Sprintf("ip-%d", i), "ua")
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 5 {
		t.Fatalf("并发准入应严格限制为 5 个，实际 %d", successes)
	}
}

func TestTempTokenStoreConcurrentReload(t *testing.T) {
	store := NewTempTokenStore(5*time.Minute, 1000, 1000)
	t.Cleanup(store.Stop)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				token, err := store.Create(i, fmt.Sprintf("user-%d", i), fmt.Sprintf("ip-%d", i), "ua")
				if err == nil {
					store.Consume(token, fmt.Sprintf("ip-%d", i), "ua")
				}
			}
		}(i)
	}
	for i := 0; i < 200; i++ {
		store.ReloadLimits(500+i%2, 1000)
	}
	wg.Wait()
}

func TestTempTokenStoreRandomFailureReturnsError(t *testing.T) {
	store := NewTempTokenStore(5 * time.Minute)
	t.Cleanup(store.Stop)
	store.randReader = errorReader{}
	if _, err := store.Create(1, "admin", "ip", "ua"); err == nil {
		t.Fatal("随机源失败应返回错误")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func mustCreateTempToken(t *testing.T, store *TempTokenStore, adminID int, username, ip, ua string) string {
	t.Helper()
	token, err := store.Create(adminID, username, ip, ua)
	if err != nil {
		t.Fatalf("创建临时令牌失败: %v", err)
	}
	return token
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
