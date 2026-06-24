package postgres

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// gcpRelationshipFact builds a supported gcp_cloud_relationship envelope whose
// source and target resource names embed the given catalog aliases. The scoped
// backfill tests use it to exercise the source-match-then-target-match ordering
// in discoverGCPCloudRelationshipEvidence (issue #3500).
func gcpRelationshipFact(sourceAlias, targetAlias string) facts.Envelope {
	return facts.Envelope{
		FactKind:      facts.GCPCloudRelationshipFactKind,
		ScopeID:       "gcp:project:demo:relationship:global",
		StableFactKey: "gcp-rel-" + sourceAlias + "-" + targetAlias,
		Payload: map[string]any{
			"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/" + sourceAlias,
			"source_asset_type":         "run.googleapis.com/Service",
			"relationship_type":         "run_service_uses_secret",
			"target_full_resource_name": "//secretmanager.googleapis.com/projects/demo/secrets/" + targetAlias,
			"target_asset_type":         "secretmanager.googleapis.com/Secret",
			"support_state":             "supported",
		},
	}
}

// TestBackfillScopedCatalogEmitsGCPEvidenceForNewTarget is the issue #3500
// regression guard for the source-side catalog gap. A pre-existing
// gcp_cloud_relationship fact points from an already-onboarded SOURCE repo into a
// newly onboarded TARGET repo. discoverGCPCloudRelationshipEvidence requires a
// unique catalog match for the SOURCE resource before it matches the target, so a
// catalog scoped to only the new target repo resolves no source match and emits
// no A->B evidence. The scoped catalog must therefore also carry the source-side
// entry for any GCP relationship targeting a new repo, so the per-commit backfill
// emits the cross-repo edge without a corpus-wide pass.
func TestBackfillScopedCatalogEmitsGCPEvidenceForNewTarget(t *testing.T) {
	t.Parallel()

	fleet := []relationships.CatalogEntry{
		{RepoID: "repo-source", Aliases: []string{"order-gateway"}},
		{RepoID: "repo-target", Aliases: []string{"payments-service"}},
		{RepoID: "repo-unrelated", Aliases: []string{"unrelated-service"}},
	}
	// Existing fact: source = repo-source (old), target = repo-target (new).
	activeFacts := []facts.Envelope{gcpRelationshipFact("order-gateway", "payments-service")}
	newRepoIDs := map[string]struct{}{"repo-target": {}}

	scoped := backfillScopedCatalog(fleet, activeFacts, newRepoIDs)
	evidence := relationships.DedupeEvidenceFacts(
		relationships.DiscoverEvidence(activeFacts, scoped),
	)

	if len(evidence) != 1 {
		t.Fatalf("scoped GCP backfill evidence = %d, want 1 (A->B edge into the new target)", len(evidence))
	}
	got := evidence[0]
	if got.SourceRepoID != "repo-source" || got.TargetRepoID != "repo-target" {
		t.Fatalf("edge = %s -> %s, want repo-source -> repo-target", got.SourceRepoID, got.TargetRepoID)
	}

	// The unrelated repo must not leak into the scoped catalog: scoping stays
	// bounded to the new repos plus the source repos that reference them.
	for _, entry := range scoped {
		if entry.RepoID == "repo-unrelated" {
			t.Fatalf("scoped catalog leaked unrelated repo %q (corpus-wide scan reintroduced)", entry.RepoID)
		}
	}
}

// TestBackfillScopedCatalogIncludesMultipleGCPSourcesForNewTarget proves the
// source-side augmentation handles several pre-existing source repos pointing
// into one newly onboarded target (issue #3500). Every old-source -> new-target
// edge must be emitted, and no unrelated repo may enter the scope.
func TestBackfillScopedCatalogIncludesMultipleGCPSourcesForNewTarget(t *testing.T) {
	t.Parallel()

	fleet := []relationships.CatalogEntry{
		{RepoID: "repo-source-a", Aliases: []string{"gateway-a"}},
		{RepoID: "repo-source-b", Aliases: []string{"gateway-b"}},
		{RepoID: "repo-target", Aliases: []string{"payments-service"}},
		{RepoID: "repo-unrelated", Aliases: []string{"unrelated-service"}},
	}
	activeFacts := []facts.Envelope{
		gcpRelationshipFact("gateway-a", "payments-service"),
		gcpRelationshipFact("gateway-b", "payments-service"),
		// An unrelated edge that never touches the new target; its source must
		// not be pulled into scope.
		gcpRelationshipFact("unrelated-service", "gateway-a"),
	}
	newRepoIDs := map[string]struct{}{"repo-target": {}}

	scoped := backfillScopedCatalog(fleet, activeFacts, newRepoIDs)
	evidence := relationships.DedupeEvidenceFacts(
		relationships.DiscoverEvidence(activeFacts, scoped),
	)

	edges := make(map[string]struct{})
	for _, fact := range evidence {
		if fact.TargetRepoID != "repo-target" {
			t.Fatalf("edge targeted %q, want only repo-target", fact.TargetRepoID)
		}
		edges[fact.SourceRepoID] = struct{}{}
	}
	for _, want := range []string{"repo-source-a", "repo-source-b"} {
		if _, ok := edges[want]; !ok {
			t.Fatalf("missing A->B edge from %q into repo-target", want)
		}
	}
	scopedIDs := make(map[string]struct{}, len(scoped))
	for _, entry := range scoped {
		scopedIDs[entry.RepoID] = struct{}{}
	}
	if _, leaked := scopedIDs["repo-unrelated"]; leaked {
		t.Fatal("scoped catalog leaked repo-unrelated; source-side scoping must follow only facts into new repos")
	}
}

// TestBackfillScopedCatalogNoGCPFactsMatchesNewRepoOnly pins that when no GCP
// relationship targets a new repo, the scoped catalog stays exactly the new-repo
// set (issue #3500 perf guard): the source-side augmentation adds nothing.
func TestBackfillScopedCatalogNoGCPFactsMatchesNewRepoOnly(t *testing.T) {
	t.Parallel()

	fleet := []relationships.CatalogEntry{
		{RepoID: "repo-source", Aliases: []string{"order-gateway"}},
		{RepoID: "repo-target", Aliases: []string{"payments-service"}},
	}
	// GCP fact targets a repo that is NOT in the new set.
	activeFacts := []facts.Envelope{gcpRelationshipFact("order-gateway", "payments-service")}
	newRepoIDs := map[string]struct{}{"repo-source": {}}

	scoped := backfillScopedCatalog(fleet, activeFacts, newRepoIDs)
	if got, want := len(scoped), 1; got != want {
		t.Fatalf("scoped catalog size = %d, want %d (only the new repo, no source augmentation)", got, want)
	}
	if scoped[0].RepoID != "repo-source" {
		t.Fatalf("scoped catalog repo = %q, want repo-source", scoped[0].RepoID)
	}
}

// BenchmarkBackfillScopedCatalogNoGCP measures backfillScopedCatalog when no GCP
// relationship fact is present (the common per-commit case): the source-side
// augmentation must short-circuit before the O(fleet) matcher build, so cost
// stays flat as the fleet scales (issue #3500 perf guard).
func BenchmarkBackfillScopedCatalogNoGCP(b *testing.B) {
	for _, fleetSize := range []int{1000, 5000} {
		fleet, sourceFacts, newRepoIDs := newBackfillScaleCorpus(fleetSize)
		b.Run(fmt.Sprintf("fleet=%d", fleetSize), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = backfillScopedCatalog(fleet, sourceFacts, newRepoIDs)
			}
		})
	}
}

// BenchmarkBackfillScopedCatalogWithGCP measures backfillScopedCatalog when a GCP
// relationship fact targets an onboarded repo, forcing the source-side
// resolution against the full catalog. This is the bounded worst case the
// issue #3500 fix accepts: it pays one O(fleet) matcher build only when a GCP
// cross-cloud edge into a new repo actually exists.
func BenchmarkBackfillScopedCatalogWithGCP(b *testing.B) {
	for _, fleetSize := range []int{1000, 5000} {
		fleet, sourceFacts, newRepoIDs := newBackfillScaleCorpus(fleetSize)
		// repo-00007 is in newRepoIDs; alias "org/repo-00007" anchors the GCP
		// target match, and an existing fleet repo is the GCP source.
		fleet = append(fleet,
			relationships.CatalogEntry{RepoID: "repo-gcp-source", Aliases: []string{"order-gateway"}},
		)
		gcpFact := gcpRelationshipFact("order-gateway", "repo-00007")
		facts := append([]facts.Envelope{gcpFact}, sourceFacts...)
		b.Run(fmt.Sprintf("fleet=%d", fleetSize), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = backfillScopedCatalog(fleet, facts, newRepoIDs)
			}
		})
	}
}
