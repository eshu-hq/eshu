// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"slices"
	"testing"
)

func TestShadowOutcomesAreClosed(t *testing.T) {
	t.Parallel()

	if want := []string{"match", "mismatch", "stored_missing"}; !slices.Equal(ShadowOutcomes(), want) {
		t.Fatalf("ShadowOutcomes() = %v, want %v", ShadowOutcomes(), want)
	}
	outcomes := ShadowOutcomes()
	outcomes[0] = "mutated"
	if ShadowOutcomes()[0] != "match" {
		t.Fatal("ShadowOutcomes returned a shared slice")
	}
}

func TestApplyStoredNameStep(t *testing.T) {
	t.Parallel()

	rows := ApplyStoredNameStep("platform", []StoredRow{
		{RawPath: "platform/env/app.hcl", RelativePath: "env/app.hcl", EvidenceKind: "terraform", NameScoped: true},
		{RawPath: "chart/values.yaml", RelativePath: "values.yaml", EvidenceKind: "helm", NameScoped: false},
		{RawPath: "platform/playbook.yml", RelativePath: "playbook.yml", EvidenceKind: "ansible", NameScoped: false},
	})
	if len(rows) != 3 {
		t.Fatalf("ApplyStoredNameStep() returned %d rows, want 3", len(rows))
	}
	// A name-scoped row loses the repository-name prefix (strip only, never a
	// second clean); any other row keeps its raw path, even when the name is
	// a path component of it.
	if rows[0].Path != "env/app.hcl" {
		t.Errorf("scoped path = %q, want %q", rows[0].Path, "env/app.hcl")
	}
	if rows[1].Path != "chart/values.yaml" {
		t.Errorf("unscoped helm path = %q, want it unchanged", rows[1].Path)
	}
	if rows[2].Path != "platform/playbook.yml" {
		t.Errorf("unscoped ansible path = %q, want it unchanged", rows[2].Path)
	}
	for i, row := range rows {
		if row.SourceRepo != "platform" {
			t.Errorf("row %d source_repo = %q, want platform", i, row.SourceRepo)
		}
	}
	if rows[0].RelativePath != "env/app.hcl" || rows[0].EvidenceKind != "terraform" {
		t.Errorf("scoped row kept %+v, want the stored relative path and kind", rows[0])
	}
}

func TestApplyStoredNameStepWithBlankName(t *testing.T) {
	t.Parallel()

	rows := ApplyStoredNameStep("", []StoredRow{
		{RawPath: "platform/env/app.hcl", RelativePath: "env/app.hcl", EvidenceKind: "terraform", NameScoped: true},
	})
	if len(rows) != 1 || rows[0].Path != "platform/env/app.hcl" {
		t.Fatalf("blank name changed the path: %v", rows)
	}
}

func TestEqualNamedRows(t *testing.T) {
	t.Parallel()

	row := func(path, repo, relative, kind string) NamedRow {
		return NamedRow{Path: path, SourceRepo: repo, RelativePath: relative, EvidenceKind: kind}
	}
	live := []NamedRow{
		row("env/app.hcl", "platform", "env/app.hcl", "terraform"),
		row("values.yaml", "platform", "values.yaml", "helm"),
	}
	same := []NamedRow{
		row("values.yaml", "platform", "values.yaml", "helm"),
		row("env/app.hcl", "platform", "env/app.hcl", "terraform"),
	}
	if !EqualNamedRows(live, same) {
		t.Error("EqualNamedRows() = false for the same rows in another order, want true")
	}
	if !EqualNamedRows(nil, nil) {
		t.Error("EqualNamedRows(nil, nil) = false, want true")
	}
	for name, other := range map[string][]NamedRow{
		"extra row":   append(slices.Clone(live), row("extra.hcl", "platform", "extra.hcl", "terraform")),
		"missing row": live[:1],
		"moved path": {
			row("env/renamed.hcl", "platform", "env/app.hcl", "terraform"),
			row("values.yaml", "platform", "values.yaml", "helm"),
		},
		"moved repo": {
			row("env/app.hcl", "other", "env/app.hcl", "terraform"),
			row("values.yaml", "platform", "values.yaml", "helm"),
		},
	} {
		if EqualNamedRows(live, other) {
			t.Errorf("EqualNamedRows() = true with a %s, want false", name)
		}
	}
}

// TestCompareNamedRowsAgreesWithCompareConfigPaths holds the struct ordering
// to the row-map ordering: the same identity in both shapes must sort the
// same way, or the shadow compare and the loader disagree on row identity.
func TestCompareNamedRowsAgreesWithCompareConfigPaths(t *testing.T) {
	t.Parallel()

	named := func(path, repo, relative, kind string) NamedRow {
		return NamedRow{Path: path, SourceRepo: repo, RelativePath: relative, EvidenceKind: kind}
	}
	mapped := func(path, repo, relative, kind string) map[string]any {
		return map[string]any{"path": path, "source_repo": repo, "relative_path": relative, "evidence_kind": kind}
	}
	pairs := [][2][4]string{
		{{"b.hcl", "svc", "b.hcl", "terraform"}, {"a.hcl", "svc", "a.hcl", "terraform"}},
		{{"a.hcl", "svc-b", "a.hcl", "terraform"}, {"a.hcl", "svc-a", "a.hcl", "terraform"}},
		{{"a.hcl", "svc", "b.hcl", "terraform"}, {"a.hcl", "svc", "a.hcl", "terraform"}},
		{{"a.hcl", "svc", "a.hcl", "helm"}, {"a.hcl", "svc", "a.hcl", "ansible"}},
		{{"a.hcl", "svc", "a.hcl", "terraform"}, {"a.hcl", "svc", "a.hcl", "terraform"}},
	}
	for _, pair := range pairs {
		left, right := pair[0], pair[1]
		got := CompareNamedRows(named(left[0], left[1], left[2], left[3]), named(right[0], right[1], right[2], right[3]))
		want := CompareConfigPaths(mapped(left[0], left[1], left[2], left[3]), mapped(right[0], right[1], right[2], right[3]))
		if (got < 0) != (want < 0) || (got > 0) != (want > 0) {
			t.Errorf("CompareNamedRows(%v, %v) = %d, CompareConfigPaths = %d: orderings disagree", left, right, got, want)
		}
	}
}
