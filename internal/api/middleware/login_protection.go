package middleware

import (
	"context"
	"strings"
	"sync"
	"time"
)

const defaultMaxLoginBudgetEntries = 10000

type LoginProtectionConfig struct {
	IPMaxFailures      int
	AccountMaxFailures int
	GlobalMaxFailures  int
	Window             time.Duration
	MaxEntries         int
}

type LoginProtection struct {
	mu        sync.Mutex
	config    LoginProtectionConfig
	byIP      map[string]*failRecord
	byAccount map[string]*failRecord
	global    failRecord
	cancel    context.CancelFunc
}

func NewLoginProtection(config LoginProtectionConfig) *LoginProtection {
	ctx, cancel := context.WithCancel(context.Background())
	p := &LoginProtection{
		config:    normalizeLoginProtectionConfig(config),
		byIP:      make(map[string]*failRecord),
		byAccount: make(map[string]*failRecord),
		cancel:    cancel,
	}
	go p.cleanup(ctx)
	return p
}

func (p *LoginProtection) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
}

func (p *LoginProtection) ReloadConfig(config LoginProtectionConfig) {
	p.mu.Lock()
	p.config = normalizeLoginProtectionConfig(config)
	p.purgeExpiredLocked(time.Now())
	p.enforceCardinalityLocked(p.byIP)
	p.enforceCardinalityLocked(p.byAccount)
	p.mu.Unlock()
}

func NormalizeLoginAccount(account string) string {
	return strings.ToLower(strings.TrimSpace(account))
}

func (p *LoginProtection) Check(ip, account string) bool {
	now := time.Now()
	account = NormalizeLoginAccount(account)
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.allowedLocked(p.byIP[ip], p.config.IPMaxFailures, now) &&
		(account == "" || p.allowedLocked(p.byAccount[account], p.config.AccountMaxFailures, now)) &&
		p.allowedLocked(&p.global, p.config.GlobalMaxFailures, now)
}

func (p *LoginProtection) RecordFailure(ip, account string) {
	now := time.Now()
	account = NormalizeLoginAccount(account)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.recordLocked(p.byIP, ip, now)
	if account != "" {
		p.recordLocked(p.byAccount, account, now)
	}
	p.incrementLocked(&p.global, now)
}

func (p *LoginProtection) RecordSuccess(ip, account string) {
	account = NormalizeLoginAccount(account)
	p.mu.Lock()
	delete(p.byIP, ip)
	if account != "" {
		delete(p.byAccount, account)
	}
	p.mu.Unlock()
}

func (p *LoginProtection) FailureCount(ip, account string) int {
	now := time.Now()
	account = NormalizeLoginAccount(account)
	p.mu.Lock()
	defer p.mu.Unlock()
	ipCount := p.countLocked(p.byIP[ip], now)
	accountCount := p.countLocked(p.byAccount[account], now)
	if accountCount > ipCount {
		return accountCount
	}
	return ipCount
}

func (p *LoginProtection) allowedLocked(rec *failRecord, limit int, now time.Time) bool {
	return p.countLocked(rec, now) < limit
}

func (p *LoginProtection) countLocked(rec *failRecord, now time.Time) int {
	if rec == nil || rec.windowStart.IsZero() || now.Sub(rec.windowStart) >= p.config.Window {
		return 0
	}
	return rec.count
}

func (p *LoginProtection) recordLocked(store map[string]*failRecord, key string, now time.Time) {
	if key == "" {
		return
	}
	if rec := store[key]; rec != nil {
		p.incrementLocked(rec, now)
		return
	}
	if len(store) >= p.config.MaxEntries {
		p.purgeMapLocked(store, now)
	}
	if len(store) >= p.config.MaxEntries {
		p.evictOldestLocked(store)
	}
	store[key] = &failRecord{count: 1, windowStart: now}
}

func (p *LoginProtection) incrementLocked(rec *failRecord, now time.Time) {
	if rec.windowStart.IsZero() || now.Sub(rec.windowStart) >= p.config.Window {
		rec.count = 1
		rec.windowStart = now
		return
	}
	rec.count++
}

func (p *LoginProtection) purgeExpiredLocked(now time.Time) {
	p.purgeMapLocked(p.byIP, now)
	p.purgeMapLocked(p.byAccount, now)
	if !p.global.windowStart.IsZero() && now.Sub(p.global.windowStart) >= p.config.Window {
		p.global = failRecord{}
	}
}

func (p *LoginProtection) purgeMapLocked(store map[string]*failRecord, now time.Time) {
	for key, rec := range store {
		if now.Sub(rec.windowStart) >= p.config.Window {
			delete(store, key)
		}
	}
}

func (p *LoginProtection) enforceCardinalityLocked(store map[string]*failRecord) {
	for len(store) > p.config.MaxEntries {
		p.evictOldestLocked(store)
	}
}

func (p *LoginProtection) evictOldestLocked(store map[string]*failRecord) {
	var oldestKey string
	var oldest time.Time
	for key, rec := range store {
		if oldestKey == "" || rec.windowStart.Before(oldest) {
			oldestKey = key
			oldest = rec.windowStart
		}
	}
	if oldestKey != "" {
		delete(store, oldestKey)
	}
}

func (p *LoginProtection) cleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			p.mu.Lock()
			p.purgeExpiredLocked(now)
			p.mu.Unlock()
		}
	}
}

func normalizeLoginProtectionConfig(config LoginProtectionConfig) LoginProtectionConfig {
	if config.IPMaxFailures <= 0 {
		config.IPMaxFailures = 5
	}
	if config.AccountMaxFailures <= 0 {
		config.AccountMaxFailures = 10
	}
	if config.GlobalMaxFailures <= 0 {
		config.GlobalMaxFailures = 100
	}
	if config.Window <= 0 {
		config.Window = 15 * time.Minute
	}
	if config.MaxEntries <= 0 {
		config.MaxEntries = defaultMaxLoginBudgetEntries
	}
	return config
}
