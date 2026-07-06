// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestNativeRepositorySnapshotterDerivesImportsMapWithoutSecondParse pins
// issue #4764: for a full-ingest snapshot (no FileTargets), php, javascript,
// typescript, and tsx files must contribute to ImportsMap without a second
// dedicated tree-sitter pre-scan pass — the parse stage's own payload is the
// only pass tree-sitter makes over these files. This is the collector-level
// analogue of php/walk_count_test.go's WalkNamed-call-count pin, but counts
// Engine.preScanOnePath's dispatch into a derive-eligible language's PreScan
// implementation instead (parser.DerivedLanguagePreScanDispatchCountForTest),
// so a regression that reintroduces the second parse fails this test even if
// ImportsMap output happens to still be correct.
//
// Not parallel: the dispatch counter is a package-level counter in the parser
// package (documented process-global, test-only).
func TestNativeRepositorySnapshotterDerivesImportsMapWithoutSecondParse(t *testing.T) {
	repoRoot := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "greeter.php"),
		"<?php\nclass Greeter {\n    public function hello(): void {}\n}\n"+
			"$anon = new class {\n    public function handle(): void {}\n};\n",
	)
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "handlers.js"),
		"const handlers = {\n  onStart() { return 1; },\n};\n"+
			"module.exports.encode = function encode(x) { return x; };\n",
	)
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "service.ts"),
		"interface Reader {}\nexport class Service implements Reader {\n  load(): void {}\n}\n",
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	parser.ResetDerivedLanguagePreScanDispatchCountForTest()

	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	got, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{RepoPath: repoRoot},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if dispatches := parser.DerivedLanguagePreScanDispatchCountForTest(); dispatches != 0 {
		t.Fatalf(
			"derived-language PreScan dispatch count = %d, want 0 (php/javascript/typescript/tsx must not run a second tree-sitter pre-scan pass on a full-ingest snapshot)",
			dispatches,
		)
	}

	wantNames := []string{"Greeter", "hello", "handle", "onStart", "encode", "Reader", "Service", "load"}
	for _, name := range wantNames {
		if _, ok := got.ImportsMap[name]; !ok {
			t.Errorf("ImportsMap missing %q (derived from parse payload): %#v", name, got.ImportsMap)
		}
	}
	// The synthesized PHP anonymous-class name is line-number-dependent; just
	// assert some anonymous_class_ entry exists rather than pinning the line.
	var sawAnonymousClass bool
	for name := range got.ImportsMap {
		if len(name) > len("anonymous_class_") && name[:len("anonymous_class_")] == "anonymous_class_" {
			sawAnonymousClass = true
			break
		}
	}
	if !sawAnonymousClass {
		t.Errorf("ImportsMap missing a derived PHP anonymous_class_ entry: %#v", got.ImportsMap)
	}
}

// TestNativeRepositorySnapshotterDeltaSyncKeepsLegacyPreScanForDerivedLanguages
// pins the safety boundary documented on partitionPreScanFilesForDerive: a
// delta sync (FileTargets set) must NOT switch derive-eligible languages onto
// the parse-derived path, because pre-scan needs the full repository file set
// (parserPreScanFiles(fullParserFiles)) while parse only visits the changed
// targets this cycle. Deriving from parse output in that case would silently
// drop ImportsMap entries for every unchanged derive-eligible file, so the
// legacy PreScan dispatch count must stay > 0 for an unchanged PHP file that
// sits outside FileTargets.
func TestNativeRepositorySnapshotterDeltaSyncKeepsLegacyPreScanForDerivedLanguages(t *testing.T) {
	repoRoot := t.TempDir()
	targetFile := filepath.Join(repoRoot, "changed.php")
	writeCollectorTestFile(t, targetFile, "<?php\nfunction changed() {}\n")
	writeCollectorTestFile(
		t,
		filepath.Join(repoRoot, "unchanged.php"),
		"<?php\nclass Unchanged {}\n",
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	parser.ResetDerivedLanguagePreScanDispatchCountForTest()

	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	got, err := snapshotter.SnapshotRepository(
		context.Background(),
		SelectedRepository{
			RepoPath:    repoRoot,
			FileTargets: []string{targetFile},
			Delta:       true,
		},
	)
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if dispatches := parser.DerivedLanguagePreScanDispatchCountForTest(); dispatches == 0 {
		t.Fatal("derived-language PreScan dispatch count = 0, want > 0 (delta sync must keep the legacy pre-scan pass for derive-eligible languages)")
	}

	unchangedPaths, ok := got.ImportsMap["Unchanged"]
	if !ok {
		t.Fatalf("ImportsMap missing unchanged PHP class entry: %#v", got.ImportsMap)
	}
	if got, want := filepath.Base(unchangedPaths[0]), "unchanged.php"; got != want {
		t.Fatalf("ImportsMap[Unchanged][0] base = %q, want %q", got, want)
	}
	if _, ok := got.ImportsMap["changed"]; !ok {
		t.Fatalf("ImportsMap missing targeted PHP function entry: %#v", got.ImportsMap)
	}
}

// TestMergeParsedFilesIntoDerivedImportsMapSkipsNonDerivedLanguages is a
// focused unit test for mergeParsedFilesIntoDerivedImportsMap's language
// gate: a payload for a language outside parser.IsDerivedPreScanLanguage
// (e.g. python) must not contribute names, since its ImportsMap contribution
// already came from the legacy pre-scan pass.
func TestMergeParsedFilesIntoDerivedImportsMapSkipsNonDerivedLanguages(t *testing.T) {
	importsMap := map[string][]string{}
	parsedFiles := []map[string]any{
		{
			"lang": "python",
			"path": "/repo/app.py",
			"functions": []map[string]any{
				{"name": "handler"},
			},
		},
		{
			"lang": "php",
			"path": "/repo/app.php",
			"functions": []map[string]any{
				{"name": "bootstrap"},
			},
		},
	}

	mergeParsedFilesIntoDerivedImportsMap(importsMap, parsedFiles)

	if _, ok := importsMap["handler"]; ok {
		t.Fatal(`importsMap["handler"] present, want absent (python is not derive-eligible)`)
	}
	paths, ok := importsMap["bootstrap"]
	if !ok {
		t.Fatal(`importsMap["bootstrap"] missing, want present (php is derive-eligible)`)
	}
	if want := []string{"/repo/app.php"}; !slices.Equal(paths, want) {
		t.Fatalf(`importsMap["bootstrap"] = %v, want %v`, paths, want)
	}
}
