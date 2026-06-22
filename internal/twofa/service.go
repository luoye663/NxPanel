package twofa

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/luoye663/nxpanel/internal/auth"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

type Service struct {
	adminRepo      *repo.AdminRepo
	tempStore      *TempTokenStore
	pendingSecrets sync.Map
	pendingCancel  context.CancelFunc
}

func NewService(adminRepo *repo.AdminRepo) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		adminRepo:     adminRepo,
		tempStore:     NewTempTokenStore(5 * time.Minute),
		pendingCancel: cancel,
	}
	go s.cleanupPending(ctx)
	return s
}

func (s *Service) GetTempStore() *TempTokenStore {
	return s.tempStore
}

func (s *Service) Stop() {
	s.tempStore.Stop()
	if s.pendingCancel != nil {
		s.pendingCancel()
	}
}

func (s *Service) cleanupPending(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UTC()
			s.pendingSecrets.Range(func(key, value any) bool {
				ps := value.(pendingSecret)
				if now.After(ps.expiresAt) {
					s.pendingSecrets.Delete(key)
				}
				return true
			})
		}
	}
}

func (s *Service) StorePendingSecret(sessionID, secret string) {
	s.pendingSecrets.Store(sessionID, pendingSecret{
		secret:    secret,
		expiresAt: time.Now().UTC().Add(5 * time.Minute),
	})
}

func (s *Service) PopPendingSecret(sessionID string) (string, bool) {
	val, ok := s.pendingSecrets.LoadAndDelete(sessionID)
	if !ok {
		return "", false
	}
	ps := val.(pendingSecret)
	if time.Now().UTC().After(ps.expiresAt) {
		return "", false
	}
	return ps.secret, true
}

type pendingSecret struct {
	secret    string
	expiresAt time.Time
}

func (s *Service) GenerateSecret(username string) (secret string, url string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "NxPanel",
		AccountName: username,
		Period:      30,
		Digits:      6,
		Algorithm:   0,
	})
	if err != nil {
		return "", "", fmt.Errorf("生成 TOTP 密钥失败: %w", err)
	}
	return key.Secret(), key.URL(), nil
}

func (s *Service) Enable(adminID int, secret, code string) ([]string, error) {
	if !totp.Validate(code, secret) {
		return nil, fmt.Errorf("验证码错误")
	}
	recoveryCodes, err := generateRecoveryCodes(8)
	if err != nil {
		return nil, fmt.Errorf("生成恢复码失败: %w", err)
	}
	hashedCodes := hashRecoveryCodes(recoveryCodes)
	codesJSON, _ := json.Marshal(hashedCodes)
	if err := s.adminRepo.UpdateTOTP(secret, true, string(codesJSON)); err != nil {
		return nil, err
	}
	return recoveryCodes, nil
}

func (s *Service) Disable(adminID int) error {
	return s.adminRepo.UpdateTOTP("", false, "[]")
}

func (s *Service) VerifyCode(secret, code string) bool {
	if secret == "" || code == "" {
		return false
	}
	if !totp.Validate(code, secret) {
		return false
	}
	return true
}

func (s *Service) VerifyAndConsumeCode(admin *repo.Admin, code string) error {
	if admin.TOTPSecret == "" || code == "" {
		return auth.ErrInvalidTOTP
	}
	if !totp.Validate(code, admin.TOTPSecret) {
		return auth.ErrInvalidTOTP
	}

	// TOTP 数学校验通过后，必须交给数据库做原子消费。
	// 不能再用内存中的 LastTOTPCode 先判断再无条件 UPDATE，否则并发请求可能同时通过。
	ok, err := s.adminRepo.ConsumeTOTPCode(admin.ID, code, time.Now().UTC(), 90*time.Second)
	if err != nil {
		return fmt.Errorf("消费 TOTP 验证码失败: %w", err)
	}
	if !ok {
		return auth.ErrTOTPCodeReplayed
	}
	return nil
}

func (s *Service) VerifyRecoveryCode(adminID int, code string) (bool, error) {
	codeHash := sha256Hex(code)
	return s.adminRepo.ConsumeRecoveryCode(codeHash)
}

func (s *Service) RegenerateRecoveryCodes(adminID int) ([]string, error) {
	recoveryCodes, err := generateRecoveryCodes(8)
	if err != nil {
		return nil, fmt.Errorf("生成恢复码失败: %w", err)
	}
	hashedCodes := hashRecoveryCodes(recoveryCodes)
	codesJSON, _ := json.Marshal(hashedCodes)
	if err := s.adminRepo.UpdateRecoveryCodes(string(codesJSON)); err != nil {
		return nil, err
	}
	return recoveryCodes, nil
}

func generateRecoveryCodes(count int) ([]string, error) {
	codes := make([]string, count)
	for i := 0; i < count; i++ {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		codes[i] = hex.EncodeToString(b)
	}
	return codes, nil
}

func hashRecoveryCodes(codes []string) []string {
	hashed := make([]string, len(codes))
	for i, c := range codes {
		hashed[i] = sha256Hex(c)
	}
	return hashed
}

func sha256Hex(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

type TempTokenEntry struct {
	AdminID     int
	Username    string
	IP          string
	UserAgent   string
	Attempts    int
	MaxAttempts int
	ExpiresAt   time.Time
}

type TempTokenStore struct {
	mu          sync.Mutex
	tokens      map[string]*TempTokenEntry
	ttl         time.Duration
	maxAttempts int
	cancel      context.CancelFunc
}

func NewTempTokenStore(ttl time.Duration) *TempTokenStore {
	ctx, cancel := context.WithCancel(context.Background())
	store := &TempTokenStore{
		tokens:      make(map[string]*TempTokenEntry),
		ttl:         ttl,
		maxAttempts: 5,
		cancel:      cancel,
	}
	go store.cleanup(ctx)
	return store
}

func (s *TempTokenStore) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *TempTokenStore) Create(adminID int, username, ip, userAgent string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	token := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = &TempTokenEntry{
		AdminID:     adminID,
		Username:    username,
		IP:          ip,
		UserAgent:   userAgent,
		MaxAttempts: s.maxAttempts,
		ExpiresAt:   time.Now().UTC().Add(s.ttl),
	}
	return token
}

func (s *TempTokenStore) ValidateContext(token, ip, userAgent string) (*TempTokenEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.validateLocked(token, ip, userAgent)
	if !ok {
		return nil, false
	}
	return cloneTempTokenEntry(entry), true
}

func (s *TempTokenStore) RecordFailure(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.tokens[token]
	if !ok {
		return
	}
	entry.Attempts++
	if entry.Attempts >= entry.MaxAttempts {
		delete(s.tokens, token)
	}
}

func (s *TempTokenStore) Consume(token, ip, userAgent string) (*TempTokenEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.validateLocked(token, ip, userAgent)
	if !ok {
		return nil, false
	}
	delete(s.tokens, token)
	return cloneTempTokenEntry(entry), true
}

func (s *TempTokenStore) validateLocked(token, ip, userAgent string) (*TempTokenEntry, bool) {
	entry, ok := s.tokens[token]
	if !ok {
		return nil, false
	}
	if time.Now().UTC().After(entry.ExpiresAt) || entry.Attempts >= entry.MaxAttempts {
		delete(s.tokens, token)
		return nil, false
	}
	// 临时令牌绑定首次登录的 IP 与 User-Agent，避免泄露后被其他客户端继续完成二阶段认证。
	if subtle.ConstantTimeCompare([]byte(entry.IP), []byte(ip)) != 1 || subtle.ConstantTimeCompare([]byte(entry.UserAgent), []byte(userAgent)) != 1 {
		return nil, false
	}
	return entry, true
}

func cloneTempTokenEntry(entry *TempTokenEntry) *TempTokenEntry {
	copyEntry := *entry
	return &copyEntry
}

func (s *TempTokenStore) cleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UTC()
			s.mu.Lock()
			for token, entry := range s.tokens {
				if now.After(entry.ExpiresAt) {
					delete(s.tokens, token)
				}
			}
			s.mu.Unlock()
		}
	}
}
