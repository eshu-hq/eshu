// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strconv"
	"strings"
	"testing"
)

// javaTargetPackage truth table (#7349). A Java import names a fully
// qualified CLASS (or member), while a package-cycle node is a PACKAGE, so
// the lift derives the target package by trimming the last dotted segment:
// a single-type import loses its class name, an on-demand import loses its
// .* first. A name with no package (single segment, default package) is
// unplaceable, never guessed.
func TestJavaTargetPackageDerivation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		module string
		want   string
	}{
		{name: "single type import loses its class", module: "com.acme.other.Helper", want: "com.acme.other"},
		{name: "on-demand import loses its star", module: "com.acme.lib.*", want: "com.acme.lib"},
		{name: "java standard library", module: "java.util.List", want: "java.util"},
		{name: "nested class member import trims one segment", module: "com.acme.other.Helper.VERSION", want: "com.acme.other.Helper"},
		{name: "single segment has no package", module: "Helper", want: ""},
		{name: "empty names nothing", module: "", want: ""},
		{name: "star alone names nothing", module: ".*", want: ""},
		{name: "surrounding whitespace is ignored", module: "  com.acme.other.Helper  ", want: "com.acme.other"},
	} {
		if got := javaTargetPackage(tc.module); got != tc.want {
			t.Errorf("%s: javaTargetPackage(%q) = %q, want %q", tc.name, tc.module, got, tc.want)
		}
	}
}

// TestCollapseJavaPackageCycleRowsFoldsFileEdges pins the #7349 fixture-side
// fold: file-level rows collapse to one row per (repository, source package,
// target package) with the earliest positive line and null-if-any-null flag
// folding, matching the production collapsed fetch. Rows no package can
// place are dropped and counted, never guessed onto a node.
func TestCollapseJavaPackageCycleRowsFoldsFileEdges(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		javaFileCycleRow("repo-1", "repo", "/r/src/com/acme/a/A.java", "a/A.java", "A.java", "com.acme.a", "com.acme.b.Bee", 9),
		javaFileCycleRow("repo-1", "repo", "/r/src/com/acme/a/A2.java", "a/A2.java", "A2.java", "com.acme.a", "com.acme.b.Bee", 3),
		javaFileCycleRow("repo-1", "repo", "/r/src/com/acme/b/B.java", "b/B.java", "B.java", "com.acme.b", "com.acme.a.Ay", 5),
		// Same-package files land on one node downstream; the pair keeps
		// the earliest positive line.
		javaFileCycleRow("repo-1", "repo", "/r/src/com/acme/a/A.java", "a/A.java", "A.java", "com.acme.a", "com.acme.b.*", 7),
		// Unplaceable rows: no source package, no derivable target.
		javaFileCycleRow("repo-1", "repo", "/r/Main.java", "Main.java", "Main.java", "", "com.acme.a.Ay", 1),
		javaFileCycleRow("repo-1", "repo", "/r/src/com/acme/a/A.java", "a/A.java", "A.java", "com.acme.a", "Lonely", 1),
	}

	collapsed, dropped := collapseJavaPackageCycleRows(rows)
	if dropped != 2 {
		t.Fatalf("dropped = %d, want the 2 unplaceable rows", dropped)
	}
	if len(collapsed) != 2 {
		t.Fatalf("collapsed = %d rows, want one per package pair", len(collapsed))
	}
	byPair := map[string]map[string]any{}
	for _, row := range collapsed {
		key := row["source_package"].(string) + "\x00" + row["target_package"].(string)
		byPair[key] = row
	}
	pair, ok := byPair["com.acme.a\x00com.acme.b"]
	if !ok {
		t.Fatalf("collapsed pairs = %v, want com.acme.a -> com.acme.b", byPair)
	}
	if got := pair["line_number"]; got != 3 {
		t.Errorf("line_number = %#v, want the earliest positive line 3", got)
	}
	for _, key := range []string{"type_only", "deferred", "inferred"} {
		if value, present := pair[key]; !present || value != false {
			t.Errorf("%s = %#v (present=%v), want explicit false from unflagged Java edges", key, value, present)
		}
	}
}

// TestCollapseJavaPackageCycleRowsChangesNoAnswer pins collapse-location
// invariance: the same file rows decoded directly and decoded after the
// fixture-side fold report the same cycles. Where the collapse happens (the
// collapsed production fetch or the reader) never changes the answer, so the
// file-row fixture proof and the live collapsed-fetch proof agree.
func TestCollapseJavaPackageCycleRowsChangesNoAnswer(t *testing.T) {
	t.Parallel()

	fileRows := []map[string]any{
		javaFileCycleRow("repo-1", "repo", "/r/a/A.java", "a/A.java", "A.java", "com.acme.a", "com.acme.b.Bee", 3),
		javaFileCycleRow("repo-1", "repo", "/r/b/B.java", "b/B.java", "B.java", "com.acme.b", "com.acme.c.Cee", 2),
		javaFileCycleRow("repo-1", "repo", "/r/c/C.java", "c/C.java", "C.java", "com.acme.c", "com.acme.a.Ay", 7),
	}
	collapsed, dropped := collapseJavaPackageCycleRows(fileRows)
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}

	req := ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Language: "java", Limit: 25}
	collapsedRows, _, err := BuildFileImportCycleRows(req, collapsed)
	if err != nil {
		t.Fatalf("BuildFileImportCycleRows(collapsed) error = %v, want nil", err)
	}
	fileResultRows, _, err := BuildFileImportCycleRows(req, fileRows)
	if err != nil {
		t.Fatalf("BuildFileImportCycleRows(file rows) error = %v, want nil", err)
	}
	if len(collapsedRows) != len(fileResultRows) {
		t.Fatalf("collapsed rows %d != file rows %d, want the same answer", len(collapsedRows), len(fileResultRows))
	}
	for i := range collapsedRows {
		left, right := importCycleRowKey(collapsedRows[i]), importCycleRowKey(fileResultRows[i])
		if left != right {
			t.Fatalf("row %d differs: %q != %q, want the same answer", i, left, right)
		}
	}
}

// TestCollapseJavaPackageCycleRowsKeepsFlagshipScaleUnderTheScanBound is the
// #7349 34k-edge tuning gate at the fetch layer: 34,563 file-level rows in
// the largest-java-repo shape must collapse to fewer package-pair rows than
// the 25,000-row internal scan limit, so the collapsed production fetch
// answers the flagship repository instead of failing closed with
// ErrImportDependencyScopeTooBroad.
func TestCollapseJavaPackageCycleRowsKeepsFlagshipScaleUnderTheScanBound(t *testing.T) {
	t.Parallel()

	rows := javaFlagshipScaleFileRows()
	collapsed, _ := collapseJavaPackageCycleRows(rows)
	t.Logf("flagship shape: %d file rows collapse to %d package pairs", len(rows), len(collapsed))
	if len(rows) != 34563 {
		t.Fatalf("file rows = %d, want the reported flagship 34,563", len(rows))
	}
	if len(collapsed) >= 25000 {
		t.Fatalf("package pairs = %d, want fewer than the 25,000-row scan bound", len(collapsed))
	}
}

// TestImportDependencyRequestAcceptsJavaFileImportCycles pins the #7349
// Validation lift: language=java is accepted for file_import_cycles, every
// other non-empty non-python language is still rejected, and an unfiltered
// request still resolves to python, exactly as before.
func TestImportDependencyRequestAcceptsJavaFileImportCycles(t *testing.T) {
	t.Parallel()

	req := ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Language: "java"}
	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil for language=java", err)
	}
	if got := req.EffectiveCycleLanguage(); got != "java" {
		t.Fatalf("EffectiveCycleLanguage() = %q, want java", got)
	}

	unfiltered := ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1"}
	if err := unfiltered.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil for an unfiltered request", err)
	}
	if got := unfiltered.EffectiveCycleLanguage(); got != "python" {
		t.Fatalf("EffectiveCycleLanguage() = %q, want python for an unfiltered request", got)
	}

	for _, language := range []string{"go", "typescript", "tsx", "javascript"} {
		accepted := ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Language: language}
		if err := accepted.Validate(); err != nil {
			t.Fatalf("Validate() error = %v, want nil for language=%s", err, language)
		}
	}
	rejected := ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "repo-1", Language: "rust"}
	if err := rejected.Validate(); err == nil {
		t.Fatal("Validate() = nil for language=rust, want rejection")
	} else if !strings.Contains(err.Error(), "java") || !strings.Contains(err.Error(), "python") {
		t.Fatalf("Validate() error = %v, want it to name the supported languages", err)
	}
}

// javaFlagshipScaleFileRows builds the deterministic #7349 flagship shape:
// 3,387 files across 424 packages (8 per package) carrying 34,563
// file-level rows, ~10 per file. Roughly 7 in 10 imports name JDK or
// third-party classes that dangle; 3 name classes in other repository
// packages and become package-pair hops after collapse.
func javaFlagshipScaleFileRows() []map[string]any {
	const files, pkgs, total = 3387, 424, 34563
	externals := []string{
		"java.util.List", "java.util.Map", "java.lang.String", "java.io.File",
		"org.slf4j.Logger", "com.google.common.collect.ImmutableList", "javax.inject.Inject",
	}
	rows := make([]map[string]any, 0, total)
	for file := 0; file < files; file++ {
		pkg := file / 8
		imports := 10
		if file < total-files*10 {
			imports = 11
		}
		for imp := 0; imp < imports; imp++ {
			target := externals[(file+imp)%len(externals)]
			if imp%10 < 3 {
				targetPkg := (pkg*7 + imp*13 + 1) % pkgs
				if targetPkg == pkg {
					targetPkg = (targetPkg + 1) % pkgs
				}
				target = "com.acme.p" + strconv.Itoa(targetPkg) + ".Helper" + strconv.Itoa(imp)
			}
			rows = append(rows, javaFileCycleRow(
				"repo-flagship", "flagship",
				"/r/src/com/acme/p"+strconv.Itoa(pkg)+"/F"+strconv.Itoa(file)+".java",
				"src/com/acme/p"+strconv.Itoa(pkg)+"/F"+strconv.Itoa(file)+".java",
				"F"+strconv.Itoa(file)+".java",
				"com.acme.p"+strconv.Itoa(pkg),
				target, len(rows)%100+1,
			))
		}
	}
	return rows
}

func javaFileCycleRow(repoID, repoName, sourcePath, sourceFile, sourceName, sourcePackage, targetModule string, line int) map[string]any {
	return map[string]any{
		"repo_id":        repoID,
		"repo_name":      repoName,
		"source_path":    sourcePath,
		"source_file":    sourceFile,
		"source_name":    sourceName,
		"language":       "java",
		"source_package": sourcePackage,
		"target_module":  targetModule,
		"line_number":    line,
		"type_only":      false,
		"deferred":       false,
		"inferred":       false,
	}
}
