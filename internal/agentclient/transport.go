// agentclient 包 — Unix Socket HTTP 传输层
//
// 封装 net/http.Transport 使其通过 Unix Socket 通信。
// 所有 HTTP 请求都通过 Unix Socket 发送到 agent 进程。
package agentclient

import (
	"context"
	"net"
	"net/http"
	"time"
)

// newUnixSocketTransport 创建 Unix Socket HTTP 传输层
//
// 工作方式：
//   - 拦截 HTTP 请求的 Dial
//   - 将 TCP 连接替换为 Unix Socket 连接
//   - 连接超时 3 秒
func newUnixSocketTransport(socketPath string, dialTimeout, idleConnTimeout time.Duration) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: dialTimeout}
			return d.DialContext(ctx, "unix", socketPath)
		},
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     idleConnTimeout,
	}
}
