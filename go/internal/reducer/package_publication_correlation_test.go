package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildPackagePublicationDecisionsMatchesVersionToRepositoryHint(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	publishedAt := time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC)
	decisions := BuildPackagePublicationDecisions([]facts.Envelope{
		packageRegistryPackageVersionFact(
			"package-version-fact",
			"pkg:npm://registry.example/team-api",
			"pkg:npm://registry.example/team-api@1.2.0",
			"1.2.0",
			publishedAt,
			observedAt,
		),
		packageSourceHintFact(
			"pkg:npm://registry.example/team-api",
			"repository",
			"https://github.com/acme/team-api",
			observedAt,
		),
		packageSourceRepositoryFact("repo-team-api", "team-api", "https://github.com/acme/team-api", false, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Outcome, PackageSourceCorrelationExact; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := decision.PackageID, "pkg:npm://registry.example/team-api"; got != want {
		t.Fatalf("PackageID = %q, want %q", got, want)
	}
	if got, want := decision.VersionID, "pkg:npm://registry.example/team-api@1.2.0"; got != want {
		t.Fatalf("VersionID = %q, want %q", got, want)
	}
	if got, want := decision.RepositoryID, "repo-team-api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if !decision.ProvenanceOnly {
		t.Fatal("ProvenanceOnly = false, want true for source-hint-only publication evidence")
	}
	if got, want := decision.CanonicalWrites, 0; got != want {
		t.Fatalf("CanonicalWrites = %d, want %d", got, want)
	}
	if got, want := decision.EvidenceFactIDs, []string{"package-version-fact"}; !sameStrings(got, want) {
		t.Fatalf("EvidenceFactIDs = %#v, want %#v", got, want)
	}
}

func TestBuildPackagePublicationDecisionsKeepsWeakHintsRejected(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackagePublicationDecisions([]facts.Envelope{
		packageRegistryPackageVersionFact(
			"package-version-fact",
			"pkg:npm://registry.example/team-api",
			"pkg:npm://registry.example/team-api@1.2.0",
			"1.2.0",
			time.Time{},
			observedAt,
		),
		packageSourceHintFact(
			"pkg:npm://registry.example/team-api",
			"homepage",
			"https://github.com/acme/team-api",
			observedAt,
		),
		packageSourceRepositoryFact("repo-team-api", "team-api", "https://github.com/acme/team-api", false, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	if got, want := decisions[0].Outcome, PackageSourceCorrelationRejected; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if decisions[0].RepositoryID != "" {
		t.Fatalf("RepositoryID = %q, want empty for rejected publication hint", decisions[0].RepositoryID)
	}
}

func TestBuildPackagePublicationDecisionsMatchesVersionScopedHints(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	wrongVersionHint := packageSourceHintFact(
		"pkg:npm://registry.example/team-api",
		"repository",
		"https://github.com/acme/wrong-version",
		observedAt,
	)
	wrongVersionHint.Payload["version_id"] = "pkg:npm://registry.example/team-api@2.0.0"
	matchingVersionHint := packageSourceHintFact(
		"pkg:npm://registry.example/team-api",
		"repository",
		"https://github.com/acme/team-api",
		observedAt,
	)
	matchingVersionHint.Payload["version_id"] = "pkg:npm://registry.example/team-api@1.2.0"

	decisions := BuildPackagePublicationDecisions([]facts.Envelope{
		packageRegistryPackageVersionFact(
			"package-version-fact",
			"pkg:npm://registry.example/team-api",
			"pkg:npm://registry.example/team-api@1.2.0",
			"1.2.0",
			time.Time{},
			observedAt,
		),
		wrongVersionHint,
		matchingVersionHint,
		packageSourceRepositoryFact("repo-team-api", "team-api", "https://github.com/acme/team-api", false, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	if got, want := decisions[0].SourceURL, "https://github.com/acme/team-api"; got != want {
		t.Fatalf("SourceURL = %q, want %q", got, want)
	}
}

func packageRegistryPackageVersionFact(
	factID string,
	packageID string,
	versionID string,
	version string,
	publishedAt time.Time,
	observedAt time.Time,
) facts.Envelope {
	payload := map[string]any{
		"package_id": packageID,
		"version_id": versionID,
		"version":    version,
	}
	if !publishedAt.IsZero() {
		payload["published_at"] = publishedAt.UTC().Format(time.RFC3339)
	}
	return facts.Envelope{
		FactID:        factID,
		FactKind:      facts.PackageRegistryPackageVersionFactKind,
		ObservedAt:    observedAt,
		IsTombstone:   false,
		SourceRef:     facts.Ref{SourceSystem: "package_registry"},
		StableFactKey: "package-version:" + versionID,
		Payload:       payload,
	}
}
