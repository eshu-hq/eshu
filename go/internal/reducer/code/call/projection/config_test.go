// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

func TestCodeCallProjectionRunnerConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := RunnerConfig{}
	if got := cfg.pollInterval(); got != worker.DefaultPollInterval {
		t.Fatalf("pollInterval() = %v, want %v", got, worker.DefaultPollInterval)
	}
	if got := cfg.leaseTTL(); got != worker.DefaultLeaseTTL {
		t.Fatalf("leaseTTL() = %v, want %v", got, worker.DefaultLeaseTTL)
	}
	if got := cfg.batchLimit(); got != worker.DefaultBatchLimit {
		t.Fatalf("batchLimit() = %d, want %d", got, worker.DefaultBatchLimit)
	}
	if got := cfg.partitionCount(); got != 1 {
		t.Fatalf("partitionCount() = %d, want 1", got)
	}
	if got := cfg.workers(); got != 1 {
		t.Fatalf("workers() = %d, want 1", got)
	}
	if got := cfg.acceptanceScanLimit(); got != DefaultAcceptanceScanLimit {
		t.Fatalf("acceptanceScanLimit() = %d, want %d", got, DefaultAcceptanceScanLimit)
	}
	if got := cfg.leaseOwner(); got != DefaultLeaseOwnerPrefix {
		t.Fatalf("leaseOwner() = %q, want %q", got, DefaultLeaseOwnerPrefix)
	}
}

func TestCodeCallProjectionRunnerConfigWorkersClampToPartitionCount(t *testing.T) {
	t.Parallel()

	cfg := RunnerConfig{
		PartitionCount: 4,
		Workers:        10,
	}

	if got, want := cfg.workers(), 4; got != want {
		t.Fatalf("workers() = %d, want partition count %d", got, want)
	}
}

func TestCodeCallProjectionRunnerConfigAcceptanceScanLimitHonorsBatchFloor(t *testing.T) {
	t.Parallel()

	cfg := RunnerConfig{
		BatchLimit:          500,
		AcceptanceScanLimit: 100,
	}

	if got, want := cfg.acceptanceScanLimit(), 500; got != want {
		t.Fatalf("acceptanceScanLimit() = %d, want batch floor %d", got, want)
	}
}

func TestCodeCallProjectionRunnerValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		runner Runner
	}{
		{
			name:   "missing intent reader",
			runner: Runner{LeaseManager: &fakeCodeCallIntentStore{leaseGranted: true}, EdgeWriter: &recordingCodeCallProjectionEdgeWriter{}, AcceptedGen: func(sharedintent.AcceptanceKey) (string, bool) { return "", false }},
		},
		{
			name:   "missing lease manager",
			runner: Runner{IntentReader: &fakeCodeCallIntentStore{leaseGranted: true}, EdgeWriter: &recordingCodeCallProjectionEdgeWriter{}, AcceptedGen: func(sharedintent.AcceptanceKey) (string, bool) { return "", false }},
		},
		{
			name:   "missing edge writer",
			runner: Runner{IntentReader: &fakeCodeCallIntentStore{leaseGranted: true}, LeaseManager: &fakeCodeCallIntentStore{leaseGranted: true}, AcceptedGen: func(sharedintent.AcceptanceKey) (string, bool) { return "", false }},
		},
		{
			name:   "missing accepted generation lookup",
			runner: Runner{IntentReader: &fakeCodeCallIntentStore{leaseGranted: true}, LeaseManager: &fakeCodeCallIntentStore{leaseGranted: true}, EdgeWriter: &recordingCodeCallProjectionEdgeWriter{}},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.runner.validate(); err == nil {
				t.Fatal("validate() error = nil, want non-nil")
			}
		})
	}
}
