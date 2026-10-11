// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"reflect"
	"strings"
	"testing"
)

// legacyDedupKey is a frozen copy of the merged load's row identity before the
// four-field order was kept in one place: path, source_repo, relative_path and
// evidence_kind, joined with a vertical bar. It shares no code with
// configPathDedupKey, so a change to the order array cannot move its own
// oracle.
func legacyDedupKey(row map[string]any) string {
	return strings.Join([]string{
		stringValue(row, "path"),
		stringValue(row, "source_repo"),
		stringValue(row, "relative_path"),
		stringValue(row, "evidence_kind"),
	}, "|")
}

// legacyAppendKey is a frozen copy of the key appendConfigArtifactRow used for
// its per-extractor dedup before the same change.
func legacyAppendKey(pathValue, repoName, relativePath, evidenceKind string) string {
	return strings.Join([]string{pathValue, repoName, relativePath, evidenceKind}, "|")
}

// keyRows holds the values the key must carry unchanged: ordinary rows, empty
// fields, the separator inside a field, and unicode. The two rows with
// local_config_asset and relative path c are a real collision: both join to
// a|b|svc|c|local_config_asset, as they always have
// (TestConfigPathKeyCollidesWhenAFieldHoldsTheSeparator).
var keyRows = []struct{ path, repo, rel, kind string }{
	{"env/a.tfvars", "svc", "env/a.tfvars", "terraform_var_file"},
	{"", "", "", ""},
	{"conf/.env", "", "docker-compose.yml", "docker_compose_env_file"},
	{"a|b", "svc", "c", "local_config_asset"},
	{"a", "b|svc", "c", "local_config_asset"},
	{"a|b", "c", "", "x"},
	{"ü-é/日本語/🙂.tf", "svc|api", "ü.tf", "terraform_module_source_path"},
	{"/configd/svc/*", "svc", "svc/res.yaml", "kustomize_policy_document_resource"},
	{"p", "r", "rel", "k|"},
}

// TestConfigPathKeyEqualsTheFrozenConstruction pins the dedup key of the merged
// load to a frozen literal construction for rows with empty fields, the
// separator and unicode.
func TestConfigPathKeyEqualsTheFrozenConstruction(t *testing.T) {
	t.Parallel()

	for _, c := range keyRows {
		row := map[string]any{
			"path": c.path, "source_repo": c.repo, "relative_path": c.rel, "evidence_kind": c.kind,
		}
		if got, want := configPathDedupKey(row), legacyDedupKey(row); got != want {
			t.Errorf("dedup key of %+v = %q, want %q", c, got, want)
		}
	}
	if got := configPathDedupKey(map[string]any{}); got != "|||" {
		t.Errorf("dedup key of an empty row = %q, want the three separators", got)
	}
}

// TestConfigPathKeyCollidesWhenAFieldHoldsTheSeparator pins the behavior the
// decision record calls out: the row identity is the four fields joined with a
// vertical bar, with no escaping and no length prefix, so a field that holds
// the bar can make two different rows share one key. The stored read model
// keys on a length-prefixed digest and does not have the weakness; this test
// states that the live path does, so a refactor cannot change it by accident.
func TestConfigPathKeyCollidesWhenAFieldHoldsTheSeparator(t *testing.T) {
	t.Parallel()

	left := map[string]any{"path": "a|b", "source_repo": "svc", "relative_path": "c", "evidence_kind": "local_config_asset"}
	right := map[string]any{"path": "a", "source_repo": "b|svc", "relative_path": "c", "evidence_kind": "local_config_asset"}
	if reflect.DeepEqual(left, right) {
		t.Fatal("the two rows are the same row; the fixture is vacuous")
	}
	const want = "a|b|svc|c|local_config_asset"
	for name, got := range map[string]string{
		"configPathDedupKey(left)":  configPathDedupKey(left),
		"configPathDedupKey(right)": configPathDedupKey(right),
		"legacyDedupKey(left)":      legacyDedupKey(left),
		"legacyDedupKey(right)":     legacyDedupKey(right),
	} {
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	// The merged load keeps the first of the two, as the live loader always has.
	merged := MergeConfigPaths([]map[string]any{left}, []map[string]any{right})
	if len(merged) != 1 || !reflect.DeepEqual(merged[0], left) {
		t.Errorf("MergeConfigPaths kept %v, want only the first row %v", merged, left)
	}

	// The per-extractor dedup collides the same way: the second append is dropped.
	seen := map[string]struct{}{}
	rows := appendConfigArtifactRow(nil, seen, "a|b", "svc", "c", "local_config_asset")
	rows = appendConfigArtifactRow(rows, seen, "a", "b|svc", "c", "local_config_asset")
	if len(rows) != 1 || stringValue(rows[0], "path") != "a|b" {
		t.Errorf("appendConfigArtifactRow kept %v, want only the first row", rows)
	}
}

// TestAppendConfigArtifactRowDuplicateDoesNotBuildARow proves the duplicate
// path of appendConfigArtifactRow allocates the key and nothing for a row: a
// repeated reference is the common case of the compose and HCL extractors, and
// the row map is built only for a key not seen before.
func TestAppendConfigArtifactRowDuplicateDoesNotBuildARow(t *testing.T) {
	// Not parallel: AllocsPerRun must not run beside other tests.
	const pathValue, repo, rel, kind = "conf/.env", "svc", "docker-compose.yml", "docker_compose_env_file"

	unique := testing.AllocsPerRun(200, func() {
		seen := map[string]struct{}{}
		_ = appendConfigArtifactRow(nil, seen, pathValue, repo, rel, kind)
	})
	seen := map[string]struct{}{}
	rows := appendConfigArtifactRow(nil, seen, pathValue, repo, rel, kind)
	duplicate := testing.AllocsPerRun(200, func() {
		if got := appendConfigArtifactRow(rows, seen, pathValue, repo, rel, kind); len(got) != 1 {
			t.Fatalf("a duplicate append changed the row count to %d", len(got))
		}
	})
	t.Logf("allocs per call: unique append %.0f, duplicate append %.0f", unique, duplicate)
	// A new seen map, a four-entry row map and the appended slice are the
	// allocations a duplicate avoids.
	if duplicate > unique-2 {
		t.Fatalf("a duplicate append allocates %.0f, a unique append %.0f; the duplicate path builds the row", duplicate, unique)
	}
}

// TestAppendConfigArtifactRowKeepsTheFrozenKey proves the key
// appendConfigArtifactRow records in the extractor's seen set is the frozen
// construction, and that it is the same key the merged load computes for the
// row it appended. Both read one order array.
func TestAppendConfigArtifactRowKeepsTheFrozenKey(t *testing.T) {
	t.Parallel()

	for _, c := range keyRows {
		if strings.TrimSpace(c.path) == "" {
			continue // an empty path is dropped before any key exists
		}
		seen := map[string]struct{}{}
		rows := appendConfigArtifactRow(nil, seen, c.path, c.repo, c.rel, c.kind)
		if len(rows) != 1 {
			// A path the clean step reduces to empty is dropped; not a key case.
			continue
		}
		stored := rows[0]
		want := legacyAppendKey(stringValue(stored, "path"), c.repo, c.rel, c.kind)
		if _, ok := seen[want]; !ok || len(seen) != 1 {
			t.Errorf("seen set for %+v = %v, want exactly %q", c, seen, want)
		}
		if got := configPathDedupKey(stored); got != want {
			t.Errorf("merged-load key %q differs from the append-time key %q for %+v", got, want, c)
		}
		// A second identical append adds no row.
		if again := appendConfigArtifactRow(rows, seen, c.path, c.repo, c.rel, c.kind); len(again) != 1 {
			t.Errorf("a repeated append of %+v added a row: %d rows", c, len(again))
		}
	}
}

// TestConfigPathKeyOrderIsPathSourceRelativeKind pins the order array itself:
// it is the sort order, the merge key and the append key at once, so a
// reordering changes all three.
func TestConfigPathKeyOrderIsPathSourceRelativeKind(t *testing.T) {
	t.Parallel()

	want := []string{"path", "source_repo", "relative_path", "evidence_kind"}
	if got := configPathKeyOrder[:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("configPathKeyOrder = %v, want %v", got, want)
	}
}
