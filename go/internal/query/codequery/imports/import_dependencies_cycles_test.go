// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imports_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

// Multi-node cycle fixtures for #6851. Edge rows use the same proof shape
// as the reciprocal tests: source_name derives the source module by
// trimming ".py", and target_module names the imported module.

func buildCycleResponse(
	t *testing.T,
	req codemodel.ImportDependencyRequest,
	edges []map[string]any,
) (bool, map[string]any) {
	t.Helper()
	rows, enumTruncated, err := codemodel.BuildFileImportCycleRows(req, edges)
	if err != nil {
		t.Fatalf("codemodel.BuildFileImportCycleRows() error = %v, want nil", err)
	}
	return enumTruncated, codemodel.ImportDependencyResponseWithCycleEnumeration(req, rows, enumTruncated)
}

func cycleStrings(t *testing.T, value any) []string {
	t.Helper()
	items, ok := value.([]string)
	if ok {
		return items
	}
	anys, ok := value.([]any)
	if !ok {
		t.Fatalf("cycle value = %#v, want a string list", value)
	}
	out := make([]string, 0, len(anys))
	for _, item := range anys {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("cycle item = %#v, want a string", item)
		}
		out = append(out, text)
	}
	return out
}

func TestBuildFileImportCycleRowsFindsThreeNodeCycle(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		importDependencyCycleProofEdge("src/a.py", "a.py", "b", 3),
		importDependencyCycleProofEdge("src/b.py", "b.py", "c", 5),
		importDependencyCycleProofEdge("src/c.py", "c.py", "a", 7),
		// An external import must not disturb the cycle.
		importDependencyCycleProofEdge("src/a.py", "a.py", "os", 1),
	}

	req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 25}
	enumTruncated, resp := buildCycleResponse(t, req, edges)
	if enumTruncated {
		t.Fatal("enumTruncated = true, want false for one small cycle")
	}
	cycles, ok := resp["cycles"].([]map[string]any)
	if !ok || len(cycles) != 1 {
		t.Fatalf("cycles = %#v, want exactly one cycle", resp["cycles"])
	}
	cycle := cycles[0]
	if got := codequery.IntVal(cycle, "cycle_length"); got != 3 {
		t.Fatalf("cycle_length = %d, want 3", got)
	}
	wantPath := []string{"src/a.py", "src/b.py", "src/c.py", "src/a.py"}
	if got := cycleStrings(t, cycle["cycle_path"]); strings.Join(got, "\x00") != strings.Join(wantPath, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", got, wantPath)
	}
	proofs, ok := cycle["cycle_edges"].([]map[string]any)
	if !ok || len(proofs) != 3 {
		t.Fatalf("cycle_edges = %#v, want three proof edges", cycle["cycle_edges"])
	}
	wantFiles := [][2]string{{"src/a.py", "src/b.py"}, {"src/b.py", "src/c.py"}, {"src/c.py", "src/a.py"}}
	wantLines := []int{3, 5, 7}
	for index, want := range wantFiles {
		if got := codequery.StringVal(proofs[index], "source_file"); got != want[0] {
			t.Fatalf("cycle_edges[%d].source_file = %q, want %q", index, got, want[0])
		}
		if got := codequery.StringVal(proofs[index], "target_file"); got != want[1] {
			t.Fatalf("cycle_edges[%d].target_file = %q, want %q", index, got, want[1])
		}
		if got := codequery.IntVal(proofs[index], "line_number"); got != wantLines[index] {
			t.Fatalf("cycle_edges[%d].line_number = %d, want %d", index, got, wantLines[index])
		}
	}
	// Existing response fields keep working.
	if got := codequery.StringVal(cycle, "source_file"); got != "src/a.py" {
		t.Fatalf("source_file = %q, want %q", got, "src/a.py")
	}
	if got := codequery.StringVal(cycle, "target_file"); got != "src/b.py" {
		t.Fatalf("target_file = %q, want %q", got, "src/b.py")
	}
	if got := codequery.IntVal(cycle, "source_line_number"); got != 3 {
		t.Fatalf("source_line_number = %d, want 3", got)
	}
	if got := codequery.IntVal(cycle, "back_edge_line_number"); got != 7 {
		t.Fatalf("back_edge_line_number = %d, want 7", got)
	}
	for _, internal := range []string{"cycle_files", "cycle_source_modules", "cycle_target_modules", "cycle_lines"} {
		if _, leaked := cycle[internal]; leaked {
			t.Fatalf("cycle row leaked internal key %q", internal)
		}
	}
}

func TestBuildFileImportCycleRowsFindsFiveNodeCycle(t *testing.T) {
	t.Parallel()

	files := []string{"v", "w", "x", "y", "z"}
	edges := make([]map[string]any, 0, len(files))
	for index, file := range files {
		target := files[(index+1)%len(files)]
		edges = append(edges, importDependencyCycleProofEdge(
			"src/"+file+".py", file+".py", target, index+1,
		))
	}

	req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 25}
	_, resp := buildCycleResponse(t, req, edges)
	cycles, ok := resp["cycles"].([]map[string]any)
	if !ok || len(cycles) != 1 {
		t.Fatalf("cycles = %#v, want exactly one 5-node cycle", resp["cycles"])
	}
	if got := codequery.IntVal(cycles[0], "cycle_length"); got != 5 {
		t.Fatalf("cycle_length = %d, want 5", got)
	}
	wantPath := []string{"src/v.py", "src/w.py", "src/x.py", "src/y.py", "src/z.py", "src/v.py"}
	if got := cycleStrings(t, cycles[0]["cycle_path"]); strings.Join(got, "\x00") != strings.Join(wantPath, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", got, wantPath)
	}
}

func TestBuildFileImportCycleRowsRespectsMaxCycleLength(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		importDependencyCycleProofEdge("src/a.py", "a.py", "b", 1),
		importDependencyCycleProofEdge("src/b.py", "b.py", "c", 2),
		importDependencyCycleProofEdge("src/c.py", "c.py", "d", 3),
		importDependencyCycleProofEdge("src/d.py", "d.py", "e", 4),
		importDependencyCycleProofEdge("src/e.py", "e.py", "f", 5),
		importDependencyCycleProofEdge("src/f.py", "f.py", "a", 6),
	}

	defaultReq := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 25}
	_, defaultResp := buildCycleResponse(t, defaultReq, edges)
	if cycles := defaultResp["cycles"].([]map[string]any); len(cycles) != 0 {
		t.Fatalf("default max length cycles = %#v, want no 6-node cycle", cycles)
	}

	longReq := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 25, MaxCycleLength: 6}
	_, longResp := buildCycleResponse(t, longReq, edges)
	cycles, ok := longResp["cycles"].([]map[string]any)
	if !ok || len(cycles) != 1 {
		t.Fatalf("max_cycle_length=6 cycles = %#v, want one 6-node cycle", longResp["cycles"])
	}
	if got := codequery.IntVal(cycles[0], "cycle_length"); got != 6 {
		t.Fatalf("cycle_length = %d, want 6", got)
	}
}

func TestBuildFileImportCycleRowsDiamondHasNoCycle(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		importDependencyCycleProofEdge("src/a.py", "a.py", "b", 1),
		importDependencyCycleProofEdge("src/a.py", "a.py", "c", 2),
		importDependencyCycleProofEdge("src/b.py", "b.py", "d", 3),
		importDependencyCycleProofEdge("src/c.py", "c.py", "d", 4),
		importDependencyCycleProofEdge("src/d.py", "d.py", "os", 5),
	}

	req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 25}
	enumTruncated, resp := buildCycleResponse(t, req, edges)
	if enumTruncated {
		t.Fatal("enumTruncated = true, want false for an acyclic graph")
	}
	if cycles := resp["cycles"].([]map[string]any); len(cycles) != 0 {
		t.Fatalf("cycles = %#v, want no cycles in a diamond", cycles)
	}
	if got := resp["truncated"]; got != false {
		t.Fatalf("truncated = %#v, want false", got)
	}
}

func TestBuildFileImportCycleRowsDedupesRotationsAndDuplicateEdges(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		importDependencyCycleProofEdge("src/c.py", "c.py", "a", 7),
		importDependencyCycleProofEdge("src/a.py", "a.py", "b", 21),
		importDependencyCycleProofEdge("src/b.py", "b.py", "c", 5),
		// A duplicate directed edge keeps the earliest line and stays one cycle.
		importDependencyCycleProofEdge("src/a.py", "a.py", "b", 3),
	}

	req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 25}
	_, resp := buildCycleResponse(t, req, edges)
	cycles, ok := resp["cycles"].([]map[string]any)
	if !ok || len(cycles) != 1 {
		t.Fatalf("cycles = %#v, want one deduplicated cycle", resp["cycles"])
	}
	if got := codequery.IntVal(cycles[0], "source_line_number"); got != 3 {
		t.Fatalf("source_line_number = %d, want earliest 3", got)
	}
	wantPath := []string{"src/a.py", "src/b.py", "src/c.py", "src/a.py"}
	if got := cycleStrings(t, cycles[0]["cycle_path"]); strings.Join(got, "\x00") != strings.Join(wantPath, "\x00") {
		t.Fatalf("cycle_path = %q, want normalized %q", got, wantPath)
	}
}

func TestBuildFileImportCycleRowsTruncatesAtEnumerationCap(t *testing.T) {
	t.Parallel()

	edges := make([]map[string]any, 0, 2002)
	for index := 0; index < 1001; index++ {
		left := fmt.Sprintf("k%04d", index)
		right := left + "z"
		edges = append(edges,
			importDependencyCycleProofEdge("src/"+left+".py", left+".py", right, 1),
			importDependencyCycleProofEdge("src/"+right+".py", right+".py", left, 2),
		)
	}

	req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 10}
	enumTruncated, resp := buildCycleResponse(t, req, edges)
	if !enumTruncated {
		t.Fatal("enumTruncated = false, want true past the 1000-cycle enumeration cap")
	}
	if got := resp["truncated"]; got != true {
		t.Fatalf("truncated = %#v, want true so the list is never silently partial", got)
	}
	coverage, ok := resp["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage = %#v, want a coverage map", resp["coverage"])
	}
	if got := coverage["cycle_enumeration_cap"]; got != 1000 {
		t.Fatalf("coverage.cycle_enumeration_cap = %#v, want 1000", coverage["cycle_enumeration_cap"])
	}
	if got := coverage["cycle_enumeration_truncated"]; got != true {
		t.Fatalf("coverage.cycle_enumeration_truncated = %#v, want true", coverage["cycle_enumeration_truncated"])
	}
	if cycles := resp["cycles"].([]map[string]any); len(cycles) != 10 {
		t.Fatalf("len(cycles) = %d, want the 10-row page", len(cycles))
	}
}

func TestBuildFileImportCycleRowsOrdersMixedLengths(t *testing.T) {
	t.Parallel()

	// File names sort opposite to cycle length on purpose: the 5-node
	// cycle owns the smallest files, so enumeration meets it first and
	// only the final length-ascending sort produces [2 3 5].
	edges := []map[string]any{
		importDependencyCycleProofEdge("src/zz1.py", "zz1.py", "zz2", 1),
		importDependencyCycleProofEdge("src/zz2.py", "zz2.py", "zz1", 2),
		importDependencyCycleProofEdge("src/mm1.py", "mm1.py", "mm2", 3),
		importDependencyCycleProofEdge("src/mm2.py", "mm2.py", "mm3", 4),
		importDependencyCycleProofEdge("src/mm3.py", "mm3.py", "mm1", 5),
		importDependencyCycleProofEdge("src/aa1.py", "aa1.py", "aa2", 6),
		importDependencyCycleProofEdge("src/aa2.py", "aa2.py", "aa3", 7),
		importDependencyCycleProofEdge("src/aa3.py", "aa3.py", "aa4", 8),
		importDependencyCycleProofEdge("src/aa4.py", "aa4.py", "aa5", 9),
		importDependencyCycleProofEdge("src/aa5.py", "aa5.py", "aa1", 10),
	}

	req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 25}
	_, resp := buildCycleResponse(t, req, edges)
	cycles, ok := resp["cycles"].([]map[string]any)
	if !ok || len(cycles) != 3 {
		t.Fatalf("cycles = %#v, want three mixed-length cycles", resp["cycles"])
	}
	for index, want := range []int{2, 3, 5} {
		if got := codequery.IntVal(cycles[index], "cycle_length"); got != want {
			t.Fatalf("cycles[%d].cycle_length = %d, want %d", index, got, want)
		}
	}
	if got := codequery.StringVal(cycles[0], "source_file"); got != "src/zz1.py" {
		t.Fatalf("cycles[0].source_file = %q, want the 2-cycle lead", got)
	}
	if got := codequery.StringVal(cycles[2], "source_file"); got != "src/aa1.py" {
		t.Fatalf("cycles[2].source_file = %q, want the 5-cycle lead", got)
	}
}

func TestCycleProofFallsBackForLegacyRows(t *testing.T) {
	t.Parallel()

	req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Limit: 25}
	resp := codemodel.ImportDependencyResponse(req, []map[string]any{{
		"repo_id": "repo-1", "repo_name": "platform",
		"source_file": "src/a.py", "target_file": "src/b.py",
		"source_module": "a", "target_module": "b",
		"source_line_number": 8, "back_edge_line_number": 13,
	}})
	cycles, ok := resp["cycles"].([]map[string]any)
	if !ok || len(cycles) != 1 {
		t.Fatalf("cycles = %#v, want the legacy row shaped", resp["cycles"])
	}
	if got := codequery.IntVal(cycles[0], "cycle_length"); got != 2 {
		t.Fatalf("cycle_length = %d, want 2", got)
	}
	wantPath := []string{"src/a.py", "src/b.py", "src/a.py"}
	if got := cycleStrings(t, cycles[0]["cycle_path"]); strings.Join(got, "\x00") != strings.Join(wantPath, "\x00") {
		t.Fatalf("cycle_path = %q, want %q", got, wantPath)
	}
	if edges, ok := cycles[0]["cycle_edges"].([]map[string]any); !ok || len(edges) != 2 {
		t.Fatalf("cycle_edges = %#v, want two proof edges", cycles[0]["cycle_edges"])
	}
}

func TestImportDependencyRequestRejectsBadMaxCycleLength(t *testing.T) {
	t.Parallel()

	for _, length := range []int{1, -3, 9, 100} {
		err := (codemodel.ImportDependencyRequest{
			QueryType: "file_import_cycles", RepoID: "repo-1", MaxCycleLength: length,
		}).Validate()
		if err == nil || !strings.Contains(err.Error(), "max_cycle_length") {
			t.Fatalf("MaxCycleLength=%d error = %v, want a max_cycle_length bound error", length, err)
		}
	}
	for _, length := range []int{0, 2, 5, 8} {
		err := (codemodel.ImportDependencyRequest{
			QueryType: "file_import_cycles", RepoID: "repo-1", MaxCycleLength: length,
		}).Validate()
		if err != nil {
			t.Fatalf("MaxCycleLength=%d error = %v, want nil", length, err)
		}
	}
	// Other query types ignore the field.
	err := (codemodel.ImportDependencyRequest{
		QueryType: "imports_by_file", RepoID: "repo-1", MaxCycleLength: 100,
	}).Validate()
	if err != nil {
		t.Fatalf("non-cycle MaxCycleLength error = %v, want nil", err)
	}
}
