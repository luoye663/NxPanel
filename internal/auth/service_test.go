package auth

import (
	"database/sql"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("测试迁移失败: %v", err)
	}
	return database
}

func TestSessionCreateAndValidate(t *testing.T) {
	database := newTestDB(t)
	svc := NewSessionService(repo.NewSessionRepo(database), 0)

	sessionID, csrfToken, err := svc.CreateSession("test-agent", "127.0.0.1")
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}

	if sessionID == "" {
		t.Error("session ID 不应为空")
	}
	if csrfToken == "" {
		t.Error("CSRF token 不应为空")
	}
	if len(sessionID) != 64 {
		t.Errorf("session ID 长度期望 64，实际 %d", len(sessionID))
	}
	if len(csrfToken) != 64 {
		t.Errorf("CSRF token 长度期望 64，实际 %d", len(csrfToken))
	}

	session, err := svc.ValidateSession(sessionID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("验证会话失败: %v", err)
	}
	if session == nil {
		t.Fatal("有效会话不应返回 nil")
	}
	if session.ID != sessionID {
		t.Error("返回的 session ID 应匹配")
	}

	if !ValidateCSRFToken(csrfToken, session.CSRFTokenHash) {
		t.Error("CSRF token 应验证通过")
	}
}

func TestSessionValidateInvalid(t *testing.T) {
	database := newTestDB(t)
	svc := NewSessionService(repo.NewSessionRepo(database), 0)

	session, err := svc.ValidateSession("nonexistent-session-id", "", "")
	if err != nil {
		t.Fatalf("验证不应返回错误: %v", err)
	}
	if session != nil {
		t.Error("不存在的 session 应返回 nil")
	}
}

func TestSessionDestroy(t *testing.T) {
	database := newTestDB(t)
	svc := NewSessionService(repo.NewSessionRepo(database), 0)

	sessionID, _, err := svc.CreateSession("test-agent", "127.0.0.1")
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}

	if err := svc.DestroySession(sessionID); err != nil {
		t.Fatalf("销毁会话失败: %v", err)
	}

	session, err := svc.ValidateSession(sessionID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("验证不应返回错误: %v", err)
	}
	if session != nil {
		t.Error("已销毁的会话应返回 nil")
	}
}

func TestAuthServiceSetup(t *testing.T) {
	database := newTestDB(t)
	svc := NewAuthService(database, 0, 0, false, false)

	exists, err := svc.AdminExists()
	if err != nil {
		t.Fatalf("AdminExists 失败: %v", err)
	}
	if exists {
		t.Error("初始状态管理员不应存在")
	}

	if err := svc.SetupAdmin("admin", "Test-password-123"); err != nil {
		t.Fatalf("SetupAdmin 失败: %v", err)
	}

	exists, err = svc.AdminExists()
	if err != nil {
		t.Fatalf("AdminExists 失败: %v", err)
	}
	if !exists {
		t.Error("初始化后管理员应存在")
	}

	err = svc.SetupAdmin("admin2", "Another-password-456")
	if err == nil {
		t.Fatal("重复初始化应返回错误")
	}
	if err != ErrAdminAlreadyExists {
		t.Errorf("期望 ErrAdminAlreadyExists，实际 %v", err)
	}
}

func TestAuthServiceLogin(t *testing.T) {
	database := newTestDB(t)
	svc := NewAuthService(database, 0, 0, false, false)

	if err := svc.SetupAdmin("admin", "Test-password-123"); err != nil {
		t.Fatalf("SetupAdmin 失败: %v", err)
	}

	result, err := svc.Login("admin", "Test-password-123", "test-agent", "127.0.0.1")
	if err != nil {
		t.Fatalf("Login 失败: %v", err)
	}
	if result.Username != "admin" {
		t.Errorf("username 期望 admin，实际 %s", result.Username)
	}
	if result.SessionID == "" {
		t.Error("session ID 不应为空")
	}
	if result.CSRFToken == "" {
		t.Error("CSRF token 不应为空")
	}

	_, err = svc.Login("admin", "wrong-password", "test-agent", "127.0.0.1")
	if err != ErrInvalidCredentials {
		t.Errorf("错误密码应返回 ErrInvalidCredentials，实际 %v", err)
	}

	_, err = svc.Login("nonexistent", "Test-password-123", "test-agent", "127.0.0.1")
	if err != ErrInvalidCredentials {
		t.Errorf("不存在的用户应返回 ErrInvalidCredentials，实际 %v", err)
	}
}

func TestAuthLogout(t *testing.T) {
	database := newTestDB(t)
	svc := NewAuthService(database, 0, 0, false, false)

	if err := svc.SetupAdmin("admin", "Test-password-123"); err != nil {
		t.Fatalf("SetupAdmin 失败: %v", err)
	}

	result, err := svc.Login("admin", "Test-password-123", "test-agent", "127.0.0.1")
	if err != nil {
		t.Fatalf("Login 失败: %v", err)
	}

	if err := svc.Logout(result.SessionID); err != nil {
		t.Fatalf("Logout 失败: %v", err)
	}

	session, err := svc.ValidateSession(result.SessionID, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("ValidateSession 失败: %v", err)
	}
	if session != nil {
		t.Error("登出后会话应无效")
	}
}

func TestSetupValidation(t *testing.T) {
	database := newTestDB(t)
	svc := NewAuthService(database, 0, 0, false, false)

	tests := []struct {
		name     string
		username string
		password string
		wantErr  error
	}{
		{"空用户名", "", "Password-123", ErrUsernameRequired},
		{"用户名过长", "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz", "Password-123", ErrUsernameTooLong},
		{"空密码", "admin", "", ErrPasswordRequired},
		{"密码太短", "admin", "1234567", ErrPasswordTooShort},
		{"密码太长", "admin", string(make([]byte, 73)), ErrPasswordTooLong},
		{"密码太弱", "admin", "password", ErrPasswordTooWeak},
		{"合法参数", "admin", "Test-password-123", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.SetupAdmin(tt.username, tt.password)
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Errorf("期望 %v，实际 %v", tt.wantErr, err)
				}
			} else {
				if err != nil {
					t.Errorf("不期望错误，实际 %v", err)
				}
			}
		})
	}
}

func TestCleanupExpired(t *testing.T) {
	database := newTestDB(t)
	sessionRepo := repo.NewSessionRepo(database)
	svc := NewSessionService(sessionRepo, 0)

	expiredTime := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	now := time.Now().UTC().Format(time.RFC3339)
	expiredSession := &repo.Session{
		ID:            "expired-session-id-00000000000000000000000000000000",
		CSRFTokenHash: "some-hash",
		UserAgent:     "test",
		IP:            "127.0.0.1",
		ExpiresAt:     expiredTime,
		CreatedAt:     now,
		LastSeenAt:    now,
	}
	if err := sessionRepo.Create(expiredSession); err != nil {
		t.Fatalf("创建过期会话失败: %v", err)
	}

	deleted, err := svc.CleanupExpired()
	if err != nil {
		t.Fatalf("CleanupExpired 失败: %v", err)
	}
	if deleted != 1 {
		t.Errorf("期望删除 1 条，实际 %d", deleted)
	}

	session, err := sessionRepo.GetByID("expired-session-id-00000000000000000000000000000000")
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if session != nil {
		t.Error("过期会话应已被删除")
	}
}

func TestSessionIPBinding(t *testing.T) {
	database := newTestDB(t)
	svc := NewSessionService(repo.NewSessionRepo(database), time.Hour, WithBindSessionIP(true))

	sessionID, _, err := svc.CreateSession("test-agent", "192.168.1.1")
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}

	session, err := svc.ValidateSession(sessionID, "192.168.1.1", "test-agent")
	if err != nil {
		t.Fatalf("相同 IP 验证应成功: %v", err)
	}
	if session == nil {
		t.Fatal("相同 IP 应返回有效会话")
	}

	session, err = svc.ValidateSession(sessionID, "10.0.0.1", "test-agent")
	if err != ErrSessionMismatch {
		t.Errorf("不同 IP 应返回 ErrSessionMismatch，实际 err=%v session=%v", err, session)
	}
}

func TestSessionUABinding(t *testing.T) {
	database := newTestDB(t)
	svc := NewSessionService(repo.NewSessionRepo(database), time.Hour, WithBindSessionUA(true))

	sessionID, _, err := svc.CreateSession("chrome-100", "127.0.0.1")
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}

	session, err := svc.ValidateSession(sessionID, "127.0.0.1", "chrome-100")
	if err != nil {
		t.Fatalf("相同 UA 验证应成功: %v", err)
	}
	if session == nil {
		t.Fatal("相同 UA 应返回有效会话")
	}

	session, err = svc.ValidateSession(sessionID, "127.0.0.1", "firefox-99")
	if err != ErrSessionMismatch {
		t.Errorf("不同 UA 应返回 ErrSessionMismatch，实际 err=%v session=%v", err, session)
	}
}
