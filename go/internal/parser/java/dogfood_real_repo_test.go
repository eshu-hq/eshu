// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

// TestDogfoodJavaRealRepoSnapshot is the offline-reproducible dogfood proof
// for eshu-hq/eshu#5399 (spun off from #5336's real-repo-validated bar in
// docs/public/languages/support-maturity.md#grade-definitions). It runs the
// production Parse() entrypoint against the committed, app-shaped corpus at
// tests/fixtures/dogfood/java_real_repo -- a synthetic Spring Boot-style
// src/main/java + src/test/java controller/service/model layout, informed
// by public patterns in Spring Boot-style services (no external repo or
// pinned SHA is cited here: java.md never carried a specific external-repo
// dogfood claim to preserve provenance for, see each fixture file's own
// header comment) -- and asserts the parser's per-file bucket counts
// (functions, classes, interfaces, annotations, imports, function_calls,
// variables) match the checked-in snapshot at
// testdata/dogfood_real_repo_snapshot.txt.
//
// This is a standing regression test, not an opt-in dump: it runs in every
// `go test ./internal/parser/java/...` and fails the moment the corpus or
// the parser output drifts from the snapshot, the same golden-file
// discipline the java_comprehensive fixture already uses in this package
// tree. Regenerate the snapshot after an intentional parser change with
// `DOGFOOD_UPDATE_SNAPSHOT=1 go test ./internal/parser/java/... -run
// TestDogfoodJavaRealRepoSnapshot`, or via scripts/dogfood-java.sh.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

const (
	javaDogfoodCorpusRoot   = "../../../../tests/fixtures/dogfood/java_real_repo"
	javaDogfoodSnapshotPath = "testdata/dogfood_real_repo_snapshot.txt"
)

func TestDogfoodJavaRealRepoSnapshot(t *testing.T) {
	t.Parallel()

	paths := javaDogfoodCorpusPaths(t)
	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		parser := tree_sitter.NewParser()
		if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_java.Language())); err != nil {
			t.Fatalf("SetLanguage(java) error = %v, want nil", err)
		}
		payload, err := Parse(path, false, shared.Options{}, parser)
		parser.Close()
		if err != nil {
			t.Fatalf("Parse(%q) error = %v, want nil", path, err)
		}
		rel, err := filepath.Rel(javaDogfoodCorpusRoot, path)
		if err != nil {
			t.Fatalf("filepath.Rel(%q, %q) error = %v, want nil", javaDogfoodCorpusRoot, path, err)
		}
		lines = append(lines, fmt.Sprintf("%s\t%s", filepath.ToSlash(rel), dogfoodBucketSummary(payload)))
	}
	sort.Strings(lines)

	got := strings.Join(lines, "\n") + "\n"
	assertDogfoodSnapshot(t, javaDogfoodSnapshotPath, got)
}

func javaDogfoodCorpusPaths(t *testing.T) []string {
	t.Helper()
	var paths []string
	err := filepath.Walk(javaDogfoodCorpusRoot, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(p) == ".java" {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("filepath.Walk(%q) error = %v, want nil", javaDogfoodCorpusRoot, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no .java fixtures found under %q", javaDogfoodCorpusRoot)
	}
	sort.Strings(paths)
	return paths
}

// dogfoodBucketSummary returns a deterministic "<bucket>=<count>" summary,
// space-joined and sorted by bucket name, for every []map[string]any bucket
// in a parser payload.
func dogfoodBucketSummary(payload map[string]any) string {
	keys := make([]string, 0, len(payload))
	for key, value := range payload {
		if _, ok := value.([]map[string]any); ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		items, _ := payload[key].([]map[string]any)
		parts = append(parts, fmt.Sprintf("%s=%d", key, len(items)))
	}
	return strings.Join(parts, " ")
}

// assertDogfoodSnapshot compares got against the checked-in snapshot file at
// path (relative to the package directory). Set DOGFOOD_UPDATE_SNAPSHOT=1 to
// regenerate the snapshot instead of asserting equality.
func assertDogfoodSnapshot(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("DOGFOOD_UPDATE_SNAPSHOT") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("os.WriteFile(%q) error = %v, want nil", path, err)
		}
		t.Logf("updated dogfood snapshot at %q", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error = %v, want nil (run with DOGFOOD_UPDATE_SNAPSHOT=1 to create it)", path, err)
	}
	if got != string(want) {
		t.Fatalf("dogfood snapshot mismatch for %q\n--- got ---\n%s\n--- want ---\n%s", path, got, string(want))
	}
}
