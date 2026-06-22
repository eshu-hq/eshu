package reducer

import (
	"testing"
	"time"
)

func consumptionDecision(packageID, consumerRepoID string) PackageConsumptionDecision {
	return PackageConsumptionDecision{
		PackageID:      packageID,
		Ecosystem:      "npm",
		PackageName:    "left-pad",
		RepositoryID:   consumerRepoID,
		RepositoryName: "consumer",
		Outcome:        PackageConsumptionManifestDeclared,
	}
}

func ownershipDecision(packageID, ownerRepoID string, outcome PackageSourceCorrelationOutcome) PackageSourceCorrelationDecision {
	return PackageSourceCorrelationDecision{
		PackageID:      packageID,
		RepositoryID:   ownerRepoID,
		RepositoryName: "owner",
		Outcome:        outcome,
	}
}

func ambiguousOwnershipDecision(packageID string, candidates []string) PackageSourceCorrelationDecision {
	return PackageSourceCorrelationDecision{
		PackageID:              packageID,
		CandidateRepositoryIDs: candidates,
		Outcome:                PackageSourceCorrelationAmbiguous,
	}
}

func repoEdgeInput(consumption []PackageConsumptionDecision, ownership []PackageSourceCorrelationDecision, publication []PackagePublicationDecision) PackageConsumptionRepoDependencyInput {
	return PackageConsumptionRepoDependencyInput{
		ScopeID:              "scope-pkg",
		GenerationID:         "gen-1",
		SourceRunID:          "run-1",
		CreatedAt:            time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		ConsumptionDecisions: consumption,
		OwnershipDecisions:   ownership,
		PublicationDecisions: publication,
	}
}

// findUpsertIntent returns the first DEPENDS_ON upsert intent matching the
// consumer/owner pair, or nil.
func findUpsertIntent(rows []SharedProjectionIntentRow, consumer, owner string) *SharedProjectionIntentRow {
	for i := range rows {
		row := rows[i]
		if anyToString(row.Payload["repo_id"]) == consumer &&
			anyToString(row.Payload["target_repo_id"]) == owner {
			return &rows[i]
		}
	}
	return nil
}

func TestBuildPackageConsumptionRepoDependencyIntentsProjectsOwnerEdge(t *testing.T) {
	input := repoEdgeInput(
		[]PackageConsumptionDecision{consumptionDecision("pkg:npm/left-pad", "repo-consumer")},
		[]PackageSourceCorrelationDecision{ownershipDecision("pkg:npm/left-pad", "repo-owner", PackageSourceCorrelationExact)},
		nil,
	)

	rows := BuildPackageConsumptionRepoDependencyIntents(input)

	intent := findUpsertIntent(rows, "repo-consumer", "repo-owner")
	if intent == nil {
		t.Fatalf("expected DEPENDS_ON upsert intent repo-consumer->repo-owner, got %d rows", len(rows))
	}
	if intent.ProjectionDomain != DomainRepoDependency {
		t.Fatalf("projection domain = %q, want %q", intent.ProjectionDomain, DomainRepoDependency)
	}
	if got := anyToString(intent.Payload["evidence_source"]); got != packageConsumptionEvidenceSource {
		t.Fatalf("evidence_source = %q, want %q", got, packageConsumptionEvidenceSource)
	}
	if got := anyToString(intent.Payload["relationship_type"]); got != "DEPENDS_ON" {
		t.Fatalf("relationship_type = %q, want DEPENDS_ON", got)
	}
	if got := anyToString(intent.Payload["action"]); got != "upsert" {
		t.Fatalf("action = %q, want upsert", got)
	}
	if intent.PartitionKey != "repo:repo-consumer->repo-owner" {
		t.Fatalf("partition_key = %q, want repo:repo-consumer->repo-owner", intent.PartitionKey)
	}
	if intent.AcceptanceUnitID != "repo-consumer" {
		t.Fatalf("acceptance_unit_id = %q, want repo-consumer", intent.AcceptanceUnitID)
	}
	if intent.RepositoryID != "repo-consumer" {
		t.Fatalf("repository_id = %q, want repo-consumer", intent.RepositoryID)
	}
	if intent.ScopeID != "scope-pkg" || intent.GenerationID != "gen-1" || intent.SourceRunID != "run-1" {
		t.Fatalf("acceptance identity mismatch: scope=%q gen=%q run=%q", intent.ScopeID, intent.GenerationID, intent.SourceRunID)
	}
}

func TestBuildPackageConsumptionRepoDependencyIntentsProjectsViaPublication(t *testing.T) {
	input := repoEdgeInput(
		[]PackageConsumptionDecision{consumptionDecision("pkg:npm/left-pad", "repo-consumer")},
		nil,
		[]PackagePublicationDecision{{
			PackageID:    "pkg:npm/left-pad",
			RepositoryID: "repo-owner",
			Outcome:      PackageSourceCorrelationDerived,
		}},
	)

	rows := BuildPackageConsumptionRepoDependencyIntents(input)
	if findUpsertIntent(rows, "repo-consumer", "repo-owner") == nil {
		t.Fatalf("expected publication-derived DEPENDS_ON edge, got %d rows", len(rows))
	}
}

func TestBuildPackageConsumptionRepoDependencyIntentsSkipsAmbiguousOwner(t *testing.T) {
	input := repoEdgeInput(
		[]PackageConsumptionDecision{consumptionDecision("pkg:npm/left-pad", "repo-consumer")},
		[]PackageSourceCorrelationDecision{ambiguousOwnershipDecision("pkg:npm/left-pad", []string{"a", "b"})},
		nil,
	)

	rows := BuildPackageConsumptionRepoDependencyIntents(input)
	if len(rows) != 0 {
		t.Fatalf("expected no edges for ambiguous owner, got %d", len(rows))
	}
}

func TestBuildPackageConsumptionRepoDependencyIntentsSkipsUnresolvedOwner(t *testing.T) {
	input := repoEdgeInput(
		[]PackageConsumptionDecision{consumptionDecision("pkg:npm/left-pad", "repo-consumer")},
		[]PackageSourceCorrelationDecision{ownershipDecision("pkg:npm/left-pad", "", PackageSourceCorrelationUnresolved)},
		nil,
	)

	rows := BuildPackageConsumptionRepoDependencyIntents(input)
	if len(rows) != 0 {
		t.Fatalf("expected no edges for unresolved owner, got %d", len(rows))
	}
}

func TestBuildPackageConsumptionRepoDependencyIntentsSkipsSelfReference(t *testing.T) {
	input := repoEdgeInput(
		[]PackageConsumptionDecision{consumptionDecision("pkg:npm/self", "repo-same")},
		[]PackageSourceCorrelationDecision{ownershipDecision("pkg:npm/self", "repo-same", PackageSourceCorrelationExact)},
		nil,
	)

	rows := BuildPackageConsumptionRepoDependencyIntents(input)
	if len(rows) != 0 {
		t.Fatalf("expected no self-reference edge, got %d", len(rows))
	}
}

func TestBuildPackageConsumptionRepoDependencyIntentsDeduplicatesPair(t *testing.T) {
	// Two packages owned by the same repo, consumed by the same repo, must
	// collapse to one repo->repo edge.
	input := repoEdgeInput(
		[]PackageConsumptionDecision{
			consumptionDecision("pkg:npm/a", "repo-consumer"),
			consumptionDecision("pkg:npm/b", "repo-consumer"),
		},
		[]PackageSourceCorrelationDecision{
			ownershipDecision("pkg:npm/a", "repo-owner", PackageSourceCorrelationExact),
			ownershipDecision("pkg:npm/b", "repo-owner", PackageSourceCorrelationDerived),
		},
		nil,
	)

	rows := BuildPackageConsumptionRepoDependencyIntents(input)
	if len(rows) != 1 {
		t.Fatalf("expected 1 deduplicated edge, got %d", len(rows))
	}
	if got := payloadInt2(rows[0].Payload, "evidence_count"); got != 2 {
		t.Fatalf("evidence_count = %d, want 2 (two packages)", got)
	}
}

func TestBuildPackageConsumptionRepoDependencyIntentsIsIdempotent(t *testing.T) {
	input := repoEdgeInput(
		[]PackageConsumptionDecision{consumptionDecision("pkg:npm/left-pad", "repo-consumer")},
		[]PackageSourceCorrelationDecision{ownershipDecision("pkg:npm/left-pad", "repo-owner", PackageSourceCorrelationExact)},
		nil,
	)

	first := BuildPackageConsumptionRepoDependencyIntents(input)
	second := BuildPackageConsumptionRepoDependencyIntents(input)
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected 1 edge each run, got %d and %d", len(first), len(second))
	}
	if first[0].IntentID != second[0].IntentID {
		t.Fatalf("intent IDs differ across runs: %q vs %q (not idempotent)", first[0].IntentID, second[0].IntentID)
	}
	if first[0].IntentID == "" {
		t.Fatal("intent ID must be non-empty")
	}
}

func TestBuildPackageConsumptionRepoDependencyIntentsSkipsMissingConsumer(t *testing.T) {
	input := repoEdgeInput(
		[]PackageConsumptionDecision{consumptionDecision("pkg:npm/left-pad", "")},
		[]PackageSourceCorrelationDecision{ownershipDecision("pkg:npm/left-pad", "repo-owner", PackageSourceCorrelationExact)},
		nil,
	)

	rows := BuildPackageConsumptionRepoDependencyIntents(input)
	if len(rows) != 0 {
		t.Fatalf("expected no edges for missing consumer repo, got %d", len(rows))
	}
}

func payloadInt2(payload map[string]any, key string) int {
	v, ok := payload[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
