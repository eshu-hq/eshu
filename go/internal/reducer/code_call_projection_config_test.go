// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestCodeCallProjectionRunnerConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := CodeCallProjectionRunnerConfig{}
	if got := cfg.pollInterval(); got != defaultSharedPollInterval {
		t.Fatalf("pollInterval() = %v, want %v", got, defaultSharedPollInterval)
	}
	if got := cfg.leaseTTL(); got != defaultLeaseTTL {
		t.Fatalf("leaseTTL() = %v, want %v", got, defaultLeaseTTL)
	}
	if got := cfg.batchLimit(); got != defaultBatchLimit {
		t.Fatalf("batchLimit() = %d, want %d", got, defaultBatchLimit)
	}
	if got := cfg.partitionCount(); got != 1 {
		t.Fatalf("partitionCount() = %d, want 1", got)
	}
	if got := cfg.workers(); got != 1 {
		t.Fatalf("workers() = %d, want 1", got)
	}
	if got := cfg.acceptanceScanLimit(); got != DefaultCodeCallAcceptanceScanLimit {
		t.Fatalf("acceptanceScanLimit() = %d, want %d", got, DefaultCodeCallAcceptanceScanLimit)
	}
	if got := cfg.leaseOwner(); got != defaultCodeCallLeaseOwner {
		t.Fatalf("leaseOwner() = %q, want %q", got, defaultCodeCallLeaseOwner)
	}
}

func TestCodeCallProjectionRunnerConfigWorkersClampToPartitionCount(t *testing.T) {
	t.Parallel()

	cfg := CodeCallProjectionRunnerConfig{
		PartitionCount: 4,
		Workers:        10,
	}

	if got, want := cfg.workers(), 4; got != want {
		t.Fatalf("workers() = %d, want partition count %d", got, want)
	}
}

func TestCodeCallProjectionRunnerConfigAcceptanceScanLimitHonorsBatchFloor(t *testing.T) {
	t.Parallel()

	cfg := CodeCallProjectionRunnerConfig{
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
		runner CodeCallProjectionRunner
	}{
		{
			name:   "missing intent reader",
			runner: CodeCallProjectionRunner{LeaseManager: &fakeCodeCallIntentStore{leaseGranted: true}, EdgeWriter: &recordingCodeCallProjectionEdgeWriter{}, AcceptedGen: func(SharedProjectionAcceptanceKey) (string, bool) { return "", false }},
		},
		{
			name:   "missing lease manager",
			runner: CodeCallProjectionRunner{IntentReader: &fakeCodeCallIntentStore{leaseGranted: true}, EdgeWriter: &recordingCodeCallProjectionEdgeWriter{}, AcceptedGen: func(SharedProjectionAcceptanceKey) (string, bool) { return "", false }},
		},
		{
			name:   "missing edge writer",
			runner: CodeCallProjectionRunner{IntentReader: &fakeCodeCallIntentStore{leaseGranted: true}, LeaseManager: &fakeCodeCallIntentStore{leaseGranted: true}, AcceptedGen: func(SharedProjectionAcceptanceKey) (string, bool) { return "", false }},
		},
		{
			name:   "missing accepted generation lookup",
			runner: CodeCallProjectionRunner{IntentReader: &fakeCodeCallIntentStore{leaseGranted: true}, LeaseManager: &fakeCodeCallIntentStore{leaseGranted: true}, EdgeWriter: &recordingCodeCallProjectionEdgeWriter{}},
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
