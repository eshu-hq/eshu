// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// --- concurrency tests ---

func TestDrainProjectorConcurrentMultipleItems(t *testing.T) {
	t.Parallel()

	items := make([]projector.ScopeGenerationWork, 10)
	for i := range items {
		items[i] = projector.ScopeGenerationWork{
			Scope: scope.IngestionScope{ScopeID: fmt.Sprintf("scope-%d", i)},
		}
	}

	ws := &concurrentWorkSource{items: items}
	sink := &concurrentWorkSink{}

	err := drainProjector(
		context.Background(),
		ws, &fakeFactStore{}, &fakeProjectionRunner{}, sink,
		nil, 0,
		4, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("drainProjector() error = %v, want nil", err)
	}
	if got := sink.acked.Load(); got != 10 {
		t.Fatalf("drainProjector() acked = %d, want 10", got)
	}
}

func TestDrainProjectorSequentialFallback(t *testing.T) {
	t.Parallel()

	items := []projector.ScopeGenerationWork{
		{Scope: scope.IngestionScope{ScopeID: "s1"}},
		{Scope: scope.IngestionScope{ScopeID: "s2"}},
	}
	ws := &concurrentWorkSource{items: items}
	sink := &concurrentWorkSink{}

	// workers=1 should use sequential path
	err := drainProjector(
		context.Background(),
		ws, &fakeFactStore{}, &fakeProjectionRunner{}, sink,
		nil, 0,
		1, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("drainProjector(workers=1) error = %v, want nil", err)
	}
	if got := sink.acked.Load(); got != 2 {
		t.Fatalf("drainProjector(workers=1) acked = %d, want 2", got)
	}
}

// TestDrainProjectorIsolatesItemFailures verifies the #4464 fix: a per-item
// projection failure is routed to the queue Fail path and does NOT abort the
// run or cancel sibling workers. drainProjector returns a non-nil incomplete
// drain error after every item is handled (acked on success, failed otherwise),
// so none is left orphaned and bootstrap does not claim clean completion.
// (Previously this asserted the buggy fail-fast behavior where one slow/timed-out
// canonical write canceled the shared context and crashed the whole run.)
func TestDrainProjectorIsolatesItemFailures(t *testing.T) {
	t.Parallel()

	projectErr := errors.New("canonical phase-group write (structural_edges): neo4j execute group timed out after 30s")
	items := make([]projector.ScopeGenerationWork, 20)
	for i := range items {
		items[i] = projector.ScopeGenerationWork{
			Scope: scope.IngestionScope{ScopeID: fmt.Sprintf("scope-%d", i)},
		}
	}

	ws := &concurrentWorkSource{items: items}
	runner := &failingProjectionRunner{failAfter: 2, err: projectErr}
	sink := &countingProjectorSink{}

	err := drainProjector(
		context.Background(),
		ws, &fakeFactStore{}, runner, sink,
		nil, 0,
		4, nil, nil, nil,
	)
	// Isolation: siblings are NOT canceled — all 20 items are handled (2 acked,
	// 18 failed) even though 18 projections errored. The run reports incomplete
	// (a non-fatal error) rather than a clean success, so bootstrap does not
	// claim completion while failed work is deferred to retry/dead-letter.
	if err == nil {
		t.Fatal("drainProjector() error = nil, want an incomplete-drain error when items failed")
	}
	if got := sink.acked.Load(); got != 2 {
		t.Fatalf("acked = %d, want 2", got)
	}
	if got := sink.failed.Load(); got != 18 {
		t.Fatalf("failed = %d, want 18 (failed items must route to the queue Fail path, not orphan)", got)
	}
	if got := sink.acked.Load() + sink.failed.Load(); got != 20 {
		t.Fatalf("handled = %d, want all 20 items handled (isolation: no sibling cancel)", got)
	}
}

func TestProjectionWorkerCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  string
		want int
	}{
		{name: "default", env: "", want: -1}, // will be runtime.NumCPU capped at 8
		{name: "explicit_2", env: "2", want: 2},
		{name: "explicit_16", env: "16", want: 16},
		{name: "zero_uses_default", env: "0", want: -1},
		{name: "negative_uses_default", env: "-1", want: -1},
		{name: "invalid_uses_default", env: "abc", want: -1},
		{name: "whitespace_trimmed", env: " 4 ", want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := projectionWorkerCount(func(key string) string {
				if key == "ESHU_PROJECTION_WORKERS" {
					return tt.env
				}
				return ""
			})
			if tt.want == -1 {
				// Default: expect NumCPU capped at 8
				maxDefault := 8
				if got < 1 || got > maxDefault {
					t.Fatalf("projectionWorkerCount(%q) = %d, want 1..%d", tt.env, got, maxDefault)
				}
			} else if got != tt.want {
				t.Fatalf("projectionWorkerCount(%q) = %d, want %d", tt.env, got, tt.want)
			}
		})
	}
}
