// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imports_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/imports"
)

// TypeScript/JavaScript package cycle fixtures for #7348. Rows carry the
// projector-owned source_package (the importing file's owning npm package
// name from the nearest package.json); the shared collapse rule joins it
// against target_module by exact name equality, so nodes are packages and
// same-package files collapse into one node with no change to the shared
// walk. Bare specifiers name the target package; relative specifiers match
// no package and close nothing.

func tsCycleProofEdge(sourceFile, sourcePackage, targetModule string, line int) map[string]any {
	return map[string]any{
		"repo_id":        "repo-1",
		"repo_name":      "proof",
		"source_path":    "/proof/" + sourceFile,
		"source_file":    sourceFile,
		"source_name":    sourceFile[strings.LastIndex(sourceFile, "/")+1:],
		"language":       "typescript",
		"source_package": sourcePackage,
		"target_module":  targetModule,
		"line_number":    line,
	}
}

func tsCycleRequest() codemodel.ImportDependencyRequest {
	return codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles",
		RepoID:    "repo-1",
		Language:  "typescript",
		Limit:     25,
	}
}

func tsCyclePaths(t *testing.T, resp map[string]any) [][]string {
	t.Helper()
	cycles, ok := resp["cycles"].([]map[string]any)
	if !ok {
		t.Fatalf("cycles = %#v, want cycle rows", resp["cycles"])
	}
	paths := make([][]string, 0, len(cycles))
	for _, cycle := range cycles {
		paths = append(paths, cycleStrings(t, cycle["cycle_path"]))
	}
	return paths
}

func TestBuildFileImportCycleRowsFindsTwoNodeTSPackageCycle(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		tsCycleProofEdge("packages/a/index.ts", "@cycles/a", "@cycles/b", 3),
		tsCycleProofEdge("packages/b/index.ts", "@cycles/b", "@cycles/a", 5),
	}

	enumeration, resp := buildCycleResponse(t, tsCycleRequest(), edges)
	if enumeration.Truncated {
		t.Fatal("enumeration.Truncated = true, want false for one small package cycle")
	}
	paths := tsCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want exactly one package cycle", paths)
	}
	want := []string{"@cycles/a", "@cycles/b", "@cycles/a"}
	if strings.Join(paths[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", paths[0], want)
	}
	cycles := resp["cycles"].([]map[string]any)
	if got := codequery.IntVal(cycles[0], "cycle_length"); got != 2 {
		t.Fatalf("cycle_length = %d, want 2", got)
	}
}

func TestBuildFileImportCycleRowsCollapsesSamePackageTSFiles(t *testing.T) {
	t.Parallel()

	// Two files in package a both import package b; package b imports a.
	// Package-level collapse must report exactly one a->b->a cycle, not one
	// per file pair.
	edges := []map[string]any{
		tsCycleProofEdge("packages/a/index.ts", "@cycles/a", "@cycles/b", 3),
		tsCycleProofEdge("packages/a/extra.ts", "@cycles/a", "@cycles/b", 9),
		tsCycleProofEdge("packages/b/index.ts", "@cycles/b", "@cycles/a", 5),
	}

	_, resp := buildCycleResponse(t, tsCycleRequest(), edges)
	paths := tsCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want one collapsed package cycle", paths)
	}
	want := []string{"@cycles/a", "@cycles/b", "@cycles/a"}
	if strings.Join(paths[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", paths[0], want)
	}
}

func TestBuildFileImportCycleRowsFindsThreeNodeTSPackageCycle(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		tsCycleProofEdge("packages/a/index.ts", "@cycles/a", "@cycles/b", 1),
		tsCycleProofEdge("packages/b/index.ts", "@cycles/b", "@cycles/c", 2),
		tsCycleProofEdge("packages/c/index.ts", "@cycles/c", "@cycles/a", 3),
		// An external import must not disturb the cycle.
		tsCycleProofEdge("packages/a/index.ts", "@cycles/a", "react", 4),
	}

	_, resp := buildCycleResponse(t, tsCycleRequest(), edges)
	paths := tsCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want exactly one 3-package cycle", paths)
	}
	want := []string{"@cycles/a", "@cycles/b", "@cycles/c", "@cycles/a"}
	if strings.Join(paths[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", paths[0], want)
	}
}

func TestBuildFileImportCycleRowsFindsFiveNodeTSPackageCycle(t *testing.T) {
	t.Parallel()

	pkgs := []string{"v", "w", "x", "y", "z"}
	edges := make([]map[string]any, 0, len(pkgs))
	for index, pkg := range pkgs {
		target := pkgs[(index+1)%len(pkgs)]
		edges = append(edges, tsCycleProofEdge(
			"packages/"+pkg+"/index.ts",
			"@cycles/"+pkg,
			"@cycles/"+target,
			index+1,
		))
	}

	_, resp := buildCycleResponse(t, tsCycleRequest(), edges)
	paths := tsCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want exactly one 5-package cycle", paths)
	}
	cycles := resp["cycles"].([]map[string]any)
	if got := codequery.IntVal(cycles[0], "cycle_length"); got != 5 {
		t.Fatalf("cycle_length = %d, want 5", got)
	}
}

func TestBuildFileImportCycleRowsTSDiamondHasNoCycle(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		tsCycleProofEdge("packages/a/index.ts", "@cycles/a", "@cycles/b", 1),
		tsCycleProofEdge("packages/a/index.ts", "@cycles/a", "@cycles/c", 2),
		tsCycleProofEdge("packages/b/index.ts", "@cycles/b", "@cycles/d", 3),
		tsCycleProofEdge("packages/c/index.ts", "@cycles/c", "@cycles/d", 4),
		tsCycleProofEdge("packages/d/index.ts", "@cycles/d", "react", 5),
	}

	enumeration, resp := buildCycleResponse(t, tsCycleRequest(), edges)
	if enumeration.Truncated {
		t.Fatal("enumeration.Truncated = true, want false for an acyclic package graph")
	}
	if paths := tsCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want no cycles in a package diamond", paths)
	}
}

func TestBuildFileImportCycleRowsTSTypeOnlyEdgeClosesNoCycle(t *testing.T) {
	t.Parallel()

	// The TS parser flags `import type` (#7432) and the shared
	// type-only/deferred exclusion drops such an edge before the walk: a
	// cycle closed only through it is not a load-time cycle.
	edges := []map[string]any{
		tsCycleProofEdge("packages/a/index.ts", "@cycles/a", "@cycles/b", 3),
	}
	closing := tsCycleProofEdge("packages/b/index.ts", "@cycles/b", "@cycles/a", 5)
	closing["type_only"] = true
	closing["deferred"] = false
	closing["inferred"] = false
	edges = append(edges, closing)

	_, resp := buildCycleResponse(t, tsCycleRequest(), edges)
	if paths := tsCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want no cycle closed through a type-only edge", paths)
	}
}

func TestBuildFileImportCycleRowsTSSamePackageEdgeIsExcluded(t *testing.T) {
	t.Parallel()

	// A relative import inside one package resolves to the same package
	// node; the shared collapse skips destination == source, so it closes
	// nothing and never surfaces as a one-node cycle.
	edges := []map[string]any{
		tsCycleProofEdge("packages/a/index.ts", "@cycles/a", "@cycles/a", 3),
	}

	_, resp := buildCycleResponse(t, tsCycleRequest(), edges)
	if paths := tsCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want no self-package cycle", paths)
	}
}

func TestBuildFileImportCycleRowsTSRelativeImportClosesNoCycle(t *testing.T) {
	t.Parallel()

	// A relative specifier names no npm package, so it matches no package
	// node: file-level relative cycles stay invisible to the package lift
	// rather than collapsing onto a wrong node.
	edges := []map[string]any{
		tsCycleProofEdge("packages/a/index.ts", "@cycles/a", "./b", 3),
		tsCycleProofEdge("packages/a/b.ts", "@cycles/a", "./index", 5),
	}

	_, resp := buildCycleResponse(t, tsCycleRequest(), edges)
	if paths := tsCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want no cycle through relative specifiers", paths)
	}
}

func TestBuildFileImportCycleRowsTSEdgesWithoutPackageAreDropped(t *testing.T) {
	t.Parallel()

	// Edges from a graph written before the projector carried
	// source_package (a file outside every package.json) cannot be placed
	// in any package, so they close nothing. They are counted in coverage
	// instead of silently passing.
	edges := []map[string]any{
		{
			"repo_id": "repo-1", "repo_name": "proof",
			"source_path": "/proof/packages/a/index.ts", "source_file": "packages/a/index.ts",
			"source_name": "index.ts", "language": "typescript",
			"target_module": "@cycles/b", "line_number": 3,
		},
		tsCycleProofEdge("packages/b/index.ts", "@cycles/b", "@cycles/a", 5),
	}

	_, resp := buildCycleResponse(t, tsCycleRequest(), edges)
	if paths := tsCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want no cycle through an unplaced edge", paths)
	}
	coverage, ok := resp["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage = %#v, want a coverage map", resp["coverage"])
	}
	if got := coverage["cycle_ts_edges_missing_package"]; got != 1 {
		t.Fatalf("coverage.cycle_ts_edges_missing_package = %#v, want 1", got)
	}
}

// tsCycleFakeGraph is the minimal GraphQuery fake for the CycleRows path:
// it captures the fetch parameters and replays canned edge rows.
type tsCycleFakeGraph struct {
	cypher string
	params map[string]any
	rows   []map[string]any
}

func (g *tsCycleFakeGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.cypher = cypher
	g.params = params
	return g.rows, nil
}

func (g *tsCycleFakeGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func TestCycleRowsFetchesTSEdgesForTSRequests(t *testing.T) {
	t.Parallel()

	graph := &tsCycleFakeGraph{rows: []map[string]any{
		tsCycleProofEdge("packages/a/index.ts", "@cycles/a", "@cycles/b", 3),
		tsCycleProofEdge("packages/b/index.ts", "@cycles/b", "@cycles/a", 5),
	}}
	rows, enumeration, err := imports.CycleRows(context.Background(), graph, tsCycleRequest())
	if err != nil {
		t.Fatalf("CycleRows() error = %v, want nil", err)
	}
	if got := graph.params["cycle_language"]; got != "typescript" {
		t.Fatalf("params[cycle_language] = %#v, want %q", got, "typescript")
	}
	if !strings.Contains(graph.cypher, "rel.source_package as source_package") {
		t.Fatalf("fetch cypher missing the source_package projection:\n%s", graph.cypher)
	}
	if enumeration.Truncated {
		t.Fatal("enumeration.Truncated = true, want false")
	}
	resp := codemodel.ImportDependencyResponseWithCycleEnumeration(tsCycleRequest(), rows, enumeration)
	if paths := tsCyclePaths(t, resp); len(paths) != 1 {
		t.Fatalf("cycles = %#v, want one package cycle through the production fetch path", paths)
	}
}

func TestCycleRowsFetchesEdgesPerTSLanguage(t *testing.T) {
	t.Parallel()

	for _, language := range []string{"tsx", "javascript"} {
		req := tsCycleRequest()
		req.Language = language
		edge := tsCycleProofEdge("packages/a/index.ts", "@cycles/a", "@cycles/b", 3)
		edge["language"] = language
		graph := &tsCycleFakeGraph{rows: []map[string]any{edge}}
		if _, _, err := imports.CycleRows(context.Background(), graph, req); err != nil {
			t.Fatalf("CycleRows(%s) error = %v, want nil", language, err)
		}
		if got := graph.params["cycle_language"]; got != language {
			t.Fatalf("params[cycle_language] = %#v, want %q", got, language)
		}
	}
}

func TestImportDependencyRequestAcceptsTSFileImportCycles(t *testing.T) {
	t.Parallel()

	for _, language := range []string{"typescript", "tsx", "javascript"} {
		if err := (codemodel.ImportDependencyRequest{
			QueryType: "file_import_cycles", RepoID: "repo-1", Language: language,
		}).Validate(); err != nil {
			t.Fatalf("%s file_import_cycles Validate() error = %v, want nil", language, err)
		}
	}
	if err := (codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles", RepoID: "repo-1", Language: "java",
	}).Validate(); err != nil {
		t.Fatalf("java file_import_cycles Validate() error = %v, want nil", err)
	}
	if err := (codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles", RepoID: "repo-1", Language: "rust",
	}).Validate(); err == nil {
		t.Fatal("rust file_import_cycles Validate() error = nil, want rejection")
	}
	// An unfiltered request stays Python-only: the lift answers explicit
	// language filters, and changing what unfiltered clients receive would
	// be a second behavior change.
	if err := (codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles", RepoID: "repo-1",
	}).Validate(); err != nil {
		t.Fatalf("unfiltered file_import_cycles Validate() error = %v, want nil", err)
	}
}
