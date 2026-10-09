// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/docs"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestDocumentationRelatedReadsBindActiveGenerationLive exercises the related
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
			kinds: []string{docs.FindingFactKind},
			build: func(scope, generation string) (string, []any) {
				return buildDocumentationFindingsSQL(documentationFindingFilter{
					ScopeID: scope, GenerationID: generation, Limit: 20,
				})
			},
		},
		{
			name: "target facts",
			kinds: []string{
				docs.EntityMentionFactKind,
				docs.ClaimCandidateFactKind,
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
	t.Run("fact id without scope uses the active generation", func(t *testing.T) {
		for _, state := range []struct {
			name, generation string
			want             []string
		}{
			{"active", docFactsGenNew, []string{"fact:" + docFactsGenNew + ":" + facts.SemanticCodeHintFactKind}},
			{"superseded", docFactsGenOld, nil},
			{"no active", docFactsGenB, nil},
		} {
			t.Run(state.name, func(t *testing.T) {
				factID := "fact:" + state.generation + ":" + facts.SemanticCodeHintFactKind
				query, args := buildSemanticEvidenceSQL(semanticEvidenceFilter{
					FactKind: facts.SemanticCodeHintFactKind,
					FactID:   factID,
					Limit:    20,
				})
				got := documentationFactPayloadIDs(t, ctx, db, query, args...)
				assertDocumentationFactIDs(t, got, state.want)
			})
		}
		factID := "fact:" + docFactsGenOld + ":" + facts.SemanticCodeHintFactKind
		query, args := buildSemanticEvidenceSQL(semanticEvidenceFilter{
			FactKind: facts.SemanticCodeHintFactKind,
			FactID:   factID, GenerationID: docFactsGenOld, Limit: 20,
		})
		assertDocumentationFactIDs(t,
			documentationFactPayloadIDs(t, ctx, db, query, args...), []string{factID})
	})
	t.Run("unscoped pages use the measured active binding shape", func(t *testing.T) {
		for _, tc := range cases {
			query, args := tc.build("", "")
			got := documentationFactPayloadIDs(t, ctx, db, query, args...)
			want := queryDocumentationIDs(t, ctx, db, `
SELECT f.fact_id FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id
  AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = ANY($1::text[]) AND NOT f.is_tombstone
ORDER BY f.observed_at DESC, f.fact_id DESC`, tc.kinds)
			assertDocumentationFactIDs(t, got, want)
			if tc.name == "findings" {
				if !strings.Contains(query, documentationFactActiveProbeClause) {
					t.Fatal("unfiltered findings must use the bounded page probe")
				}
				continue
			}
			if !strings.Contains(query, documentationFactActiveScopeJoinSQL) ||
				strings.Contains(query, documentationFactActiveProbeClause) {
				t.Fatalf("%s must bind active scopes with a join", tc.name)
			}
		}
		filteredFindings, _ := buildDocumentationFindingsSQL(documentationFindingFilter{
			SourceID: "source:related-generation", Limit: 20,
		})
		if !strings.Contains(filteredFindings, documentationFactActiveScopeJoinSQL) ||
			strings.Contains(filteredFindings, documentationFactActiveProbeClause) {
			t.Fatal("filtered findings must bind active scopes with a join")
		}
	})
	t.Run("source-only coverage of an explicit historical generation", func(t *testing.T) {
		query, args := buildDocumentationSourceOnlySQL(documentationFindingFilter{
			ScopeID: docFactsScopeA, GenerationID: docFactsGenOld, SourceID: docFactsSourceA,
		})
		var total, sources, documents, sections, links int
		if err := db.QueryRowContext(ctx, query, args...).Scan(
			&total, &sources, &documents, &sections, &links,
		); err != nil {
			t.Fatal(err)
		}
		if total != docFactsRowsPerG || sources != 1 {
			t.Fatalf("historical source-only facts = %d (sources %d), want %d (sources 1)",
				total, sources, docFactsRowsPerG)
		}
	})
	t.Run("read models label historical and missing active generations", func(t *testing.T) {
		reader := NewContentReader(db)
		findings, err := reader.DocumentationFindings(ctx, documentationFindingFilter{
			ScopeID: docFactsScopeA, GenerationID: docFactsGenOld,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(findings.Findings) != 1 || findings.Binding.IsActive ||
			findings.Freshness.State != querycontract.FreshnessStale {
			t.Fatalf("historical findings state = %#v", findings)
		}
		findings, err = reader.DocumentationFindings(ctx, documentationFindingFilter{ScopeID: docFactsScopeB})
		if err != nil {
			t.Fatal(err)
		}
		if len(findings.Findings) != 0 ||
			findings.EmptyReason != querycontract.DocumentationFactEmptyNoActiveGeneration ||
			findings.Freshness.State != querycontract.FreshnessUnavailable {
			t.Fatalf("failed scope findings state = %#v", findings)
		}
		semantic, err := reader.SemanticEvidence(ctx, semanticEvidenceFilter{
			FactKind: facts.SemanticCodeHintFactKind,
			ScopeID:  docFactsScopeA, GenerationID: docFactsGenOld,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(semantic.Rows) != 1 || semantic.Binding.IsActive ||
			semantic.Freshness.State != querycontract.FreshnessStale {
			t.Fatalf("historical semantic state = %#v", semantic)
		}
		semantic, err = reader.SemanticEvidence(ctx, semanticEvidenceFilter{
			FactKind: facts.SemanticCodeHintFactKind, ScopeID: docFactsScopeB,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(semantic.Rows) != 0 ||
			semantic.EmptyReason != querycontract.DocumentationFactEmptyNoActiveGeneration ||
			semantic.Freshness.State != querycontract.FreshnessUnavailable {
			t.Fatalf("failed scope semantic state = %#v", semantic)
		}
	})
	t.Run("scope labels honor caller grants", func(t *testing.T) {
		reader := NewContentReader(db)
		findings, err := reader.DocumentationFindings(ctx, documentationFindingFilter{
			ScopeID: docFactsScopeA, AllowedScopeIDs: []string{docFactsScopeD},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(findings.Findings) != 0 || findings.EmptyReason != querycontract.DocumentationFactEmptyScopeNotFound ||
			findings.Binding.GenerationID != "" {
			t.Fatalf("out-of-grant findings disclosed scope state: %#v", findings)
		}
		semantic, err := reader.SemanticEvidence(ctx, semanticEvidenceFilter{
			FactKind: facts.SemanticDocumentationObservationFactKind,
			ScopeID:  docFactsScopeA, AllowedScopeIDs: []string{docFactsScopeD},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(semantic.Rows) != 0 || semantic.EmptyReason != querycontract.DocumentationFactEmptyScopeNotFound ||
			semantic.Binding.GenerationID != "" {
			t.Fatalf("out-of-grant semantic evidence disclosed scope state: %#v", semantic)
		}
	})
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
			docs.FindingFactKind,
			docs.EntityMentionFactKind,
			docs.ClaimCandidateFactKind,
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
