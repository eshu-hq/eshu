// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ghactionsref"
)

// ghaRefPinParityFixtureYAML is the both-paths-agree regression tripwire
// fixture for issue #5372. It is duplicated BYTE-FOR-BYTE from
// go/internal/relationships/github_actions_ref_pin_parity_test.go
// (same constant name there). See that file's doc comment for the full
// rationale: the two packages do not import each other, so this fixture is
// fed independently to each package's discovery/extraction path, and both
// must derive the identical (slug, ref_value, pinned) triple set asserted
// here and there against ghaRefPinParityExpected. Keep both copies in sync
// when the fixture changes.
const ghaRefPinParityFixtureYAML = `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: octo-org/octo-action@a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2
      - uses: octo-org/mutable-action@main
  deploy:
    uses: octo-org/octo-repo/.github/workflows/deploy.yml@v2
`

// ghaRefPin is one (slug, ref_value) pair the parity fixture must yield,
// alongside its Pinned() classification. Duplicated in the relationships
// package's twin test.
type ghaRefPin struct {
	slug     string
	refValue string
	pinned   bool
}

func (p ghaRefPin) key() string { return p.slug + "@" + p.refValue }

// ghaRefPinParityExpected is the fixed (slug, ref_value, pinned) triple set
// both discovery paths must derive from ghaRefPinParityFixtureYAML. Duplicated
// in the relationships package's twin test.
var ghaRefPinParityExpected = []ghaRefPin{
	{slug: "octo-org/octo-action", refValue: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", pinned: true},
	{slug: "octo-org/mutable-action", refValue: "main", pinned: false},
	{slug: "octo-org/octo-repo", refValue: "v2", pinned: false},
}

func sortedGHARefPins(pins []ghaRefPin) []ghaRefPin {
	sorted := append([]ghaRefPin(nil), pins...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].key() < sorted[j].key() })
	return sorted
}

// TestGitHubActionsRefPinParity_QueryPath proves the query-package extraction
// path (extractGitHubActionsDependencyRefs) derives the fixed (slug,
// ref_value, pinned) triple set from the shared parity fixture -- the same
// set go/internal/relationships/github_actions_ref_pin_parity_test.go proves
// discoverGitHubActionsEvidence derives from the byte-identical fixture. See
// that file's doc comment for the both-paths-agree contract.
func TestGitHubActionsRefPinParity_QueryPath(t *testing.T) {
	t.Parallel()

	refs := extractGitHubActionsDependencyRefs(ghaRefPinParityFixtureYAML)
	if refs == nil {
		t.Fatal("extractGitHubActionsDependencyRefs() = nil, want non-nil")
	}
	if len(refs.actionRepositories) != len(refs.actionRefs) {
		t.Fatalf("actionRepositories/actionRefs length mismatch: %d vs %d", len(refs.actionRepositories), len(refs.actionRefs))
	}

	var got []ghaRefPin
	for i, slug := range refs.actionRepositories {
		_, _, refValue := ghactionsref.Parse(refs.actionRefs[i])
		if refValue == "" {
			continue
		}
		got = append(got, ghaRefPin{slug: slug, refValue: refValue, pinned: ghactionsref.Pinned(refValue)})
	}
	for i, slug := range refs.reusableWorkflowRepos {
		_, _, refValue := ghactionsref.Parse(refs.reusableWorkflowRefs[i])
		if refValue == "" {
			continue
		}
		got = append(got, ghaRefPin{slug: slug, refValue: refValue, pinned: ghactionsref.Pinned(refValue)})
	}

	gotSorted := sortedGHARefPins(got)
	wantSorted := sortedGHARefPins(ghaRefPinParityExpected)
	if len(gotSorted) != len(wantSorted) {
		t.Fatalf("query path triple count = %d, want %d\ngot:  %#v\nwant: %#v", len(gotSorted), len(wantSorted), gotSorted, wantSorted)
	}
	for i := range gotSorted {
		if gotSorted[i] != wantSorted[i] {
			t.Errorf("query path triple[%d] = %+v, want %+v", i, gotSorted[i], wantSorted[i])
		}
	}
}
