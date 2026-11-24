// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/clock"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestCICDCompletionOwedMarkerFencesOnTheWorkQueueLeaseOwner guards the #10972
// PR-4 wire: the completion-owed marker's UPDATE fences on lease_owner, so its
// owner must be the one the reducer work queue claims with. A mismatch matches
// zero rows, fails every changed ci_cd write and retries it forever. The owner
// is compared with the queue configureReducerQueue builds, not a constant.
func TestCICDCompletionOwedMarkerFencesOnTheWorkQueueLeaseOwner(t *testing.T) {
	t.Parallel()

	writer := newCICDRunCorrelationWriter(beginnerDB{})
	if writer.Beginner == nil || writer.DB == nil {
		t.Fatal("ci_cd writer is missing its beginner or database with a beginner database")
	}
	marker, ok := writer.CompletionOwed.(postgres.CICDCompletionOwedMarker)
	if !ok {
		t.Fatalf("CompletionOwed = %T, want postgres.CICDCompletionOwedMarker", writer.CompletionOwed)
	}
	queue, err := configureReducerQueue(
		nil, runtimecfg.RetryPolicyConfig{}, nil, false, func(string) string { return "" },
		runtimecfg.GraphBackendNornicDB, clock.System(), nil, nil,
	)
	if err != nil {
		t.Fatalf("configureReducerQueue: %v", err)
	}
	if marker.LeaseOwner == "" || marker.LeaseOwner != queue.LeaseOwner {
		t.Fatalf("completion-owed marker lease owner = %q, want the work queue's %q", marker.LeaseOwner, queue.LeaseOwner)
	}

	// Without a beginner the writer keeps the autocommit write: no verdict, no
	// marker, so every ack emits.
	bare := newCICDRunCorrelationWriter(nil)
	if bare.Beginner != nil || bare.CompletionOwed != nil {
		t.Fatal("a database without a beginner must leave the writer's beginner and marker unset")
	}
}
