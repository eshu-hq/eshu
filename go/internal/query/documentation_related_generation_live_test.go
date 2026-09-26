// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestDocumentationRelatedReadsBindActiveGenerationLive exercises the three
// production SQL builders over retained, active, and failed generations.
func TestDocumentationRelatedReadsBindActiveGenerationLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		4*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	seedDocumentationFactsGenerationRowSet(t, ctx, db)
	seedDocumentationRelatedGenerationRows(t, ctx, db)

	cases := []struct {
		name  string
		kinds []string
		build func(scope, generation string) (string, []any)
	}{
		{
			name:  "findings",
			kinds: []string{facts.DocumentationFindingFactKind},
			build: func(scope, generation string) (string, []any) {
				return buildDocumentationFindingsSQL(documentationFindingFilter{
					ScopeID: scope, GenerationID: generation, Limit: 20,
				})
			},
		},
		{
			name: "target facts",
			kinds: []string{
				facts.DocumentationEntityMentionFactKind,
				facts.DocumentationClaimCandidateFactKind,
				facts.SemanticDocumentationObservationFactKind,
			},
			build: func(scope, generation string) (string, []any) {
				return buildDocumentationTargetFactsSQL(documentationFindingFilter{
					ScopeID: scope, GenerationID: generation, Limit: 20,
				})
			},
		},
		{
			name:  "semantic observations",
			kinds: []string{facts.SemanticDocumentationObservationFactKind},
			build: func(scope, generation string) (string, []any) {
				return buildSemanticEvidenceSQL(semanticEvidenceFilter{
					FactKind: facts.SemanticDocumentationObservationFactKind,
					ScopeID:  scope, GenerationID: generation, Limit: 20,
				})
			},
		},
		{
			name:  "semantic code hints",
			kinds: []string{facts.SemanticCodeHintFactKind},
			build: func(scope, generation string) (string, []any) {
				return buildSemanticEvidenceSQL(semanticEvidenceFilter{
					FactKind: facts.SemanticCodeHintFactKind,
					ScopeID:  scope, GenerationID: generation, Limit: 20,
				})
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, state := range []struct {
				name, scope, generation, wantGeneration string
			}{
				{"default active", docFactsScopeA, "", docFactsGenNew},
				{"explicit superseded", docFactsScopeA, docFactsGenOld, docFactsGenOld},
				{"no active", docFactsScopeB, "", ""},
			} {
				t.Run(state.name, func(t *testing.T) {
					query, args := tc.build(state.scope, state.generation)
					got := documentationFactPayloadIDs(t, ctx, db, query, args...)
					want := documentationRelatedReferenceIDs(t, ctx, db, state.scope, state.wantGeneration, tc.kinds)
					assertDocumentationFactIDs(t, got, want)
				})
			}
		})
	}
}

func seedDocumentationRelatedGenerationRows(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, pair := range []struct{ scope, generation string }{
		{docFactsScopeA, docFactsGenOld},
		{docFactsScopeA, docFactsGenNew},
		{docFactsScopeB, docFactsGenB},
	} {
		_, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  'fact:' || $2::text || ':' || kind, $1, $2, kind,
  'stable:' || $2::text || ':' || kind,
  'proof', 'proof', 'key:' || $2::text || ':' || kind,
  TIMESTAMPTZ '2026-09-01 00:00:00+00', clock_timestamp(),
  jsonb_build_object(
    'fact_id', 'fact:' || $2::text || ':' || kind,
    'source_id', 'source:related-generation',
    'source', jsonb_build_object('source_id', 'source:related-generation')
  )
FROM unnest($3::text[]) AS kinds(kind)`, pair.scope, pair.generation, []string{
			facts.DocumentationFindingFactKind,
			facts.DocumentationEntityMentionFactKind,
			facts.DocumentationClaimCandidateFactKind,
			facts.SemanticDocumentationObservationFactKind,
			facts.SemanticCodeHintFactKind,
		})
		if err != nil {
			t.Fatalf("seed related rows: %v", err)
		}
	}
}

func documentationRelatedReferenceIDs(
	t *testing.T, ctx context.Context, db *sql.DB, scope, generation string, kinds []string,
) []string {
	t.Helper()
	if generation == "" {
		return nil
	}
	return queryDocumentationIDs(t, ctx, db, `
SELECT fact_id FROM fact_records
WHERE scope_id = $1 AND generation_id = $2 AND fact_kind = ANY($3::text[])
  AND NOT is_tombstone
ORDER BY observed_at DESC, fact_id DESC`, scope, generation, kinds)
}
