package accesspolicy

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func awaitLeaseReferences(t *testing.T, service *Service, siteID string, want int) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		service.leasesMu.Lock()
		entry := service.leases[siteID]
		got := 0
		if entry != nil {
			got = entry.refs
		}
		service.leasesMu.Unlock()
		if got == want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("lease refs for %s: got %d, want %d", siteID, got, want)
		case <-tick.C:
		}
	}
}

func TestLegacyLeaseBlocksActivationUntilLegacyWriteCompletes(t *testing.T) {
	service, agent, _, _ := policyServiceFixture(t)
	release, err := service.LegacyLease("site_policy")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	finished := make(chan error, 1)
	go func() {
		_, err := service.Save(context.Background(), "site_policy", serviceAllowPolicy(), "activation")
		finished <- err
	}()
	// Seeing the registered waiter proves Save has reached the activation
	// boundary; this assertion does not depend on an arbitrary sleep interval.
	awaitLeaseReferences(t, service, "site_policy", 2)
	select {
	case err := <-finished:
		t.Fatalf("activation passed held legacy lease: %v", err)
	default:
	}
	if len(agent.transactions) != 0 {
		t.Fatal("activation applied files before legacy writer completed")
	}
	if p, err := service.store.Get("site_policy"); err != nil || p != nil {
		t.Fatalf("activation persisted through legacy lease: %#v, %v", p, err)
	}
	release()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("activation did not resume after legacy writer finished")
	}
	if len(agent.transactions) != 1 {
		t.Fatal("resumed activation must apply exactly once")
	}
	awaitLeaseReferences(t, service, "site_policy", 0)
}

func TestWaitingLegacyLeaseRechecksPolicyAfterActivation(t *testing.T) {
	service, _, _, _ := policyServiceFixture(t)
	activationRelease := service.acquireSite("site_policy")
	defer activationRelease()
	finished := make(chan error, 1)
	go func() {
		release, err := service.LegacyLease("site_policy")
		if release != nil {
			release()
		}
		finished <- err
	}()
	awaitLeaseReferences(t, service, "site_policy", 2)
	if _, err := service.store.SaveDesired("site_policy", serviceAllowPolicy(), 0); err != nil {
		t.Fatal(err)
	}
	activationRelease()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("waiting legacy writer failed to recheck policy")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("legacy waiter did not resume")
	}
	awaitLeaseReferences(t, service, "site_policy", 0)
}

func TestSiteLeasesRemainIndependentAndCleanUp(t *testing.T) {
	service := &Service{}
	release := service.acquireSite("held")
	defer release()
	other := make(chan struct{})
	go func() { done := service.acquireSite("other"); done(); close(other) }()
	select {
	case <-other:
	case <-time.After(3 * time.Second):
		t.Fatal("one site's lease blocked another site")
	}
	release()
	var active atomic.Int32
	var overlap atomic.Bool
	var workers sync.WaitGroup
	for i := 0; i < 64; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			done := service.acquireSite("shared")
			if active.Add(1) != 1 {
				overlap.Store(true)
			}
			active.Add(-1)
			done()
			done() // Duplicate deferred cleanup must not corrupt references.
			done = service.acquireSite(fmt.Sprintf("site-%d", i))
			done()
		}(i)
	}
	workers.Wait()
	if overlap.Load() {
		t.Fatal("same-site lease admitted concurrent writers")
	}
	service.leasesMu.Lock()
	defer service.leasesMu.Unlock()
	if len(service.leases) != 0 {
		t.Fatalf("completed sites retained %d lease entries", len(service.leases))
	}
}

func TestBackupLeaseWaitsForLegacyWrites(t *testing.T) {
	service, _, _, _ := policyServiceFixture(t)
	release, err := service.LegacyLease("site_policy")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	called := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- service.withBackupLock(context.Background(), "site_policy", func() error { close(called); return nil })
	}()
	awaitLeaseReferences(t, service, "site_policy", 2)
	select {
	case <-called:
		t.Fatal("backup callback ran while legacy write was pending")
	default:
	}
	release()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("backup did not resume after legacy write")
	}
}
