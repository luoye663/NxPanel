package systemmetrics

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockingCollector struct {
	active  atomic.Int32
	max     atomic.Int32
	started chan struct{}
	release chan struct{}
}

func TestSubscribeRejectedDuringConcurrentClose(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	collector := &blockingCollector{started: make(chan struct{}, 128), release: make(chan struct{})}
	close(collector.release)
	svc := newService(root, time.Second, collector)

	var callers sync.WaitGroup
	for range 64 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			ctx, cancelSub := context.WithCancel(context.Background())
			_, unsub := svc.Subscribe(ctx, "summary")
			cancelSub()
			unsub()
		}()
	}
	cancel()
	closed := make(chan struct{})
	go func() {
		svc.Close()
		close(closed)
	}()
	callers.Wait()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("metrics Close blocked during concurrent subscriptions")
	}

	ch, _ := svc.Subscribe(context.Background(), "summary")
	if _, ok := <-ch; ok {
		t.Fatal("subscription after lifecycle cancellation was accepted")
	}
}

func (c *blockingCollector) Collect(CollectOptions) (Snapshot, error) {
	active := c.active.Add(1)
	defer c.active.Add(-1)
	for {
		current := c.max.Load()
		if active <= current || c.max.CompareAndSwap(current, active) {
			break
		}
	}
	c.started <- struct{}{}
	<-c.release
	return Snapshot{}, nil
}

func TestUnsubscribeResubscribeNeverOverlapsCollection(t *testing.T) {
	root, cancel := context.WithCancel(context.Background())
	collector := &blockingCollector{started: make(chan struct{}, 3), release: make(chan struct{})}
	svc := newService(root, time.Second, collector)
	ctx1, cancel1 := context.WithCancel(context.Background())
	_, unsub1 := svc.Subscribe(ctx1, "summary")
	<-collector.started
	unsub1()
	cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	_, unsub2 := svc.Subscribe(ctx2, "detail")
	select {
	case <-collector.started:
		t.Fatal("replacement collection started before prior collection exited")
	case <-time.After(50 * time.Millisecond):
	}
	close(collector.release)
	select {
	case <-collector.started:
	case <-time.After(time.Second):
		t.Fatal("replacement subscription did not resume collection")
	}
	if got := collector.max.Load(); got != 1 {
		t.Fatalf("maximum concurrent collections = %d, want 1", got)
	}
	unsub2()
	cancel2()
	cancel()
	svc.Close()
}
