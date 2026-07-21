// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"testing"
)

// TestEnrichWorkflowArtifactRowUnpinnedActionRefs proves the repository
// workflow-artifact rollup (issue #5372, #5335 real-consumer requirement)
// surfaces unpinned_action_refs -- the raw `owner/repo@ref` strings for every
// third-party action step whose ref is not a full-length commit SHA -- and
// sets the corresponding signal. A pinned action (full 40-hex SHA) and
// actions/checkout (excluded from action-repository detection entirely) must
// not appear.
func TestEnrichWorkflowArtifactRowUnpinnedActionRefs(t *testing.T) {
	t.Parallel()

	content := `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: octo-org/octo-action@a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2
      - uses: octo-org/mutable-action@main
      - uses: octo-org/tagged-action@v1.2.3
`
	row := map[string]any{}
	enrichWorkflowArtifactRow(row, content)

	got, _ := row["unpinned_action_refs"].([]string)
	sort.Strings(got)
	want := []string{"octo-org/mutable-action@main", "octo-org/tagged-action@v1.2.3"}
	if len(got) != len(want) {
		t.Fatalf("unpinned_action_refs = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("unpinned_action_refs[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	signals, _ := row["signals"].([]string)
	found := false
	for _, s := range signals {
		if s == "unpinned_action_refs" {
			found = true
		}
	}
	if !found {
		t.Errorf("signals = %#v, want it to contain %q", signals, "unpinned_action_refs")
	}
}

// TestEnrichWorkflowArtifactRowOmitsUnpinnedActionRefsWhenAllPinned proves
// that a workflow whose only third-party action is pinned to a full commit
// SHA gets no unpinned_action_refs field and no signal -- never a fabricated
// empty-but-present field.
func TestEnrichWorkflowArtifactRowOmitsUnpinnedActionRefsWhenAllPinned(t *testing.T) {
	t.Parallel()

	content := `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: octo-org/octo-action@a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2
`
	row := map[string]any{}
	enrichWorkflowArtifactRow(row, content)

	if v, ok := row["unpinned_action_refs"]; ok {
		t.Errorf("row[unpinned_action_refs] = %#v, want absent", v)
	}
	signals, _ := row["signals"].([]string)
	for _, s := range signals {
		if s == "unpinned_action_refs" {
			t.Errorf("signals = %#v, must not contain unpinned_action_refs when every action is pinned", signals)
		}
	}
}
