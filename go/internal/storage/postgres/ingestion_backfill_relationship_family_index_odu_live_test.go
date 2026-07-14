// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestRelationshipFamilyIndexOduQueryProof(t *testing.T) {
	proof := openRelationshipFamilyProofDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	odu, catalog, partitions := seedRelationshipFamilyProofOdu(t, ctx, proof.scoped)
	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("proof Odù catalog produced no deferred-query parameters")
	}
	worst := partitions[0]
	assertRelationshipFamilyWorstShape(t, ctx, proof.scoped, worst)
	oldQuery := relationshipFamilyOldQuery(t)

	oldPlanBefore := explainDeferredRelationshipFamilyQuery(
		t, ctx, proof.scoped, oldQuery, params, worst,
	)
	aliasPlan := explainRelationshipFamilyQuery(
		t,
		ctx,
		proof.scoped,
		relationshipFamilyAliasArmQuery(),
		params.nonRepoIDLike,
		worst.ScopeID,
		worst.GenerationID,
	)

	indexBuild := createRelationshipFamilyProofIndex(t, ctx, proof.scoped)
	candidateQuery := relationshipFamilyCandidateQuery(t)
	candidatePlan := explainDeferredRelationshipFamilyQuery(
		t, ctx, proof.scoped, candidateQuery, params, worst,
	)
	narrowReferenceQuery := relationshipFamilyNarrowReferenceQuery()
	narrowReferencePlan := explainDeferredRelationshipFamilyQuery(
		t, ctx, proof.scoped, narrowReferenceQuery, params, worst,
	)
	oldPlanAfter := explainDeferredRelationshipFamilyQuery(
		t, ctx, proof.scoped, oldQuery, params, worst,
	)

	if !relationshipFamilyPlanUsesIndex(candidatePlan.Root, relationshipFamilyProofIndexName) {
		t.Fatalf("candidate plan does not use %s", relationshipFamilyProofIndexName)
	}
	if relationshipFamilyPlanUsesIndex(oldPlanAfter.Root, relationshipFamilyProofIndexName) {
		t.Fatalf("shipped query unexpectedly uses candidate index without the direct source predicate")
	}
	if candidatePlan.ExecutionMS >= oldPlanAfter.ExecutionMS {
		t.Fatalf("candidate query is not faster on retained-shape Odù: old=%.3fms candidate=%.3fms", oldPlanAfter.ExecutionMS, candidatePlan.ExecutionMS)
	}
	if narrowReferencePlan.ExecutionMS >= 100 {
		t.Fatalf("narrow-reference candidate = %.3fms, want under 100ms", narrowReferencePlan.ExecutionMS)
	}

	oldFacts, oldDurations := runRelationshipFamilyProofAcrossPartitions(
		t, ctx, proof.scoped, oldQuery, params, partitions,
	)
	newFacts, newDurations := runRelationshipFamilyProofAcrossPartitions(
		t, ctx, proof.scoped, narrowReferenceQuery, params, partitions,
	)
	assertRelationshipFamilyExactness(t, odu, catalog, oldFacts, newFacts)

	oldCritical := maximumDuration(oldDurations)
	newCritical := maximumDuration(newDurations)
	if newCritical >= oldCritical {
		t.Fatalf("candidate eight-task critical path is not faster: old=%s candidate=%s", oldCritical, newCritical)
	}
	var indexBytes int64
	if err := proof.scoped.QueryRowContext(
		ctx,
		"SELECT pg_relation_size($1::regclass)",
		relationshipFamilyProofIndexName,
	).Scan(&indexBytes); err != nil {
		t.Fatalf("read relationship-family proof index size: %v", err)
	}
	oldHits, oldReads := relationshipFamilyPlanBuffers(oldPlanAfter.Root)
	newHits, newReads := relationshipFamilyPlanBuffers(narrowReferencePlan.Root)
	t.Logf(
		"relationship-family Odù proof source_rows=60988 family_candidates=84 old_before_ms=%.3f old_after_ms=%.3f alias_ms=%.3f candidate_ms=%.3f narrow_reference_ms=%.3f old_buffers_hit=%d old_buffers_read=%d candidate_buffers_hit=%d candidate_buffers_read=%d index_build=%s index_bytes=%d eight_task_old=%s eight_task_candidate=%s",
		oldPlanBefore.ExecutionMS,
		oldPlanAfter.ExecutionMS,
		aliasPlan.ExecutionMS,
		candidatePlan.ExecutionMS,
		narrowReferencePlan.ExecutionMS,
		oldHits,
		oldReads,
		newHits,
		newReads,
		indexBuild,
		indexBytes,
		oldCritical,
		newCritical,
	)
	t.Logf("shipped old plan:\n%s", relationshipFamilyPlanSummary(oldPlanAfter.Root))
	t.Logf("candidate plan:\n%s", relationshipFamilyPlanSummary(candidatePlan.Root))
	t.Logf("narrow-reference candidate plan:\n%s", relationshipFamilyPlanSummary(narrowReferencePlan.Root))
}

func assertRelationshipFamilyWorstShape(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	partition scopeGenerationPartition,
) {
	t.Helper()
	predicate := deferredRelationshipFamilyCandidatePredicateSQL
	query := `
SELECT
  count(*),
  count(*) FILTER (WHERE ` + predicate + `)
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind IN ('content', 'file', 'gcp_cloud_relationship')
`
	var sourceRows int
	var candidates int
	if err := db.QueryRowContext(ctx, query, partition.ScopeID, partition.GenerationID).Scan(&sourceRows, &candidates); err != nil {
		t.Fatalf("measure worst Odù partition: %v", err)
	}
	if sourceRows != 14190 || candidates != 12 {
		t.Fatalf("worst Odù partition shape = source_rows:%d candidates:%d, want 14190/12", sourceRows, candidates)
	}
}

func explainDeferredRelationshipFamilyQuery(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	params deferredScopedFactQueryParams,
	partition scopeGenerationPartition,
) relationshipFamilyPlan {
	t.Helper()
	args := deferredRelationshipFamilyProofArgs(params, partition)
	return explainRelationshipFamilyQuery(t, ctx, db, query, args...)
}

func deferredRelationshipFamilyProofArgs(
	params deferredScopedFactQueryParams,
	partition scopeGenerationPartition,
) []any {
	ownRepoID := deferredScopedFactOwnRepoIDFromScope(partition.ScopeID)
	regex, ok := buildDeferredRepoIDRegex([]string(params.repoIDValues), ownRepoID)
	var regexParam sql.NullString
	if ok {
		regexParam = sql.NullString{String: regex, Valid: true}
	}
	return []any{
		params.nonRepoIDLike,
		params.repoIDValues,
		partition.ScopeID,
		partition.GenerationID,
		regexParam,
		ownRepoID,
		deferredRepoIDReferenceKeys(params.repoIDValues, params.repoIDReferenceKey),
	}
}

func runRelationshipFamilyProofAcrossPartitions(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	params deferredScopedFactQueryParams,
	partitions []scopeGenerationPartition,
) ([]facts.Envelope, []time.Duration) {
	t.Helper()
	var all []facts.Envelope
	durations := make([]time.Duration, 0, len(partitions))
	for _, partition := range partitions {
		started := time.Now()
		rows, err := db.QueryContext(ctx, query, deferredRelationshipFamilyProofArgs(params, partition)...)
		if err != nil {
			t.Fatalf("run relationship-family proof query for %s: %v", partition.ScopeID, err)
		}
		loaded, err := collectRelationshipFamilyProofRows(rows)
		if err != nil {
			t.Fatalf("collect relationship-family proof query for %s: %v", partition.ScopeID, err)
		}
		durations = append(durations, time.Since(started))
		all = append(all, loaded...)
	}
	return all, durations
}

func collectRelationshipFamilyProofRows(rows *sql.Rows) ([]facts.Envelope, error) {
	defer func() { _ = rows.Close() }()
	var loaded []facts.Envelope
	for rows.Next() {
		envelope, err := scanFactEnvelope(rows)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, envelope)
	}
	return loaded, rows.Err()
}

func assertRelationshipFamilyExactness(
	t *testing.T,
	odu ifa.Odu,
	catalog []relationships.CatalogEntry,
	oldFacts, newFacts []facts.Envelope,
) {
	t.Helper()
	oldIDs := factIDSet(oldFacts)
	newIDs := factIDSet(newFacts)
	missing, unexpected := bidirectionalFactIDDiff(oldIDs, newIDs)
	if len(missing) != 0 || len(unexpected) != 0 {
		t.Fatalf("candidate fact_id diff is not 0/0: missing=%v unexpected=%v", missing, unexpected)
	}
	if len(oldFacts) != len(newFacts) {
		t.Fatalf("loaded fact count differs: old=%d candidate=%d", len(oldFacts), len(newFacts))
	}
	if len(newIDs) != len(newFacts) {
		t.Fatalf("candidate returned duplicate fact_ids: rows=%d distinct=%d", len(newFacts), len(newIDs))
	}

	oldEvidence := relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(oldFacts, catalog))
	newEvidence := relationships.DedupeEvidenceFacts(relationships.DiscoverEvidence(newFacts, catalog))
	expectedEvidence := relationships.DedupeEvidenceFacts(ifa.DiscoveredEvidence(odu))
	if !evidenceSetsEqual(oldEvidence, newEvidence) || !evidenceSetsEqual(newEvidence, expectedEvidence) {
		t.Fatalf("relationship evidence diverged: old=%v candidate=%v expected=%v", oldEvidence, newEvidence, expectedEvidence)
	}
	if len(newEvidence) != 8 {
		t.Fatalf("deduped evidence count = %d, want 8", len(newEvidence))
	}
	for _, item := range newEvidence {
		if item.SourceRepoID == item.TargetRepoID {
			t.Fatalf("self relationship escaped candidate: %+v", item)
		}
		if item.TargetRepoID == "repository:target-07-extra" {
			t.Fatalf("prefix collision escaped candidate: %+v", item)
		}
	}
	dualArmID := ""
	for _, fact := range odu.Facts {
		if _, ok := fact.Payload["linked_repo_id"]; ok {
			dualArmID = fact.FactID
			break
		}
	}
	count := 0
	for _, fact := range newFacts {
		if fact.FactID == dualArmID {
			count++
		}
	}
	if dualArmID == "" || count != 1 {
		t.Fatalf("dual-arm fact occurrence = %d, want exactly 1 (fact_id=%q)", count, dualArmID)
	}
}

func bidirectionalFactIDDiff(oldIDs, newIDs []string) ([]string, []string) {
	oldSet := make(map[string]struct{}, len(oldIDs))
	newSet := make(map[string]struct{}, len(newIDs))
	for _, id := range oldIDs {
		oldSet[id] = struct{}{}
	}
	for _, id := range newIDs {
		newSet[id] = struct{}{}
	}
	var missing []string
	var unexpected []string
	for id := range oldSet {
		if _, ok := newSet[id]; !ok {
			missing = append(missing, id)
		}
	}
	for id := range newSet {
		if _, ok := oldSet[id]; !ok {
			unexpected = append(unexpected, id)
		}
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	return missing, unexpected
}

func maximumDuration(values []time.Duration) time.Duration {
	var maximum time.Duration
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}
