// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package offlinetier_test

// java_package_cycles_live_test.go is the backend-required graph-truth proof
// for #7349's Java package lift of file_import_cycles. The production Java
// read collapses to package pairs in the statement: the target package
// derives from the imported class name (a .* suffix already names the
// package, otherwise the last dotted segment is cut), the flags fold
// null-if-any-null else AND, and edges with no source_package never leave
// the database. A fixture-row unit test cannot show any of that, so this
// seeds real Java edges at class level and reads them through the production
// imports.CycleRows and response shaper.
//
// The edges carry exactly the properties the projector writes: the three
// flags as explicit booleans plus source_package as an explicit string on
// every edge, and one legacy edge written with only a line number. It seeds
// edges directly instead of going through the canonical writer;
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

const javaCyclesRepoID = "7349-java-cycles"

// javaCycleSeedEdge names one seeded IMPORTS edge: the importing file, its
// package, the imported class-level module, and the properties to SET. A nil
// props map writes only the line number, which is what an edge written before
// the mapping (or the flags) existed looks like in the graph.
type javaCycleSeedEdge struct {
	file   string
	pkg    string
	target string
	props  map[string]any
}

func javaCycleSeedEdges() []javaCycleSeedEdge {
	runtime := map[string]any{"line_number": 1, "type_only": false, "deferred": false, "inferred": false}
	withPkg := func(pkg string) map[string]any {
		props := map[string]any{"source_package": pkg}
		for key, value := range runtime {
			props[key] = value
		}
		return props
	}
	return []javaCycleSeedEdge{
		// A two-package cycle, with a second file in package a proving the
		// collapse reports one cycle, not one per file pair.
		{file: "ja_a", pkg: "t7349.live.a", target: "t7349.live.b.Bee", props: withPkg("t7349.live.a")},
		{file: "ja_a2", pkg: "t7349.live.a", target: "t7349.live.b.Bee", props: withPkg("t7349.live.a")},
		{file: "jb_b", pkg: "t7349.live.b", target: "t7349.live.a.Ay", props: withPkg("t7349.live.b")},
		// A diamond arm that must close no cycle.
		{file: "jc_c", pkg: "t7349.live.c", target: "t7349.live.a.Ay", props: withPkg("t7349.live.c")},
		{file: "jc_c", pkg: "t7349.live.c", target: "t7349.live.b.Bee", props: withPkg("t7349.live.c")},
		// An on-demand import of a cycle package: it derives to the package
		// and still closes nothing on its own.
		{file: "jg_g", pkg: "t7349.live.g", target: "t7349.live.b.*", props: withPkg("t7349.live.g")},
		// A static member import trims to the enclosing class, which names
		// no repository package: it must close nothing, never a phantom.
		{file: "jf_f", pkg: "t7349.live.f", target: "t7349.live.a.Ay.FLAG", props: withPkg("t7349.live.f")},
		// A legacy edge with no properties at all: the collapsed fetch
		// pre-filters it, so it closes nothing and is counted nowhere.
		{file: "jd_d", target: "t7349.live.e.Eee"},
		{file: "je_e", pkg: "t7349.live.e", target: "t7349.live.d.Dee", props: withPkg("t7349.live.e")},
	}
}

func javaCycleCleanup(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	for _, s := range []string{
		`MATCH (f:File) WHERE f.repo_id = $repo_id DETACH DELETE f`,
		`MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`,
		// Module nodes are global; delete only the names this fixture created.
		`MATCH (m:Module) WHERE m.name STARTS WITH 't7349.live.' DETACH DELETE m`,
	} {
		if err := exec.Execute(ctx, cypher.Statement{Cypher: s, Parameters: map[string]any{"repo_id": javaCyclesRepoID}}); err != nil {
			t.Fatalf("cleanup %q: %v", s, err)
		}
	}
}

func seedJavaCycleGraph(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	if err := exec.Execute(ctx, cypher.Statement{
		Cypher:     `CREATE (:Repository {id: $repo_id, name: 'java-cycles'})`,
		Parameters: map[string]any{"repo_id": javaCyclesRepoID},
	}); err != nil {
		t.Fatalf("seed repository: %v", err)
	}
	files := map[string]bool{}
	modules := map[string]bool{}
	for _, e := range javaCycleSeedEdges() {
		files[e.file] = true
		modules[e.target] = true
	}
	for base := range files {
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher: `MATCH (r:Repository {id: $repo_id})
CREATE (r)-[:REPO_CONTAINS]->(:File {path: $path, relative_path: $rel, name: $rel, language: 'java', repo_id: $repo_id})`,
			Parameters: map[string]any{"repo_id": javaCyclesRepoID, "path": "/java-cycles/" + base + ".java", "rel": base + ".java"},
		}); err != nil {
			t.Fatalf("seed file %s: %v", base, err)
		}
	}
	for module := range modules {
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher:     `CREATE (:Module {name: $name, lang: 'java'})`,
			Parameters: map[string]any{"name": module},
		}); err != nil {
			t.Fatalf("seed module %s: %v", module, err)
		}
	}
	for _, e := range javaCycleSeedEdges() {
		props := map[string]any{"line_number": 1}
		for key, value := range e.props {
			props[key] = value
		}
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher: `MATCH (f:File {path: $path}), (m:Module {name: $module, lang: 'java'})
CREATE (f)-[r:IMPORTS]->(m) SET r += $props`,
			Parameters: map[string]any{"path": "/java-cycles/" + e.file + ".java", "module": e.target, "props": props},
		}); err != nil {
			t.Fatalf("seed edge %s->%s: %v", e.file, e.target, err)
		}
	}
}

// TestFileImportCyclesJavaTruthAgainstARealGraph reads the seeded Java
// package graph through the production reader. The a<->b packages report
// exactly one collapsed cycle, the diamond arm, the on-demand leg, the
// static member leg, and the legacy edge close nothing, and the cycle is
// labelled runtime with the java package query shape.
func TestFileImportCyclesJavaTruthAgainstARealGraph(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 to run the Java package-cycle proof against a real graph backend", liveTierEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	javaCycleCleanup(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		javaCycleCleanup(cleanCtx, t, exec)
	})
	seedJavaCycleGraph(ctx, t, exec)

	req := codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles",
		RepoID:    javaCyclesRepoID,
		Language:  "java",
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
	wantPath := []string{"t7349.live.a", "t7349.live.b", "t7349.live.a"}
	if strings.Join(path, "\x00") != strings.Join(wantPath, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", path, wantPath)
	}
	if got := cycles[0]["cycle_label"]; got != "runtime" {
		t.Fatalf("cycle_label = %#v, want runtime", got)
	}

	coverage, _ := resp["coverage"].(map[string]any)
	if got := coverage["query_shape"]; got != "java_package_import_cycle" {
		t.Fatalf("coverage.query_shape = %#v, want java_package_import_cycle", got)
	}
	if got := coverage["cycle_java_edges_missing_package"]; got != 0 {
		t.Fatalf("coverage.cycle_java_edges_missing_package = %#v, want 0: the collapsed fetch pre-filters unplaced edges", got)
	}
	flags, _ := coverage["cycle_edge_flags"].(map[string]any)
	// Seven deduplicated pairs: a<->b (two files fold to one), c->a, c->b,
	// g->b, f->phantom, e->d. The legacy edge never leaves the database.
	if got := flags["edges_considered"]; got != 7 {
		t.Fatalf("coverage.cycle_edge_flags.edges_considered = %#v, want 7 deduplicated Java package pairs", got)
	}
	t.Logf("cycle=%q coverage=%v", path, coverage)
}
