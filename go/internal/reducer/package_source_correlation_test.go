package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildPackageSourceCorrelationDecisionsClassifiesExactRepositoryHint(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageSourceCorrelationDecisions([]facts.Envelope{
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
	if decision.Outcome != PackageSourceCorrelationExact {
		t.Fatalf("Outcome = %q, want %q", decision.Outcome, PackageSourceCorrelationExact)
	}
	if decision.RepositoryID != "repo-team-api" {
		t.Fatalf("RepositoryID = %q, want repo-team-api", decision.RepositoryID)
	}
	if decision.CanonicalWrites != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0 until package ownership admission is proven", decision.CanonicalWrites)
	}
	if !decision.ProvenanceOnly {
		t.Fatal("ProvenanceOnly = false, want true until corroborating build or release evidence exists")
	}
}

func TestBuildPackageSourceCorrelationDecisionsClassifiesDerivedRepositoryHint(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageSourceCorrelationDecisions([]facts.Envelope{
		packageSourceHintFact(
			"pkg:npm://registry.example/team-api",
			"repository",
			"https://github.com/acme/team-api",
			observedAt,
		),
		packageSourceRepositoryFact("repo-team-api", "team-api", "git@github.com:acme/team-api.git", false, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	if got, want := decisions[0].Outcome, PackageSourceCorrelationDerived; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if got, want := decisions[0].Reason, "source hint matches repository remote after git URL canonicalization"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
}

func TestBuildPackageSourceCorrelationDecisionsKeepsAmbiguousHintsOutOfOwnership(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageSourceCorrelationDecisions([]facts.Envelope{
		packageSourceHintFact(
			"pkg:npm://registry.example/team-api",
			"repository",
			"https://github.com/acme/team-api",
			observedAt,
		),
		packageSourceRepositoryFact("repo-team-api", "team-api", "https://github.com/acme/team-api", false, observedAt),
		packageSourceRepositoryFact("repo-team-api-fork", "team-api-fork", "git@github.com:acme/team-api.git", false, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	decision := decisions[0]
	if got, want := decision.Outcome, PackageSourceCorrelationAmbiguous; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if decision.RepositoryID != "" {
		t.Fatalf("RepositoryID = %q, want empty for ambiguous decision", decision.RepositoryID)
	}
	if got, want := decision.CandidateRepositoryIDs, []string{"repo-team-api", "repo-team-api-fork"}; !sameStrings(got, want) {
		t.Fatalf("CandidateRepositoryIDs = %#v, want %#v", got, want)
	}
}

func TestBuildPackageSourceCorrelationDecisionsClassifiesUnresolvedHints(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageSourceCorrelationDecisions([]facts.Envelope{
		packageSourceHintFact(
			"pkg:npm://registry.example/team-api",
			"repository",
			"https://github.com/acme/team-api",
			observedAt,
		),
		packageSourceRepositoryFact("repo-other", "other", "https://github.com/acme/other", false, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	if got, want := decisions[0].Outcome, PackageSourceCorrelationUnresolved; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
}

func TestBuildPackageSourceCorrelationDecisionsClassifiesStaleRepositoryFacts(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageSourceCorrelationDecisions([]facts.Envelope{
		packageSourceHintFact(
			"pkg:npm://registry.example/team-api",
			"repository",
			"https://github.com/acme/team-api",
			observedAt,
		),
		packageSourceRepositoryFact("repo-team-api", "team-api", "https://github.com/acme/team-api", true, observedAt),
	})

	if got, want := len(decisions), 1; got != want {
		t.Fatalf("len(decisions) = %d, want %d", got, want)
	}
	if got, want := decisions[0].Outcome, PackageSourceCorrelationStale; got != want {
		t.Fatalf("Outcome = %q, want %q", got, want)
	}
	if decisions[0].RepositoryID != "" {
		t.Fatalf("RepositoryID = %q, want empty for stale source", decisions[0].RepositoryID)
	}
}

func TestBuildPackageSourceCorrelationDecisionsRejectsWeakHomepageHints(t *testing.T) {
	observedAt := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	decisions := BuildPackageSourceCorrelationDecisions([]facts.Envelope{
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
	if got, want := decisions[0].Reason, "hint kind homepage is provenance-only and cannot prove repository ownership"; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
}

func packageSourceHintFact(packageID, hintKind, normalizedURL string, observedAt time.Time) facts.Envelope {
	return facts.Envelope{
		FactKind:   facts.PackageRegistrySourceHintFactKind,
		ObservedAt: observedAt,
		Payload: map[string]any{
			"package_id":        packageID,
			"hint_kind":         hintKind,
			"normalized_url":    normalizedURL,
			"raw_url":           normalizedURL,
			"confidence_reason": "test",
		},
	}
}

func packageSourceRepositoryFact(
	repositoryID string,
	repositoryName string,
	remoteURL string,
	tombstone bool,
	observedAt time.Time,
) facts.Envelope {
	return facts.Envelope{
		FactKind:      factKindRepository,
		ObservedAt:    observedAt,
		IsTombstone:   tombstone,
		StableFactKey: "repository:" + repositoryID,
		Payload: map[string]any{
			"graph_id":   repositoryID,
			"name":       repositoryName,
			"remote_url": remoteURL,
		},
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}
