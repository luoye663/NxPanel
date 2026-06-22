package app

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultServiceLogRotateInterval = time.Hour
	defaultServiceLogRotateMaxSize  = 50 * 1024 * 1024
	defaultServiceLogRotateMaxCount = 30
	defaultServiceLogRotateMaxAge   = 720 * time.Hour
	minServiceLogRotateInterval     = time.Minute
	serviceLogTimestampLayout       = "20060102_150405"
)

type rotatedServiceLogFile struct {
	path    string
	modTime time.Time
}

// RotatingFileWriter 是 API/Agent 运行日志专用 writer，只在超过大小时切割当前活跃日志。
type RotatingFileWriter struct {
	mu       sync.Mutex
	path     string
	file     *os.File
	size     int64
	maxSize  int64
	maxCount int
	maxAge   time.Duration
	interval time.Duration
	stopCh   chan struct{}
	closed   bool
}

func NewRotatingFileWriter(path string, cfg ServiceLogRotateConfig) (*RotatingFileWriter, error) {
	maxSize := ParseSizeOrDefault(cfg.MaxSize, defaultServiceLogRotateMaxSize)
	if maxSize <= 0 {
		maxSize = defaultServiceLogRotateMaxSize
	}
	maxCount := cfg.MaxCount
	if maxCount <= 0 {
		maxCount = defaultServiceLogRotateMaxCount
	}
	maxAge := ParseDurationOrDefault(cfg.MaxAge, defaultServiceLogRotateMaxAge)
	if maxAge <= 0 {
		maxAge = defaultServiceLogRotateMaxAge
	}
	interval := ParseDurationOrDefault(cfg.Interval, defaultServiceLogRotateInterval)
	if interval < minServiceLogRotateInterval {
		interval = minServiceLogRotateInterval
	}

	w := &RotatingFileWriter{
		path:     path,
		maxSize:  maxSize,
		maxCount: maxCount,
		maxAge:   maxAge,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
	if err := w.openActive(); err != nil {
		return nil, err
	}
	go w.run()
	return w, nil
}

func (w *RotatingFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, errors.New("rotating log writer closed")
	}
	if w.file == nil {
		if err := w.openActive(); err != nil {
			return 0, err
		}
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxSize {
		if err := w.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *RotatingFileWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	close(w.stopCh)
	f := w.file
	w.file = nil
	w.mu.Unlock()

	if f != nil {
		return f.Close()
	}
	return nil
}

func (w *RotatingFileWriter) run() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.checkAndCleanup()
		case <-w.stopCh:
			return
		}
	}
}

func (w *RotatingFileWriter) checkAndCleanup() {
	w.mu.Lock()
	if !w.closed {
		if info, err := os.Stat(w.path); err == nil {
			w.size = info.Size()
			if w.size > w.maxSize {
				_ = w.rotateLocked()
			}
		}
	}
	w.mu.Unlock()
	_ = cleanupRotatedServiceLogs(w.path, w.maxCount, w.maxAge)
}

func (w *RotatingFileWriter) openActive() error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.size = info.Size()
	return nil
}

func (w *RotatingFileWriter) rotateLocked() error {
	if w.size <= 0 {
		return nil
	}
	oldFile := w.file
	w.file = nil
	if oldFile != nil {
		_ = oldFile.Close()
	}

	rotatedPath := nextServiceLogRotatedPath(w.path, time.Now().UTC())
	if err := os.Rename(w.path, rotatedPath); err != nil {
		// rename 失败时重新打开原活跃文件，优先保证后续日志仍可写。
		return w.openActive()
	}
	if err := w.openActive(); err != nil {
		return err
	}
	_ = cleanupRotatedServiceLogs(w.path, w.maxCount, w.maxAge)
	return nil
}

func nextServiceLogRotatedPath(activePath string, now time.Time) string {
	dir := filepath.Dir(activePath)
	base := filepath.Base(activePath)
	stamp := now.Format(serviceLogTimestampLayout)
	path := filepath.Join(dir, base+"."+stamp)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	for i := 1; ; i++ {
		candidate := filepath.Join(dir, base+"."+stamp+"."+strconv.Itoa(i))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func cleanupRotatedServiceLogs(activePath string, maxCount int, maxAge time.Duration) error {
	if maxCount <= 0 || maxAge <= 0 {
		return nil
	}
	dir := filepath.Dir(activePath)
	base := filepath.Base(activePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	files := make([]rotatedServiceLogFile, 0, len(entries))
	prefix := base + "."
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		if len(rest) < len(serviceLogTimestampLayout) {
			continue
		}
		if _, err := time.Parse(serviceLogTimestampLayout, rest[:len(serviceLogTimestampLayout)]); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, rotatedServiceLogFile{path: filepath.Join(dir, name), modTime: info.ModTime()})
	}
	if len(files) <= maxCount {
		return nil
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})
	cutoff := time.Now().Add(-maxAge)
	for i := maxCount; i < len(files); i++ {
		if files[i].modTime.Before(cutoff) {
			_ = os.Remove(files[i].path)
		}
	}
	return nil
}
