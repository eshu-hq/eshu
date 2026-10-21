// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package orphan

import (
	"context"
	"testing"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// liveBoltSweepExecutor adapts the bolt test runner to the sweep's
// single-statement Executor surface.
type liveBoltSweepExecutor struct {
	runner *boltRetractTestRunner
}

// Execute runs one sweep statement in an ExecuteWrite transaction.
func (e *liveBoltSweepExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	return e.runner.runCypherGroup(ctx, stmt)
}

// liveBoltSweepReader answers the sweep's S1/S2 reads against the live
// backend, so the cycle under test runs the production read shapes while
// the clock stays injected through the policy.
type liveBoltSweepReader struct {
	runner *boltRetractTestRunner
}

// Run executes a sweep read and returns its rows untouched.
func (r *liveBoltSweepReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return r.runner.runCypher(ctx, cypher, params)
}

// TestRetirementDemotedRepositoryReapedAfterTTLWithInjectedClock is the
// #8659 AC2 proof: a Repository node the G6 demotion left behind (evidence
// source retirement/repository, path cleared) is reaped by the orphan sweep
// once no owner names it and its mark ages past the TTL, while a
// projector/canonical Repository node is never marked and never swept. The
// clock is injected through the policy: no step sleeps for the TTL.
func TestRetirementDemotedRepositoryReapedAfterTTLWithInjectedClock(t *testing.T) {
	runner := openBoltTestRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	now := time.Unix(1_800_000_000, 0).UTC()
	ttl := time.Hour
	demotedID := "repo-sweep-demoted"
	liveID := "repo-sweep-live"

	seed := `CREATE (d:Repository {id: $demotedID, evidence_source: 'retirement/repository', generation_id: $sentinel})
CREATE (l:Repository {id: $liveID, path: '/live', evidence_source: 'projector/canonical', scope_id: 's-live', generation_id: 'g1'})`
	if err := runner.runCypherSingle(ctx, sourcecypher.Statement{Cypher: seed, Parameters: map[string]any{
		"demotedID": demotedID, "liveID": liveID, "sentinel": "retired:rr_sweep",
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_ = runner.runCypherSingle(cleanupCtx, sourcecypher.Statement{
			Cypher:     `MATCH (r:Repository) WHERE r.id IN $ids DETACH DELETE r`,
			Parameters: map[string]any{"ids": []string{demotedID, liveID}},
		})
	})

	store := NewOrphanSweepStore(&liveBoltSweepExecutor{runner: runner}, &liveBoltSweepReader{runner: runner})
	policyAt := func(clock time.Time) OrphanSweepPolicy {
		return OrphanSweepPolicy{
			OrphanTTL:  ttl,
			BatchLimit: 100,
			CountLimit: 1000,
			Labels:     []string{string(OrphanSweepLabelRepository)},
			Now:        clock,
		}
	}
	markerOf := func(id string) any {
		rows, err := runner.runCypher(ctx,
			`MATCH (r:Repository {id: $id}) RETURN r.eshu_orphan_observed_at_unix AS marker`,
			map[string]any{"id": id})
		if err != nil {
			t.Fatalf("marker read %s: %v", id, err)
		}
		if len(rows) != 1 {
			return "absent-node"
		}
		return rows[0]["marker"]
	}
	exists := func(id string) bool {
		rows, err := runner.runCypher(ctx,
			`MATCH (r:Repository {id: $id}) RETURN count(r) AS count`,
			map[string]any{"id": id})
		if err != nil {
			t.Fatalf("exists read %s: %v", id, err)
		}
		count, _ := rows[0]["count"].(int64)
		return count == 1
	}

	// Cycle 1 at T0: the demoted node is marked, nothing is deleted, and
	// the canonical node is not marked at all (the S4 guard excludes
	// projector/canonical at write time).
	result, err := store.SweepOrphanNodes(ctx, policyAt(now))
	if err != nil {
		t.Fatalf("cycle 1 SweepOrphanNodes: %v", err)
	}
	if result.Marked[string(OrphanSweepLabelRepository)] != 1 {
		t.Fatalf("cycle 1 marked = %d, want 1 (the demoted node only)", result.Marked[string(OrphanSweepLabelRepository)])
	}
	if result.Deleted[string(OrphanSweepLabelRepository)] != 0 {
		t.Fatalf("cycle 1 deleted = %d, want 0 (TTL has not passed)", result.Deleted[string(OrphanSweepLabelRepository)])
	}
	if marker := markerOf(demotedID); marker != now.Unix() {
		t.Fatalf("demoted marker = %v, want %d", marker, now.Unix())
	}
	if marker := markerOf(liveID); marker != nil {
		t.Fatalf("live marker = %v, want NULL (canonical nodes are never marked)", marker)
	}

	// Cycle 2 before the TTL: the mark ages but the node survives.
	if _, err := store.SweepOrphanNodes(ctx, policyAt(now.Add(30*time.Minute))); err != nil {
		t.Fatalf("cycle 2 SweepOrphanNodes: %v", err)
	}
	if !exists(demotedID) {
		t.Fatal("demoted node swept 30m after marking with a 1h TTL")
	}

	// Cycle 3 past the TTL: the demoted node is reaped and reported, the
	// canonical node survives unmarked.
	result, err = store.SweepOrphanNodes(ctx, policyAt(now.Add(ttl+time.Minute)))
	if err != nil {
		t.Fatalf("cycle 3 SweepOrphanNodes: %v", err)
	}
	if result.Deleted[string(OrphanSweepLabelRepository)] != 1 {
		t.Fatalf("cycle 3 deleted = %d, want 1 (the aged demoted node)", result.Deleted[string(OrphanSweepLabelRepository)])
	}
	found := false
	for _, id := range result.DeletedRepositoryIDs {
		if id == demotedID {
			found = true
		}
	}
	if !found {
		t.Fatalf("DeletedRepositoryIDs = %v, want the demoted id reported", result.DeletedRepositoryIDs)
	}
	if exists(demotedID) {
		t.Fatal("demoted node still present after the TTL passed")
	}
	if !exists(liveID) {
		t.Fatal("canonical node swept: the guard must exclude projector/canonical at delete time")
	}
	if marker := markerOf(liveID); marker != nil {
		t.Fatalf("live marker = %v, want NULL after all cycles", marker)
	}
}
