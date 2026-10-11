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
// fields, the separator inside a field, unicode, and a pair that differ only
// in where the separator sits (they collide, as they always have).
var keyRows = []struct{ path, repo, rel, kind string }{
	{"env/a.tfvars", "svc", "env/a.tfvars", "terraform_var_file"},
	{"", "", "", ""},
	{"conf/.env", "", "docker-compose.yml", "docker_compose_env_file"},
	{"a|b", "svc", "c", "local_config_asset"},
	{"a", "svc", "b|c", "local_config_asset"},
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
