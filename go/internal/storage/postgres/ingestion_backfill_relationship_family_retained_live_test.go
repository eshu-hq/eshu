// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	relationshipFamilyRetainedProofDSNEnv        = "ESHU_RELATIONSHIP_FAMILY_RETAINED_PROOF_DSN"
	relationshipFamilyRetainedProofScopeEnv      = "ESHU_RELATIONSHIP_FAMILY_RETAINED_SCOPE_ID"
	relationshipFamilyRetainedProofGenerationEnv = "ESHU_RELATIONSHIP_FAMILY_RETAINED_GENERATION_ID"
	relationshipFamilyRetainedProofRunOldEnv     = "ESHU_RELATIONSHIP_FAMILY_RETAINED_RUN_OLD"
)

// TestRelationshipFamilyIndexRetainedWorstScopeProof runs the shipped and
// candidate queries on a session-local copy of one retained worst partition.
// The source database remains read-only: only PostgreSQL temporary objects are
// created, and all public relationship-reference/catalog state is read in place.
func TestRelationshipFamilyIndexRetainedWorstScopeProof(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(relationshipFamilyRetainedProofDSNEnv))
	scopeID := strings.TrimSpace(os.Getenv(relationshipFamilyRetainedProofScopeEnv))
	generationID := strings.TrimSpace(os.Getenv(relationshipFamilyRetainedProofGenerationEnv))
	if dsn == "" || scopeID == "" || generationID == "" {
		t.Skip("set retained proof DSN, scope, and generation to run the bounded relationship-family proof")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open retained proof postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, "SET statement_timeout = '120s'; SET temp_buffers = '512MB'"); err != nil {
		t.Fatalf("bound retained proof session: %v", err)
	}

	catalog, _, err := loadRepositoryCatalog(ctx, SQLDB{DB: db})
	if err != nil {
		t.Fatalf("load retained repository catalog: %v", err)
	}
	params, ok := buildDeferredScopedFactQueryParams(catalog)
	if !ok {
		t.Fatal("retained catalog produced no deferred-query parameters")
	}
	partition := scopeGenerationPartition{ScopeID: scopeID, GenerationID: generationID}
	copyDuration, indexDuration := prepareRelationshipFamilyRetainedTempPartition(t, ctx, db, partition)
	assertRelationshipFamilyWorstShape(t, ctx, db, partition)
	if _, err := db.ExecContext(ctx, "SET default_transaction_read_only = on"); err != nil {
		t.Fatalf("make retained proof connection read-only: %v", err)
	}

	newQuery := relationshipFamilyNarrowReferenceQuery()
	newPlan := explainDeferredRelationshipFamilyQuery(t, ctx, db, newQuery, params, partition)
	if !relationshipFamilyPlanUsesIndex(newPlan.Root, relationshipFamilyProofIndexName) {
		t.Fatalf("retained candidate plan does not use %s", relationshipFamilyProofIndexName)
	}
	if newPlan.ExecutionMS >= 1000 {
		t.Fatalf("retained candidate = %.3fms, want under 1000ms schedule assumption", newPlan.ExecutionMS)
	}
	newFacts, _ := runRelationshipFamilyProofAcrossPartitions(
		t, ctx, db, newQuery, params, []scopeGenerationPartition{partition},
	)
	if hasDuplicateFactIDs(newFacts) {
		t.Fatalf("retained candidate returned duplicate fact IDs")
	}
	t.Logf(
		"relationship-family retained candidate source_rows=14190 family_candidates=12 catalog_entries=%d candidate_ms=%.3f copy=%s index_build=%s candidate_rows=%d",
		len(catalog), newPlan.ExecutionMS, copyDuration, indexDuration, len(newFacts),
	)
	t.Logf("retained candidate plan:\n%s", relationshipFamilyPlanSummary(newPlan.Root))
	if strings.TrimSpace(os.Getenv(relationshipFamilyRetainedProofRunOldEnv)) != "true" {
		return
	}

	oldPlan := explainDeferredRelationshipFamilyQuery(
		t, ctx, db, relationshipFamilyOldQuery(t), params, partition,
	)
	oldFacts, _ := runRelationshipFamilyProofAcrossPartitions(
		t, ctx, db, relationshipFamilyOldQuery(t), params, []scopeGenerationPartition{partition},
	)
	missing, unexpected := bidirectionalFactIDDiff(factIDs(oldFacts), factIDs(newFacts))
	if len(missing) != 0 || len(unexpected) != 0 {
		t.Fatalf("retained old/new fact_id diff missing=%d unexpected=%d", len(missing), len(unexpected))
	}
	if len(oldFacts) != len(newFacts) || hasDuplicateFactIDs(oldFacts) || hasDuplicateFactIDs(newFacts) {
		t.Fatalf(
			"retained result cardinality old=%d new=%d old_duplicates=%t new_duplicates=%t",
			len(oldFacts), len(newFacts), hasDuplicateFactIDs(oldFacts), hasDuplicateFactIDs(newFacts),
		)
	}

	t.Logf(
		"relationship-family retained proof source_rows=14190 family_candidates=12 catalog_entries=%d old_ms=%.3f candidate_ms=%.3f copy=%s index_build=%s old_rows=%d new_rows=%d fact_id_diff=0/0",
		len(catalog), oldPlan.ExecutionMS, newPlan.ExecutionMS, copyDuration, indexDuration, len(oldFacts), len(newFacts),
	)
	t.Logf("retained shipped plan:\n%s", relationshipFamilyPlanSummary(oldPlan.Root))
}

func prepareRelationshipFamilyRetainedTempPartition(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	partition scopeGenerationPartition,
) (time.Duration, time.Duration) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
CREATE TEMP TABLE fact_records
(LIKE public.fact_records INCLUDING DEFAULTS INCLUDING GENERATED INCLUDING CONSTRAINTS)
ON COMMIT PRESERVE ROWS`); err != nil {
		t.Fatalf("create retained temporary fact table: %v", err)
	}
	copyStarted := time.Now()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
SELECT *
FROM public.fact_records
WHERE scope_id = $1
  AND generation_id = $2
  AND fact_kind IN ('content', 'file', 'gcp_cloud_relationship')`, partition.ScopeID, partition.GenerationID); err != nil {
		t.Fatalf("copy retained worst partition: %v", err)
	}
	copyDuration := time.Since(copyStarted)
	if _, err := db.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze retained temporary fact table: %v", err)
	}

	predicate := strings.ReplaceAll(deferredRelationshipFamilyCandidatePredicateSQL, "fact.", "")
	indexStarted := time.Now()
	if _, err := db.ExecContext(ctx,
		"CREATE INDEX "+relationshipFamilyProofIndexName+
			" ON fact_records (scope_id, generation_id, observed_at, fact_id) WHERE fact_kind IN ('content', 'file', 'gcp_cloud_relationship') AND "+predicate,
	); err != nil {
		t.Fatalf("create retained temporary candidate index: %v", err)
	}
	indexDuration := time.Since(indexStarted)
	if _, err := db.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze retained indexed temporary fact table: %v", err)
	}
	var persistence string
	if err := db.QueryRowContext(ctx,
		"SELECT relpersistence::text FROM pg_class WHERE oid = 'fact_records'::regclass",
	).Scan(&persistence); err != nil {
		t.Fatalf("read retained proof relation persistence: %v", err)
	}
	if persistence != "t" {
		t.Fatalf("retained proof fact_records persistence = %q, want temporary t", persistence)
	}
	return copyDuration, indexDuration
}

func hasDuplicateFactIDs(envelopes []facts.Envelope) bool {
	seen := make(map[string]struct{}, len(envelopes))
	for _, envelope := range envelopes {
		if _, ok := seen[envelope.FactID]; ok {
			return true
		}
		seen[envelope.FactID] = struct{}{}
	}
	return false
}
