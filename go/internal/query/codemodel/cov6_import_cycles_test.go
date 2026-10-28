// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"
	"testing"
)

func cov6CycleEdge(repo, sourceFile, sourceModule, targetModule string, line int, state importCycleEdgeState) importCycleEdge {
	return importCycleEdge{
		repoID: repo, sourcePath: "src/" + sourceFile, sourceFile: sourceFile,
		sourceModule: sourceModule, language: "python",
		targetModule: targetModule, lineNumber: line, state: state,
	}
}

func TestCov6ImportDependencyRequestValidate(t *testing.T) {
	t.Parallel()
	if err := (ImportDependencyRequest{QueryType: "bogus", RepoID: "r"}).Validate(); err == nil {
		t.Fatal("bogus query_type validates, want rejection")
	}
	if err := (ImportDependencyRequest{Limit: 201, RepoID: "r"}).Validate(); err == nil {
		t.Fatal("limit 201 validates, want rejection")
	}
	if err := (ImportDependencyRequest{Limit: -1, RepoID: "r"}).Validate(); err == nil {
		t.Fatal("negative limit validates, want rejection")
	}
	if err := (ImportDependencyRequest{Offset: -1, RepoID: "r"}).Validate(); err == nil {
		t.Fatal("negative offset validates, want rejection")
	}
	if err := (ImportDependencyRequest{Offset: 10001, RepoID: "r"}).Validate(); err == nil {
		t.Fatal("huge offset validates, want rejection")
	}
	if err := (ImportDependencyRequest{}).Validate(); err == nil {
		t.Fatal("scopeless request validates, want rejection")
	}
	if err := (ImportDependencyRequest{QueryType: "imports_by_file", TargetFile: "b.py", RepoID: "r"}).Validate(); err == nil {
		t.Fatal("target_file on imports_by_file validates, want rejection")
	}
	cycles := ImportDependencyRequest{QueryType: "file_import_cycles", SourceFile: "a.py", Language: "cobol"}
	if err := cycles.Validate(); err == nil {
		t.Fatal("cobol cycles validate, want rejection")
	}
	bounded := ImportDependencyRequest{QueryType: "file_import_cycles", SourceFile: "a.py", MaxCycleLength: 99}
	if err := bounded.Validate(); err == nil {
		t.Fatal("max_cycle_length 99 validates, want rejection")
	}
	valid := ImportDependencyRequest{QueryType: "file_import_cycles", SourceFile: "a.py", Language: "java"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("java cycles request rejected: %v", err)
	}
	if got := (ImportDependencyRequest{}).EffectiveQueryType(); got != "imports_by_file" {
		t.Fatalf("default query type = %q, want imports_by_file", got)
	}
	if got := (ImportDependencyRequest{Language: " Go "}).NormalizedLanguage(); got != "go" {
		t.Fatalf("normalized language = %q, want go", got)
	}
	if got := (ImportDependencyRequest{}).EffectiveCycleLanguage(); got != "python" {
		t.Fatalf("default cycle language = %q, want python", got)
	}
	if got := (ImportDependencyRequest{}).effectiveMaxCycleLength(); got != importCycleDefaultMaxLength {
		t.Fatalf("default max cycle length = %d, want %d", got, importCycleDefaultMaxLength)
	}
	if got := (ImportDependencyRequest{MaxCycleLength: 4}).effectiveMaxCycleLength(); got != 4 {
		t.Fatalf("explicit max cycle length = %d, want 4", got)
	}
	if got := (ImportDependencyRequest{}).QueryLimit(); got != 26 {
		t.Fatalf("default query limit = %d, want 26", got)
	}
	if names := importDependencyQueryTypeNames(); len(names) != 6 {
		t.Fatalf("query type names = %v, want 6 families", names)
	}
}

func TestCov6ImportCycleRowMatching(t *testing.T) {
	t.Parallel()
	row := map[string]any{
		"repo_id": "r", "source_file": "a.py", "target_file": "b.py",
		"source_module": "a", "target_module": "b",
		"cycle_files":          []any{"a.py", "b.py"},
		"cycle_source_modules": []any{"a", "b"},
		"cycle_target_modules": []any{"b", "a"},
	}
	if !importCycleRowMatches(ImportDependencyRequest{RepoID: "r", SourceFile: "a.py"}, row) {
		t.Fatal("member anchor must match its cycle")
	}
	if importCycleRowMatches(ImportDependencyRequest{RepoID: "other"}, row) {
		t.Fatal("foreign repo must not match")
	}
	if importCycleRowMatches(ImportDependencyRequest{RepoID: "r", SourceFile: "zzz.py"}, row) {
		t.Fatal("non-member anchor must not match")
	}
	if !matchesAnyRequestValue("", []string{"a"}) || !matchesAnyRequestValue("a", []string{"a"}) {
		t.Fatal("empty anchor or exact hit must match")
	}
	if matchesAnyRequestValue("z", []string{"a"}) {
		t.Fatal("non-member anchor must not match")
	}
	// Legacy scalar-only rows fall back to the scalar file/module keys.
	scalar := map[string]any{
		"repo_id": "r", "source_file": "a.py", "target_file": "b.py",
		"source_module": "a", "target_module": "b",
	}
	if !importCycleRowMatches(ImportDependencyRequest{RepoID: "r", TargetModule: "b"}, scalar) {
		t.Fatal("scalar fallback row must match on target module")
	}
	if got := importCycleStringList(row, "cycle_files"); len(got) != 2 || got[0] != "a.py" {
		t.Fatalf("string list = %v, want [a.py b.py]", got)
	}
	if got := importCycleStringList(map[string]any{"k": []string{"x"}}, "k"); len(got) != 1 {
		t.Fatalf("[]string form = %v, want [x]", got)
	}
	if importCycleStringList(map[string]any{"k": []any{1}}, "k") != nil {
		t.Fatal("non-string members must read back nil")
	}
	if importCycleStringList(map[string]any{}, "missing") != nil {
		t.Fatal("missing key must read back nil")
	}
	if !strings.Contains(importCycleRowKey(row), "r") {
		t.Fatalf("row key = %q, want repo-scoped key", importCycleRowKey(row))
	}
}

func TestCov6CompareImportCycleRows(t *testing.T) {
	t.Parallel()
	twoHop := map[string]any{"repo_id": "r", "cycle_files": []any{"a.py", "b.py"}, "source_file": "a.py", "target_file": "b.py", "source_line_number": 1, "back_edge_line_number": 2}
	threeHop := map[string]any{"repo_id": "r", "cycle_files": []any{"a.py", "b.py", "c.py"}, "source_file": "a.py", "target_file": "b.py", "source_line_number": 1, "back_edge_line_number": 2}
	if compareImportCycleRows(twoHop, threeHop) >= 0 {
		t.Fatal("shorter cycle must order first")
	}
	other := map[string]any{"repo_id": "s", "cycle_files": []any{"a.py", "b.py"}, "source_file": "a.py", "target_file": "b.py", "source_line_number": 1, "back_edge_line_number": 2}
	if compareImportCycleRows(twoHop, other) >= 0 {
		t.Fatal("repo r must order before repo s")
	}
	later := map[string]any{"repo_id": "r", "cycle_files": []any{"a.py", "b.py"}, "source_file": "a.py", "target_file": "b.py", "source_line_number": 9, "back_edge_line_number": 2}
	if compareImportCycleRows(twoHop, later) >= 0 {
		t.Fatal("earlier source line must order first")
	}
	backLater := map[string]any{"repo_id": "r", "cycle_files": []any{"a.py", "b.py"}, "source_file": "a.py", "target_file": "b.py", "source_line_number": 1, "back_edge_line_number": 9}
	if compareImportCycleRows(twoHop, backLater) >= 0 {
		t.Fatal("earlier back-edge line must order first")
	}
	// Cycle-level comparison mirrors the row contract.
	left := importCycle{steps: []importCycleStep{{file: "a.py", lineNumber: 1}, {file: "b.py", lineNumber: 2}}}
	right := importCycle{steps: []importCycleStep{{file: "a.py", lineNumber: 1}}}
	if compareImportCycles(left, right) <= 0 {
		t.Fatal("longer cycle must order after shorter")
	}
	alt := importCycle{steps: []importCycleStep{{file: "a.py", lineNumber: 1}, {file: "c.py", lineNumber: 2}}}
	if compareImportCycles(left, alt) >= 0 {
		t.Fatal("b.py path must order before c.py path")
	}
	if compareImportCycles(left, left) != 0 {
		t.Fatal("identical cycles must compare equal")
	}
	laterLine := importCycle{steps: []importCycleStep{{file: "a.py", lineNumber: 1}, {file: "b.py", lineNumber: 9}}}
	if compareImportCycles(left, laterLine) >= 0 {
		t.Fatal("earlier line must order first on identical paths")
	}
}

func TestCov6EarlierImportCycleHop(t *testing.T) {
	t.Parallel()
	runtime := importCycleHop{destination: "b", lineNumber: 5, state: edgeStateRuntime}
	inferred := importCycleHop{destination: "b", lineNumber: 1, state: edgeStateInferred}
	// Proof strength beats line number: runtime wins over an earlier inferred line.
	if !earlierImportCycleHop(runtime, inferred) || earlierImportCycleHop(inferred, runtime) {
		t.Fatal("runtime proof must beat an earlier inferred line")
	}
	if got := hopRank(edgeStateRuntime); got != 3 {
		t.Fatalf("runtime hop rank = %d, want 3", got)
	}
	if got := hopRank(edgeStateInferred); got != 2 {
		t.Fatalf("inferred hop rank = %d, want 2", got)
	}
	if got := hopRank(edgeStateUnknown); got != 1 {
		t.Fatalf("unknown hop rank = %d, want 1", got)
	}
	nonPositive := importCycleHop{destination: "b", lineNumber: 0, state: edgeStateRuntime}
	if earlierImportCycleHop(nonPositive, runtime) {
		t.Fatal("non-positive candidate line must never win")
	}
	if !earlierImportCycleHop(runtime, nonPositive) {
		t.Fatal("positive line must beat a non-positive current line")
	}
	early := importCycleHop{destination: "b", lineNumber: 2, state: edgeStateRuntime}
	late := importCycleHop{destination: "b", lineNumber: 7, state: edgeStateRuntime}
	if !earlierImportCycleHop(early, late) {
		t.Fatal("earlier line must win on equal proof")
	}
	alpha := importCycleHop{destination: "b", targetModule: "a", lineNumber: 2, state: edgeStateRuntime}
	beta := importCycleHop{destination: "b", targetModule: "b", lineNumber: 2, state: edgeStateRuntime}
	if !earlierImportCycleHop(alpha, beta) {
		t.Fatal("module name must break line ties")
	}
	// Folding equal proofs keeps the left edge.
	if got := foldImportCycleEdgeState(edgeStateRuntime, edgeStateRuntime); got != edgeStateRuntime {
		t.Fatalf("fold(runtime,runtime) = %v, want runtime", got)
	}
}

func TestCov6EnumerateReciprocalCycle(t *testing.T) {
	t.Parallel()
	edges := []importCycleEdge{
		cov6CycleEdge("r", "a.py", "a", "b", 3, edgeStateRuntime),
		cov6CycleEdge("r", "b.py", "b", "a", 7, edgeStateRuntime),
	}
	cycles, enumeration := enumerateImportCycles(edges, importCycleDefaultMaxLength)
	if len(cycles) != 1 {
		t.Fatalf("reciprocal pair enumerates %d cycles, want 1", len(cycles))
	}
	if len(cycles[0].steps) != 2 {
		t.Fatalf("reciprocal cycle has %d steps, want 2", len(cycles[0].steps))
	}
	_ = enumeration
	// A zero step budget reports truncation instead of hanging.
	_, starved := enumerateImportCyclesWithinBudget(edges, importCycleDefaultMaxLength, 0)
	if !starved.Truncated {
		t.Fatal("zero step budget must report truncation")
	}
	// Type-only edges cannot close a load-time cycle: the production path
	// partitions them out before the walk ever sees them.
	typeOnly := []importCycleEdge{
		cov6CycleEdge("r", "a.py", "a", "b", 3, edgeStateTypeOnly),
		cov6CycleEdge("r", "b.py", "b", "a", 7, edgeStateRuntime),
	}
	kept, _ := partitionImportCycleEdges(typeOnly)
	if got, _ := enumerateImportCycles(kept, importCycleDefaultMaxLength); len(got) != 0 {
		t.Fatalf("type-only edge closes %d cycles, want 0", len(got))
	}
}

func TestCov6ImportDependencyCycleProof(t *testing.T) {
	t.Parallel()
	// Legacy scalar rows fall back to the reciprocal two-edge proof.
	scalar := map[string]any{
		"source_file": "a.py", "target_file": "b.py",
		"source_module": "a", "target_module": "b",
		"source_line_number": 3, "back_edge_line_number": 7,
	}
	length, path, proofEdges := importDependencyCycleProof(scalar)
	if length != 2 || len(path) != 3 || path[0] != "a.py" || path[2] != "a.py" || len(proofEdges) != 2 {
		t.Fatalf("legacy proof = (%d,%v,%d edges), want (2,[a b a],2)", length, path, len(proofEdges))
	}
	full := map[string]any{
		"cycle_files":          []any{"a.py", "b.py"},
		"cycle_source_modules": []any{"a", "b"},
		"cycle_target_modules": []any{"b", "a"},
		"cycle_lines":          []any{3, 7},
		"cycle_flag_states":    []any{"runtime", "runtime"},
	}
	length, path, proofEdges = importDependencyCycleProof(full)
	if length != 2 || len(path) != 3 || len(proofEdges) != 2 {
		t.Fatalf("full proof = (%d,%v,%d edges), want (2,closed path,2)", length, path, len(proofEdges))
	}
	if proofEdges[0]["relationship_type"] != "IMPORTS" || proofEdges[0]["line_number"] != 3 || proofEdges[0]["flag_state"] != "runtime" {
		t.Fatalf("first proof edge = %v, want IMPORTS with line and flag state", proofEdges[0])
	}
	if got := importCycleIntList(full, "cycle_lines"); len(got) != 2 || got[0] != 3 {
		t.Fatalf("int list = %v, want [3 7]", got)
	}
	if got := importCycleIntList(map[string]any{"k": []int{4}}, "k"); len(got) != 1 {
		t.Fatalf("[]int form = %v, want [4]", got)
	}
	if importCycleIntList(map[string]any{"k": []any{"x"}}, "k") != nil {
		t.Fatal("non-numeric members must read back nil")
	}
}

func TestCov6CycleEdgeRowsAndCounters(t *testing.T) {
	t.Parallel()
	pythonRow := func() map[string]any {
		return map[string]any{
			"repo_id": "r", "repo_name": "repo", "source_path": "src/a.py", "source_file": "a.py",
			"source_name": "a.py", "language": "python", "target_module": "b", "line_number": 3,
			"type_only": false, "deferred": false, "inferred": false,
		}
	}
	req := ImportDependencyRequest{RepoID: "r"}
	edge, ok := importCycleEdgeFromRow(req, pythonRow())
	if !ok || edge.sourceModule != "a" || edge.targetModule != "b" {
		t.Fatalf("python edge = (%+v,%v), want modules a->b", edge, ok)
	}
	if _, ok := importCycleEdgeFromRow(req, map[string]any{"language": "cobol"}); ok {
		t.Fatal("cobol row must not decode to a cycle edge")
	}
	scoped := ImportDependencyRequest{RepoID: "other"}
	if _, ok := importCycleEdgeFromRow(scoped, pythonRow()); ok {
		t.Fatal("foreign repo row must not decode under a repo filter")
	}
	unfiltered := ImportDependencyRequest{}
	goRow := pythonRow()
	goRow["language"] = "go"
	if _, ok := importCycleEdgeFromRow(unfiltered, goRow); ok {
		t.Fatal("unfiltered requests stay python-only")
	}
	deduped := deduplicateImportCycleEdges(req, []map[string]any{pythonRow(), pythonRow()})
	if len(deduped) != 1 {
		t.Fatalf("deduplicated edges = %d, want 1", len(deduped))
	}
	if pythonSourceModule("pkg/mod.py") != "pkg/mod" || pythonSourceModule("README.md") != "" {
		t.Fatal("pythonSourceModule must strip .py and reject non-python")
	}
	javaRow := map[string]any{"language": "java", "repo_id": "r", "source_file": "A.java", "source_package": "", "target_module": "com.x.B"}
	if got := countJavaEdgesMissingPackage(javaRowSlice(javaRow)); got != 1 {
		t.Fatalf("java missing-package count = %d, want 1", got)
	}
	packaged := map[string]any{"language": "java", "repo_id": "r", "source_file": "A.java", "source_package": "com.x", "target_module": "com.x.B"}
	if got := countJavaEdgesMissingPackage(javaRowSlice(packaged)); got != 0 {
		t.Fatalf("packaged java count = %d, want 0", got)
	}
	goMissing := map[string]any{"language": "go", "repo_id": "r", "source_file": "a.go", "source_package": "", "target_module": "other/pkg"}
	if got := countGoEdgesMissingPackage([]map[string]any{goMissing}); got != 1 {
		t.Fatalf("go missing-package count = %d, want 1", got)
	}
	tsMissing := map[string]any{"language": "typescript", "repo_id": "r", "source_file": "a.ts", "source_package": "", "target_module": "b"}
	if got := countTSPackageUnknown([]map[string]any{tsMissing}); got != 1 {
		t.Fatalf("ts unknown-package count = %d, want 1", got)
	}
	if !IsJavaCycleLanguage("java") || IsJavaCycleLanguage("kotlin") {
		t.Fatal("java cycle language gate mismatch")
	}
}

func javaRowSlice(rows ...map[string]any) []map[string]any { return rows }

func TestCov6WriteCypherNodeHelpers(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	writeFileNode(&sb, "f", "file_path", "a.go")
	if sb.String() != `(f:File {relative_path: $file_path})` {
		t.Fatalf("file node = %q, want path-qualified node", sb.String())
	}
	sb.Reset()
	writeFileNode(&sb, "f", "file_path", "  ")
	if sb.String() != `(f:File)` {
		t.Fatalf("blank file node = %q, want bare label", sb.String())
	}
	sb.Reset()
	writeModuleNode(&sb, "m", "module", "pkg.mod")
	if sb.String() != `(m:Module {name: $module})` {
		t.Fatalf("module node = %q, want name-qualified node", sb.String())
	}
	sb.Reset()
	writeModuleNode(&sb, "m", "module", "")
	if sb.String() != `(m:Module)` {
		t.Fatalf("blank module node = %q, want bare label", sb.String())
	}
}
