// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// TestReopenCircuitBreakerParksMassMissPass is the RED test for the
// deferred-maintenance reopen storm bound. Storm shape (eshu QA, Nov 27):
// a 96%-miss pass reopens the whole succeeded floor pool in one pass
// (64→3→50→442→670 reopen waves on pass cadence) because the memo gate
// misses never heal (0 memo writes in 36h) and the miss path reopens
// unconditionally. With the breaker, a same-pass gate outcome that would
// reopen more than half of a non-trivial candidate pool parks the pass
// instead: zero reopen execs, one alertable log line, and the rows stay
// succeeded for the next pass to retry.
func TestReopenCircuitBreakerParksMassMissPass(t *testing.T) {
	t.Parallel()

	// 50 succeeded candidates, skip-set covering only 2 partitions:
	// a 96% miss that must park, not reopen 48 rows.
	items := make([][3]string, 0, 50)
	skipped := map[backfill.ScopeGenerationPartition]struct{}{}
	for i := 0; i < 50; i++ {
		scope := "scope-mass"
		gen := "gen-mass"
		if i < 2 {
			scope = "scope-hit"
			gen = "gen-hit"
		}
		id := "wi-mass"
		items = append(items, [3]string{id + "-" + string(rune('a'+i/26)) + string(rune('0'+i%26)), scope, gen})
	}
	skipped[backfill.ScopeGenerationPartition{ScopeID: "scope-hit", GenerationID: "gen-hit"}] = struct{}{}

	for _, domain := range []string{"deployment_mapping", "code_import_repo_edge"} {
		t.Run(domain, func(t *testing.T) {
			// Sequential on purpose: both subtests redirect the global
			// log sink to assert the park line, and parallel siblings
			// interleave those lines (flaky missing-park-line failure).
			original := log.Writer()
			t.Cleanup(func() { log.SetOutput(original) })
			var buf bytes.Buffer
			log.SetOutput(&buf)

			recorder, tracer := recordingTracer()
			tx := &fakeTx{queryResponses: []queueFakeRows{reopenListingRows(items...)}}
			store := NewIngestionStore(tx)
			var err error
			if domain == "deployment_mapping" {
				err = store.reopenDeploymentMappingWorkItemsWithSkipSet(context.Background(), tracer, nil, skipped)
			} else {
				err = store.reopenCodeImportRepoEdgeWorkItemsWithSkipSet(context.Background(), tracer, nil, skipped)
			}
			if err != nil {
				t.Fatalf("reopen error = %v, want nil", err)
			}
			if len(tx.execs) != 0 {
				t.Fatalf("reopen execs = %d, want 0 (a 96%%-miss pass must park, not reopen the floor)", len(tx.execs))
			}
			got := singleReopenSpan(t, recorder)
			if got.ints["reopened"] != 0 {
				t.Fatalf("reopen span reopened = %d, want 0", got.ints["reopened"])
			}
			if got.ints["parked_by_breaker"] != 48 {
				t.Fatalf("reopen span parked_by_breaker = %d, want 48", got.ints["parked_by_breaker"])
			}
			output := buf.String()
			if !strings.Contains(output, "maintenance_reopen_circuit_breaker_open") ||
				!strings.Contains(output, "domain="+domain) {
				t.Fatalf("missing circuit-breaker park line for domain %s\noutput:\n%s", domain, output)
			}
		})
	}
}

// TestReopenCircuitBreakerLetsSmallPoolsThrough pins the breaker's floor:
// below the minimum candidate pool a total miss still reopens (tiny corpora
// and one-shot passes must not stall on the breaker).
func TestReopenCircuitBreakerLetsSmallPoolsThrough(t *testing.T) {
	t.Parallel()

	items := reopenListingRows(
		[3]string{"wi-1", "scope-a", "gen-a"},
		[3]string{"wi-2", "scope-b", "gen-b"},
		[3]string{"wi-3", "scope-c", "gen-c"},
	)
	// Empty but non-nil: the gate ran and found zero redundant partitions.
	skipped := map[backfill.ScopeGenerationPartition]struct{}{}

	recorder, tracer := recordingTracer()
	tx := &fakeTx{queryResponses: []queueFakeRows{items}}
	store := NewIngestionStore(tx)
	if err := store.reopenDeploymentMappingWorkItemsWithSkipSet(context.Background(), tracer, nil, skipped); err != nil {
		t.Fatalf("reopen error = %v, want nil", err)
	}
	if len(tx.execs) != 3 {
		t.Fatalf("reopen execs = %d, want 3 (small pools bypass the breaker)", len(tx.execs))
	}
	got := singleReopenSpan(t, recorder)
	if got.ints["reopened"] != 3 {
		t.Fatalf("reopen span reopened = %d, want 3", got.ints["reopened"])
	}
}

// TestReopenCircuitBreakerExemptsStandaloneNilSkipSet pins the bootstrap
// contract: a nil skip-set is an explicit standalone one-shot with no
// periodic loop behind it, so the breaker never applies there, however
// large the pool.
func TestReopenCircuitBreakerExemptsStandaloneNilSkipSet(t *testing.T) {
	t.Parallel()

	items := make([][3]string, 0, 50)
	for i := 0; i < 50; i++ {
		items = append(items, [3]string{"wi-standalone", "scope-s", "gen-s"})
	}
	// Distinct work item ids: the reopen loop issues one exec per row.
	for i := range items {
		items[i][0] = "wi-standalone-" + string(rune('a'+i/26)) + string(rune('0'+i%26))
	}

	tx := &fakeTx{queryResponses: []queueFakeRows{reopenListingRows(items...)}}
	store := NewIngestionStore(tx)
	if err := store.reopenDeploymentMappingWorkItemsWithSkipSet(context.Background(), nil, nil, nil); err != nil {
		t.Fatalf("reopen error = %v, want nil", err)
	}
	if len(tx.execs) != 50 {
		t.Fatalf("reopen execs = %d, want 50 (nil skip-set reopens unconditionally)", len(tx.execs))
	}
}

// TestEvaluateReopenCircuitBreakerThresholds pins the decision boundary:
// park only above the minimum pool AND strictly above the miss fraction.
func TestEvaluateReopenCircuitBreakerThresholds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		candidates int
		toReopen   int
		wantPark   bool
	}{
		{"empty_pool", 0, 0, false},
		{"below_min_pool_total_miss", maintenanceReopenBreakerMinCandidates - 1, maintenanceReopenBreakerMinCandidates - 1, false},
		{"at_min_pool_total_miss", maintenanceReopenBreakerMinCandidates, maintenanceReopenBreakerMinCandidates, true},
		{"at_min_pool_half_miss", maintenanceReopenBreakerMinCandidates, maintenanceReopenBreakerMinCandidates / 2, false},
		{"large_pool_96pct_miss", 16844, 16141, true},
		{"large_pool_4pct_miss", 16844, 703, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := evaluateReopenCircuitBreaker(tc.candidates, tc.toReopen); got != tc.wantPark {
				t.Fatalf("evaluateReopenCircuitBreaker(%d, %d) = %v, want %v",
					tc.candidates, tc.toReopen, got, tc.wantPark)
			}
		})
	}
}
