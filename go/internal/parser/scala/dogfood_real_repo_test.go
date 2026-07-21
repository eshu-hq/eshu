// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scala

// TestDogfoodScalaRealRepoSnapshot is the offline-reproducible dogfood proof
// for eshu-hq/eshu#5399 (spun off from #5336's real-repo-validated bar in
// docs/public/languages/support-maturity.md#grade-definitions). It runs the
// production Parse() entrypoint against the committed, app-shaped corpus at
// tests/fixtures/dogfood/scala_real_repo -- a synthetic Play-style
// app/{controllers,models,services} layout whose shape is informed by
// public patterns in playframework/playframework at pinned SHA
// bcdc682de2250bbd0f2788bc5acc06f6d66ad5a7 and scala/scala at pinned SHA
// 25075e9b9b79954a0f99de515618901818822e62 (provenance metadata only:
// neither repo is fetched or vendored, see each fixture file's own header
// comment; those SHAs match the historical Issue #105 dogfood run cited in
// docs/public/languages/scala.md) -- and asserts the parser's per-file
// bucket counts (functions, classes, traits, imports, function_calls,
// variables) match the checked-in snapshot at
// testdata/dogfood_real_repo_snapshot.txt.
//
// This is a standing regression test, not an opt-in dump: it runs in every
// `go test ./internal/parser/scala/...` and fails the moment the corpus or
// the parser output drifts from the snapshot, the same golden-file
// discipline the scala_comprehensive fixture already uses in this package
// tree. Regenerate the snapshot after an intentional parser change with
// `DOGFOOD_UPDATE_SNAPSHOT=1 go test ./internal/parser/scala/... -run
// TestDogfoodScalaRealRepoSnapshot`, or via scripts/dogfood-scala.sh.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_scala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
)

const (
	scalaDogfoodCorpusRoot   = "../../../../tests/fixtures/dogfood/scala_real_repo"
	scalaDogfoodSnapshotPath = "testdata/dogfood_real_repo_snapshot.txt"
)

func TestDogfoodScalaRealRepoSnapshot(t *testing.T) {
	t.Parallel()

	paths := scalaDogfoodCorpusPaths(t)
	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_scala.Language())); err != nil {
		t.Fatalf("SetLanguage(scala) error = %v, want nil", err)
	}
	defer parser.Close()

	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		payload, err := Parse(path, false, shared.Options{}, parser)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v, want nil", path, err)
		}
		rel, err := filepath.Rel(scalaDogfoodCorpusRoot, path)
		if err != nil {
			t.Fatalf("filepath.Rel(%q, %q) error = %v, want nil", scalaDogfoodCorpusRoot, path, err)
		}
		lines = append(lines, fmt.Sprintf("%s\t%s", filepath.ToSlash(rel), dogfoodBucketSummary(payload)))
	}
	sort.Strings(lines)

	got := strings.Join(lines, "\n") + "\n"
	assertDogfoodSnapshot(t, scalaDogfoodSnapshotPath, got)
}

func scalaDogfoodCorpusPaths(t *testing.T) []string {
	t.Helper()
	var paths []string
	err := filepath.Walk(scalaDogfoodCorpusRoot, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(p) == ".scala" {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("filepath.Walk(%q) error = %v, want nil", scalaDogfoodCorpusRoot, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no .scala fixtures found under %q", scalaDogfoodCorpusRoot)
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
