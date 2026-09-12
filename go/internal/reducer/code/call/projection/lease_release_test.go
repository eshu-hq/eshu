// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"sync"
	"testing"
	"time"
)

// ctxCheckingLeaseManager is a local copy of the reducer root's own test
// double (shared_projection_worker_lease_heartbeat_test.go): it records
// whether the context passed to ReleasePartitionLease was already
// cancelled, the way a real sql.DB.ExecContext call would observe and fail
// fast on a dead context. Go test files cannot share unexported symbols
// across a package boundary (issue #6061).
type ctxCheckingLeaseManager struct {
	mu            sync.Mutex
	claimResult   bool
	released      bool
	releaseCtxErr error
}

func (l *ctxCheckingLeaseManager) ClaimPartitionLease(_ context.Context, _ string, _, _ int, _ string, _ time.Duration) (bool, error) {
	return l.claimResult, nil
}

func (l *ctxCheckingLeaseManager) ReleasePartitionLease(ctx context.Context, _ string, _, _ int, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.released = true
	l.releaseCtxErr = ctx.Err()
	if l.releaseCtxErr != nil {
		return l.releaseCtxErr
	}
	return nil
}

// TestCodeCallProjectionRunnerReleasesLeaseWithLiveContext proves an empty
// code-call partition releases its lease after stopping the heartbeat. The
// release must use the caller context because stopping the heartbeat cancels
// the derived lease context before the deferred release executes.
func TestCodeCallProjectionRunnerReleasesLeaseWithLiveContext(t *testing.T) {
	t.Parallel()

	reader := &fakeCodeCallIntentStore{}
	lease := &ctxCheckingLeaseManager{claimResult: true}
	runner := Runner{
		IntentReader: reader,
		LeaseManager: lease,
		EdgeWriter:   &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: RunnerConfig{
			LeaseTTL:       30 * time.Second,
			BatchLimit:     10,
			PartitionCount: 1,
			Workers:        1,
		},
	}

	if _, err := runner.processPartitionOnce(
		context.Background(),
		time.Date(2026, time.July, 13, 20, 0, 0, 0, time.UTC),
		0,
		1,
	); err != nil {
		t.Fatalf("processPartitionOnce() error = %v", err)
	}

	if lease.releaseCtxErr != nil {
		t.Fatalf("ReleasePartitionLease observed a canceled context: %v", lease.releaseCtxErr)
	}
	if !lease.released {
		t.Fatal("lease was not released")
	}
}
