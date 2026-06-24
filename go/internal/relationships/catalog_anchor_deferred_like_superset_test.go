package relationships

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// deferredLikeSupersetSim reproduces the issue #3710 LIKE-substring SQL predicate
// of listDeferredScopedRelationshipFactRecordsQuery in pure Go. It is the
// production form: the $2 repo_id arm is a plain substring test
// (lower(payload::text) LIKE '%' || value || '%') with NO token-boundary
// constraint, so it selects a strict SUPERSET of the boundary-regex sim
// (deferredSelfExclusionSim). The only refinement that turns that superset back
// into correct evidence is the in-memory catalogMatcher, exercised here through
// DiscoverEvidence.
//
// A fact is selected iff:
//
//	lower(payload::text) LIKE ANY($1 non-repo_id anchors)
//	OR EXISTS rid IN $2 repo_id values: rid <> own_repo_id AND payload CONTAINS rid
//
// The $1 arm is unchanged from the regex sim; only the $2 arm widens from a
// boundary-delimited match to a plain substring match.
func deferredLikeSupersetSim(
	t *testing.T,
	envelope facts.Envelope,
	nonRepoIDAnchors, repoIDValues []string,
) bool {
	t.Helper()
	raw, err := json.Marshal(envelope.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	text := strings.ToLower(string(raw))

	for _, anchor := range nonRepoIDAnchors {
		anchor = strings.ToLower(strings.TrimSpace(anchor))
		if anchor != "" && strings.Contains(text, anchor) {
			return true
		}
	}

	// $2 arm: EXISTS a catalog repo_id value that is NOT the row's own and appears
	// anywhere in the payload as a plain substring (mirrors the production SQL
	// `lower(payload::text) LIKE '%' || value || '%'`). No boundary check: this is
	// the deliberate over-select the matcher refines.
	ownRepoID, _ := envelope.Payload["repo_id"].(string)
	ownRepoID = strings.ToLower(strings.TrimSpace(ownRepoID))
	for _, value := range repoIDValues {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || value == ownRepoID {
			continue
		}
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

// TestDeferredLikeSupersetMatcherRefinesToBoundaryEvidence is the issue #3710
// truth-equivalence gate. The deferred fact-LOAD SQL was changed from a
// per-row boundary regex to a plain LIKE substring on the repo_id ($2) arm so the
// pg_trgm GIN index can drive a Bitmap Index Scan. LIKE selects a strict superset
// of the regex form, so correctness now depends entirely on the in-memory
// catalogMatcher (catalog_matcher.go) refining that superset back to the boundary-
// safe whole-token evidence set.
//
// The gate proves, over every #3668 case plus a substring-but-not-boundary case:
//
//  1. Superset: every fact the regex sim selects, the LIKE sim also selects.
//  2. Truth-equivalence: DiscoverEvidence over the LIKE-selected load equals
//     DiscoverEvidence over the full corpus — no edge added, none dropped.
//  3. The substring-but-not-boundary fact is selected by LIKE, NOT by the regex,
//     and produces ZERO evidence, proving the matcher DROPS the over-select.
func TestDeferredLikeSupersetMatcherRefinesToBoundaryEvidence(t *testing.T) {
	t.Parallel()

	type likeCase struct {
		name string
		// envelope is one source fact.
		envelope facts.Envelope
		// catalog is the full catalog the fact is discovered against.
		catalog []CatalogEntry
		// likeSelected is whether the LIKE-superset SQL must select the fact.
		likeSelected bool
		// regexSelected is whether the older boundary-regex SQL selected the fact.
		regexSelected bool
		// substringOnly marks the LIKE-but-not-regex over-select case the matcher
		// must drop to zero evidence.
		substringOnly bool
	}

	cases := []likeCase{
		{
			// #3668 cross-repo: references another repo's repo_id verbatim — selected
			// by both forms, produces a real edge.
			name: "cross_repo_by_repo_id",
			envelope: facts.Envelope{
				ScopeID: "scope:app",
				Payload: map[string]any{
					"repo_id":       "repo-app",
					"artifact_type": "github_actions_workflow",
					"relative_path": ".github/workflows/ci.yaml",
					"content":       "uses: org/deploy-toolkit/.github/workflows/deploy.yaml@main",
				},
			},
			catalog: []CatalogEntry{
				{RepoID: "repo-app", Aliases: []string{"repo-app", "edge-app"}},
				{RepoID: "deploy-toolkit", Aliases: []string{"deploy-toolkit"}},
			},
			likeSelected:  true,
			regexSelected: true,
		},
		{
			// #3668 prefix collision: own repo_id is a prefix of the target repo_id;
			// references the target by full value — both forms select, real edge.
			name: "prefix_collision_full_target",
			envelope: facts.Envelope{
				ScopeID: "scope:app",
				Payload: map[string]any{
					"repo_id":       "github.com/org/app",
					"artifact_type": "terraform",
					"relative_path": "main.tf",
					"content":       `app_repo = "github.com/org/app-config"`,
				},
			},
			catalog: []CatalogEntry{
				{RepoID: "github.com/org/app-config", Aliases: []string{"github.com/org/app-config"}},
				{RepoID: "github.com/org/app", Aliases: []string{"github.com/org/app", "edge-app"}},
			},
			likeSelected:  true,
			regexSelected: true,
		},
		{
			// #3668 self-match: references ONLY its own repo_id — neither form selects,
			// no evidence either way.
			name: "pure_self_match",
			envelope: facts.Envelope{
				ScopeID: "scope:orders",
				Payload: map[string]any{
					"repo_id":       "repo-orders",
					"artifact_type": "terraform",
					"relative_path": "main.tf",
					"content":       `tags = { repo = "repo-orders" }`,
				},
			},
			catalog: []CatalogEntry{
				{RepoID: "repo-orders", Aliases: []string{"repo-orders", "order-gateway"}},
			},
			likeSelected:  false,
			regexSelected: false,
		},
		{
			// #3668 no-match: references no catalog token at all — neither form
			// selects, no evidence.
			name: "no_match",
			envelope: facts.Envelope{
				ScopeID: "scope:alpha",
				Payload: map[string]any{
					"repo_id":       "repo-alpha",
					"artifact_type": "terraform",
					"relative_path": "main.tf",
					"content":       `locals { setting = "value" }`,
				},
			},
			catalog: []CatalogEntry{
				{RepoID: "repo-alpha", Aliases: []string{"repo-alpha", "gamma"}},
				{RepoID: "repo-beta", Aliases: []string{"repo-beta", "delta"}},
			},
			likeSelected:  false,
			regexSelected: false,
		},
		{
			// THE substring-but-not-boundary case (issue #3710). The target repo_id
			// "repo-fleet-1" appears in the payload only as an interior substring of a
			// LARGER token "repo-fleet-15" (no token boundary). The boundary regex
			// REJECTS it; the LIKE substring SELECTS it (the over-select); the matcher
			// DROPS it because "repo-fleet-1" != the whole token "repo-fleet-15", so
			// the final evidence is empty — identical to full-corpus discovery.
			name: "substring_not_boundary_over_select",
			envelope: facts.Envelope{
				ScopeID: "scope:src",
				Payload: map[string]any{
					"repo_id":       "repo-src",
					"artifact_type": "terraform",
					"relative_path": "main.tf",
					"content":       `dependency = "repo-fleet-15"`,
				},
			},
			catalog: []CatalogEntry{
				{RepoID: "repo-src", Aliases: []string{"repo-src", "edge-src"}},
				{RepoID: "repo-fleet-1", Aliases: []string{"repo-fleet-1"}},
			},
			likeSelected:  true,
			regexSelected: false,
			substringOnly: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			nonRepoID := nonRepoIDAnchorsSim(tc.catalog)
			repoIDValues := CatalogRepoIDValues(tc.catalog)

			likeSelected := deferredLikeSupersetSim(t, tc.envelope, nonRepoID, repoIDValues)
			regexSelected := deferredSelfExclusionSim(t, tc.envelope, nonRepoID, repoIDValues)

			if likeSelected != tc.likeSelected {
				t.Fatalf("LIKE-superset selection = %v, want %v", likeSelected, tc.likeSelected)
			}
			if regexSelected != tc.regexSelected {
				t.Fatalf("boundary-regex selection = %v, want %v", regexSelected, tc.regexSelected)
			}

			// Superset invariant: LIKE selects everything the regex selected.
			if regexSelected && !likeSelected {
				t.Fatalf("LIKE form dropped a fact the boundary regex selected; not a superset")
			}

			// Truth-equivalence: evidence from the LIKE-selected load equals evidence
			// from the full corpus. The full corpus here is the single fact, so the
			// LIKE-selected load is either that fact or empty.
			fullEvidence := DedupeEvidenceFacts(DiscoverEvidence([]facts.Envelope{tc.envelope}, tc.catalog))

			var likeLoad []facts.Envelope
			if likeSelected {
				likeLoad = []facts.Envelope{tc.envelope}
			}
			likeEvidence := DedupeEvidenceFacts(DiscoverEvidence(likeLoad, tc.catalog))

			if !reflect.DeepEqual(canonicalEvidence(fullEvidence), canonicalEvidence(likeEvidence)) {
				t.Fatalf("LIKE-selected evidence != full-corpus evidence\nfull: %v\nlike: %v",
					canonicalEvidence(fullEvidence), canonicalEvidence(likeEvidence))
			}

			if tc.substringOnly {
				if !likeSelected || regexSelected {
					t.Fatalf("substring-only case must be LIKE-selected and regex-rejected, got like=%v regex=%v",
						likeSelected, regexSelected)
				}
				if len(likeEvidence) != 0 {
					t.Fatalf("matcher did not drop the substring-only over-select; evidence=%v",
						canonicalEvidence(likeEvidence))
				}
			}
		})
	}
}
