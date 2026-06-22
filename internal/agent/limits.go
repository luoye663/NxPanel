package agent

import (
	"time"

	"github.com/luoye663/nxpanel/internal/app"
)

const (
	defaultAgentMaxReadBytes     = 16 * 1024 * 1024
	defaultAgentMaxDownloadBytes = 256 * 1024 * 1024
	defaultAgentDownloadTimeout  = 2 * time.Minute
	minAgentLimitBytes           = 1 * 1024 * 1024
	maxAgentLimitBytes           = 4 * 1024 * 1024 * 1024
	minAgentDownloadTimeout      = 5 * time.Second
	maxAgentDownloadTimeout      = 30 * time.Minute
)

func (s *Server) maxReadBytes() int64 {
	if s == nil || s.cfg == nil {
		return defaultAgentMaxReadBytes
	}
	return clampAgentBytes(app.ParseSizeOrDefault(s.cfg.Agent.MaxReadSize, defaultAgentMaxReadBytes), defaultAgentMaxReadBytes)
}

func (s *Server) clampReadBytes(requested, defaultBytes int64) int64 {
	if requested <= 0 {
		requested = defaultBytes
	}
	maxBytes := s.maxReadBytes()
	if requested > maxBytes {
		return maxBytes
	}
	return requested
}

func (s *Server) maxDownloadBytes() int64 {
	if s == nil || s.cfg == nil {
		return defaultAgentMaxDownloadBytes
	}
	return clampAgentBytes(app.ParseSizeOrDefault(s.cfg.Agent.MaxDownloadSize, defaultAgentMaxDownloadBytes), defaultAgentMaxDownloadBytes)
}

func (s *Server) downloadTimeout() time.Duration {
	if s == nil || s.cfg == nil {
		return defaultAgentDownloadTimeout
	}
	timeout := app.ParseDurationOrDefault(s.cfg.Agent.DownloadTimeout, defaultAgentDownloadTimeout)
	if timeout < minAgentDownloadTimeout {
		return minAgentDownloadTimeout
	}
	if timeout > maxAgentDownloadTimeout {
		return maxAgentDownloadTimeout
	}
	return timeout
}

func clampAgentBytes(value, defaultValue int64) int64 {
	// 配置值来自 YAML/环境变量，这里做上下界钳制，避免误填 0 或超大值绕过资源保护。
	if value <= 0 {
		return defaultValue
	}
	if value < minAgentLimitBytes {
		return minAgentLimitBytes
	}
	if value > maxAgentLimitBytes {
		return maxAgentLimitBytes
	}
	return value
}
