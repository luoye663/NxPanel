package sse

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestConcurrentPublishAndHistoryKeepsBoundedSnapshot(t *testing.T) {
	stream := NewHub().CreateStream("concurrent")
	var wg sync.WaitGroup
	for publisher := 0; publisher < 4; publisher++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < 250; i++ {
				stream.PublishData(strconv.Itoa(offset*250 + i))
				if len(stream.History()) > 200 {
					t.Errorf("history exceeded bound")
				}
			}
		}(publisher)
	}
	wg.Wait()
	if got := len(stream.History()); got != 200 {
		t.Fatalf("history length = %d, want 200", got)
	}
}

func TestSubscribeSnapshotHasNoGapOrDuplicate(t *testing.T) {
	stream := NewHub().CreateStream("ordering")
	started := make(chan struct{})
	resume := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			if i == 25 {
				close(started)
				<-resume
			}
			stream.PublishData(strconv.Itoa(i))
		}
		stream.PublishDone("")
	}()
	<-started

	subCh := make(chan Subscription, 1)
	go func() { subCh <- stream.SubscribeSnapshot() }()
	close(resume)
	sub := <-subCh
	defer sub.Unsubscribe()
	events := append([]Event(nil), sub.History...)
	for event := range sub.Events {
		events = append(events, event)
	}

	seen := make(map[string]int, 101)
	for _, event := range events {
		seen[event.Data]++
	}
	for i := 0; i < 100; i++ {
		value := strconv.Itoa(i)
		if seen[value] != 1 {
			t.Fatalf("event %s observed %d times", value, seen[value])
		}
	}
	if seen["[DONE]"] != 1 {
		t.Fatalf("DONE observed %d times", seen["[DONE]"])
	}
}

func TestClosedStreamReplayAndPrune(t *testing.T) {
	hub := NewHub()
	stream := hub.CreateStream("closed")
	stream.PublishDone("terminal")

	first := stream.SubscribeSnapshot()
	second := stream.SubscribeSnapshot()
	for index, sub := range []Subscription{first, second} {
		if !sub.Closed {
			t.Fatalf("subscription %d should report closed", index)
		}
		if len(sub.History) != 2 || sub.History[0].Data != "terminal" || sub.History[1].Data != "[DONE]" {
			t.Fatalf("subscription %d history = %#v", index, sub.History)
		}
	}
	if pruned := hub.PruneClosedBefore(time.Now().Add(time.Second)); pruned != 1 {
		t.Fatalf("pruned = %d, want 1", pruned)
	}
	if hub.GetStream("closed") != nil {
		t.Fatal("closed stream remains after prune")
	}
}
