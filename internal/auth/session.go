package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

const (
	DefaultSessionDuration   = 24 * time.Hour
	SessionUserAgentMaxBytes = 512
	SessionIPMaxBytes        = 64
)

const SessionCookieName = "openrest_session"

const CSRFCookieName = "csrf-token"

type SessionService struct {
	sessionRepo     *repo.SessionRepo
	adminRepo       *repo.AdminRepo
	sessionDuration time.Duration
	maxSessions     int
	bindIP          bool
	bindUA          bool
}

type SessionServiceOption func(*SessionService)

func WithMaxSessions(max int) SessionServiceOption {
	return func(s *SessionService) { s.maxSessions = max }
}

func WithBindSessionIP(bind bool) SessionServiceOption {
	return func(s *SessionService) { s.bindIP = bind }
}

func WithBindSessionUA(bind bool) SessionServiceOption {
	return func(s *SessionService) { s.bindUA = bind }
}

func WithAdminRepo(ar *repo.AdminRepo) SessionServiceOption {
	return func(s *SessionService) { s.adminRepo = ar }
}

func NewSessionService(sessionRepo *repo.SessionRepo, sessionDuration time.Duration, opts ...SessionServiceOption) *SessionService {
	if sessionDuration <= 0 {
		sessionDuration = DefaultSessionDuration
	}
	s := &SessionService{sessionRepo: sessionRepo, sessionDuration: sessionDuration}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *SessionService) GetSessionDuration() time.Duration {
	return s.sessionDuration
}

func (s *SessionService) ReloadSecurityConfig(maxSessions int, bindIP, bindUA bool) {
	s.maxSessions = maxSessions
	s.bindIP = bindIP
	s.bindUA = bindUA
}

func (s *SessionService) CreateSession(userAgent, ip string) (sessionID, csrfToken string, err error) {
	userAgent = NormalizeSessionUserAgent(userAgent)
	ip = NormalizeSessionIP(ip)
	if s.maxSessions > 0 && s.adminRepo != nil {
		for {
			count, cerr := s.adminRepo.CountSessions()
			if cerr != nil {
				slog.Warn("统计会话数量失败", "error", cerr)
				break
			}
			if count < s.maxSessions {
				break
			}
			if derr := s.adminRepo.DeleteOldestSession(); derr != nil {
				slog.Warn("删除最旧会话失败", "error", derr)
				break
			}
		}
	}

	sessionID = app.NewSessionID()
	csrfToken = app.NewCSRFToken()
	csrfHash := sha256Hash(csrfToken)

	now := time.Now().UTC()
	expiresAt := now.Add(s.sessionDuration)

	session := &repo.Session{
		ID:            sessionID,
		CSRFTokenHash: csrfHash,
		UserAgent:     userAgent,
		IP:            ip,
		ExpiresAt:     expiresAt.Format(time.RFC3339),
		CreatedAt:     now.Format(time.RFC3339),
		LastSeenAt:    now.Format(time.RFC3339),
	}

	if err := s.sessionRepo.Create(session); err != nil {
		return "", "", fmt.Errorf("创建 session 失败: %w", err)
	}

	return sessionID, csrfToken, nil
}

func (s *SessionService) ValidateSession(sessionID, currentIP, currentUA string) (*repo.Session, error) {
	currentIP = NormalizeSessionIP(currentIP)
	currentUA = NormalizeSessionUserAgent(currentUA)
	session, err := s.sessionRepo.GetByID(sessionID)
	if err != nil {
		return nil, fmt.Errorf("查询 session 失败: %w", err)
	}
	if session == nil {
		return nil, nil
	}

	expiresAt, err := time.Parse(time.RFC3339, session.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("解析 session 过期时间失败: %w", err)
	}
	if time.Now().UTC().After(expiresAt) {
		_ = s.sessionRepo.Delete(sessionID)
		return nil, nil
	}

	if s.bindIP && currentIP != "" && session.IP != currentIP {
		_ = s.sessionRepo.Delete(sessionID)
		return nil, ErrSessionMismatch
	}
	if s.bindUA && currentUA != "" && session.UserAgent != currentUA {
		_ = s.sessionRepo.Delete(sessionID)
		return nil, ErrSessionMismatch
	}

	_ = s.sessionRepo.TouchLastSeen(sessionID)

	return session, nil
}

func NormalizeSessionUserAgent(value string) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) <= SessionUserAgentMaxBytes {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func NormalizeSessionIP(value string) string {
	return truncateSessionContext(value, SessionIPMaxBytes)
}

func truncateSessionContext(value string, maxBytes int) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func (s *SessionService) DestroySession(sessionID string) error {
	return s.sessionRepo.Delete(sessionID)
}

func (s *SessionService) DestroyAllSessions() error {
	return s.sessionRepo.DeleteAll()
}

func (s *SessionService) CleanupExpired() (int64, error) {
	return s.sessionRepo.DeleteExpired()
}

func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
