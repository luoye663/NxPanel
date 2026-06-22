package systemmetrics

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

type Service struct {
	collector *Collector
	interval  time.Duration

	// 一个 Service 只启动一个采集循环，所有 SSE 客户端复用同一份采样结果。
	// 这样多个浏览器同时打开仪表盘时，不会重复扫描 /proc 造成额外压力。
	mu      sync.Mutex
	subs    map[uint64]subscriber
	nextSub uint64
	stop    chan struct{}
	running bool
	last    *Snapshot
}

type subscriber struct {
	ch chan Snapshot
	// scope 记录当前客户端需要哪些详情字段，用于下发前过滤 Snapshot。
	scope Scope
}

func NewService(interval time.Duration) *Service {
	if interval < time.Second {
		interval = time.Second
	}
	return &Service{
		collector: NewCollector(),
		interval:  interval,
		subs:      make(map[uint64]subscriber),
	}
}

func (s *Service) Subscribe(ctx context.Context, scope string) (<-chan Snapshot, func()) {
	s.mu.Lock()
	s.nextSub++
	id := s.nextSub
	ch := make(chan Snapshot, 4)
	s.subs[id] = subscriber{ch: ch, scope: ParseScope(scope)}
	if s.last != nil {
		// 新订阅者先拿最后一帧，页面刷新时能尽快显示数据，不必等下一个 tick。
		ch <- FilterSnapshot(*s.last, scope)
	}
	if !s.running {
		// 只有存在订阅者时才启动采集；最后一个订阅者离开时会停止。
		s.startLocked()
	}
	s.mu.Unlock()

	unsub := func() {
		s.mu.Lock()
		if existing, ok := s.subs[id]; ok {
			close(existing.ch)
			delete(s.subs, id)
		}
		if len(s.subs) == 0 && s.running {
			// 无订阅者时停止循环，降低后台空跑和 GC 压力。
			close(s.stop)
			s.running = false
		}
		s.mu.Unlock()
	}

	go func() {
		<-ctx.Done()
		unsub()
	}()

	return ch, unsub
}

func (s *Service) Close() {
	s.mu.Lock()
	if s.running {
		close(s.stop)
		s.running = false
	}
	for id, ch := range s.subs {
		close(ch.ch)
		delete(s.subs, id)
	}
	s.mu.Unlock()
}

func (s *Service) startLocked() {
	s.stop = make(chan struct{})
	s.running = true
	stop := s.stop
	go s.loop(stop)
}

func (s *Service) loop(stop <-chan struct{}) {
	s.publishOnce()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.publishOnce()
		case <-stop:
			return
		}
	}
}

func (s *Service) publishOnce() {
	// top 进程需要扫描 /proc/[pid]，比基础指标更重；只有弹窗详情需要时才采集。
	includeTop := s.needsTop()
	snapshot, err := s.collector.Collect(CollectOptions{IncludeTop: includeTop})
	if err != nil {
		slog.Debug("采集系统指标失败", "error", err)
		return
	}

	s.mu.Lock()
	s.last = &snapshot
	for _, sub := range s.subs {
		// 每个订阅者按自己的 scope 收到裁剪后的数据，避免无用字段反复序列化/传输。
		filtered := FilterSnapshot(snapshot, sub.scope.raw)
		select {
		case sub.ch <- filtered:
		default:
		}
	}
	s.mu.Unlock()
}

func (s *Service) needsTop() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subs {
		if sub.scope.LoadDetail || sub.scope.CPUDetail || sub.scope.MemoryDetail {
			return true
		}
	}
	return false
}

func MarshalSnapshot(snapshot Snapshot) ([]byte, error) {
	return json.Marshal(snapshot)
}
