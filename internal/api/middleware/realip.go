package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
)

type contextKeyRealIP string

const RealIPKey contextKeyRealIP = "real_ip"

func GetRealIP(ctx context.Context) string {
	if v, ok := ctx.Value(RealIPKey).(string); ok {
		return v
	}
	return ""
}

type trustedProxyState struct {
	mu          sync.RWMutex
	hasExplicit bool
	nets        []*net.IPNet
}

var globalTrustedState trustedProxyState

func TrustedRealIP(trustedProxies []string) func(http.Handler) http.Handler {
	reloadTrustedProxiesLocked(trustedProxies)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIPFromRequest(r)
			ctx := context.WithValue(r.Context(), RealIPKey, ip)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractIPFromRequest(r *http.Request) string {
	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}

	globalTrustedState.mu.RLock()
	hasExplicit := globalTrustedState.hasExplicit
	nets := globalTrustedState.nets
	globalTrustedState.mu.RUnlock()

	if hasExplicit && isTrusted(remoteIP, nets) {
		if ip := extractClientIPFromXFF(r.Header.Get("X-Forwarded-For"), nets); ip != "" {
			return ip
		}
		if ip := parseHeaderIP(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
	}

	return remoteIP
}

func extractClientIPFromXFF(xff string, trustedNets []*net.IPNet) string {
	parts := strings.Split(xff, ",")
	var firstValid string
	for i := len(parts) - 1; i >= 0; i-- {
		ip := parseHeaderIP(parts[i])
		if ip == "" {
			continue
		}
		firstValid = ip
		// XFF 从左到右表示“客户端 -> 代理链”。从右往左跳过可信代理，遇到的第一个非可信 IP 才是客户端地址。
		if !isTrusted(ip, trustedNets) {
			return ip
		}
	}
	// 当整条代理链都在可信列表内时，使用最左侧合法 IP，避免把最后一级代理误记为客户端。
	return firstValid
}

func parseHeaderIP(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	return ip.String()
}

func isTrusted(ipStr string, trustedNets []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range trustedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func SetTrustedNets(nets []*net.IPNet) {
	globalTrustedState.mu.Lock()
	defer globalTrustedState.mu.Unlock()
	globalTrustedState.nets = nets
	globalTrustedState.hasExplicit = len(nets) > 0
}

func ReloadTrustedProxies(proxies []string) {
	globalTrustedState.mu.Lock()
	defer globalTrustedState.mu.Unlock()
	reloadTrustedProxiesLocked(proxies)
}

func reloadTrustedProxiesLocked(proxies []string) {
	nets := make([]*net.IPNet, 0, len(proxies))
	for _, cidr := range proxies {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			if ip := net.ParseIP(cidr); ip != nil {
				if ip.To4() != nil {
					_, ipNet, _ = net.ParseCIDR(cidr + "/32")
				} else {
					_, ipNet, _ = net.ParseCIDR(cidr + "/128")
				}
			}
		}
		if ipNet != nil {
			nets = append(nets, ipNet)
		}
	}
	globalTrustedState.nets = nets
	globalTrustedState.hasExplicit = len(nets) > 0
}

func IsFromTrustedProxy(r *http.Request) bool {
	globalTrustedState.mu.RLock()
	nets := globalTrustedState.nets
	globalTrustedState.mu.RUnlock()

	if len(nets) == 0 {
		return false
	}
	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}
	return isTrusted(remoteIP, nets)
}
