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
	defaultCommandOutputBytes    = 32 * 1024 * 1024
	defaultCommandDiagnostic     = 64 * 1024
	defaultLogLineBytes          = 64 * 1024
)

func (s *Server) maxReadBytes() int64 {
	if s == nil || s.cfg == nil {
		return defaultAgentMaxReadBytes
	}
	return clampAgentBytes(app.ParseSizeOrDefault(s.cfg.Agent.MaxReadSize, defaultAgentMaxReadBytes), defaultAgentMaxReadBytes)
}

func (s *Server) commandOutputLimits() (int64, int64) {
	if s == nil || s.cfg == nil {
		return defaultCommandOutputBytes, defaultCommandDiagnostic
	}
	maxOutput := clampSize(s.cfg.Agent.Resources.CommandOutputMaxSize, defaultCommandOutputBytes, 64*1024, 256*1024*1024)
	maxDiagnostic := clampSize(s.cfg.Agent.Resources.CommandDiagnosticMaxSize, defaultCommandDiagnostic, 1024, 1024*1024)
	if maxDiagnostic > maxOutput {
		maxDiagnostic = maxOutput
	}
	return maxOutput, maxDiagnostic
}

func (s *Server) logLineMaxBytes() int {
	if s == nil || s.cfg == nil {
		return defaultLogLineBytes
	}
	return int(clampSize(s.cfg.Agent.Resources.LogLineMaxSize, defaultLogLineBytes, 1024, 1024*1024))
}

type accessScanLimits struct {
	maxBytes     int64
	maxLines     int64
	timeout      time.Duration
	maxLineBytes int
	rotatedFiles int
}

func (s *Server) accessScanLimits() accessScanLimits {
	const (
		hardBytes   = 64 * 1024 * 1024
		hardLines   = int64(500000)
		hardLine    = 32 * 1024
		hardRotated = 32
	)
	if s == nil || s.cfg == nil {
		return accessScanLimits{hardBytes, hardLines, 300 * time.Second, hardLine, hardRotated}
	}
	c := s.cfg.Agent.Resources
	return accessScanLimits{
		maxBytes:     clampSize(c.AccessScanMaxBytes, hardBytes, 1024, hardBytes),
		maxLines:     clampInt64(c.AccessScanMaxLines, hardLines, 1, hardLines),
		timeout:      clampDuration(c.AccessScanTimeout, 300*time.Second, time.Second, 300*time.Second),
		maxLineBytes: int(clampSize(c.AccessScanLineMaxSize, hardLine, 256, hardLine)),
		rotatedFiles: int(clampInt64(int64(c.AccessScanRotatedFiles), hardRotated, 0, hardRotated)),
	}
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
