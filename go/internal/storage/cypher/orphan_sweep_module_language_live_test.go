// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
	"time"
)

// TestLiveOrphanSweepModuleSameNameDifferentLanguages pins what the orphan
// sweep does now that canonical import Module identity is (name, lang) and its
// sweep key -- n.name -- is therefore no longer unique inside the class the
// sweep owns (`uid IS NULL`).
//
// The sweep's Go-side anti-join keys connectivity by that single string, so two
// same-named modules in different languages share one entry: the pair counts as
// connected when EITHER node has a relationship. The consequence is bounded and
// one-directional, and this test proves the direction:
//
//   - It never deletes a connected node, and never deletes a disconnected node
//     that shares a name with a connected one. That is the safe failure.
//   - A disconnected module whose same-named sibling is still imported is NOT
//     swept. It lingers as a disconnected node, and it stays visible in the
//     GraphOrphanNodeCounts gauge, so an operator sees a Module orphan count
//     that does not drain rather than silent wrong query truth. No query
//     traverses to it: every Module read reaches the node through an IMPORTS or
//     CONTAINS edge it no longer has.
//
// Fixing this properly means giving the sweep a composite key across its S1/S2
// read and its three key-anchored writes, which is a larger change than the
// identity fix and is deliberately not folded in here. This test exists so the
// behavior is pinned and cannot drift silently in either direction.
//
// Gate: ESHU_CYPHER_BOLT_DSN must point at a NornicDB backend.
func TestLiveOrphanSweepModuleSameNameDifferentLanguages(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const (
		name     = "time-5691-langsweep"
		peerPath = "/eshu-test/langsweep/main.go"
	)
	cleanup := func() {
		_ = boltWriteStatement(ctx, runner,
			`MATCH (n:Module {name: $name}) DETACH DELETE n`, map[string]any{"name": name})
		_ = boltWriteStatement(ctx, runner,
			`MATCH (f:File {path: $path}) DETACH DELETE f`, map[string]any{"path": peerPath})
	}
	cleanup()
	t.Cleanup(cleanup)

	// A connected Go module and a disconnected Python module, same name. Both
	// are canonical import modules (no uid), so both are in the sweep's class.
	if err := boltWriteStatement(ctx, runner,
		`CREATE (go:Module {name: $name, lang: 'go', evidence_source: 'projector/canonical'}),
                (f:File {path: $path, evidence_source: 'projector/canonical'}),
                (f)-[:IMPORTS]->(go)`,
		map[string]any{"name": name, "path": peerPath}); err != nil {
		t.Fatalf("seed connected go module: %v", err)
	}
	if err := boltWriteStatement(ctx, runner,
		`CREATE (:Module {name: $name, lang: 'python', evidence_source: 'projector/canonical'})`,
		map[string]any{"name": name}); err != nil {
		t.Fatalf("seed disconnected python module: %v", err)
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

	for cycle := 0; cycle < 2; cycle++ {
		if _, err := store.SweepOrphanNodes(ctx, policy); err != nil {
			t.Fatalf("cycle %d SweepOrphanNodes: %v", cycle+1, err)
		}
		clock = clock.Add(2 * time.Second)
	}

	// Both survive: the connected one because it is connected, the
	// disconnected one because it shares its sweep key with the connected one.
	assertBoltCount(t, ctx, runner,
		`MATCH (n:Module {name: $name}) WHERE n.lang = 'go' RETURN count(n) AS count`,
		map[string]any{"name": name}, 1, "connected go module preserved")
	assertBoltCount(t, ctx, runner,
		`MATCH (n:Module {name: $name}) WHERE n.lang = 'python' RETURN count(n) AS count`,
		map[string]any{"name": name}, 1,
		"disconnected python module retained -- it shares the sweep's name key with a connected sibling")

	// And the IMPORTS edge the connected module holds is intact, so the sweep
	// did not damage live graph truth while leaving the sibling behind.
	assertBoltCount(t, ctx, runner,
		`MATCH (:File {path: $path})-[r:IMPORTS]->(:Module) RETURN count(r) AS count`,
		map[string]any{"path": peerPath}, 1, "live IMPORTS edge untouched")
}
