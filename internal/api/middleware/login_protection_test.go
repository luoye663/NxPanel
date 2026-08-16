package middleware

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLoginProtectionAccountBudgetAcrossRotatingIPs(t *testing.T) {
	p := NewLoginProtection(LoginProtectionConfig{
		IPMaxFailures:      10,
		AccountMaxFailures: 3,
		GlobalMaxFailures:  100,
		Window:             time.Minute,
	})
	t.Cleanup(p.Stop)
	for i := 0; i < 3; i++ {
		ip := fmt.Sprintf("192.0.2.%d", i)
		if !p.Check(ip, " Admin ") {
			t.Fatalf("第 %d 次失败前不应受限", i+1)
		}
		p.RecordFailure(ip, " Admin ")
	}
	if p.Check("192.0.2.99", "ADMIN") {
		t.Fatal("轮换 IP 不应绕过规范化账号预算")
	}
}

func TestLoginProtectionConcurrentReload(t *testing.T) {
	p := NewLoginProtection(LoginProtectionConfig{IPMaxFailures: 10, AccountMaxFailures: 10, GlobalMaxFailures: 1000, Window: time.Minute})
	t.Cleanup(p.Stop)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				ip := fmt.Sprintf("ip-%d", i)
				_ = p.Check(ip, "admin")
				p.RecordFailure(ip, "admin")
				_ = p.FailureCount(ip, "admin")
			}
		}(i)
	}
	for i := 0; i < 200; i++ {
		p.ReloadConfig(LoginProtectionConfig{IPMaxFailures: 10 + i%2, AccountMaxFailures: 20, GlobalMaxFailures: 10000, Window: time.Minute, MaxEntries: 100})
	}
	wg.Wait()
}

func TestLoginProtectionGlobalBudgetIsNotResetBySuccess(t *testing.T) {
	p := NewLoginProtection(LoginProtectionConfig{
		IPMaxFailures:      10,
		AccountMaxFailures: 10,
		GlobalMaxFailures:  2,
		Window:             time.Minute,
	})
	t.Cleanup(p.Stop)
	p.RecordFailure("192.0.2.1", "first")
	p.RecordSuccess("192.0.2.1", "first")
	p.RecordFailure("192.0.2.2", "second")
	if p.Check("192.0.2.3", "third") {
		t.Fatal("单次成功不应重置全局失败预算")
	}
}

func TestLoginProtectionBoundsTrackedMaps(t *testing.T) {
	p := NewLoginProtection(LoginProtectionConfig{
		IPMaxFailures:      100,
		AccountMaxFailures: 100,
		GlobalMaxFailures:  100,
		Window:             time.Minute,
		MaxEntries:         3,
	})
	t.Cleanup(p.Stop)
	for i := 0; i < 10; i++ {
		p.RecordFailure(fmt.Sprintf("ip-%d", i), fmt.Sprintf("user-%d", i))
	}
	if len(p.byIP) > 3 || len(p.byAccount) > 3 {
		t.Fatalf("跟踪表超过容量: ip=%d account=%d", len(p.byIP), len(p.byAccount))
	}
}
