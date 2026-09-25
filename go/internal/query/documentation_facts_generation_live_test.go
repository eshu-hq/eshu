// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// Scopes and generations seeded by seedDocumentationFactsGenerationRowSet.
const (
	docFactsScopeA   = "scope:docfacts-a"        // one superseded + one active generation
	docFactsGenOld   = "generation:docfacts-a-1" // superseded
	docFactsGenNew   = "generation:docfacts-a-2" // active
	docFactsScopeD   = "scope:docfacts-d"        // second active scope
	docFactsGenD     = "generation:docfacts-d-1"
	docFactsScopeB   = "scope:docfacts-b" // active generation dead-lettered: NULL active
	docFactsGenB     = "generation:docfacts-b-1"
	docFactsScopeC   = "scope:docfacts-c" // first generation still pending
	docFactsGenC     = "generation:docfacts-c-1"
	docFactsScopeE   = "scope:docfacts-e" // active generation with no facts
	docFactsGenE     = "generation:docfacts-e-1"
	docFactsSourceA  = "doc-source:docfacts-a"
	docFactsRowsPerG = 30
)

// TestDocumentationFactsBindActiveGenerationLive proves #7128 against a real
// PostgreSQL: the default read serves only the scope's active generation, an
// explicit generation_id keeps returning that generation's exact rows, and the
// row order and paging match a hand-written reference statement. It is opt-in
// because it creates a disposable database and applies the real bootstrap DDL.
func TestDocumentationFactsBindActiveGenerationLive(t *testing.T) {
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
	reader := NewContentReader(db)

	t.Run("default read returns only the active generation in reference order", func(t *testing.T) {
		got := documentationFactIDs(t, reader, documentationFactFilter{ScopeID: docFactsScopeA, Limit: 200})
		want := documentationReferenceFactIDs(t, ctx, db, docFactsScopeA, docFactsGenNew, "")
		assertDocumentationFactIDs(t, got, want)
		if len(got) == 0 {
			t.Fatal("seed produced no active-generation rows")
		}
		for _, id := range got {
			if strings.Contains(id, docFactsGenOld) {
				t.Fatalf("default read returned superseded-generation fact %s", id)
			}
		}
	})

	t.Run("explicit superseded generation keeps its exact rows", func(t *testing.T) {
		got := documentationFactIDs(t, reader, documentationFactFilter{
			ScopeID: docFactsScopeA, GenerationID: docFactsGenOld, Limit: 200,
		})
		want := documentationReferenceFactIDs(t, ctx, db, docFactsScopeA, docFactsGenOld, "")
		assertDocumentationFactIDs(t, got, want)
	})

	t.Run("selective fact_kind filter keeps the generation bind", func(t *testing.T) {
		got := documentationFactIDs(t, reader, documentationFactFilter{
			ScopeID: docFactsScopeA, FactKind: "documentation_section", Limit: 200,
		})
		want := documentationReferenceFactIDs(t, ctx, db, docFactsScopeA, docFactsGenNew, "documentation_section")
		assertDocumentationFactIDs(t, got, want)
		if len(got) == 0 || len(got) >= docFactsRowsPerG {
			t.Fatalf("section filter returned %d rows, want a strict, non-empty subset of the %d active rows", len(got), docFactsRowsPerG)
		}
	})

	t.Run("offset paging walks the active generation without duplicates or gaps", func(t *testing.T) {
		want := documentationReferenceFactIDs(t, ctx, db, docFactsScopeA, docFactsGenNew, "")
		var got []string
		for offset := 0; offset < len(want); offset += 7 {
			page := documentationFactIDs(t, reader, documentationFactFilter{
				ScopeID: docFactsScopeA, Limit: 7, Offset: offset,
			})
			got = append(got, page...)
		}
		assertDocumentationFactIDs(t, got, want)
	})

	t.Run("scoped token keeps the active bind", func(t *testing.T) {
		got := documentationFactIDs(t, reader, documentationFactFilter{
			ScopeID: docFactsScopeA, Limit: 200, AllowedScopeIDs: []string{docFactsScopeA},
		})
		want := documentationReferenceFactIDs(t, ctx, db, docFactsScopeA, docFactsGenNew, "")
		assertDocumentationFactIDs(t, got, want)
		denied := documentationFactIDs(t, reader, documentationFactFilter{
			ScopeID: docFactsScopeA, Limit: 200, AllowedScopeIDs: []string{docFactsScopeD},
		})
		if len(denied) != 0 {
			t.Fatalf("a token granted only %s read %d rows of %s", docFactsScopeD, len(denied), docFactsScopeA)
		}
	})

	t.Run("anchor-only source read serves only active generations", func(t *testing.T) {
		for _, filter := range []documentationFactFilter{
			{SourceID: docFactsSourceA, Limit: 200},
			{SourceID: docFactsSourceA, Limit: 200, AllowedScopeIDs: []string{docFactsScopeA}},
		} {
			got := documentationFactIDs(t, reader, filter)
			want := documentationReferenceFactIDsBySource(t, ctx, db, docFactsSourceA)
			assertDocumentationFactIDs(t, got, want)
			for _, id := range got {
				if strings.Contains(id, docFactsGenOld) || strings.Contains(id, docFactsGenB) {
					t.Fatalf("anchor-only read returned inactive-generation fact %s", id)
				}
			}
		}
	})

	t.Run("kind-only source read serves one row per active scope", func(t *testing.T) {
		got := documentationFactIDs(t, reader, documentationFactFilter{FactKind: "documentation_source", Limit: 200})
		want := []string{
			"fact:" + docFactsGenD + ":source",
			"fact:" + docFactsGenNew + ":source",
		}
		if !sameDocumentationIDSet(got, want) {
			t.Fatalf("kind-only source read = %v, want exactly the active sources %v", got, want)
		}
	})

	t.Run("probe form and join form return identical rows", func(t *testing.T) {
		// The kind-only source read binds through the per-row probe; the same
		// read written with the INNER JOIN must return the same ordered rows.
		// The seed makes the comparison bite: source facts of every scope tie on
		// observed_at (fact_id breaks the tie), two scopes are active, one has a
		// superseded generation, and two scopes have a NULL active generation
		// (dead-lettered and pending) whose facts both forms must exclude.
		filter := documentationFactFilter{FactKind: "documentation_source", Limit: 200}
		probeSQL, args := buildDocumentationFactsSQL(filter)
		if documentationFactBindingForm(filter) != documentationFactBindingActiveProbe ||
			!strings.Contains(probeSQL, documentationFactActiveProbeClause) {
			t.Fatalf("filter did not select the probe form:\n%s", probeSQL)
		}
		joinSQL := strings.Replace(probeSQL, " AND "+documentationFactActiveProbeClause, "", 1)
		joinSQL = strings.Replace(joinSQL, "FROM fact_records\n", "FROM fact_records"+documentationFactActiveScopeJoinSQL+"\n", 1)
		if joinSQL == probeSQL || strings.Contains(joinSQL, documentationFactActiveProbeClause) {
			t.Fatalf("join-form derivation did not change the statement:\n%s", joinSQL)
		}
		probeIDs := documentationFactPayloadIDs(t, ctx, db, probeSQL, args...)
		joinIDs := documentationFactPayloadIDs(t, ctx, db, joinSQL, args...)
		assertDocumentationFactIDs(t, probeIDs, joinIDs)
		want := []string{
			"fact:" + docFactsGenNew + ":source",
			"fact:" + docFactsGenD + ":source",
		}
		if !sameDocumentationIDSet(probeIDs, want) {
			t.Fatalf("probe form = %v, want exactly the active sources %v (NULL-active and superseded generations excluded)", probeIDs, want)
		}
		// Paging one row at a time walks the same order through both forms.
		for offset := 0; offset < 3; offset++ {
			page := documentationFactFilter{FactKind: "documentation_source", Limit: 1, Offset: offset}
			pageSQL, pageArgs := buildDocumentationFactsSQL(page)
			pageJoin := strings.Replace(pageSQL, " AND "+documentationFactActiveProbeClause, "", 1)
			pageJoin = strings.Replace(pageJoin, "FROM fact_records\n", "FROM fact_records"+documentationFactActiveScopeJoinSQL+"\n", 1)
			assertDocumentationFactIDs(t,
				documentationFactPayloadIDs(t, ctx, db, pageSQL, pageArgs...),
				documentationFactPayloadIDs(t, ctx, db, pageJoin, pageArgs...))
		}
	})

	t.Run("scope without an active generation returns no rows and says why", func(t *testing.T) {
		for _, tc := range []struct {
			scope      string
			wantReason string
			wantState  querycontract.FreshnessState
			wantCause  querycontract.FreshnessCause
		}{
			{docFactsScopeB, querycontract.DocumentationFactEmptyNoActiveGeneration, querycontract.FreshnessUnavailable, querycontract.FreshnessCauseDeadLetteredDomain},
			{docFactsScopeC, querycontract.DocumentationFactEmptyNoActiveGeneration, querycontract.FreshnessBuilding, querycontract.FreshnessCausePendingRepoGeneration},
			{docFactsScopeE, querycontract.DocumentationFactEmptyNoRows, "", ""},
			{"scope:docfacts-missing", querycontract.DocumentationFactEmptyScopeNotFound, "", ""},
		} {
			got, err := reader.DocumentationFacts(ctx, documentationFactFilter{ScopeID: tc.scope, Limit: 10})
			if err != nil {
				t.Fatalf("%s: %v", tc.scope, err)
			}
			if len(got.Facts) != 0 {
				t.Fatalf("%s returned %d rows, want none (never a latest-generation fallback)", tc.scope, len(got.Facts))
			}
			if got.EmptyReason != tc.wantReason || got.Freshness.State != tc.wantState || got.Freshness.Cause != tc.wantCause {
				t.Fatalf("%s: reason=%q freshness=%#v, want reason=%q state=%q cause=%q",
					tc.scope, got.EmptyReason, got.Freshness, tc.wantReason, tc.wantState, tc.wantCause)
			}
		}
	})

	t.Run("explicit generation is labelled from scope_generations", func(t *testing.T) {
		for _, tc := range []struct {
			generation string
			wantState  querycontract.FreshnessState
			wantActive bool
		}{
			{docFactsGenNew, "", true},
			{docFactsGenOld, querycontract.FreshnessStale, false},
			{docFactsGenC, querycontract.FreshnessBuilding, false},
			{"generation:docfacts-pruned", querycontract.FreshnessUnavailable, false},
		} {
			got, err := reader.DocumentationFacts(ctx, documentationFactFilter{
				ScopeID: docFactsScopeA, GenerationID: tc.generation, Limit: 5,
			})
			if err != nil {
				t.Fatalf("%s: %v", tc.generation, err)
			}
			if tc.generation == docFactsGenC {
				// Belongs to another scope: the read must not label it for scope A.
				if got.Freshness.State != querycontract.FreshnessUnavailable {
					t.Fatalf("cross-scope generation labelled %q, want unavailable", got.Freshness.State)
				}
				continue
			}
			if got.Freshness.State != tc.wantState || got.Binding.IsActive != tc.wantActive {
				t.Fatalf("%s: freshness=%q is_active=%v, want %q/%v", tc.generation, got.Freshness.State, got.Binding.IsActive, tc.wantState, tc.wantActive)
			}
		}
	})
}

func documentationFactIDs(t *testing.T, reader *ContentReader, filter documentationFactFilter) []string {
	t.Helper()
	got, err := reader.DocumentationFacts(t.Context(), filter)
	if err != nil {
		t.Fatalf("DocumentationFacts(%+v) error = %v", filter, err)
	}
	ids := make([]string, 0, len(got.Facts))
	for _, fact := range got.Facts {
		ids = append(ids, fact["fact_id"].(string))
	}
	return ids
}

func assertDocumentationFactIDs(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fact ids differ from the reference statement\n got  (%d): %v\n want (%d): %v", len(got), got, len(want), want)
	}
}

func sameDocumentationIDSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, id := range want {
		if !seen[id] {
			return false
		}
	}
	return true
}

// documentationReferenceFactIDs is the hand-written reference: today's
// statement restricted to one generation, in the total order the handler uses.
func documentationReferenceFactIDs(
	t *testing.T, ctx context.Context, db *sql.DB, scopeID, generationID, factKind string,
) []string {
	t.Helper()
	kinds := "fact_kind IN (" + documentationCollectedFactKindSQLList() + ")"
	args := []any{scopeID, generationID}
	if factKind != "" {
		kinds = "fact_kind = $3"
		args = append(args, factKind)
	}
	return queryDocumentationIDs(t, ctx, db, `
SELECT fact_id FROM fact_records
WHERE is_tombstone = FALSE AND `+kinds+` AND scope_id = $1 AND generation_id = $2
ORDER BY observed_at DESC, fact_id DESC`, args...)
}

// documentationReferenceFactIDsBySource is the reference for an anchor-only
// read: the source_id payload match restricted to each scope's active
// generation, written with an explicit join.
func documentationReferenceFactIDsBySource(t *testing.T, ctx context.Context, db *sql.DB, sourceID string) []string {
	t.Helper()
	return queryDocumentationIDs(t, ctx, db, `
SELECT f.fact_id FROM fact_records f
JOIN scope_generations g ON g.generation_id = f.generation_id AND g.status = 'active'
WHERE f.is_tombstone = FALSE AND f.fact_kind IN (`+documentationCollectedFactKindSQLList()+`)
  AND f.payload->>'source_id' = $1
ORDER BY f.observed_at DESC, f.fact_id DESC`, sourceID)
}

// documentationFactPayloadIDs runs a production-shaped facts statement and
// returns the fact_id of each returned payload, in row order.
func documentationFactPayloadIDs(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("facts statement: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		payload, err := scanJSONPayload(rows)
		if err != nil {
			t.Fatalf("scan payload: %v", err)
		}
		ids = append(ids, payload["fact_id"].(string))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("facts rows: %v", err)
	}
	return ids
}

func queryDocumentationIDs(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("reference query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan reference id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reference rows: %v", err)
	}
	return ids
}

type docFactsSeedScope struct {
	scopeID, generationID, generationStatus, scopeStatus string
	active                                               bool
	facts                                                bool
}

// seedDocumentationFactsGenerationRowSet seeds the small two-generation
// fixture. Scope A holds a superseded and an active generation whose facts tie
// on observed_at, so the fact_id tiebreak decides the order. B, C, and E cover
// the no-active-generation states; D is a second active scope.
func seedDocumentationFactsGenerationRowSet(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, s := range []docFactsSeedScope{
		{docFactsScopeA, docFactsGenOld, "superseded", "active", false, true},
		{docFactsScopeA, docFactsGenNew, "active", "active", true, true},
		{docFactsScopeD, docFactsGenD, "active", "active", true, true},
		{docFactsScopeB, docFactsGenB, "failed", "failed", false, true},
		{docFactsScopeC, docFactsGenC, "pending", "pending", false, true},
		{docFactsScopeE, docFactsGenE, "active", "active", true, false},
	} {
		seedDocumentationFactsScopeGeneration(t, ctx, db, s)
	}
}

func seedDocumentationFactsScopeGeneration(t *testing.T, ctx context.Context, db *sql.DB, s docFactsSeedScope) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', 'proof', $1, 'proof', 'proof', clock_timestamp(), clock_timestamp(), $2,
  jsonb_build_object('repo', $1::text))
ON CONFLICT (scope_id) DO NOTHING`, s.scopeID, s.scopeStatus); err != nil {
		t.Fatalf("seed scope %s: %v", s.scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES ($1, $2, 'proof', clock_timestamp(), clock_timestamp(), $3,
  CASE WHEN $3 IN ('active', 'superseded') THEN clock_timestamp() END)`,
		s.generationID, s.scopeID, s.generationStatus); err != nil {
		t.Fatalf("seed generation %s: %v", s.generationID, err)
	}
	if s.active {
		if _, err := db.ExecContext(ctx,
			`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
			s.generationID, s.scopeID); err != nil {
			t.Fatalf("activate %s: %v", s.generationID, err)
		}
	}
	if !s.facts {
		return
	}
	// A source fact plus documentation documents and sections whose observed_at
	// collides in groups of five.
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  'fact:' || $1::text || ':' || CASE WHEN n = 0 THEN 'source' ELSE lpad(n::text, 4, '0') END,
  $2, $1,
  CASE WHEN n = 0 THEN 'documentation_source'
       WHEN n %% 2 = 0 THEN 'documentation_section' ELSE 'documentation_document' END,
  'stable:' || $1::text || ':' || n,
  'proof', 'proof', 'key:' || $1::text || ':' || n,
  TIMESTAMPTZ '2026-09-01 00:00:00+00' + ((n / 5) * interval '1 second'),
  clock_timestamp(),
  jsonb_build_object('source_id', $3::text, 'document_id', 'doc:' || n)
FROM generate_series(0, %d) AS n`, docFactsRowsPerG-1),
		s.generationID, s.scopeID, docFactsSourceIDFor(s.scopeID)); err != nil {
		t.Fatalf("seed facts %s: %v", s.generationID, err)
	}
}

// docFactsSourceIDFor gives scope A's generations and scope B the same source
// id, so an anchor-only read on it must cross scopes and generations and still
// return only the active rows.
func docFactsSourceIDFor(scopeID string) string {
	if scopeID == docFactsScopeA || scopeID == docFactsScopeB {
		return docFactsSourceA
	}
	return "doc-source:" + scopeID
}
