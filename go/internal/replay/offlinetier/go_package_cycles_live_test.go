// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// go_package_cycles_live_test.go is the backend-required graph-truth proof
// for #7347's Go package lift of file_import_cycles. The reader projects the
// projector-owned source_package raw, and the whole lift rests on how a real
// backend hands back that property: it must come back as the written string,
// and a never-written one must come back as a null the reader excludes as
// package-unknown, never as an empty package that would collapse every
// legacy edge onto one node. A fixture-row unit test cannot show that, so
// this seeds real Go edges and reads them through the production
// imports.CycleRows and response shaper.
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

const goCyclesRepoID = "7347-go-cycles"

// goCycleSeedEdge names one seeded IMPORTS edge: the importing file, its
// package, the imported module, and the properties to SET. A nil props map
// writes only the line number, which is what an edge written before the
// mapping (or the flags) existed looks like in the graph.
type goCycleSeedEdge struct {
	file   string
	pkg    string
	target string
	props  map[string]any
}

func goCycleSeedEdges() []goCycleSeedEdge {
	runtime := map[string]any{"line_number": 1, "type_only": false, "deferred": false, "inferred": false}
	withPkg := func(pkg string) map[string]any {
		props := map[string]any{"source_package": pkg}
		for key, value := range runtime {
			props[key] = value
		}
		return props
	}
	return []goCycleSeedEdge{
		// A two-package cycle, with a second file in package a proving the
		// collapse reports one cycle, not one per file pair.
		{file: "ga_a", pkg: "7347go_a", target: "7347go_b", props: withPkg("7347go_a")},
		{file: "ga_a2", pkg: "7347go_a", target: "7347go_b", props: withPkg("7347go_a")},
		{file: "gb_b", pkg: "7347go_b", target: "7347go_a", props: withPkg("7347go_b")},
		// A diamond arm that must close no cycle.
		{file: "gc_c", pkg: "7347go_c", target: "7347go_a", props: withPkg("7347go_c")},
		{file: "gc_c", pkg: "7347go_c", target: "7347go_b", props: withPkg("7347go_c")},
		// A legacy edge with no properties at all: unplaceable, closes
		// nothing, counted in coverage.
		{file: "gd_d", target: "7347go_e"},
		{file: "ge_e", pkg: "7347go_e", target: "7347go_d", props: withPkg("7347go_e")},
	}
}

func goCycleCleanup(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, s := range []string{
		`MATCH (f:File) WHERE f.repo_id = $repo_id DETACH DELETE f`,
		`MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`,
		// Module nodes are global; delete only the names this fixture created.
		`MATCH (m:Module) WHERE m.name STARTS WITH '7347go_' DETACH DELETE m`,
	} {
		if err := exec.Execute(ctx, cypher.Statement{Cypher: s, Parameters: map[string]any{"repo_id": goCyclesRepoID}}); err != nil {
			t.Fatalf("cleanup %q: %v", s, err)
		}
	}
}

func seedGoCycleGraph(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `CREATE (:Repository {id: $repo_id, name: 'go-cycles'})`,
		Parameters: map[string]any{"repo_id": goCyclesRepoID},
	}); err != nil {
		t.Fatalf("seed repository: %v", err)
	}
	files := map[string]bool{}
	modules := map[string]bool{}
	for _, e := range goCycleSeedEdges() {
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
CREATE (r)-[:REPO_CONTAINS]->(:File {path: $path, relative_path: $rel, name: $rel, language: 'go', repo_id: $repo_id})`,
			Parameters: map[string]any{"repo_id": goCyclesRepoID, "path": "/go-cycles/" + base + ".go", "rel": base + ".go"},
		}); err != nil {
			t.Fatalf("seed file %s: %v", base, err)
		}
	}
	for module := range modules {
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher:     `CREATE (:Module {name: $name, lang: 'go'})`,
			Parameters: map[string]any{"name": module},
		}); err != nil {
			t.Fatalf("seed module %s: %v", module, err)
		}
	}
	for _, e := range goCycleSeedEdges() {
		props := map[string]any{"line_number": 1}
		for key, value := range e.props {
			props[key] = value
		}
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher: `MATCH (f:File {path: $path}), (m:Module {name: $module, lang: 'go'})
CREATE (f)-[r:IMPORTS]->(m) SET r += $props`,
			Parameters: map[string]any{"path": "/go-cycles/" + e.file + ".go", "module": e.target, "props": props},
		}); err != nil {
			t.Fatalf("seed edge %s->%s: %v", e.file, e.target, err)
		}
	}
}

// TestFileImportCyclesGoTruthAgainstARealGraph reads the seeded Go package
// graph through the production reader. The a<->b packages report exactly one
// collapsed cycle, the diamond arm and the legacy edge close nothing, the
// legacy edge is counted as missing-package, and the cycle is labelled
// runtime with the go package query shape.
func TestFileImportCyclesGoTruthAgainstARealGraph(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the Go package-cycle proof against a real graph backend", liveTierEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	goCycleCleanup(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		goCycleCleanup(cleanCtx, t, exec)
	})
	seedGoCycleGraph(ctx, t, exec)

	req := codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles",
		RepoID:    goCyclesRepoID,
		Language:  "go",
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
	wantPath := []string{"7347go_a", "7347go_b", "7347go_a"}
	if strings.Join(path, "\x00") != strings.Join(wantPath, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", path, wantPath)
	}
	if got := cycles[0]["cycle_label"]; got != "runtime" {
		t.Fatalf("cycle_label = %#v, want runtime", got)
	}

	coverage, _ := resp["coverage"].(map[string]any)
	if got := coverage["query_shape"]; got != "go_package_import_cycle" {
		t.Fatalf("coverage.query_shape = %#v, want go_package_import_cycle", got)
	}
	if got := coverage["cycle_go_edges_missing_package"]; got != 1 {
		t.Fatalf("coverage.cycle_go_edges_missing_package = %#v, want 1 legacy edge", got)
	}
	flags, _ := coverage["cycle_edge_flags"].(map[string]any)
	if got := flags["edges_considered"]; got != 6 {
		t.Fatalf("coverage.cycle_edge_flags.edges_considered = %#v, want 6 deduplicated Go edges", got)
	}
	t.Logf("cycle=%q coverage=%v", path, coverage)
}
