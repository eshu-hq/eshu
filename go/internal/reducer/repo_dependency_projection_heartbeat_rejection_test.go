// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRepoDependencyLeaseHeartbeatRecordsExplicitRejectionDespiteConcurrentStop(t *testing.T) {
	t.Parallel()

	leases := &blockingRejectPartitionLeaseManager{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	runner := RepoDependencyProjectionRunner{
		LeaseManager: leases,
		Config:       RepoDependencyProjectionRunnerConfig{LeaseTTL: time.Millisecond},
	}
	heartbeatCtx, stopHeartbeat := runner.startLeaseHeartbeat(context.Background())

	select {
	case <-leases.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("heartbeat renewal did not enter ClaimPartitionLease")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- stopHeartbeat() }()

	select {
	case <-heartbeatCtx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("orderly heartbeat stop did not cancel its context")
	}
	close(leases.release)

	select {
	case err := <-stopDone:
		if err == nil {
			t.Fatal("stopHeartbeat() error = nil, want explicit lease rejection surfaced")
		}
		if !strings.Contains(err.Error(), "heartbeat") {
			t.Fatalf("stopHeartbeat() error = %v, want heartbeat attribution", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stopHeartbeat() did not return")
	}
}

type blockingRejectPartitionLeaseManager struct {
	entered chan struct{}
	release chan struct{}
}

func (m *blockingRejectPartitionLeaseManager) ClaimPartitionLease(
	context.Context,
	string,
	int,
	int,
	string,
	time.Duration,
) (bool, error) {
	close(m.entered)
	<-m.release
	return false, nil
}

func (*blockingRejectPartitionLeaseManager) ReleasePartitionLease(
	context.Context,
	string,
	int,
	int,
	string,
) error {
	return nil
}
