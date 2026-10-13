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

// Java package cycle fixtures for #7349. Collapsed rows carry the
// projector-owned source_package (the importing file's declared package) and
// the imported target_package; the shared collapse rule joins them by exact
// package-name equality, so nodes are packages and same-package files collapse
// into one node with no change to the shared walk. File-level rows carry
// source_package plus the imported class in target_module and decode to the
// same answer, so where the collapse happens never changes the result.

func javaCycleProofEdge(sourcePackage, targetPackage string, line int) map[string]any {
	return map[string]any{
		"repo_id":        "repo-1",
		"repo_name":      "proof",
		"language":       "java",
		"source_package": sourcePackage,
		"target_package": targetPackage,
		"line_number":    line,
		"type_only":      false,
		"deferred":       false,
		"inferred":       false,
	}
}

func javaCycleFileRow(sourceFile, sourcePackage, targetModule string, line int) map[string]any {
	return map[string]any{
		"repo_id":        "repo-1",
		"repo_name":      "proof",
		"source_path":    "/proof/" + sourceFile,
		"source_file":    sourceFile,
		"source_name":    sourceFile[strings.LastIndex(sourceFile, "/")+1:],
		"language":       "java",
		"source_package": sourcePackage,
		"target_module":  targetModule,
		"line_number":    line,
		"type_only":      false,
		"deferred":       false,
		"inferred":       false,
	}
}

func javaCycleRequest() codemodel.ImportDependencyRequest {
	return codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles",
		RepoID:    "repo-1",
		Language:  "java",
		Limit:     25,
	}
}

func javaCyclePaths(t *testing.T, resp map[string]any) [][]string {
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

func TestBuildFileImportCycleRowsFindsTwoNodeJavaPackageCycle(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		javaCycleProofEdge("com.acme.a", "com.acme.b", 3),
		javaCycleProofEdge("com.acme.b", "com.acme.a", 5),
	}

	enumeration, resp := buildCycleResponse(t, javaCycleRequest(), edges)
	if enumeration.Truncated {
		t.Fatal("enumeration.Truncated = true, want false for one small package cycle")
	}
	paths := javaCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want exactly one package cycle", paths)
	}
	want := []string{"com.acme.a", "com.acme.b", "com.acme.a"}
	if strings.Join(paths[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", paths[0], want)
	}
	cycles := resp["cycles"].([]map[string]any)
	if got := codequery.IntVal(cycles[0], "cycle_length"); got != 2 {
		t.Fatalf("cycle_length = %d, want 2", got)
	}
}

func TestBuildFileImportCycleRowsCollapsesSameJavaPackageFiles(t *testing.T) {
	t.Parallel()

	// Two files in package a both import package b; package b imports a.
	// Package-level collapse must report exactly one a->b->a cycle, not one
	// per file pair. File-level rows prove the collapse, not the fetch.
	edges := []map[string]any{
		javaCycleFileRow("src/com/acme/a/A.java", "com.acme.a", "com.acme.b.Bee", 3),
		javaCycleFileRow("src/com/acme/a/A2.java", "com.acme.a", "com.acme.b.Bee", 9),
		javaCycleFileRow("src/com/acme/b/B.java", "com.acme.b", "com.acme.a.Ay", 5),
	}

	_, resp := buildCycleResponse(t, javaCycleRequest(), edges)
	paths := javaCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want one collapsed package cycle", paths)
	}
	want := []string{"com.acme.a", "com.acme.b", "com.acme.a"}
	if strings.Join(paths[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", paths[0], want)
	}
}

func TestBuildFileImportCycleRowsFindsThreeNodeJavaPackageCycle(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		javaCycleProofEdge("com.acme.a", "com.acme.b", 1),
		javaCycleProofEdge("com.acme.b", "com.acme.c", 2),
		javaCycleProofEdge("com.acme.c", "com.acme.a", 3),
		// An external import must not disturb the cycle.
		javaCycleProofEdge("com.acme.a", "java.util", 4),
	}

	_, resp := buildCycleResponse(t, javaCycleRequest(), edges)
	paths := javaCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want exactly one package cycle", paths)
	}
	want := []string{"com.acme.a", "com.acme.b", "com.acme.c", "com.acme.a"}
	if strings.Join(paths[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", paths[0], want)
	}
}

func TestBuildFileImportCycleRowsFindsFiveNodeJavaPackageCycle(t *testing.T) {
	t.Parallel()

	pkgs := []string{"com.acme.v", "com.acme.w", "com.acme.x", "com.acme.y", "com.acme.z"}
	var edges []map[string]any
	for i := range pkgs {
		edges = append(edges, javaCycleProofEdge(pkgs[i], pkgs[(i+1)%len(pkgs)], i+1))
	}

	_, resp := buildCycleResponse(t, javaCycleRequest(), edges)
	paths := javaCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want exactly one 5-package cycle", paths)
	}
	cycles := resp["cycles"].([]map[string]any)
	if got := codequery.IntVal(cycles[0], "cycle_length"); got != 5 {
		t.Fatalf("cycle_length = %d, want 5", got)
	}
}

func TestBuildFileImportCycleRowsFindsSevenNodeJavaPackageCycle(t *testing.T) {
	t.Parallel()

	pkgs := []string{"com.acme.p1", "com.acme.p2", "com.acme.p3", "com.acme.p4", "com.acme.p5", "com.acme.p6", "com.acme.p7"}
	var edges []map[string]any
	for i := range pkgs {
		edges = append(edges, javaCycleProofEdge(pkgs[i], pkgs[(i+1)%len(pkgs)], i+1))
	}

	req := javaCycleRequest()
	req.MaxCycleLength = 7
	_, resp := buildCycleResponse(t, req, edges)
	paths := javaCyclePaths(t, resp)
	if len(paths) != 1 {
		t.Fatalf("cycles = %#v, want exactly one 7-package cycle", paths)
	}
	cycles := resp["cycles"].([]map[string]any)
	if got := codequery.IntVal(cycles[0], "cycle_length"); got != 7 {
		t.Fatalf("cycle_length = %d, want 7", got)
	}

	// The default length bound must not see it: a 7-node cycle is a deep
	// request, not a default answer.
	_, defaultResp := buildCycleResponse(t, javaCycleRequest(), edges)
	if paths := javaCyclePaths(t, defaultResp); len(paths) != 0 {
		t.Fatalf("default-length cycles = %#v, want none for a 7-package cycle", paths)
	}
}

func TestBuildFileImportCycleRowsDiamondClosesNoJavaCycle(t *testing.T) {
	t.Parallel()

	// m imports n and o; n and o import p. A diamond is not a cycle.
	edges := []map[string]any{
		javaCycleProofEdge("com.acme.m", "com.acme.n", 1),
		javaCycleProofEdge("com.acme.m", "com.acme.o", 2),
		javaCycleProofEdge("com.acme.n", "com.acme.p", 3),
		javaCycleProofEdge("com.acme.o", "com.acme.p", 4),
	}

	_, resp := buildCycleResponse(t, javaCycleRequest(), edges)
	if paths := javaCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want no cycle from a diamond", paths)
	}
}

func TestBuildFileImportCycleRowsFanInClosesNoJavaCycle(t *testing.T) {
	t.Parallel()

	// Three packages import one hub; the hub imports only the outside
	// world. Fan-in is not a cycle.
	edges := []map[string]any{
		javaCycleProofEdge("com.acme.f1", "com.acme.hub", 1),
		javaCycleProofEdge("com.acme.f2", "com.acme.hub", 2),
		javaCycleProofEdge("com.acme.f3", "com.acme.hub", 3),
		javaCycleProofEdge("com.acme.hub", "java.util", 4),
	}

	_, resp := buildCycleResponse(t, javaCycleRequest(), edges)
	if paths := javaCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want no cycle from fan-in", paths)
	}
}

func TestBuildFileImportCycleRowsJavaEdgesWithoutPackageAreDropped(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		javaCycleProofEdge("com.acme.a", "com.acme.b", 3),
		{
			"repo_id": "repo-1", "repo_name": "proof", "language": "java",
			"source_package": "", "target_package": "com.acme.a",
			"line_number": 5, "type_only": false, "deferred": false, "inferred": false,
		},
	}

	enumeration, resp := buildCycleResponse(t, javaCycleRequest(), edges)
	if paths := javaCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want none: the closing edge has no package", paths)
	}
	if enumeration.JavaPackageUnknown != 1 {
		t.Fatalf("JavaPackageUnknown = %d, want the 1 unplaceable edge counted", enumeration.JavaPackageUnknown)
	}
	coverage, ok := resp["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage = %#v, want a coverage map", resp["coverage"])
	}
	if got := coverage["cycle_java_edges_missing_package"]; got != 1 {
		t.Fatalf("coverage.cycle_java_edges_missing_package = %#v, want 1", got)
	}
	if got := coverage["query_shape"]; got != "java_package_import_cycle" {
		t.Fatalf("coverage.query_shape = %#v, want java_package_import_cycle", got)
	}
}

func TestBuildFileImportCycleRowsUnfilteredRequestStaysPythonOnly(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		javaCycleProofEdge("com.acme.a", "com.acme.b", 3),
		javaCycleProofEdge("com.acme.b", "com.acme.a", 5),
	}
	req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 25}

	_, resp := buildCycleResponse(t, req, edges)
	if paths := javaCyclePaths(t, resp); len(paths) != 0 {
		t.Fatalf("cycles = %#v, want none: unfiltered requests stay Python-only", paths)
	}
}

func TestBuildFileImportCycleRowsCollapseLocationChangesNoAnswer(t *testing.T) {
	t.Parallel()

	// The same package graph fed as collapsed rows and as file-level rows
	// must report the same cycle: where the collapse happens (statement or
	// reader) never changes the answer.
	collapsed := []map[string]any{
		javaCycleProofEdge("com.acme.a", "com.acme.b", 3),
		javaCycleProofEdge("com.acme.b", "com.acme.c", 2),
		javaCycleProofEdge("com.acme.c", "com.acme.a", 7),
	}
	fileLevel := []map[string]any{
		javaCycleFileRow("src/com/acme/a/A.java", "com.acme.a", "com.acme.b.Bee", 3),
		javaCycleFileRow("src/com/acme/b/B.java", "com.acme.b", "com.acme.c.Cee", 2),
		javaCycleFileRow("src/com/acme/c/C.java", "com.acme.c", "com.acme.a.Ay", 7),
	}

	_, collapsedResp := buildCycleResponse(t, javaCycleRequest(), collapsed)
	_, fileResp := buildCycleResponse(t, javaCycleRequest(), fileLevel)
	collapsedPaths := javaCyclePaths(t, collapsedResp)
	filePaths := javaCyclePaths(t, fileResp)
	if strings.Join(collapsedPaths[0], "\x00") != strings.Join(filePaths[0], "\x00") {
		t.Fatalf("collapsed paths %q != file-level paths %q, want the same answer", collapsedPaths, filePaths)
	}
}

// javaCycleFakeGraph is the minimal GraphQuery fake for the CycleRows path:
// it captures the fetch parameters and replays canned edge rows.
type javaCycleFakeGraph struct {
	cypher string
	params map[string]any
	rows   []map[string]any
}

func (g *javaCycleFakeGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	g.cypher = cypher
	g.params = params
	return g.rows, nil
}

func (g *javaCycleFakeGraph) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func TestCycleRowsFetchesJavaPackagePairsForJavaRequests(t *testing.T) {
	t.Parallel()

	graph := &javaCycleFakeGraph{rows: []map[string]any{
		javaCycleProofEdge("com.acme.a", "com.acme.b", 3),
		javaCycleProofEdge("com.acme.b", "com.acme.a", 5),
	}}
	rows, enumeration, err := imports.CycleRows(context.Background(), graph, javaCycleRequest())
	if err != nil {
		t.Fatalf("CycleRows() error = %v, want nil", err)
	}
	if got := graph.params["cycle_language"]; got != "java" {
		t.Fatalf("params[cycle_language] = %#v, want %q", got, "java")
	}
	if !strings.Contains(graph.cypher, "target_package") {
		t.Fatalf("fetch cypher missing the collapsed target_package projection:\n%s", graph.cypher)
	}
	if !strings.Contains(graph.cypher, "rel.source_package IS NOT NULL") {
		t.Fatalf("fetch cypher missing the unplaced-edge pre-filter:\n%s", graph.cypher)
	}
	if enumeration.Truncated {
		t.Fatal("enumeration.Truncated = true, want false")
	}
	resp := codemodel.ImportDependencyResponseWithCycleEnumeration(javaCycleRequest(), rows, enumeration)
	if paths := javaCyclePaths(t, resp); len(paths) != 1 {
		t.Fatalf("cycles = %#v, want one package cycle through the production fetch path", paths)
	}
}
