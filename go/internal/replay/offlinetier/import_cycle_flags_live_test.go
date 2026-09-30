// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// import_cycle_flags_live_test.go is the backend-required graph-truth proof for
// #7346's flag-consuming half of file_import_cycles. The reader projects the
// IMPORTS edge flags raw, and the whole design rests on how a real backend hands
// back a property that was never written: it must come back as a null the reader
// classifies as unknown, never as false. A fixture-row unit test cannot show
// that, so this seeds real edges and reads them through the production
// imports.CycleRows and response shaper.
//
// The edges carry exactly the properties the projector writes (#7345): the three
// flags as explicit booleans on every edge, plus one legacy edge written without
// them. It seeds edges directly instead of going through the canonical writer;
// TestCanonicalImportEdgesGraphTruth covers the writer half, and the two are
// not joined into one writer-to-reader case.
//
// Skills active: golang-engineering, cypher-query-rigor, eshu-correlation-truth.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/imports"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const cycleFlagsRepoID = "7346-cycle-flags"

// liveGraphQuery adapts the live tier's executor to the reader's GraphQuery
// port, so the production reader runs against the real backend.
type liveGraphQuery struct{ exec liveExecutor }

func (g liveGraphQuery) Run(ctx context.Context, cypherText string, params map[string]any) ([]map[string]any, error) {
	return g.exec.Run(ctx, cypherText, params)
}

func (g liveGraphQuery) RunSingle(ctx context.Context, cypherText string, params map[string]any) (map[string]any, error) {
	rows, err := g.exec.Run(ctx, cypherText, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// cycleFlagsFixture names every file, and every IMPORTS edge with the flag
// properties the projector writes. A nil property is omitted entirely, which is
// what an edge written before the flags existed looks like in the graph.
type cycleFlagsEdge struct {
	from, to                  string
	typeOnly, deferred, infer any
}

func cycleFlagsEdges() []cycleFlagsEdge {
	f := func(b bool) any { return b }
	return []cycleFlagsEdge{
		// A runtime cycle: every flag explicitly false.
		{"rt_a", "rt_b", f(false), f(false), f(false)},
		{"rt_b", "rt_a", f(false), f(false), f(false)},
		// A TYPE_CHECKING cycle: closed only through a type-only import.
		{"tc_a", "tc_b", f(false), f(false), f(false)},
		{"tc_b", "tc_a", f(true), f(false), f(false)},
		// A function-local cycle: closed only through a deferred import.
		{"fl_a", "fl_b", f(false), f(false), f(false)},
		{"fl_b", "fl_a", f(false), f(true), f(false)},
		// A legacy cycle: one edge has no flag properties at all.
		{"lg_a", "lg_b", f(false), f(false), f(false)},
		{"lg_b", "lg_a", nil, nil, nil},
		// An inferred cycle: one edge is a guess.
		{"in_a", "in_b", f(false), f(false), f(false)},
		{"in_b", "in_a", f(false), f(false), f(true)},
		// The Python relative-import fallback "./xi_b" never equals a module name,
		// so it must not create a hop to the file xi_b, inferred or not.
		{"xi_a", "./xi_b", f(false), f(false), f(true)},
		{"xi_b", "xi_a", f(false), f(false), f(false)},
	}
}

func cycleFlagsCleanup(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, s := range []string{
		`MATCH (f:File) WHERE f.repo_id = $repo_id DETACH DELETE f`,
		`MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`,
		// Module nodes are global; delete only the names this fixture created.
		`MATCH (m:Module) WHERE m.name STARTS WITH '7346flags_' DETACH DELETE m`,
	} {
		if err := exec.Execute(ctx, cypher.Statement{Cypher: s, Parameters: map[string]any{"repo_id": cycleFlagsRepoID}}); err != nil {
			t.Fatalf("cleanup %q: %v", s, err)
		}
	}
}

func seedCycleFlagsGraph(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	// File and module names carry a prefix so a shared backend's other data is
	// never read or deleted.
	name := func(base string) string { return "7346flags_" + base }
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `CREATE (:Repository {id: $repo_id, name: 'cycle-flags'})`,
		Parameters: map[string]any{"repo_id": cycleFlagsRepoID},
	}); err != nil {
		t.Fatalf("seed repository: %v", err)
	}
	files := map[string]bool{}
	modules := map[string]bool{}
	for _, e := range cycleFlagsEdges() {
		files[e.from] = true
		modules[name(e.to)] = true
	}
	for base := range files {
		modules[name(base)] = true
	}
	for base := range files {
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher: `MATCH (r:Repository {id: $repo_id})
CREATE (r)-[:REPO_CONTAINS]->(:File {path: $path, relative_path: $rel, name: $rel, language: 'python', repo_id: $repo_id})`,
			Parameters: map[string]any{"repo_id": cycleFlagsRepoID, "path": "/cycle-flags/" + name(base) + ".py", "rel": name(base) + ".py"},
		}); err != nil {
			t.Fatalf("seed file %s: %v", base, err)
		}
	}
	for module := range modules {
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher:     `CREATE (:Module {name: $name, lang: 'python'})`,
			Parameters: map[string]any{"name": module},
		}); err != nil {
			t.Fatalf("seed module %s: %v", module, err)
		}
	}
	for _, e := range cycleFlagsEdges() {
		props := map[string]any{"line_number": 1}
		for key, value := range map[string]any{"type_only": e.typeOnly, "deferred": e.deferred, "inferred": e.infer} {
			if value != nil {
				props[key] = value
			}
		}
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher: `MATCH (f:File {path: $path}), (m:Module {name: $module, lang: 'python'})
CREATE (f)-[r:IMPORTS]->(m) SET r += $props`,
			Parameters: map[string]any{"path": "/cycle-flags/" + name(e.from) + ".py", "module": name(e.to), "props": props},
		}); err != nil {
			t.Fatalf("seed edge %s->%s: %v", e.from, e.to, err)
		}
	}
}

// TestFileImportCyclesFlagTruthAgainstARealGraph reads the seeded graph through
// the production reader. A cycle closed only through a type-only or a deferred
// import is absent, a legacy edge with no flag properties yields flags_unknown
// (never runtime), an inferred edge yields ambiguous, an explicit-false cycle is
// runtime, the relative-import fallback closes nothing, and coverage counts add up.
func TestFileImportCyclesFlagTruthAgainstARealGraph(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the cycle-flag proof against a real graph backend", liveTierEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	cycleFlagsCleanup(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cycleFlagsCleanup(cleanCtx, t, exec)
	})
	seedCycleFlagsGraph(ctx, t, exec)

	req := codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles",
		RepoID:    cycleFlagsRepoID,
		Limit:     50,
		Access:    querycontract.RepositoryAccessFilter{AllScopes: true},
	}
	rows, enumeration, err := imports.CycleRows(ctx, liveGraphQuery{exec: exec}, req)
	if err != nil {
		t.Fatalf("CycleRows: %v", err)
	}
	resp := codemodel.ImportDependencyResponseWithCycleEnumeration(req, rows, enumeration)

	cycles, _ := resp["cycles"].([]map[string]any)
	got := map[string]string{}
	for _, cycle := range cycles {
		path, _ := cycle["cycle_path"].([]string)
		got[strings.TrimPrefix(path[0], "7346flags_")] = fmt.Sprint(cycle["cycle_label"])
	}
	want := map[string]string{
		"rt_a.py": "runtime",       // every flag explicitly false
		"lg_a.py": "flags_unknown", // one edge has no flag properties
		"in_a.py": "ambiguous",     // one edge is inferred
	}
	keys := func(m map[string]string) []string {
		out := make([]string, 0, len(m))
		for k, v := range m {
			out = append(out, k+"="+v)
		}
		sort.Strings(out)
		return out
	}
	if strings.Join(keys(got), ",") != strings.Join(keys(want), ",") {
		t.Fatalf("cycles by first file and label = %v, want %v (no type-only, deferred, or ./x cycle)", keys(got), keys(want))
	}

	coverage, _ := resp["coverage"].(map[string]any)
	flags, _ := coverage["cycle_edge_flags"].(map[string]any)
	wantCounts := map[string]int{
		"edges_considered":   12,
		"type_only_excluded": 1,
		"deferred_excluded":  1,
		"inferred":           2, // in_b->in_a and xi_a->./xi_b
		"flags_unknown":      1,
	}
	for key, count := range wantCounts {
		if flags[key] != count {
			t.Errorf("coverage.cycle_edge_flags.%s = %#v, want %d", key, flags[key], count)
		}
	}
	t.Logf("cycles=%v coverage=%v", keys(got), flags)
}
