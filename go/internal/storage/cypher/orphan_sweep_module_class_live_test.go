// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
	"time"
)

// TestLiveOrphanAntiJoinModuleClassRestriction proves the #5313 review finding
// that Module.name is not unique across node classes: canonical imported
// modules are MERGEd on {name} (no uid), while semantic module entities are
// MERGEd on {uid} and also carry a name. It seeds a canonical imported Module
// (name only, orphan) and a connected semantic Module with the SAME name (uid
// set), then asserts the sweep detects and deletes only the canonical orphan
// while the same-name connected semantic module is untouched -- the
// `uid IS NULL` class restriction working end to end on NornicDB.
//
// Gate: ESHU_CYPHER_BOLT_DSN must point at a NornicDB backend.
func TestLiveOrphanAntiJoinModuleClassRestriction(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const (
		name = "index-5313-review"
		peer = "peer-5313-review"
	)
	cleanup := func() {
		_ = boltWriteStatement(ctx, runner,
			`MATCH (n) WHERE n.name = $name OR n.path = $peer DETACH DELETE n`,
			map[string]any{"name": name, "peer": peer})
	}
	cleanup()
	t.Cleanup(cleanup)

	// Canonical imported Module: {name} only, no uid, evidence_source set, orphan.
	if err := boltWriteStatement(ctx, runner,
		`CREATE (:Module {name: $name, evidence_source: 'resolver/import'})`,
		map[string]any{"name": name}); err != nil {
		t.Fatalf("seed canonical import module: %v", err)
	}
	// Semantic Module: SAME name, uid set, connected to a peer.
	if err := boltWriteStatement(ctx, runner,
		`CREATE (m:Module {name: $name, uid: 'sem-5313-review', evidence_source: 'parser/semantic-entities'}),
                (p:File {path: $peer, evidence_source: 'projector/canonical'}),
                (m)-[:CONTAINS]->(p)`,
		map[string]any{"name": name, "peer": peer}); err != nil {
		t.Fatalf("seed semantic module: %v", err)
	}

	clock := time.Unix(1_800_000_000, 0).UTC()
	store := NewOrphanSweepStore(&boltTestExecutor{runner: runner}, &boltOrphanSweepReader{runner: runner})
	store.Now = func() time.Time { return clock }
	policy := OrphanSweepPolicy{
		OrphanTTL:  1 * time.Second,
		BatchLimit: 100,
		CountLimit: 1000,
		Labels:     []string{"Module"},
	}

	// Cycle 1: mark the canonical orphan. The same-name semantic module is
	// excluded by the uid IS NULL class predicate and never becomes a candidate,
	// so it cannot mask the orphan.
	if _, err := store.SweepOrphanNodes(ctx, policy); err != nil {
		t.Fatalf("cycle 1 SweepOrphanNodes: %v", err)
	}
	// Cycle 2: aged past TTL -> sweep the canonical orphan.
	clock = clock.Add(2 * time.Second)
	if _, err := store.SweepOrphanNodes(ctx, policy); err != nil {
		t.Fatalf("cycle 2 SweepOrphanNodes: %v", err)
	}

	assertBoltCount(t, ctx, runner,
		`MATCH (n:Module {name: $name}) WHERE n.uid IS NULL RETURN count(n) AS count`,
		map[string]any{"name": name}, 0, "canonical import module (uid null) deleted")
	assertBoltCount(t, ctx, runner,
		`MATCH (n:Module {uid: 'sem-5313-review'}) RETURN count(n) AS count`,
		nil, 1, "connected same-name semantic module (uid set) preserved")
}
