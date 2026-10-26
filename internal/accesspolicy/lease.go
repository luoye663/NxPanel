package accesspolicy

import "sync"

// siteLease serializes legacy writes with policy activation for one site. Only
// holders and waiters retain entries, so completed requests leave no map growth.
type siteLease struct {
	mu   sync.Mutex
	refs int
}

// acquireSite must precede the proxy writer lock and the policy service mutex.
// The returned release function is idempotent.
func (s *Service) acquireSite(siteID string) func() {
	s.leasesMu.Lock()
	if s.leases == nil {
		s.leases = make(map[string]*siteLease)
	}
	entry := s.leases[siteID]
	if entry == nil {
		entry = &siteLease{}
		s.leases[siteID] = entry
	}
	entry.refs++
	s.leasesMu.Unlock()
	entry.mu.Lock()
	var once sync.Once
	return func() {
		once.Do(func() {
			entry.mu.Unlock()
			s.leasesMu.Lock()
			entry.refs--
			if entry.refs == 0 {
				delete(s.leases, siteID)
			}
			s.leasesMu.Unlock()
		})
	}
}
