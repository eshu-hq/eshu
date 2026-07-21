// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ghactionsref"
)

// ghaRefPinParityFixtureYAML is the both-paths-agree regression tripwire
// fixture for issue #5372. It is duplicated BYTE-FOR-BYTE in
// go/internal/query/github_actions_ref_pin_parity_test.go
// (ghaRefPinParityFixtureYAML there). The two packages do not import each
// other (go/internal/relationships and go/internal/query are independent
// leaves), so this is fed to discoverGitHubActionsEvidence here and to
// extractGitHubActionsDependencyRefs in the query package's twin test; both
// must derive the identical (slug, ref_value, pinned) triple set from it,
// asserted here and there against ghaRefPinParityExpected. If a future change
// makes ONE package's ref-splitting diverge from ghactionsref while the other
// still agrees with it, only that package's test fails -- which is the
// signal that the two paths have silently diverged. Keep both copies in sync
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
// alongside its Pinned() classification. Duplicated in the query package's
// twin test -- see ghaRefPinParityFixtureYAML's doc comment.
type ghaRefPin struct {
	slug     string
	refValue string
	pinned   bool
}

func (p ghaRefPin) key() string { return p.slug + "@" + p.refValue }

// ghaRefPinParityExpected is the fixed (slug, ref_value, pinned) triple set
// both discovery paths must derive from ghaRefPinParityFixtureYAML. Duplicated
// in the query package's twin test.
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

// TestGitHubActionsRefPinParity_RelationshipsPath proves the
// relationships-package discovery path (discoverGitHubActionsEvidence, driven
// through the real exported DiscoverEvidence entry point) derives the fixed
// (slug, ref_value, pinned) triple set from the shared parity fixture. See
// ghaRefPinParityFixtureYAML's doc comment for the both-paths-agree contract
// this enforces with the query package's twin test.
func TestGitHubActionsRefPinParity_RelationshipsPath(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-service",
			Payload: map[string]any{
				"artifact_type": "github_actions_workflow",
				"relative_path": ".github/workflows/ci.yaml",
				"content":       ghaRefPinParityFixtureYAML,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-octo-action", Aliases: []string{"octo-action"}},
		{RepoID: "repo-mutable-action", Aliases: []string{"mutable-action"}},
		{RepoID: "repo-octo-repo", Aliases: []string{"octo-repo"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)

	var got []ghaRefPin
	for _, fact := range evidence {
		switch fact.EvidenceKind {
		case EvidenceKindGitHubActionsActionRepository:
			slug, _ := fact.Details["action_repo"].(string)
			refValue, _ := fact.Details["action_ref_name"].(string)
			got = append(got, ghaRefPin{slug: slug, refValue: refValue, pinned: ghactionsref.Pinned(refValue)})
		case EvidenceKindGitHubActionsReusableWorkflow:
			slug, _ := fact.Details["workflow_repo"].(string)
			refValue, _ := fact.Details["workflow_ref_name"].(string)
			got = append(got, ghaRefPin{slug: slug, refValue: refValue, pinned: ghactionsref.Pinned(refValue)})
		}
	}

	gotSorted := sortedGHARefPins(got)
	wantSorted := sortedGHARefPins(ghaRefPinParityExpected)
	if len(gotSorted) != len(wantSorted) {
		t.Fatalf("relationships path triple count = %d, want %d\ngot:  %#v\nwant: %#v", len(gotSorted), len(wantSorted), gotSorted, wantSorted)
	}
	for i := range gotSorted {
		if gotSorted[i] != wantSorted[i] {
			t.Errorf("relationships path triple[%d] = %+v, want %+v", i, gotSorted[i], wantSorted[i])
		}
	}
}
