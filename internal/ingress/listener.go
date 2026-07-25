package ingress

import (
	"net"
	"sync"
)

type ConnectionLimiter struct {
	mu        sync.Mutex
	globalMax int
	perIPMax  int
	total     int
	byIP      map[string]int
}

func NewConnectionLimiter(globalMax, perIPMax int) *ConnectionLimiter {
	if globalMax <= 0 {
		globalMax = 256
	}
	if perIPMax <= 0 {
		perIPMax = 20
	}
	return &ConnectionLimiter{globalMax: globalMax, perIPMax: perIPMax, byIP: make(map[string]int)}
}

func (l *ConnectionLimiter) Acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= l.globalMax || l.byIP[ip] >= l.perIPMax {
		return false
	}
	l.total++
	l.byIP[ip]++
	return true
}

func (l *ConnectionLimiter) Release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byIP[ip] <= 0 {
		return
	}
	l.total--
	l.byIP[ip]--
	if l.byIP[ip] == 0 {
		delete(l.byIP, ip)
	}
}

func (l *ConnectionLimiter) Counts() (int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.total, len(l.byIP)
}

func LimitListener(inner net.Listener, globalMax, perIPMax int) net.Listener {
	return &limitedListener{Listener: inner, limiter: NewConnectionLimiter(globalMax, perIPMax)}
}

type limitedListener struct {
	net.Listener
	limiter *ConnectionLimiter
}

func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		ip := remoteIP(conn.RemoteAddr())
		if l.limiter.Acquire(ip) {
			return &limitedConn{Conn: conn, limiter: l.limiter, ip: ip}, nil
		}
		_ = conn.Close()
	}
}

type limitedConn struct {
	net.Conn
	limiter *ConnectionLimiter
	ip      string
	once    sync.Once
}

func (c *limitedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.limiter.Release(c.ip) })
	return err
}

func remoteIP(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err == nil {
		return host
	}
	return addr.String()
}
