package sse

import (
	"fmt"
	"sync"
	"time"
)

type Event struct {
	ID    string
	Event string
	Data  string
}

type Stream struct {
	id       string
	mu       sync.RWMutex
	subs     map[uint64]chan Event
	nextSub  uint64
	done     chan struct{}
	closed   bool
	closedAt time.Time
	history  []Event
	maxHist  int
}

type Subscription struct {
	Events      <-chan Event
	History     []Event
	Closed      bool
	Unsubscribe func()
}

type Hub struct {
	streams sync.Map
}

func NewHub() *Hub {
	return &Hub{}
}

func (h *Hub) CreateStream(id string) *Stream {
	val, _ := h.streams.LoadOrStore(id, &Stream{
		id:      id,
		subs:    make(map[uint64]chan Event),
		done:    make(chan struct{}),
		maxHist: 200,
	})
	return val.(*Stream)
}

func (h *Hub) GetStream(id string) *Stream {
	val, ok := h.streams.Load(id)
	if !ok {
		return nil
	}
	return val.(*Stream)
}

func (h *Hub) CloseStream(id string) {
	val, ok := h.streams.Load(id)
	if !ok {
		return
	}
	val.(*Stream).close()
}

func (h *Hub) CloseAll() {
	h.streams.Range(func(key, val any) bool {
		s := val.(*Stream)
		s.close()
		h.streams.Delete(key)
		return true
	})
}

func (h *Hub) PruneClosedBefore(cutoff time.Time) int {
	pruned := 0
	h.streams.Range(func(key, val any) bool {
		stream := val.(*Stream)
		stream.mu.RLock()
		shouldPrune := stream.closed && !stream.closedAt.After(cutoff)
		stream.mu.RUnlock()
		if shouldPrune && h.streams.CompareAndDelete(key, val) {
			pruned++
		}
		return true
	})
	return pruned
}

func (s *Stream) Subscribe() (<-chan Event, func()) {
	sub := s.SubscribeSnapshot()
	return sub.Events, sub.Unsubscribe
}

func (s *Stream) SubscribeSnapshot() Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()

	history := append([]Event(nil), s.history...)
	if s.closed {
		ch := make(chan Event)
		close(ch)
		return Subscription{Events: ch, History: history, Closed: true, Unsubscribe: func() {}}
	}

	s.nextSub++
	id := s.nextSub
	ch := make(chan Event, 256)
	s.subs[id] = ch

	unsub := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if existing, ok := s.subs[id]; ok {
			close(existing)
			delete(s.subs, id)
		}
	}

	return Subscription{Events: ch, History: history, Unsubscribe: unsub}
}

func (s *Stream) Publish(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.publishLocked(event)
}

func (s *Stream) publishLocked(event Event) {
	if s.maxHist > 0 {
		s.history = append(s.history, event)
		if len(s.history) > s.maxHist {
			s.history = s.history[len(s.history)-s.maxHist:]
		}
	}

	for _, ch := range s.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *Stream) PublishData(data string) {
	s.Publish(Event{Data: data})
}

func (s *Stream) PublishDone(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if data != "" {
		s.publishLocked(Event{Data: data})
	}
	s.publishLocked(Event{Data: "[DONE]"})
	s.closeLocked(time.Now())
}

func (s *Stream) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

func (s *Stream) History() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make([]Event, len(s.history))
	copy(cp, s.history)
	return cp
}

func (s *Stream) SubscriberCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subs)
}

func (s *Stream) String() string {
	return fmt.Sprintf("Stream(%s, closed=%v, subs=%d)", s.id, s.IsClosed(), s.SubscriberCount())
}

func (s *Stream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked(time.Now())
}

func (s *Stream) closeLocked(now time.Time) {
	if s.closed {
		return
	}
	s.closed = true
	s.closedAt = now
	close(s.done)

	for id, ch := range s.subs {
		close(ch)
		delete(s.subs, id)
	}
}
