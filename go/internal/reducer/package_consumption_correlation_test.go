package reducer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildPackageConsumptionDecisionsMatchesManifestDependencies(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageConsumptionDecisions([]facts.Envelope{
		packageRegistryPackageFact(
			"pkg:npm://registry.example/@eshu/core-api",
			"npm",
			"core-api",
			"@eshu",
			observedAt,
		),
		packageSourceRepositoryFact("repo-web", "web", "https://github.com/acme/web", false, observedAt),
		packageManifestDependencyFact(
			"repo-web",
			"web",
			"package.json",
			"@eshu/core-api",
			"npm",
			"^1.2.0",
			observedAt,
		),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Outcome, PackageConsumptionManifestDeclared; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := decision.PackageID, "pkg:npm://registry.example/@eshu/core-api"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
	if got, want := decision.RepositoryID, "repo-web"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := decision.RelativePath, "package.json"; got != want {
		t.Fatalf("RelativePath = %q, want %q", got, want)
	}
	if got, want := decision.DependencyRange, "^1.2.0"; got != want {
		t.Fatalf("DependencyRange = %q, want %q", got, want)
	}
	if decision.ProvenanceOnly {
		t.Fatal("ProvenanceOnly = true, want false for manifest-backed consumption truth")
	}
	if got, want := decision.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
}

func TestPostgresPackageCorrelationWriterPersistsOwnershipAndConsumptionFacts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	db := &fakeWorkloadIdentityExecer{}
	writer := PostgresPackageCorrelationWriter{
		DB:  db,
		Now: func() time.Time { return now },
	}

	result, err := writer.WritePackageCorrelations(context.Background(), PackageCorrelationWrite{
		IntentID:     "intent-package",
		ScopeID:      "scope-package",
		GenerationID: "generation-package",
		SourceSystem: "package_registry",
		Cause:        "package source hints observed",
		OwnershipDecisions: []PackageSourceCorrelationDecision{
			{
				PackageID:       "pkg:npm://registry.example/team-api",
				HintKind:        "repository",
				SourceURL:       "https://github.com/acme/team-api",
				RepositoryID:    "repo-team-api",
				RepositoryName:  "team-api",
				Outcome:         PackageSourceCorrelationExact,
				Reason:          "source hint matches repository remote exactly",
				ProvenanceOnly:  true,
				CanonicalWrites: 0,
			},
		},
		ConsumptionDecisions: []PackageConsumptionDecision{
			{
				PackageID:       "pkg:npm://registry.example/team-api",
				Ecosystem:       "npm",
				PackageName:     "team-api",
				RepositoryID:    "repo-web",
				RepositoryName:  "web",
				RelativePath:    "package.json",
				ManifestSection: "dependencies",
				DependencyRange: "^1.2.0",
				Outcome:         PackageConsumptionManifestDeclared,
				Reason:          "git manifest dependency matches package registry identity",
				CanonicalWrites: 1,
				EvidenceFactIDs: []string{"dep-fact"},
			},
		},
		PublicationDecisions: []PackagePublicationDecision{
			{
				PackageID:       "pkg:npm://registry.example/team-api",
				VersionID:       "pkg:npm://registry.example/team-api@1.2.0",
				Version:         "1.2.0",
				RepositoryID:    "repo-team-api",
				RepositoryName:  "team-api",
				SourceURL:       "https://github.com/acme/team-api",
				Outcome:         PackageSourceCorrelationExact,
				Reason:          "source hint matches repository remote exactly",
				ProvenanceOnly:  true,
				CanonicalWrites: 0,
				EvidenceFactIDs: []string{"package-version-fact"},
			},
		},
	})
	if err != nil {
		t.Fatalf("WritePackageCorrelations() error = %v, want nil", err)
	}
	if got, want := result.CanonicalWrites, 1; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := result.FactsWritten, 3; got != want {
		t.Fatalf("FactsWritten = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 3; got != want {
		t.Fatalf("ExecContext calls = %d, want %d", got, want)
	}
	ownershipPayload := unmarshalPackageCorrelationPayload(t, db.execs[0].args[14])
	if got, want := ownershipPayload["correlation_kind"], packageOwnershipCorrelationFactKind; got != want {
		t.Fatalf("correlation_kind = %#v, want %#v", got, want)
	}
	if got, want := ownershipPayload["relationship_kind"], "ownership"; got != want {
		t.Fatalf("relationship_kind = %#v, want %#v", got, want)
	}
	if got, want := ownershipPayload["provenance_only"], true; got != want {
		t.Fatalf("provenance_only = %#v, want %#v", got, want)
	}
	consumptionPayload := unmarshalPackageCorrelationPayload(t, db.execs[1].args[14])
	if got, want := consumptionPayload["correlation_kind"], packageConsumptionCorrelationFactKind; got != want {
		t.Fatalf("correlation_kind = %#v, want %#v", got, want)
	}
	if got, want := consumptionPayload["canonical_writes"], float64(1); got != want {
		t.Fatalf("canonical_writes = %#v, want %#v", got, want)
	}
	publicationPayload := unmarshalPackageCorrelationPayload(t, db.execs[2].args[14])
	if got, want := publicationPayload["correlation_kind"], packagePublicationCorrelationFactKind; got != want {
		t.Fatalf("correlation_kind = %#v, want %#v", got, want)
	}
	if got, want := publicationPayload["relationship_kind"], "publication"; got != want {
		t.Fatalf("relationship_kind = %#v, want %#v", got, want)
	}
	if got, want := publicationPayload["version_id"], "pkg:npm://registry.example/team-api@1.2.0"; got != want {
		t.Fatalf("version_id = %#v, want %#v", got, want)
	}
	if got, want := publicationPayload["provenance_only"], true; got != want {
		t.Fatalf("provenance_only = %#v, want %#v", got, want)
	}
}

func TestPackagePublicationIdentityIncludesSourceHintIdentity(t *testing.T) {
	t.Parallel()

	write := PackageCorrelationWrite{
		ScopeID:      "scope-package",
		GenerationID: "generation-package",
	}
	repositoryHint := PackagePublicationDecision{
		PackageID:           "pkg:npm://registry.example/team-api",
		VersionID:           "pkg:npm://registry.example/team-api@1.2.0",
		SourceURL:           "https://github.com/acme/team-api",
		SourceHintFactID:    "source-hint-repository",
		SourceHintKind:      "repository",
		SourceHintVersionID: "pkg:npm://registry.example/team-api@1.2.0",
	}
	homepageHint := PackagePublicationDecision{
		PackageID:           repositoryHint.PackageID,
		VersionID:           repositoryHint.VersionID,
		SourceURL:           repositoryHint.SourceURL,
		SourceHintFactID:    "source-hint-homepage",
		SourceHintKind:      "homepage",
		SourceHintVersionID: repositoryHint.SourceHintVersionID,
	}

	if got, want := packagePublicationFactID(write, repositoryHint), packagePublicationFactID(write, homepageHint); got == want {
		t.Fatalf("packagePublicationFactID collapsed distinct source hints to %q", got)
	}
	if got, want := packagePublicationStableFactKey(write, repositoryHint), packagePublicationStableFactKey(write, homepageHint); got == want {
		t.Fatalf("packagePublicationStableFactKey collapsed distinct source hints to %q", got)
	}
	payload := packagePublicationPayload(write, repositoryHint)
	if got, want := payload["source_hint_kind"], "repository"; got != want {
		t.Fatalf("source_hint_kind = %#v, want %#v", got, want)
	}
	if got, want := payload["source_hint_fact_id"], "source-hint-repository"; got != want {
		t.Fatalf("source_hint_fact_id = %#v, want %#v", got, want)
	}
}
