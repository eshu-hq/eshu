// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// TestCodeCallProjectionRunnerOrderlyStopDoesNotMisreportInFlightRenewalCancellation
// is the code-call half of the reducer root's former
// lease_heartbeat_orderly_stop_test.go, which split when the runner moved
// out of root (issue #6061): the ProcessPartitionOnce half stays with its
// subject.
func TestCodeCallProjectionRunnerOrderlyStopDoesNotMisreportInFlightRenewalCancellation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	row := codeCallProjectionTestRow("intent-code-call-cancel", "gen-1", now)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain:     []sharedintent.Row{row},
		pendingByAcceptance: map[string][]sharedintent.Row{"scope-a|repo-a|run-1": {row}},
	}
	leases := newInFlightCancellationLeaseManager()
	runner := Runner{
		IntentReader: reader,
		LeaseManager: leases,
		EdgeWriter:   waitForLeaseRenewalWriter{renewalStarted: leases.renewalStarted},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		ReadinessLookup: func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) {
			return true, true
		},
		Config: RunnerConfig{
			LeaseTTL:   2 * time.Millisecond,
			BatchLimit: 10,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runner.processOnce(ctx, now)
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil after orderly stop cancels an in-flight renewal", err)
	}
	if !result.LeaseAcquired {
		t.Fatal("LeaseAcquired = false, want true")
	}
	if got, want := len(reader.marked), 1; got != want {
		t.Fatalf("completed intents = %d, want %d", got, want)
	}
	if !leases.wasReleased() {
		t.Fatal("partition lease was not released")
	}
}

// inFlightCancellationLeaseManager grants the initial claim, then blocks the
// first renewal until the heartbeat's own context is canceled by orderly
// shutdown. The renewalStarted close is a happens-before signal: production
// work cannot complete until a renewal is genuinely in flight.
type inFlightCancellationLeaseManager struct {
	mu             sync.Mutex
	claimCount     int
	released       bool
	renewalStarted chan struct{}
	renewalOnce    sync.Once
}

func newInFlightCancellationLeaseManager() *inFlightCancellationLeaseManager {
	return &inFlightCancellationLeaseManager{renewalStarted: make(chan struct{})}
}

func (m *inFlightCancellationLeaseManager) ClaimPartitionLease(
	ctx context.Context,
	_ string,
	_ int,
	_ int,
	_ string,
	_ time.Duration,
) (bool, error) {
	m.mu.Lock()
	m.claimCount++
	claimCount := m.claimCount
	m.mu.Unlock()
	if claimCount == 1 {
		return true, nil
	}

	m.renewalOnce.Do(func() { close(m.renewalStarted) })
	<-ctx.Done()
	return false, ctx.Err()
}

func (m *inFlightCancellationLeaseManager) ReleasePartitionLease(
	context.Context,
	string,
	int,
	int,
	string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.released = true
	return nil
}

func (m *inFlightCancellationLeaseManager) wasReleased() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.released
}

type waitForLeaseRenewalWriter struct {
	renewalStarted <-chan struct{}
}

func (w waitForLeaseRenewalWriter) RetractEdges(
	context.Context,
	string,
	[]sharedintent.Row,
	string,
) error {
	return nil
}

func (w waitForLeaseRenewalWriter) WriteEdges(
	ctx context.Context,
	_ string,
	_ []sharedintent.Row,
	_ string,
) (sharedintent.WriteReport, error) {
	select {
	case <-w.renewalStarted:
		return sharedintent.WriteReport{}, nil
	case <-ctx.Done():
		return sharedintent.WriteReport{}, ctx.Err()
	}
}
