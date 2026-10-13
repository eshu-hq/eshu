// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// TestRepositoryScopedCatalogBoundsToNewRepos pins the scope-bounded contract
// (issue #3500): the per-commit relationship backfill must build its catalog
// matcher from only the repositories onboarded by the current generation, never
// the whole fleet. The returned scope is exactly the new-repo entries regardless
// of how large the refreshed catalog is, so matcher build and discovery cost
// scale with the onboarding delta, not O(all repositories).
func TestRepositoryScopedCatalogBoundsToNewRepos(t *testing.T) {
	t.Parallel()

	fleet := make([]relationships.CatalogEntry, 0, 1000)
	for i := 0; i < 1000; i++ {
		repoID := fmt.Sprintf("repo-%04d", i)
		fleet = append(fleet, relationships.CatalogEntry{
			RepoID:  repoID,
			Aliases: []string{repoID, fmt.Sprintf("org/%s", repoID)},
		})
	}
	newRepoIDs := map[string]struct{}{
		"repo-0007": {},
		"repo-0042": {},
	}

	scoped := RepositoryScopedCatalog(fleet, newRepoIDs)

	if got, want := len(scoped), len(newRepoIDs); got != want {
		t.Fatalf("scoped catalog size = %d, want %d (must bound to new repos, not the %d-repo fleet)", got, want, len(fleet))
	}
	gotIDs := make([]string, 0, len(scoped))
	for _, entry := range scoped {
		if _, ok := newRepoIDs[entry.RepoID]; !ok {
			t.Fatalf("scoped catalog leaked non-new repo %q", entry.RepoID)
		}
		gotIDs = append(gotIDs, entry.RepoID)
		// Aliases must be preserved verbatim so matching truth is unchanged.
		if len(entry.Aliases) != 2 {
			t.Fatalf("scoped entry %q aliases = %v, want full alias set preserved", entry.RepoID, entry.Aliases)
		}
	}
	sort.Strings(gotIDs)
	if want := []string{"repo-0007", "repo-0042"}; !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("scoped catalog repo ids = %v, want %v", gotIDs, want)
	}
}

// TestBackfillScopedCatalogDiscoversSameEvidenceAsFullCatalog proves the
// scope-bounded backfill preserves correlation truth (issue #3500 accuracy
// gate): discovering evidence with the new-repo-scoped catalog yields exactly
// the same evidence that the prior full-catalog-then-filter path produced. The
// fleet here carries many unrelated repos plus the source facts that reference
// one newly onboarded target; both paths must agree edge-for-edge.
func TestBackfillScopedCatalogDiscoversSameEvidenceAsFullCatalog(t *testing.T) {
	t.Parallel()

	fleet := []relationships.CatalogEntry{
		{RepoID: "repo-app", Aliases: []string{"app-repo"}},
		{RepoID: "repo-infra", Aliases: []string{"infra-repo"}},
		{RepoID: "repo-unrelated-1", Aliases: []string{"unrelated-1"}},
		{RepoID: "repo-unrelated-2", Aliases: []string{"unrelated-2"}},
	}
	// Source facts from a pre-existing infra repo that reference the newly
	// onboarded app repo via Terraform content.
	sourceFacts := []facts.Envelope{{
		FactKind: "content",
		ScopeID:  "scope-infra",
		Payload: map[string]any{
			"repo_id":       "repo-infra",
			"artifact_type": "terraform",
			"relative_path": "main.tf",
			"content":       `app_repo = "app-repo"` + "\n" + `unrelated = "unrelated-1"`,
		},
	}}
	newRepoIDs := map[string]struct{}{"repo-app": {}}

	// Reference: prior full-catalog discovery, then filter to the new repos.
	reference := filterEvidenceByTargetRepo(
		relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(sourceFacts, fleet)),
		newRepoIDs,
	)
	// New: scope the catalog to the new repos before discovery (no post-filter).
	scoped := relationships.DedupeEvidenceFacts(
		relationships.DiscoverEvidence(sourceFacts, RepositoryScopedCatalog(fleet, newRepoIDs)),
	)

	if !reflect.DeepEqual(scoped, reference) {
		t.Fatalf("scoped backfill evidence diverged from full-catalog reference:\nscoped    = %#v\nreference = %#v", scoped, reference)
	}
	if len(scoped) == 0 {
		t.Fatal("expected at least one evidence fact targeting the onboarded repo")
	}
	for _, fact := range scoped {
		if fact.TargetRepoID != "repo-app" {
			t.Fatalf("scoped evidence targeted %q, want only the onboarded repo-app", fact.TargetRepoID)
		}
	}
}

// newBackfillScaleCorpus builds a fleet of fleetSize repositories and a set of
// source content facts that reference the two onboarded target repos, for the
// scope-bounded backfill scale benchmarks (issue #3500). The onboarding delta is
// fixed at two repos regardless of fleetSize, so the benchmarks isolate how
// backfill cost responds to fleet growth.
func newBackfillScaleCorpus(fleetSize int) ([]relationships.CatalogEntry, []facts.Envelope, map[string]struct{}) {
	fleet := make([]relationships.CatalogEntry, 0, fleetSize)
	for i := 0; i < fleetSize; i++ {
		repoID := fmt.Sprintf("repo-%05d", i)
		fleet = append(fleet, relationships.CatalogEntry{
			RepoID:  repoID,
			Aliases: []string{repoID, fmt.Sprintf("org/%s", repoID)},
		})
	}
	newRepoIDs := map[string]struct{}{"repo-00007": {}, "repo-00042": {}}

	// One source content fact per fleet repo, mirroring the corpus-wide fact
	// load the backfill performs. A handful reference the onboarded repos.
	sourceFacts := make([]facts.Envelope, 0, fleetSize)
	for i := 0; i < fleetSize; i++ {
		repoID := fmt.Sprintf("repo-%05d", i)
		content := fmt.Sprintf("module_source = %q", repoID)
		if i%500 == 0 {
			content = `app = "repo-00007"` + "\n" + `infra = "repo-00042"`
		}
		sourceFacts = append(sourceFacts, facts.Envelope{
			FactKind: "content",
			ScopeID:  "scope-" + repoID,
			Payload: map[string]any{
				"repo_id":       repoID,
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content":       content,
			},
		})
	}
	return fleet, sourceFacts, newRepoIDs
}

// BenchmarkBackfillDiscoveryFullCatalog measures the prior per-commit backfill
// shape: discover evidence against the whole fleet catalog, then filter to the
// onboarded repos. Matcher build and per-fact match cost grow with fleet size,
// so this benchmark regresses as the fleet scales (the O(all-repos) behavior
// issue #3500 removes).
func BenchmarkBackfillDiscoveryFullCatalog(b *testing.B) {
	for _, fleetSize := range []int{1000, 5000} {
		fleet, sourceFacts, newRepoIDs := newBackfillScaleCorpus(fleetSize)
		b.Run(fmt.Sprintf("fleet=%d", fleetSize), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = filterEvidenceByTargetRepo(
					relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(sourceFacts, fleet)),
					newRepoIDs,
				)
			}
		})
	}
}

// BenchmarkBackfillDiscoveryScoped measures the scope-bounded backfill shape
// (issue #3500): discover evidence against only the onboarded repos' catalog.
// Matcher build cost is bounded by the onboarding delta, so per-fact match cost
// stays flat as the fleet scales from 1k to 5k repos.
func BenchmarkBackfillDiscoveryScoped(b *testing.B) {
	for _, fleetSize := range []int{1000, 5000} {
		fleet, sourceFacts, newRepoIDs := newBackfillScaleCorpus(fleetSize)
		scoped := RepositoryScopedCatalog(fleet, newRepoIDs)
		b.Run(fmt.Sprintf("fleet=%d", fleetSize), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = relationships.DedupeEvidenceFacts(
					relationships.DiscoverEvidence(sourceFacts, scoped),
				)
			}
		})
	}
}
