// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

func TestCodeCallProjectionRunnerWaitsForReducerGraphDrainBeforeLease(t *testing.T) {
	t.Parallel()

	reader := &fakeCodeCallIntentStore{leaseGranted: true}
	runner := Runner{
		IntentReader:      reader,
		LeaseManager:      reader,
		EdgeWriter:        &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:       func(sharedintent.AcceptanceKey) (string, bool) { return "", false },
		ReducerGraphDrain: staticReducerGraphDrain{active: true},
		Config:            RunnerConfig{BatchLimit: 10},
	}

	result, err := runner.processOnce(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil", err)
	}
	if result.BlockedReadiness != 1 {
		t.Fatalf("BlockedReadiness = %d, want 1", result.BlockedReadiness)
	}
	if got := reader.claimsCount(); got != 0 {
		t.Fatalf("lease claims = %d, want 0 while reducer graph work is active", got)
	}
}

// TestCodeCallProjectionRunnerWaitsForCanonicalCodeQuiescence proves a
// cross-repository edge cannot claim a lease before possible callee nodes exist.
func TestCodeCallProjectionRunnerWaitsForCanonicalCodeQuiescence(t *testing.T) {
	t.Parallel()

	reader := &fakeCodeCallIntentStore{leaseGranted: true}
	runner := Runner{
		IntentReader:      reader,
		LeaseManager:      reader,
		EdgeWriter:        &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:       func(sharedintent.AcceptanceKey) (string, bool) { return "", false },
		ReducerGraphDrain: staticReducerGraphDrain{uncommittedCanonical: true},
		Config:            RunnerConfig{BatchLimit: 10},
	}

	result, err := runner.processOnce(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil", err)
	}
	if result.BlockedReadiness != 1 {
		t.Fatalf("BlockedReadiness = %d, want 1", result.BlockedReadiness)
	}
	if got := reader.claimsCount(); got != 0 {
		t.Fatalf("lease claims = %d, want 0 while canonical code scopes are uncommitted", got)
	}
}

func TestCodeCallProjectionRunnerQuiescenceWithoutDrain(t *testing.T) {
	t.Parallel()

	reader := &fakeCodeCallIntentStore{leaseGranted: true}
	runner := Runner{
		IntentReader:        reader,
		LeaseManager:        reader,
		EdgeWriter:          &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:         func(sharedintent.AcceptanceKey) (string, bool) { return "", false },
		CanonicalQuiescence: staticReducerGraphDrain{uncommittedCanonical: true},
		Config:              RunnerConfig{BatchLimit: 10},
	}

	result, err := runner.processOnce(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil", err)
	}
	if result.BlockedReadiness != 1 {
		t.Fatalf("BlockedReadiness = %d, want 1", result.BlockedReadiness)
	}
	if got := reader.claimsCount(); got != 0 {
		t.Fatalf("lease claims = %d, want 0 while canonical code scopes are uncommitted", got)
	}
}

func TestCodeCallProjectionRunnerQuiescenceCheckErrorFailsCycle(t *testing.T) {
	t.Parallel()

	reader := &fakeCodeCallIntentStore{leaseGranted: true}
	runner := Runner{
		IntentReader:      reader,
		LeaseManager:      reader,
		EdgeWriter:        &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:       func(sharedintent.AcceptanceKey) (string, bool) { return "", false },
		ReducerGraphDrain: staticReducerGraphDrain{uncommittedErr: errors.New("test quiescence check failure")},
		Config:            RunnerConfig{BatchLimit: 10},
	}

	if _, err := runner.processOnce(context.Background(), time.Now().UTC()); err == nil {
		t.Fatal("processOnce() error = nil, want quiescence check failure")
	}
	if got := reader.claimsCount(); got != 0 {
		t.Fatalf("lease claims = %d, want 0 when the quiescence check fails", got)
	}
}
