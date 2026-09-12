// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestProcessPartitionOnceOrderlyStopDoesNotMisreportInFlightRenewalCancellation
// is the ProcessPartitionOnce half of this file; the code-call half moved to
// [projection] (code/call/projection/lease_heartbeat_orderly_stop_test.go)
// with the runner (issue #6061).
func TestProcessPartitionOnceOrderlyStopDoesNotMisreportInFlightRenewalCancellation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{pending: []SharedProjectionIntentRow{
		{
			IntentID:         "intent-shared-cancel",
			ProjectionDomain: "platform_infra",
			PartitionKey:     "pk-a",
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			RepositoryID:     "repo-a",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Payload:          map[string]any{"platform_id": "p1", "action": "upsert"},
			CreatedAt:        now,
		},
	}}
	leases := newInFlightCancellationLeaseManager()
	cfg := PartitionProcessorConfig{
		Domain:         "platform_infra",
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       2 * time.Millisecond,
		BatchLimit:     10,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := ProcessPartitionOnce(
		ctx,
		now,
		cfg,
		leases,
		reader,
		waitForLeaseRenewalWriter{renewalStarted: leases.renewalStarted},
		acceptedGenerationFixed("gen-1", true),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v, want nil after orderly stop cancels an in-flight renewal", err)
	}
	if !result.LeaseAcquired {
		t.Fatal("LeaseAcquired = false, want true")
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
	[]SharedProjectionIntentRow,
	string,
) error {
	return nil
}

func (w waitForLeaseRenewalWriter) WriteEdges(
	ctx context.Context,
	_ string,
	_ []SharedProjectionIntentRow,
	_ string,
) (SharedProjectionWriteReport, error) {
	select {
	case <-w.renewalStarted:
		return SharedProjectionWriteReport{}, nil
	case <-ctx.Done():
		return SharedProjectionWriteReport{}, ctx.Err()
	}
}
