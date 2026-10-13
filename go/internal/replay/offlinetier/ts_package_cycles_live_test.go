// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// ts_package_cycles_live_test.go is the backend-required graph-truth proof
// for #7348's TypeScript/JavaScript package lift of file_import_cycles. The
// reader projects the projector-owned source_package raw, and the whole lift
// rests on how a real backend hands back that property: it must come back as
// the written string, and a never-written one must come back as a null the
// reader excludes as package-unknown, never as an empty package that would
// collapse every legacy edge onto one node. A fixture-row unit test cannot
// show that, so this seeds real TS edges and reads them through the
// production imports.CycleRows and response shaper.
//
// The edges carry exactly the properties the projector writes: the three
// flags as explicit booleans plus source_package as an explicit string on
// every edge, and one legacy edge written with neither. It seeds edges
// directly instead of going through the canonical writer;
// TestCanonicalImportEdgesGraphTruth covers the writer half, and
// TestCanonicalNodeWriterCarriesImportSourcePackage pins the writer's
// statement and parameters.
//
// Skills active: golang-engineering, cypher-query-rigor, eshu-correlation-truth.
//

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/imports"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const tsCyclesRepoID = "7348-ts-cycles"

// tsCycleSeedEdge names one seeded IMPORTS edge: the importing file, its
// npm package, the imported module, and the properties to SET. A nil props
// map writes only the line number, which is what an edge written before the
// mapping (or the flags) existed looks like in the graph.
type tsCycleSeedEdge struct {
	file   string
	pkg    string
	target string
	props  map[string]any
}

func tsCycleSeedEdges() []tsCycleSeedEdge {
	runtime := map[string]any{"line_number": 1, "type_only": false, "deferred": false, "inferred": false}
	withPkg := func(pkg string) map[string]any {
		props := map[string]any{"source_package": pkg}
		for key, value := range runtime {
			props[key] = value
		}
		return props
	}
	return []tsCycleSeedEdge{
		// A two-package cycle, with a second file in package a proving the
		// collapse reports one cycle, not one per file pair.
		{file: "ta_a", pkg: "7348ts_a", target: "7348ts_b", props: withPkg("7348ts_a")},
		{file: "ta_a2", pkg: "7348ts_a", target: "7348ts_b", props: withPkg("7348ts_a")},
		{file: "tb_b", pkg: "7348ts_b", target: "7348ts_a", props: withPkg("7348ts_b")},
		// A diamond arm that must close no cycle.
		{file: "tc_c", pkg: "7348ts_c", target: "7348ts_a", props: withPkg("7348ts_c")},
		{file: "tc_c", pkg: "7348ts_c", target: "7348ts_b", props: withPkg("7348ts_c")},
		// A legacy edge with no properties at all: unplaceable, closes
		// nothing, counted in coverage.
		{file: "td_d", target: "7348ts_e"},
		{file: "te_e", pkg: "7348ts_e", target: "7348ts_d", props: withPkg("7348ts_e")},
	}
}

func tsCycleCleanup(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, s := range []string{
		`MATCH (f:File) WHERE f.repo_id = $repo_id DETACH DELETE f`,
		`MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`,
		// Module nodes are global; delete only the names this fixture created.
		`MATCH (m:Module) WHERE m.name STARTS WITH '7348ts_' DETACH DELETE m`,
	} {
		if err := exec.Execute(ctx, cypher.Statement{Cypher: s, Parameters: map[string]any{"repo_id": tsCyclesRepoID}}); err != nil {
			t.Fatalf("cleanup %q: %v", s, err)
		}
	}
}

func seedTSCycleGraph(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `CREATE (:Repository {id: $repo_id, name: 'ts-cycles'})`,
		Parameters: map[string]any{"repo_id": tsCyclesRepoID},
	}); err != nil {
		t.Fatalf("seed repository: %v", err)
	}
	files := map[string]bool{}
	modules := map[string]bool{}
	for _, e := range tsCycleSeedEdges() {
		files[e.file] = true
		modules[e.target] = true
		// Every file's own package is a module node too: the collapse joins
		// target names against declaring packages.
		if e.pkg != "" {
			modules[e.pkg] = true
		}
	}
	for base := range files {
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher: `MATCH (r:Repository {id: $repo_id})
CREATE (r)-[:REPO_CONTAINS]->(:File {path: $path, relative_path: $rel, name: $rel, language: 'typescript', repo_id: $repo_id})`,
			Parameters: map[string]any{"repo_id": tsCyclesRepoID, "path": "/ts-cycles/" + base + ".ts", "rel": base + ".ts"},
		}); err != nil {
			t.Fatalf("seed file %s: %v", base, err)
		}
	}
	for module := range modules {
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher:     `CREATE (:Module {name: $name, lang: 'typescript'})`,
			Parameters: map[string]any{"name": module},
		}); err != nil {
			t.Fatalf("seed module %s: %v", module, err)
		}
	}
	for _, e := range tsCycleSeedEdges() {
		props := map[string]any{"line_number": 1}
		for key, value := range e.props {
			props[key] = value
		}
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher: `MATCH (f:File {path: $path}), (m:Module {name: $module, lang: 'typescript'})
CREATE (f)-[r:IMPORTS]->(m) SET r += $props`,
			Parameters: map[string]any{"path": "/ts-cycles/" + e.file + ".ts", "module": e.target, "props": props},
		}); err != nil {
			t.Fatalf("seed edge %s->%s: %v", e.file, e.target, err)
		}
	}
}

// TestFileImportCyclesTSTruthAgainstARealGraph reads the seeded TS package
// graph through the production reader. The a<->b packages report exactly one
// collapsed cycle, the diamond arm and the legacy edge close nothing, the
// legacy edge is counted as missing-package, and the cycle is labelled
// runtime with the TS package query shape.
func TestFileImportCyclesTSTruthAgainstARealGraph(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the TS package-cycle proof against a real graph backend", liveTierEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	tsCycleCleanup(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		tsCycleCleanup(cleanCtx, t, exec)
	})
	seedTSCycleGraph(ctx, t, exec)

	req := codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles",
		RepoID:    tsCyclesRepoID,
		Language:  "typescript",
		Limit:     50,
		Access:    querycontract.RepositoryAccessFilter{AllScopes: true},
	}
	rows, enumeration, err := imports.CycleRows(ctx, liveGraphQuery{exec: exec}, req)
	if err != nil {
		t.Fatalf("CycleRows: %v", err)
	}
	resp := codemodel.ImportDependencyResponseWithCycleEnumeration(req, rows, enumeration)

	cycles, _ := resp["cycles"].([]map[string]any)
	if len(cycles) != 1 {
		t.Fatalf("cycles = %#v, want exactly one collapsed package cycle", resp["cycles"])
	}
	path, _ := cycles[0]["cycle_path"].([]string)
	wantPath := []string{"7348ts_a", "7348ts_b", "7348ts_a"}
	if strings.Join(path, "\x00") != strings.Join(wantPath, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", path, wantPath)
	}
	if got := cycles[0]["cycle_label"]; got != "runtime" {
		t.Fatalf("cycle_label = %#v, want runtime", got)
	}

	coverage, _ := resp["coverage"].(map[string]any)
	if got := coverage["query_shape"]; got != "ts_package_import_cycle" {
		t.Fatalf("coverage.query_shape = %#v, want ts_package_import_cycle", got)
	}
	if got := coverage["cycle_ts_edges_missing_package"]; got != 1 {
		t.Fatalf("coverage.cycle_ts_edges_missing_package = %#v, want 1 legacy edge", got)
	}
	flags, _ := coverage["cycle_edge_flags"].(map[string]any)
	if got := flags["edges_considered"]; got != 6 {
		t.Fatalf("coverage.cycle_edge_flags.edges_considered = %#v, want 6 deduplicated TS edges", got)
	}
	t.Logf("cycle=%q coverage=%v", path, coverage)
}
