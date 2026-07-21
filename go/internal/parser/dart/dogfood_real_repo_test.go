// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

// TestDogfoodDartRealRepoSnapshot is the offline-reproducible dogfood proof
// for eshu-hq/eshu#5399 (spun off from #5336's real-repo-validated bar in
// docs/public/languages/support-maturity.md#grade-definitions). It runs the
// production Parse() entrypoint against the committed, app-shaped corpus at
// tests/fixtures/dogfood/dart_real_repo -- a synthetic lib/, lib/src/, test/
// layout whose shape is informed by public patterns in flutter/flutter and
// dart-lang/http (provenance metadata only: neither repo is fetched or
// vendored, see each fixture file's own header comment) -- and asserts the
// parser's per-file, row-level output matches the checked-in snapshot at
// testdata/dogfood_real_repo_snapshot.txt. The snapshot pins one line per
// emitted entity/relationship (functions, classes, imports, function_calls,
// variables) with its identifying fields -- name, line number, call target,
// dead-code root metadata -- not merely the per-bucket counts, so a regression
// that corrupts an identifier while preserving the counts still fails the
// snapshot (see TestDogfoodDartRowLevelCatchesCountPreservingCorruption).
//
// This is a standing regression test, not an opt-in dump: it runs in every
// `go test ./internal/parser/dart/...` and fails the moment the corpus or
// the parser output drifts from the snapshot, the same golden-file
// discipline the dart_comprehensive fixture already uses in this package
// tree. Regenerate the snapshot after an intentional parser change with
// `DOGFOOD_UPDATE_SNAPSHOT=1 go test ./internal/parser/dart/... -run
// TestDogfoodDartRealRepoSnapshot`, or via scripts/dogfood-dart.sh.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

const (
	dartDogfoodCorpusRoot   = "../../../../tests/fixtures/dogfood/dart_real_repo"
	dartDogfoodSnapshotPath = "testdata/dogfood_real_repo_snapshot.txt"
)

func TestDogfoodDartRealRepoSnapshot(t *testing.T) {
	t.Parallel()

	paths := dartDogfoodCorpusPaths(t)
	var lines []string
	for _, path := range paths {
		payload, err := Parse(path, false, shared.Options{})
		if err != nil {
			t.Fatalf("Parse(%q) error = %v, want nil", path, err)
		}
		rel, err := filepath.Rel(dartDogfoodCorpusRoot, path)
		if err != nil {
			t.Fatalf("filepath.Rel(%q, %q) error = %v, want nil", dartDogfoodCorpusRoot, path, err)
		}
		relSlash := filepath.ToSlash(rel)
		for _, row := range dogfoodRowLines(payload) {
			lines = append(lines, fmt.Sprintf("%s\t%s", relSlash, row))
		}
	}
	sort.Strings(lines)

	got := strings.Join(lines, "\n") + "\n"
	assertDogfoodSnapshot(t, dartDogfoodSnapshotPath, got)
}

func dartDogfoodCorpusPaths(t *testing.T) []string {
	t.Helper()
	var paths []string
	err := filepath.Walk(dartDogfoodCorpusRoot, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(p) == ".dart" {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("filepath.Walk(%q) error = %v, want nil", dartDogfoodCorpusRoot, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no .dart fixtures found under %q", dartDogfoodCorpusRoot)
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

// dogfoodRowLines returns deterministic, row-level snapshot lines for every
// emitted entity/relationship bucket row plus the framework_semantics route
// evidence in payload. Each line is "<bucket>\t<canonical fields>" and
// carries the identifying fields a parser regression would corrupt while
// preserving the coarse per-bucket counts: name/identifier, line number, the
// call target (full_name) for calls, dead-code root metadata
// (dead_code_root_kinds), and route method/path/handler evidence. The dogfood
// corpus is intentionally small (a handful of app-shaped fixtures), so this is
// a complete projection of the identifying fields with no truncation; the
// lines are sorted for a stable, diff-friendly golden file.
func dogfoodRowLines(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var lines []string
	for _, key := range keys {
		switch value := payload[key].(type) {
		case []map[string]any:
			for _, row := range value {
				lines = append(lines, key+"\t"+dogfoodCanonicalMap(row))
			}
		case map[string]any:
			// framework_semantics: route evidence surfaced as a top-level map.
			lines = append(lines, key+"\t"+dogfoodCanonicalValue(value))
		}
	}
	sort.Strings(lines)
	return lines
}

// dogfoodCanonicalMap renders a row map as a deterministic, key-sorted
// "key=value" projection so a change to any identifying field surfaces as a
// snapshot diff.
func dogfoodCanonicalMap(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+dogfoodCanonicalValue(m[key]))
	}
	return strings.Join(parts, " ")
}

// dogfoodCanonicalValue renders any parser payload value deterministically.
// Strings are quoted so embedded spaces or route delimiters cannot blur field
// boundaries; slices and nested maps (route entries, framework metadata) are
// rendered recursively in their emitted order. An unexpected type falls
// through to a quoted %v so it is loud in the snapshot rather than silently
// dropped.
func dogfoodCanonicalValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return strconv.Quote(t)
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case []string:
		parts := make([]string, len(t))
		for i, s := range t {
			parts[i] = strconv.Quote(s)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case []map[string]string:
		parts := make([]string, len(t))
		for i, m := range t {
			parts[i] = "{" + dogfoodCanonicalMap(dogfoodStringMapToAny(m)) + "}"
		}
		return "[" + strings.Join(parts, ",") + "]"
	case []map[string]any:
		parts := make([]string, len(t))
		for i, m := range t {
			parts[i] = "{" + dogfoodCanonicalMap(m) + "}"
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]string:
		return "{" + dogfoodCanonicalMap(dogfoodStringMapToAny(t)) + "}"
	case map[string]any:
		return "{" + dogfoodCanonicalMap(t) + "}"
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = dogfoodCanonicalValue(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	default:
		return strconv.Quote(fmt.Sprintf("%v", t))
	}
}

func dogfoodStringMapToAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for key, value := range m {
		out[key] = value
	}
	return out
}

// TestDogfoodDartRowLevelCatchesCountPreservingCorruption pins the reason the
// snapshot was upgraded from per-bucket counts to row-level rows (#5399): a
// parser regression that corrupts an identifying field while preserving every
// bucket count is invisible to dogfoodBucketSummary but must be caught by
// dogfoodRowLines. It swaps two functions' line numbers (a count-preserving
// corruption) and asserts the count-only summary stays identical (the gap the
// old snapshot left open) while the row-level projection diverges (the gap the
// new snapshot closes). If dogfoodRowLines ever regresses to a count-only
// projection, the second assertion fails.
func TestDogfoodDartRowLevelCatchesCountPreservingCorruption(t *testing.T) {
	t.Parallel()

	clean := map[string]any{
		"functions": []map[string]any{
			{"name": "handle", "line_number": 10, "lang": "dart"},
			{"name": "render", "line_number": 20, "lang": "dart"},
		},
	}
	// Same two functions, same names, same bucket count -- only the line
	// numbers are swapped, exactly the corruption class the count-only
	// snapshot could not see.
	corrupt := map[string]any{
		"functions": []map[string]any{
			{"name": "handle", "line_number": 20, "lang": "dart"},
			{"name": "render", "line_number": 10, "lang": "dart"},
		},
	}

	if got, want := dogfoodBucketSummary(corrupt), dogfoodBucketSummary(clean); got != want {
		t.Fatalf("count-only summary changed under a count-preserving corruption: got %q, want %q", got, want)
	}

	cleanRows := strings.Join(dogfoodRowLines(clean), "\n")
	corruptRows := strings.Join(dogfoodRowLines(corrupt), "\n")
	if cleanRows == corruptRows {
		t.Fatalf("row-level projection failed to catch a count-preserving line-number corruption:\n%s", cleanRows)
	}
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
