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

// Go package cycle fixtures for #7347. Rows carry the projector-owned
// source_package (the importing file's package import path); the shared
// collapse rule joins it against target_module by exact name equality, so
// nodes are packages and same-package files collapse into one node with no
// change to the shared walk.

func goCycleProofEdge(sourceFile, sourcePackage, targetModule string, line int) map[string]any {
	return map[string]any{
		"repo_id":        "repo-1",
		"repo_name":      "proof",
		"source_path":    "/proof/" + sourceFile,
		"source_file":    sourceFile,
		"source_name":    sourceFile[strings.LastIndex(sourceFile, "/")+1:],
		"language":       "go",
		"source_package": sourcePackage,
		"target_module":  targetModule,
		"line_number":    line,
	}
}

func goCycleRequest() codemodel.ImportDependencyRequest {
	return codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles",
		RepoID:    "repo-1",
		Language:  "go",
		Limit:     25,
	}
}

func goCyclePaths(t *testing.T, resp map[string]any) [][]string {
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

func TestBuildFileImportCycleRowsFindsTwoNodeGoPackageCycle(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		goCycleProofEdge("pkg/a/a.go", "example.com/repo/pkg/a", "example.com/repo/pkg/b", 3),
		goCycleProofEdge("pkg/b/b.go", "example.com/repo/pkg/b", "example.com/repo/pkg/a", 5),
	}

	enumeration, resp := buildCycleResponse(t, goCycleRequest(), edges)
	if enumeration.Truncated {
		t.Fatal("enumeration.Truncated = true, want false for one small package cycle")
	}
	paths := goCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want exactly one package cycle", paths)
	}
	want := []string{"example.com/repo/pkg/a", "example.com/repo/pkg/b", "example.com/repo/pkg/a"}
	if strings.Join(paths[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", paths[0], want)
	}
	cycles := resp["cycles"].([]map[string]any)
	if got := codequery.IntVal(cycles[0], "cycle_length"); got != 2 {
		t.Fatalf("cycle_length = %d, want 2", got)
	}
}

func TestBuildFileImportCycleRowsCollapsesSamePackageFiles(t *testing.T) {
	t.Parallel()

	// Two files in package a both import package b; package b imports a.
	// Package-level collapse must report exactly one a->b->a cycle, not one
	// per file pair.
	edges := []map[string]any{
		goCycleProofEdge("pkg/a/a.go", "example.com/repo/pkg/a", "example.com/repo/pkg/b", 3),
		goCycleProofEdge("pkg/a/a2.go", "example.com/repo/pkg/a", "example.com/repo/pkg/b", 9),
		goCycleProofEdge("pkg/b/b.go", "example.com/repo/pkg/b", "example.com/repo/pkg/a", 5),
	}

	_, resp := buildCycleResponse(t, goCycleRequest(), edges)
	paths := goCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want one collapsed package cycle", paths)
	}
	want := []string{"example.com/repo/pkg/a", "example.com/repo/pkg/b", "example.com/repo/pkg/a"}
	if strings.Join(paths[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", paths[0], want)
	}
}

func TestBuildFileImportCycleRowsFindsThreeNodeGoPackageCycle(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		goCycleProofEdge("pkg/a/a.go", "example.com/repo/pkg/a", "example.com/repo/pkg/b", 1),
		goCycleProofEdge("pkg/b/b.go", "example.com/repo/pkg/b", "example.com/repo/pkg/c", 2),
		goCycleProofEdge("pkg/c/c.go", "example.com/repo/pkg/c", "example.com/repo/pkg/a", 3),
		// An external import must not disturb the cycle.
		goCycleProofEdge("pkg/a/a.go", "example.com/repo/pkg/a", "fmt", 4),
	}

	_, resp := buildCycleResponse(t, goCycleRequest(), edges)
	paths := goCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want exactly one 3-package cycle", paths)
	}
	want := []string{
		"example.com/repo/pkg/a", "example.com/repo/pkg/b",
		"example.com/repo/pkg/c", "example.com/repo/pkg/a",
	}
	if strings.Join(paths[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", paths[0], want)
	}
}

func TestBuildFileImportCycleRowsFindsFiveNodeGoPackageCycle(t *testing.T) {
	t.Parallel()

	pkgs := []string{"v", "w", "x", "y", "z"}
	edges := make([]map[string]any, 0, len(pkgs))
	for index, pkg := range pkgs {
		target := pkgs[(index+1)%len(pkgs)]
		edges = append(edges, goCycleProofEdge(
			"pkg/"+pkg+"/"+pkg+".go",
			"example.com/repo/pkg/"+pkg,
			"example.com/repo/pkg/"+target,
			index+1,
		))
	}

	_, resp := buildCycleResponse(t, goCycleRequest(), edges)
	paths := goCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want exactly one 5-package cycle", paths)
	}
	cycles := resp["cycles"].([]map[string]any)
	if got := codequery.IntVal(cycles[0], "cycle_length"); got != 5 {
		t.Fatalf("cycle_length = %d, want 5", got)
	}
}

func TestBuildFileImportCycleRowsGoDiamondHasNoCycle(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		goCycleProofEdge("pkg/a/a.go", "example.com/repo/pkg/a", "example.com/repo/pkg/b", 1),
		goCycleProofEdge("pkg/a/a.go", "example.com/repo/pkg/a", "example.com/repo/pkg/c", 2),
		goCycleProofEdge("pkg/b/b.go", "example.com/repo/pkg/b", "example.com/repo/pkg/d", 3),
		goCycleProofEdge("pkg/c/c.go", "example.com/repo/pkg/c", "example.com/repo/pkg/d", 4),
		goCycleProofEdge("pkg/d/d.go", "example.com/repo/pkg/d", "fmt", 5),
	}

	enumeration, resp := buildCycleResponse(t, goCycleRequest(), edges)
	if enumeration.Truncated {
		t.Fatal("enumeration.Truncated = true, want false for an acyclic package graph")
	}
	if paths := goCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want no cycles in a package diamond", paths)
	}
}

func TestBuildFileImportCycleRowsGoTypeOnlyEdgeClosesNoCycle(t *testing.T) {
	t.Parallel()

	// Go parsers never emit type-only imports (#7344 decision gate), but the
	// shared type-only/deferred exclusion still applies when such an edge
	// reaches the reader: a cycle closed only through it is not reported.
	edges := []map[string]any{
		goCycleProofEdge("pkg/a/a.go", "example.com/repo/pkg/a", "example.com/repo/pkg/b", 3),
	}
	closing := goCycleProofEdge("pkg/b/b.go", "example.com/repo/pkg/b", "example.com/repo/pkg/a", 5)
	closing["type_only"] = true
	closing["deferred"] = false
	closing["inferred"] = false
	edges = append(edges, closing)

	_, resp := buildCycleResponse(t, goCycleRequest(), edges)
	if paths := goCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want no cycle closed through a type-only edge", paths)
	}
}

func TestBuildFileImportCycleRowsGoSamePackageEdgeIsExcluded(t *testing.T) {
	t.Parallel()

	// A self-import cannot exist in Go (the compiler forbids package cycles,
	// let alone self-imports), but a defensive same-package edge must never
	// surface as a one-node cycle.
	edges := []map[string]any{
		goCycleProofEdge("pkg/a/a.go", "example.com/repo/pkg/a", "example.com/repo/pkg/a", 3),
	}

	_, resp := buildCycleResponse(t, goCycleRequest(), edges)
	if paths := goCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want no self-package cycle", paths)
	}
}

func TestBuildFileImportCycleRowsGoEdgesWithoutPackageAreDropped(t *testing.T) {
	t.Parallel()

	// Edges from a graph written before the projector carried source_package
	// cannot be placed in any package, so they close nothing. They are
	// counted in coverage instead of silently passing.
	edges := []map[string]any{
		{
			"repo_id": "repo-1", "repo_name": "proof",
			"source_path": "/proof/pkg/a/a.go", "source_file": "pkg/a/a.go",
			"source_name": "a.go", "language": "go",
			"target_module": "example.com/repo/pkg/b", "line_number": 3,
		},
		goCycleProofEdge("pkg/b/b.go", "example.com/repo/pkg/b", "example.com/repo/pkg/a", 5),
	}

	_, resp := buildCycleResponse(t, goCycleRequest(), edges)
	if paths := goCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want no cycle through an unplaced edge", paths)
	}
	coverage, ok := resp["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage = %#v, want a coverage map", resp["coverage"])
	}
	if got := coverage["cycle_go_edges_missing_package"]; got != 1 {
		t.Fatalf("coverage.cycle_go_edges_missing_package = %#v, want 1", got)
	}
}

// goCycleFakeGraph is the minimal GraphQuery fake for the CycleRows path:
// it captures the fetch parameters and replays canned edge rows.
type goCycleFakeGraph struct {
	cypher string
	params map[string]any
	rows   []map[string]any
}

func (g *goCycleFakeGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.cypher = cypher
	g.params = params
	return g.rows, nil
}

func (g *goCycleFakeGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func TestCycleRowsFetchesGoEdgesForGoRequests(t *testing.T) {
	t.Parallel()

	graph := &goCycleFakeGraph{rows: []map[string]any{
		goCycleProofEdge("pkg/a/a.go", "example.com/repo/pkg/a", "example.com/repo/pkg/b", 3),
		goCycleProofEdge("pkg/b/b.go", "example.com/repo/pkg/b", "example.com/repo/pkg/a", 5),
	}}
	rows, enumeration, err := imports.CycleRows(context.Background(), graph, goCycleRequest())
	if err != nil {
		t.Fatalf("CycleRows() error = %v, want nil", err)
	}
	if got := graph.params["cycle_language"]; got != "go" {
		t.Fatalf("params[cycle_language] = %#v, want %q", got, "go")
	}
	if !strings.Contains(graph.cypher, "rel.source_package as source_package") {
		t.Fatalf("fetch cypher missing the source_package projection:\n%s", graph.cypher)
	}
	if enumeration.Truncated {
		t.Fatal("enumeration.Truncated = true, want false")
	}
	resp := codemodel.ImportDependencyResponseWithCycleEnumeration(goCycleRequest(), rows, enumeration)
	if paths := goCyclePaths(t, resp); len(paths) != 1 {
		t.Fatalf("cycles = %#v, want one package cycle through the production fetch path", paths)
	}
}

func TestImportDependencyRequestAcceptsGoFileImportCycles(t *testing.T) {
	t.Parallel()

	if err := (codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles", RepoID: "repo-1", Language: "go",
	}).Validate(); err != nil {
		t.Fatalf("go file_import_cycles Validate() error = %v, want nil", err)
	}
	if err := (codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles", RepoID: "repo-1", Language: "typescript",
	}).Validate(); err == nil {
		t.Fatal("typescript file_import_cycles Validate() error = nil, want rejection")
	}
}
