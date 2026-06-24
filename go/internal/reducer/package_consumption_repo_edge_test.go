// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestPackageConsumptionRepoEdgeSourceRunIDIsStableAcrossGenerations(t *testing.T) {
	// The acceptance source-run id must be a function of the package-registry
	// scope only, NOT the generation. Embedding the generation makes the
	// downstream shared-projection intent id change every generation, so the
	// same consumer/owner edge is not recognized as the same edge across
	// generations and the DEPENDS_ON MERGE loses idempotency (issue #3579,
	// review comment 3455350029). It mirrors crossRepoContributionSourceRunID.
	genN := packageConsumptionRepoEdgeSourceRunID("scope-pkg", "gen-1")
	genN1 := packageConsumptionRepoEdgeSourceRunID("scope-pkg", "gen-2")
	if genN != genN1 {
		t.Fatalf("source-run id changed across generations: %q vs %q", genN, genN1)
	}
	if genN != "package_consumption_repo_dependency:scope-pkg" {
		t.Fatalf("source-run id = %q, want package_consumption_repo_dependency:scope-pkg", genN)
	}
	// Distinct scopes must still partition.
	if other := packageConsumptionRepoEdgeSourceRunID("scope-other", "gen-1"); other == genN {
		t.Fatalf("distinct scopes collided: %q", other)
	}
	// Empty scope must be deterministic and not leak the generation.
	if empty := packageConsumptionRepoEdgeSourceRunID("", "gen-1"); empty != "package_consumption_repo_dependency" {
		t.Fatalf("empty-scope source-run id = %q, want package_consumption_repo_dependency", empty)
	}
}

func TestBuildPackageConsumptionRepoDependencyIntentsStableAcceptanceKeyAcrossGenerations(t *testing.T) {
	// Re-projecting the same consumer/owner edge in a new generation (with the
	// stable scope-only source-run id) must land on the SAME shared-projection
	// acceptance key (scope, acceptance unit, source-run). That stable key is
	// what lets the repo-dependency lane reconstruct the consumer's active edge
	// snapshot across generations and refresh the edge instead of leaving a
	// duplicate/orphan. The intent id legitimately still varies by generation,
	// because generation_id is part of the intent identity and is how the lane
	// selects the newest generation per acceptance unit (issue #3579, review
	// comment 3455350029).
	build := func(generationID string) []SharedProjectionIntentRow {
		return BuildPackageConsumptionRepoDependencyIntents(PackageConsumptionRepoDependencyInput{
			ScopeID:      "scope-pkg",
			GenerationID: generationID,
			SourceRunID:  packageConsumptionRepoEdgeSourceRunID("scope-pkg", generationID),
			CreatedAt:    time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
			ConsumptionDecisions: []PackageConsumptionDecision{
				consumptionDecision("pkg:npm/left-pad", "repo-consumer"),
			},
			OwnershipDecisions: []PackageSourceCorrelationDecision{
				ownershipDecision("pkg:npm/left-pad", "repo-owner", PackageSourceCorrelationExact),
			},
		})
	}

	genN := build("gen-1")
	genN1 := build("gen-2")
	if len(genN) != 1 || len(genN1) != 1 {
		t.Fatalf("expected 1 edge per generation, got %d and %d", len(genN), len(genN1))
	}

	keyN, ok := genN[0].AcceptanceKey()
	if !ok {
		t.Fatal("gen-1 edge has no acceptance key")
	}
	keyN1, ok := genN1[0].AcceptanceKey()
	if !ok {
		t.Fatal("gen-2 edge has no acceptance key")
	}
	if keyN != keyN1 {
		t.Fatalf("acceptance key changed across generations: %+v vs %+v (refresh would not target the same edge)", keyN, keyN1)
	}
	if keyN.SourceRunID != "package_consumption_repo_dependency:scope-pkg" {
		t.Fatalf("acceptance source-run id = %q, want package_consumption_repo_dependency:scope-pkg", keyN.SourceRunID)
	}
	// The generation must still differentiate the intent rows so the lane can
	// pick the newest generation per acceptance unit.
	if genN[0].GenerationID == genN1[0].GenerationID {
		t.Fatal("generation id must differ across generations")
	}
}

func TestBuildPackageConsumptionRepoEdgeRefreshIntentsRetractsDisappearedOwner(t *testing.T) {
	// A consumer that had a package-consumption DEPENDS_ON edge in a previous
	// generation but now resolves no owner must emit a refresh/retraction intent
	// for its package-consumption edges so the stale edge is removed by the
	// shared repo-dependency lane (issue #3579, review comment 3455350032).
	input := repoEdgeInput(
		[]PackageConsumptionDecision{consumptionDecision("pkg:npm/left-pad", "repo-consumer")},
		[]PackageSourceCorrelationDecision{ownershipDecision("pkg:npm/left-pad", "", PackageSourceCorrelationUnresolved)},
		nil,
	)

	// No edges are projected for an unowned consumer.
	if upserts := BuildPackageConsumptionRepoDependencyIntents(input); len(upserts) != 0 {
		t.Fatalf("expected no upsert edges for unowned consumer, got %d", len(upserts))
	}

	refreshRows := BuildPackageConsumptionRepoEdgeRefreshIntents(input)
	if len(refreshRows) != 1 {
		t.Fatalf("expected one refresh/retract intent for the disappeared consumer, got %d", len(refreshRows))
	}
	row := refreshRows[0]
	if got := anyToString(row.Payload["action"]); got != "retract" {
		t.Fatalf("refresh action = %q, want retract", got)
	}
	if got := anyToString(row.Payload["evidence_source"]); got != packageConsumptionEvidenceSource {
		t.Fatalf("refresh evidence_source = %q, want %q", got, packageConsumptionEvidenceSource)
	}
	if got := anyToString(row.Payload["repo_id"]); got != "repo-consumer" {
		t.Fatalf("refresh repo_id = %q, want repo-consumer", got)
	}
	if got := anyToString(row.Payload["target_repo_id"]); got != "" {
		t.Fatalf("refresh target_repo_id = %q, want blank for retract-only row", got)
	}
	if row.AcceptanceUnitID != "repo-consumer" || row.RepositoryID != "repo-consumer" {
		t.Fatalf("refresh acceptance/repo mismatch: acceptance=%q repo=%q", row.AcceptanceUnitID, row.RepositoryID)
	}
	if row.ProjectionDomain != DomainRepoDependency {
		t.Fatalf("refresh domain = %q, want %q", row.ProjectionDomain, DomainRepoDependency)
	}
	if row.ScopeID != "scope-pkg" || row.SourceRunID != "run-1" {
		t.Fatalf("refresh acceptance identity mismatch: scope=%q run=%q", row.ScopeID, row.SourceRunID)
	}
}

func TestBuildPackageConsumptionRepoEdgeRefreshIntentsSkipsOwnedConsumer(t *testing.T) {
	// A consumer whose package resolves to an owner is projected as an upsert,
	// not a retraction, so it must not appear in the refresh set.
	input := repoEdgeInput(
		[]PackageConsumptionDecision{consumptionDecision("pkg:npm/left-pad", "repo-consumer")},
		[]PackageSourceCorrelationDecision{ownershipDecision("pkg:npm/left-pad", "repo-owner", PackageSourceCorrelationExact)},
		nil,
	)

	if rows := BuildPackageConsumptionRepoEdgeRefreshIntents(input); len(rows) != 0 {
		t.Fatalf("expected no refresh intents for an owned consumer, got %d", len(rows))
	}
}

func TestBuildPackageConsumptionRepoEdgeRefreshIntentsRetractsSelfReferenceOnlyConsumer(t *testing.T) {
	// A self-referential package (consumer == owner) yields no upsert edge. The
	// consumer still gets a refresh/retract intent: if it held a real cross-repo
	// package-consumption edge in a prior generation, that edge is now stale and
	// must be removed. The retract is a no-op when no prior edge exists.
	input := repoEdgeInput(
		[]PackageConsumptionDecision{consumptionDecision("pkg:npm/self", "repo-same")},
		[]PackageSourceCorrelationDecision{ownershipDecision("pkg:npm/self", "repo-same", PackageSourceCorrelationExact)},
		nil,
	)

	if upserts := BuildPackageConsumptionRepoDependencyIntents(input); len(upserts) != 0 {
		t.Fatalf("expected no self-reference upsert edge, got %d", len(upserts))
	}

	rows := BuildPackageConsumptionRepoEdgeRefreshIntents(input)
	if len(rows) != 1 {
		t.Fatalf("expected one refresh intent for the self-reference-only consumer, got %d", len(rows))
	}
	if got := anyToString(rows[0].Payload["action"]); got != "retract" {
		t.Fatalf("refresh action = %q, want retract", got)
	}
	if got := anyToString(rows[0].Payload["repo_id"]); got != "repo-same" {
		t.Fatalf("refresh repo_id = %q, want repo-same", got)
	}
}

func TestBuildPackageConsumptionRepoEdgeRefreshIntentsDeduplicatesConsumer(t *testing.T) {
	// A consumer with two unowned packages must yield exactly one refresh intent,
	// not one per package, so the lane reprocesses the consumer once.
	input := repoEdgeInput(
		[]PackageConsumptionDecision{
			consumptionDecision("pkg:npm/a", "repo-consumer"),
			consumptionDecision("pkg:npm/b", "repo-consumer"),
		},
		[]PackageSourceCorrelationDecision{
			ownershipDecision("pkg:npm/a", "", PackageSourceCorrelationUnresolved),
			ownershipDecision("pkg:npm/b", "", PackageSourceCorrelationUnresolved),
		},
		nil,
	)

	if rows := BuildPackageConsumptionRepoEdgeRefreshIntents(input); len(rows) != 1 {
		t.Fatalf("expected one deduplicated refresh intent, got %d", len(rows))
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
