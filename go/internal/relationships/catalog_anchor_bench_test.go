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
