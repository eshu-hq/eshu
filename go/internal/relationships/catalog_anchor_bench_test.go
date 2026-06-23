package relationships

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// buildFleetCorpus synthesizes a fleet of fleetSize repositories, each with one
// content fact that references its own (non-onboarded) repository alias plus a
// catalog entry. It also appends a single onboarding fact whose content
// references the newly onboarded repository, so a per-commit backfill must
// discover exactly one new edge regardless of fleet size. This models the
// per-commit backfill input: one new repo against an existing fleet.
func buildFleetCorpus(fleetSize int) (full []facts.Envelope, catalog []CatalogEntry, newRepoCatalog []CatalogEntry) {
	full = make([]facts.Envelope, 0, fleetSize+1)
	catalog = make([]CatalogEntry, 0, fleetSize+1)
	for i := 0; i < fleetSize; i++ {
		alias := fmt.Sprintf("fleet-service-%d", i)
		repoID := fmt.Sprintf("repo:fleet-%d", i)
		full = append(full, facts.Envelope{
			ScopeID: repoID,
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       fmt.Sprintf(`app_repo = "%s"`, alias),
			},
		})
		catalog = append(catalog, CatalogEntry{RepoID: repoID, Aliases: []string{alias}})
	}

	// The onboarding source fact references the new repo's alias.
	full = append(full, facts.Envelope{
		ScopeID: "repo:onboarding-source",
		Payload: map[string]any{
			"artifact_type": "terraform",
			"relative_path": "main.tf",
			"content":       `app_repo = "newly-onboarded-service"`,
		},
	})
	newRepoCatalog = []CatalogEntry{{RepoID: "repo:newly-onboarded", Aliases: []string{"newly-onboarded-service"}}}
	catalog = append(catalog, newRepoCatalog...)
	return full, catalog, newRepoCatalog
}

// payloadMatchesAnchorsBench mirrors the SQL predicate for the benchmark.
func payloadMatchesAnchorsBench(envelope facts.Envelope, anchors []string) bool {
	raw, err := json.Marshal(envelope.Payload)
	if err != nil {
		return false
	}
	text := strings.ToLower(string(raw))
	for _, anchor := range anchors {
		if anchor != "" && strings.Contains(text, anchor) {
			return true
		}
	}
	return false
}

// benchmarkBackfillDiscovery runs the per-commit backfill discovery cost for one
// newly onboarded repository against a fleet of fleetSize existing repositories.
// The "Full" variant matches the pre-#3570 behavior: discover over every fleet
// fact. The "Scoped" variant matches the post-#3570 behavior: discover only over
// the facts the anchor predicate would return. The scoped variant's cost stays
// roughly constant as fleetSize grows because the anchor-scoped fact set does not
// grow with the fleet.
func benchmarkBackfillDiscovery(b *testing.B, fleetSize int, scoped bool) {
	full, _, newRepoCatalog := buildFleetCorpus(fleetSize)
	scopedCatalog := newRepoCatalog
	anchors := CatalogPayloadAnchors(newRepoCatalog)

	corpus := full
	if scoped {
		corpus = make([]facts.Envelope, 0, 4)
		for _, envelope := range full {
			if payloadMatchesAnchorsBench(envelope, anchors) {
				corpus = append(corpus, envelope)
			}
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DedupeEvidenceFacts(DiscoverEvidence(corpus, scopedCatalog))
	}
}

// buildDeferredFleetCorpus models the corpus-wide deferred backfill input
// (issue #3569 / #3659): every repository is an eligible relationship target, so
// the catalog spans the whole fleet. CRITICAL: every fact payload carries its
// OWN repo_id, the realistic shape Git content/file facts always have (the
// pre-#3659 benchmark omitted it, which is exactly why the old benchmark looked
// 6x/11x faster — its synthetic payloads never self-matched the repo_id anchor).
//
// Each fleet repo contributes one edge-forming content fact that references
// another fleet repo's alias, plus orphanPerRepo orphan facts whose content
// references no OTHER catalog repository — only their own repo_id. Under the
// #3659 self-exclusion predicate the orphan facts are excluded because stripping
// their own repo_id leaves no cross-repo substring, while the pre-#3659
// full-corpus (and naive-anchor) load shipped and iterated all of them.
func buildDeferredFleetCorpus(fleetSize, orphanPerRepo int) (full []facts.Envelope, catalog []CatalogEntry) {
	full = make([]facts.Envelope, 0, fleetSize*(1+orphanPerRepo))
	catalog = make([]CatalogEntry, 0, fleetSize)
	for i := 0; i < fleetSize; i++ {
		alias := fmt.Sprintf("fleet-service-%d", i)
		repoID := fmt.Sprintf("repo-fleet-%d", i)
		catalog = append(catalog, CatalogEntry{RepoID: repoID, Aliases: []string{repoID, alias}})

		// One edge-forming fact: repo i references repo (i+1) mod fleetSize by the
		// other repo's NAME alias. The payload carries this repo's own repo_id.
		targetAlias := fmt.Sprintf("fleet-service-%d", (i+1)%fleetSize)
		full = append(full, facts.Envelope{
			ScopeID: repoID,
			Payload: map[string]any{
				"repo_id":       repoID,
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       fmt.Sprintf(`app_repo = "%s"`, targetAlias),
			},
		})

		// Orphan facts reference no OTHER catalog repo; their content mentions only
		// their own repo_id and a third-party image. Under #3659 these are excluded
		// (own repo_id stripped, nothing cross-repo remains), so the deferred load
		// genuinely bounds them out instead of self-matching the whole corpus.
		for j := 0; j < orphanPerRepo; j++ {
			full = append(full, facts.Envelope{
				ScopeID: repoID,
				Payload: map[string]any{
					"repo_id":       repoID,
					"artifact_type": "docker_compose",
					"relative_path": "docker-compose.yaml",
					"content":       fmt.Sprintf("services:\n  web:\n    image: third-party-vendor-image-%d-%d\n", i, j),
				},
			})
		}
	}
	return full, catalog
}

// deferredSelfExclusionMatchesBench mirrors the #3659 deferred SQL predicate for
// the benchmark: $1 non-repo_id anchors OR ($2 full-repo_id-value match after the
// fact's own repo_id is stripped from the payload text).
func deferredSelfExclusionMatchesBench(envelope facts.Envelope, nonRepoIDAnchors, repoIDValues []string) bool {
	raw, err := json.Marshal(envelope.Payload)
	if err != nil {
		return false
	}
	text := strings.ToLower(string(raw))
	for _, anchor := range nonRepoIDAnchors {
		if anchor != "" && strings.Contains(text, anchor) {
			return true
		}
	}
	ownRepoID, _ := envelope.Payload["repo_id"].(string)
	ownRepoID = strings.ToLower(strings.TrimSpace(ownRepoID))
	stripped := text
	if ownRepoID != "" {
		stripped = strings.ReplaceAll(text, ownRepoID, "")
	}
	for _, value := range repoIDValues {
		if value != "" && strings.Contains(stripped, strings.ToLower(value)) {
			return true
		}
	}
	return false
}

// benchmarkDeferredBackfillDiscovery runs the corpus-wide deferred backfill
// discovery cost over the whole fleet catalog (issue #3569 / #3659). The "Full"
// variant discovers over every committed fact; the "Scoped" variant discovers
// only over the facts the #3659 deferred self-exclusion predicate returns,
// dropping the orphan facts whose only repo_id match is their own. Both variants
// must yield identical evidence (proven by TestDeferredSelfExclusionTruthEquiva-
// lence); this measures the work the self-exclusion-scoped load avoids on
// representative repo_id-bearing payloads.
func benchmarkDeferredBackfillDiscovery(b *testing.B, fleetSize, orphanPerRepo int, scoped bool) {
	full, catalog := buildDeferredFleetCorpus(fleetSize, orphanPerRepo)

	corpus := full
	if scoped {
		nonRepoID := nonRepoIDAnchorsSim2(catalog)
		repoIDValues := CatalogRepoIDValues(catalog)
		corpus = make([]facts.Envelope, 0, fleetSize)
		for _, envelope := range full {
			if deferredSelfExclusionMatchesBench(envelope, nonRepoID, repoIDValues) {
				corpus = append(corpus, envelope)
			}
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DedupeEvidenceFacts(DiscoverEvidence(corpus, catalog))
	}
}

// nonRepoIDAnchorsSim2 mirrors backfillNonRepoIDAnchorTerms for the benchmark:
// CatalogPayloadAnchors over each entry's aliases with the repo_id (Aliases[0])
// stripped, lowercased.
func nonRepoIDAnchorsSim2(catalog []CatalogEntry) []string {
	stripped := make([]CatalogEntry, 0, len(catalog))
	for _, entry := range catalog {
		if len(entry.Aliases) <= 1 {
			continue
		}
		stripped = append(stripped, CatalogEntry{RepoID: entry.RepoID, Aliases: entry.Aliases[1:]})
	}
	anchors := CatalogPayloadAnchors(stripped)
	out := make([]string, 0, len(anchors))
	for _, a := range anchors {
		out = append(out, strings.ToLower(a))
	}
	return out
}

func BenchmarkDeferredBackfillDiscoveryFullFleet1k(b *testing.B) {
	benchmarkDeferredBackfillDiscovery(b, 1000, 4, false)
}

func BenchmarkDeferredBackfillDiscoveryScopedFleet1k(b *testing.B) {
	benchmarkDeferredBackfillDiscovery(b, 1000, 4, true)
}

func BenchmarkDeferredBackfillDiscoveryFullFleet5k(b *testing.B) {
	benchmarkDeferredBackfillDiscovery(b, 5000, 4, false)
}

func BenchmarkDeferredBackfillDiscoveryScopedFleet5k(b *testing.B) {
	benchmarkDeferredBackfillDiscovery(b, 5000, 4, true)
}

func BenchmarkBackfillDiscoveryFullFleet1k(b *testing.B) {
	benchmarkBackfillDiscovery(b, 1000, false)
}

func BenchmarkBackfillDiscoveryScopedFleet1k(b *testing.B) {
	benchmarkBackfillDiscovery(b, 1000, true)
}

func BenchmarkBackfillDiscoveryFullFleet5k(b *testing.B) {
	benchmarkBackfillDiscovery(b, 5000, false)
}

func BenchmarkBackfillDiscoveryScopedFleet5k(b *testing.B) {
	benchmarkBackfillDiscovery(b, 5000, true)
}
