package systemmetrics

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

type snapshotCollector interface {
	Collect(CollectOptions) (Snapshot, error)
}

type Service struct {
	collector snapshotCollector
	interval  time.Duration
	ctx       context.Context
	cancel    context.CancelFunc

	mu        sync.Mutex
	subs      map[uint64]subscriber
	nextSub   uint64
	last      *Snapshot
	wake      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	closing   bool
}

type subscriber struct {
	ch    chan Snapshot
	scope Scope
}

func NewService(parent context.Context, interval time.Duration) *Service {
	return newService(parent, interval, NewCollector())
}

func newService(parent context.Context, interval time.Duration, collector snapshotCollector) *Service {
	if interval < time.Second {
		interval = time.Second
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Service{
		collector: collector,
		interval:  interval,
		ctx:       ctx,
		cancel:    cancel,
		subs:      make(map[uint64]subscriber),
		wake:      make(chan struct{}, 1),
	}
	s.wg.Add(1)
	go s.loop()
	return s
}

func (s *Service) Subscribe(ctx context.Context, scope string) (<-chan Snapshot, func()) {
	s.mu.Lock()
	if s.closing || s.ctx.Err() != nil || ctx.Err() != nil {
		s.mu.Unlock()
		ch := make(chan Snapshot)
		close(ch)
		return ch, func() {}
	}
	s.nextSub++
	id := s.nextSub
	ch := make(chan Snapshot, 4)
	s.subs[id] = subscriber{ch: ch, scope: ParseScope(scope)}
	if s.last != nil {
		ch <- FilterSnapshot(*s.last, scope)
	}
	s.wg.Add(1)
	s.mu.Unlock()
	s.notify()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			s.mu.Lock()
			if existing, ok := s.subs[id]; ok {
				close(existing.ch)
				delete(s.subs, id)
			}
			s.mu.Unlock()
			s.notify()
		})
	}

	go func() {
		defer s.wg.Done()
		select {
		case <-ctx.Done():
		case <-s.ctx.Done():
		}
		unsub()
	}()

	return ch, unsub
}

func (s *Service) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		s.cancel()
		s.mu.Unlock()
	})
	s.wg.Wait()
	s.mu.Lock()
	for id, sub := range s.subs {
		close(sub.ch)
		delete(s.subs, id)
	}
	s.mu.Unlock()
}

func (s *Service) loop() {
	defer s.wg.Done()
	var timer *time.Timer
	for {
		if !s.hasSubscribers() {
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
				continue
			}
		}

		s.publishOnce()
		if timer == nil {
			timer = time.NewTimer(s.interval)
		} else {
			timer.Reset(s.interval)
		}
		select {
		case <-s.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-s.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

func (s *Service) publishOnce() {
	includeTop := s.needsTop()
	snapshot, err := s.collector.Collect(CollectOptions{IncludeTop: includeTop})
	if err != nil {
		slog.Debug("采集系统指标失败", "error", err)
		return
	}

	s.mu.Lock()
	s.last = &snapshot
	for _, sub := range s.subs {
		filtered := FilterSnapshot(snapshot, sub.scope.raw)
		select {
		case sub.ch <- filtered:
		default:
		}
	}
	s.mu.Unlock()
}

func (s *Service) hasSubscribers() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs) > 0
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

func (s *Service) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func MarshalSnapshot(snapshot Snapshot) ([]byte, error) {
	return json.Marshal(snapshot)
}
