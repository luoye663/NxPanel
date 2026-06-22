// auth 包 — 认证服务
//
// AuthService 整合密码管理、Session 管理和 CSRF 管理，
// 为 API handler 和中间件提供统一的认证接口。
//
// 使用方式：
//   - handler 调用 AuthService.SetupAdmin / Login / Logout
//   - 中间件调用 AuthService.ValidateSession / ValidateCSRF
package auth

import (
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/luoye663/nxpanel/internal/db/repo"
	"golang.org/x/crypto/bcrypt"
)

var (
	reLowercase = regexp.MustCompile(`[a-z]`)
	reUppercase = regexp.MustCompile(`[A-Z]`)
	reDigit     = regexp.MustCompile(`[0-9]`)
	reSpecial   = regexp.MustCompile(`[^a-zA-Z0-9]`)
)

// AuthService 认证服务，整合密码、Session、CSRF 功能
type AuthService struct {
	db         *sql.DB
	adminRepo  *repo.AdminRepo
	sessionSvc *SessionService
}

// NewAuthService 创建认证服务
func NewAuthService(db *sql.DB, sessionDuration time.Duration, maxSessions int, bindIP, bindUA bool) *AuthService {
	adminRepo := repo.NewAdminRepo(db)
	return &AuthService{
		db:         db,
		adminRepo:  adminRepo,
		sessionSvc: NewSessionService(
			repo.NewSessionRepo(db),
			sessionDuration,
			WithMaxSessions(maxSessions),
			WithBindSessionIP(bindIP),
			WithBindSessionUA(bindUA),
			WithAdminRepo(adminRepo),
		),
	}
}

// GetSessionDuration 返回当前会话有效期
func (s *AuthService) GetSessionDuration() time.Duration {
	return s.sessionSvc.GetSessionDuration()
}

// ReloadSecurityConfig 热重载会话安全配置
func (s *AuthService) ReloadSecurityConfig(maxSessions int, bindIP, bindUA bool) {
	s.sessionSvc.ReloadSecurityConfig(maxSessions, bindIP, bindUA)
}

// AdminExists 检查管理员是否已初始化
// 用于 setup/admin handler 判断是否允许初始化
func (s *AuthService) AdminExists() (bool, error) {
	return s.adminRepo.Exists()
}

// SetupAdmin 初始化管理员账户（只能执行一次）
//
// 参数：
//   - username: 管理员用户名
//   - password: 明文密码
//
// 返回：
//   - 错误信息（管理员已存在时返回 ErrAdminAlreadyExists）
func (s *AuthService) SetupAdmin(username, password string) error {
	// 检查是否已存在管理员
	exists, err := s.adminRepo.Exists()
	if err != nil {
		return fmt.Errorf("检查管理员状态失败: %w", err)
	}
	if exists {
		return ErrAdminAlreadyExists
	}

	// 验证参数
	if err := validateSetupParams(username, password); err != nil {
		return err
	}

	// 对密码进行哈希
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("密码哈希失败: %w", err)
	}

	// 写入数据库
	if err := s.adminRepo.Create(username, hash, PasswordAlgo); err != nil {
		return fmt.Errorf("创建管理员失败: %w", err)
	}

	return nil
}

// dummyBcryptHash 用于用户不存在时执行等价耗时的 bcrypt 比较，防止时序攻击
// 这是一个合法的 bcrypt hash（对应明文 "dummy-password-not-used"）
const dummyBcryptHash = "$2a$12$ViW5mCQkVg8Y6xH2KFJBnOKbWITgQ3wL5GQeQnQ5f7s1rKPTEQBWG"

// LoginResult 登录成功后的返回结果
type LoginResult struct {
	SessionID   string
	CSRFToken   string
	Username    string
	TOTPEnabled bool
	AdminID     int
}

func (s *AuthService) Login(username, password, userAgent, ip string) (*LoginResult, error) {
	admin, err := s.adminRepo.GetByUsername(username)
	if err != nil {
		return nil, fmt.Errorf("查询管理员失败: %w", err)
	}
	if admin == nil {
		bcrypt.CompareHashAndPassword([]byte(dummyBcryptHash), []byte(password))
		return nil, ErrInvalidCredentials
	}

	if !VerifyPassword(password, admin.PasswordHash) {
		return nil, ErrInvalidCredentials
	}

	if admin.TOTPEnabled {
		return &LoginResult{
			Username:    admin.Username,
			TOTPEnabled: true,
			AdminID:     admin.ID,
		}, nil
	}

	sessionID, csrfToken, err := s.sessionSvc.CreateSession(userAgent, ip)
	if err != nil {
		return nil, fmt.Errorf("创建会话失败: %w", err)
	}

	return &LoginResult{
		SessionID:   sessionID,
		CSRFToken:   csrfToken,
		Username:    admin.Username,
		TOTPEnabled: false,
		AdminID:     admin.ID,
	}, nil
}

// ValidateSession 验证会话有效性（供 auth 中间件使用）
func (s *AuthService) ValidateSession(sessionID, currentIP, currentUA string) (*repo.Session, error) {
	return s.sessionSvc.ValidateSession(sessionID, currentIP, currentUA)
}

// ValidateCSRF 验证 CSRF Token（供 CSRF 中间件使用）
func (s *AuthService) ValidateCSRF(token string, session *repo.Session) bool {
	return ValidateCSRFToken(token, session.CSRFTokenHash)
}

// Logout 登出（销毁会话）
func (s *AuthService) Logout(sessionID string) error {
	return s.sessionSvc.DestroySession(sessionID)
}

// GetAdminInfo 获取管理员信息（用于 /auth/me 接口）
func (s *AuthService) GetAdminInfo() (*repo.Admin, error) {
	return s.adminRepo.Get()
}

// ChangePassword 修改管理员密码，成功后清除所有会话
func (s *AuthService) ChangePassword(currentPassword, newPassword string) error {
	if currentPassword == "" {
		return ErrPasswordRequired
	}
	if newPassword == "" {
		return ErrPasswordRequired
	}

	admin, err := s.adminRepo.Get()
	if err != nil {
		return fmt.Errorf("查询管理员失败: %w", err)
	}
	if admin == nil {
		return ErrInvalidCredentials
	}

	if !VerifyPassword(currentPassword, admin.PasswordHash) {
		return ErrInvalidCredentials
	}

	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("密码哈希失败: %w", err)
	}

	if err := s.adminRepo.UpdatePassword(hash, PasswordAlgo); err != nil {
		return fmt.Errorf("更新密码失败: %w", err)
	}

	if err := s.sessionSvc.DestroyAllSessions(); err != nil {
		slog.Error("密码修改后清除会话失败", "error", err)
	}

	return nil
}

func (s *AuthService) CreateSession(userAgent, ip string) (string, string, error) {
	return s.sessionSvc.CreateSession(userAgent, ip)
}

func (s *AuthService) CleanupExpiredSessions() (int64, error) {
	return s.sessionSvc.CleanupExpired()
}

// ============================================================
// 参数验证
// ============================================================

// validateSetupParams 验证 setup/admin 请求的参数
func validateSetupParams(username, password string) error {
	if username == "" {
		return ErrUsernameRequired
	}
	if len(username) > 50 {
		return ErrUsernameTooLong
	}
	if password == "" {
		return ErrPasswordRequired
	}
	if err := validatePasswordStrength(password); err != nil {
		return err
	}
	return nil
}

// validatePasswordStrength 校验密码强度
// 要求至少 8 位，不超过 72 字节（bcrypt 限制），且包含至少 3 种字符类别
func validatePasswordStrength(password string) error {
	if len(password) < 8 {
		return ErrPasswordTooShort
	}
	if len(password) > 72 {
		return ErrPasswordTooLong
	}
	categories := 0
	if reLowercase.MatchString(password) {
		categories++
	}
	if reUppercase.MatchString(password) {
		categories++
	}
	if reDigit.MatchString(password) {
		categories++
	}
	if reSpecial.MatchString(password) {
		categories++
	}
	if categories < 3 {
		return ErrPasswordTooWeak
	}
	return nil
}
