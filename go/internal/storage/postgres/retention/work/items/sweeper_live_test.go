// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package items

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestPruneWorkItemsLiveKeepsRowsNeededForFailureReporting proves the
// retention pass never deletes the newest terminal failure of a scope, the
// newest row of a scope, or the row of a scope's active generation, while it
// does delete the superseded rows around them.
func TestPruneWorkItemsLiveKeepsRowsNeededForFailureReporting(t *testing.T) {
	database, ctx := isolatedLiveDB(t)
	seedRun(t, ctx, database, "run-closed", "failed")

	// A permanently failing scope: every cycle failed terminally and nothing
	// ever completed, so its newest failure is unresolved.
	seedRows(t, ctx, database,
		seedRow{id: "perm-0", runID: "run-closed", scopeID: "scope-perm", status: "failed_terminal", ageCycles: 0},
		seedRow{id: "perm-1", runID: "run-closed", scopeID: "scope-perm", status: "failed_terminal", ageCycles: 1},
		seedRow{id: "perm-2", runID: "run-closed", scopeID: "scope-perm", status: "failed_terminal", ageCycles: 2},
		seedRow{id: "perm-3", runID: "run-closed", scopeID: "scope-perm", status: "failed_terminal", ageCycles: 3},
	)
	// A scope that failed and then recovered. Its active generation is an
	// older completed cycle, not the newest row.
	seedRows(t, ctx, database,
		seedRow{id: "heal-0", runID: "run-closed", scopeID: "scope-heal", status: "failed_terminal", ageCycles: 0},
		seedRow{id: "heal-1", runID: "run-closed", scopeID: "scope-heal", status: "completed", ageCycles: 1},
		seedRow{id: "heal-2", runID: "run-closed", scopeID: "scope-heal", status: "failed_terminal", ageCycles: 2},
		seedRow{id: "heal-3", runID: "run-closed", scopeID: "scope-heal", status: "completed", ageCycles: 3},
		seedRow{id: "heal-4", runID: "run-closed", scopeID: "scope-heal", status: "completed", ageCycles: 4},
		seedRow{id: "heal-5", runID: "run-closed", scopeID: "scope-heal", status: "completed", ageCycles: 5},
	)
	seedActiveGeneration(t, ctx, database, "scope-heal", "gen-heal-3")

	sweeper := NewSweeper(postgres.SQLDB{DB: database})
	deleted := drainToWrap(t, ctx, sweeper, 1000)

	want := []string{
		"heal-2", // newest terminal failure of scope-heal
		"heal-3", // scope-heal's active generation
		"heal-5", // newest row of scope-heal
		"perm-3", // newest (unresolved) terminal failure and newest row of scope-perm
	}
	if got := remainingIDs(t, ctx, database); !slices.Equal(got, want) {
		t.Fatalf("remaining work items = %v, want %v", got, want)
	}
	if deleted != 6 {
		t.Fatalf("deleted = %d, want 6", deleted)
	}
	if orphans := orphanClaims(t, ctx, database); orphans != 0 {
		t.Fatalf("orphan claims = %d, want 0 (claims cascade with their work item)", orphans)
	}
}

// TestPruneWorkItemsLiveLeavesLiveAndYoungRowsAlone proves the pass deletes
// only terminal rows of closed runs older than the cutoff: claimed, pending,
// retrying and expired rows, rows of open runs, and young rows survive.
func TestPruneWorkItemsLiveLeavesLiveAndYoungRowsAlone(t *testing.T) {
	database, ctx := isolatedLiveDB(t)
	seedRun(t, ctx, database, "run-closed", "complete")
	seedRun(t, ctx, database, "run-open", "reducer_converging")

	seedRows(t, ctx, database,
		// Control: an old superseded completed row that is eligible.
		seedRow{id: "busy-00-done", runID: "run-closed", scopeID: "scope-busy", status: "completed", ageCycles: 0},
		// Live states are never eligible, even old and in a closed run.
		seedRow{id: "busy-01-claimed", runID: "run-closed", scopeID: "scope-busy", status: "claimed", ageCycles: 1},
		seedRow{id: "busy-02-pending", runID: "run-closed", scopeID: "scope-busy", status: "pending", ageCycles: 2},
		seedRow{id: "busy-03-retrying", runID: "run-closed", scopeID: "scope-busy", status: "failed_retryable", ageCycles: 3},
		seedRow{id: "busy-04-expired", runID: "run-closed", scopeID: "scope-busy", status: "expired", ageCycles: 4},
		// Terminal rows of an open run stay: run reconciliation still counts them.
		seedRow{id: "busy-05-open-done", runID: "run-open", scopeID: "scope-busy", status: "completed", ageCycles: 5},
		seedRow{id: "busy-06-open-failed", runID: "run-open", scopeID: "scope-busy", status: "failed_terminal", ageCycles: 6},
		// Young terminal rows stay until they age past the cutoff.
		seedRow{id: "busy-07-young-done", runID: "run-closed", scopeID: "scope-busy", status: "completed", ageCycles: 7, young: true},
		seedRow{id: "busy-08-young-failed", runID: "run-closed", scopeID: "scope-busy", status: "failed_terminal", ageCycles: 8, young: true},
		seedRow{id: "busy-09-newest", runID: "run-closed", scopeID: "scope-busy", status: "completed", ageCycles: 9, young: true},
	)

	sweeper := NewSweeper(postgres.SQLDB{DB: database})
	if deleted := drainToWrap(t, ctx, sweeper, 1000); deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (only the control row)", deleted)
	}
	got := remainingIDs(t, ctx, database)
	if slices.Contains(got, "busy-00-done") {
		t.Fatal("control row busy-00-done survived; the pass deleted nothing eligible")
	}
	if len(got) != 9 {
		t.Fatalf("remaining work items = %v, want the 9 protected rows", got)
	}
	var activeClaims int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_claims WHERE status = 'active'`).Scan(&activeClaims); err != nil {
		t.Fatalf("count active claims: %v", err)
	}
	if activeClaims != 1 {
		t.Fatalf("active claims = %d, want 1 (the claimed row's lease stays)", activeClaims)
	}
}

// TestPruneWorkItemsLiveRespectsBatchBoundAndIsIdempotent proves one batch
// scans and deletes at most BatchSize rows, a pass stops at MaxBatches, and a
// second complete walk over a pruned table deletes nothing.
func TestPruneWorkItemsLiveRespectsBatchBoundAndIsIdempotent(t *testing.T) {
	database, ctx := isolatedLiveDB(t)
	seedRun(t, ctx, database, "run-closed", "complete")
	var rows []seedRow
	for cycle := range 10 {
		rows = append(rows, seedRow{
			id: "bulk-" + string(rune('a'+cycle)), runID: "run-closed", scopeID: "scope-bulk",
			status: "completed", ageCycles: cycle,
		})
	}
	seedRows(t, ctx, database, rows...)
	sweeper := NewSweeper(postgres.SQLDB{DB: database})

	result, err := sweeper.PruneWorkItems(ctx, liveAsOf, testPolicy(3, 1))
	if err != nil {
		t.Fatalf("PruneWorkItems() error = %v, want nil", err)
	}
	if result.Batches != 1 || result.ScannedRows != 3 || result.Deleted() != 3 || result.Wrapped {
		t.Fatalf("bounded pass = %+v, want 1 batch scanning and deleting 3 rows without wrapping", result)
	}
	if got := len(remainingIDs(t, ctx, database)); got != 7 {
		t.Fatalf("remaining after bounded pass = %d, want 7", got)
	}

	if deleted := drainToWrap(t, ctx, sweeper, 3); deleted != 6 {
		t.Fatalf("deleted by the rest of the walk = %d, want 6 (all but the newest row)", deleted)
	}
	before := remainingIDs(t, ctx, database)
	if !slices.Equal(before, []string{"bulk-j"}) {
		t.Fatalf("remaining after full walk = %v, want [bulk-j]", before)
	}
	if deleted := drainToWrap(t, ctx, sweeper, 3); deleted != 0 {
		t.Fatalf("second full walk deleted %d rows, want 0", deleted)
	}
	if after := remainingIDs(t, ctx, database); !slices.Equal(after, before) {
		t.Fatalf("second full walk changed the table: %v -> %v", before, after)
	}
	size, err := sweeper.WorkItemTableSize(ctx)
	if err != nil {
		t.Fatalf("WorkItemTableSize() error = %v, want nil", err)
	}
	if size.Bytes <= 0 {
		t.Fatalf("WorkItemTableSize().Bytes = %d, want positive", size.Bytes)
	}
}
