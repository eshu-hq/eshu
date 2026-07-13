// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

// TestCodeCallProjectionRunnerReleasesLeaseWithLiveContext proves an empty
// code-call partition releases its lease after stopping the heartbeat. The
// release must use the caller context because stopping the heartbeat cancels
// the derived lease context before the deferred release executes.
func TestCodeCallProjectionRunnerReleasesLeaseWithLiveContext(t *testing.T) {
	t.Parallel()

	reader := &fakeCodeCallIntentStore{}
	lease := &ctxCheckingLeaseManager{claimResult: true}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		LeaseManager: lease,
		EdgeWriter:   &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: CodeCallProjectionRunnerConfig{
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
