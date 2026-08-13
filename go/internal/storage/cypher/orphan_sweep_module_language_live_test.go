// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
	"time"
)

// TestLiveOrphanSweepModuleSameNameDifferentLanguages runs the orphan sweep
// against the pinned backend for the case that made its key wrong: canonical
// import Module identity is (name, lang), so a Go `time` and a Python `time`
// are two nodes, and a sweep keyed on name alone treated them as one.
//
// Both directions are asserted here, because the composite key had to fix one
// without breaking the other:
//
//   - The disconnected module is deleted even though its same-named sibling in
//     another language is still imported. Under the name-only key it was never
//     marked and never swept, and it stayed in GraphOrphanNodeCounts as a
//     Module orphan count that could not drain.
//   - The connected module and its live IMPORTS edge are untouched. Making the
//     key exact must not turn a connected node into a delete candidate.
//
// The unit-level siblings in orphan_sweep_composite_key_test.go cover the
// paging. This test is what proves the emitted Cypher -- the tuple cursor, the
// map-valued UNWIND rows, and the coalesce(n.lang, ”) comparison -- behaves on
// the real backend rather than only in the fixture.
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

	assertBoltCount(t, ctx, runner,
		`MATCH (n:Module {name: $name}) WHERE n.lang = 'go' RETURN count(n) AS count`,
		map[string]any{"name": name}, 1, "connected go module preserved")
	assertBoltCount(t, ctx, runner,
		`MATCH (n:Module {name: $name}) WHERE n.lang = 'python' RETURN count(n) AS count`,
		map[string]any{"name": name}, 0,
		"disconnected python module swept -- its sweep key is (name, lang), so a connected "+
			"same-named sibling no longer stands in for it")

	// And the IMPORTS edge the connected module holds is intact, so the sweep
	// removed the orphan without damaging live graph truth.
	assertBoltCount(t, ctx, runner,
		`MATCH (:File {path: $path})-[r:IMPORTS]->(:Module) RETURN count(r) AS count`,
		map[string]any{"path": peerPath}, 1, "live IMPORTS edge untouched")
}

// TestLiveOrphanSweepModuleWithNoLanguageIsStillReachable covers the node shape
// the composite key had to be careful about: a canonical Module projected by a
// release before the (name, lang) cutover, whose language was settled with
// `SET m.lang = coalesce(m.lang, row.language)` and therefore has no lang
// property at all when the row carried none.
//
// On the pinned backend a bare `n.lang > $cursor_1` never matches such a node,
// so it would fall outside every page of the S1 read and never be swept -- the
// same under-deletion the composite key exists to remove, reintroduced through
// the back door. The reads and writes compare coalesce(n.lang, ”) instead.
//
// Gate: ESHU_CYPHER_BOLT_DSN must point at a NornicDB backend.
func TestLiveOrphanSweepModuleWithNoLanguageIsStillReachable(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const name = "legacy-6102-nolang"
	cleanup := func() {
		_ = boltWriteStatement(ctx, runner,
			`MATCH (n:Module {name: $name}) DETACH DELETE n`, map[string]any{"name": name})
	}
	cleanup()
	t.Cleanup(cleanup)

	if err := boltWriteStatement(ctx, runner,
		`CREATE (:Module {name: $name, evidence_source: 'projector/canonical'})`,
		map[string]any{"name": name}); err != nil {
		t.Fatalf("seed language-less module: %v", err)
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

	assertBoltCount(t, ctx, runner,
		`MATCH (n:Module {name: $name}) RETURN count(n) AS count`,
		map[string]any{"name": name}, 0,
		"a canonical module with no lang property is still paged, marked, and swept")
}
