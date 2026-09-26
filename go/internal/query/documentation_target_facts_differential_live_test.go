// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestDocumentationTargetFactsSplitReadMatchesLegacyLive is the #7126 row
// equivalence proof for the split target-facts read. It runs the pre-change
// single-statement read and the production two-branch read against the same
// disposable PostgreSQL database (real bootstrap schema) and requires the
// ordered fact_id list and every payload to be identical. The fixture covers
// all three read kinds including semantic.documentation_observation rows that
// match and do not match the target, exact observed_at ties within and across
// kinds, tombstones, a fact kind outside both statements, a second scope, the
// ACL join path, and the limit edges (1, below, equal to, and above the match
// count, and the default cap).
func TestDocumentationTargetFactsSplitReadMatchesLegacyLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		4*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	seedTargetFactsScopes(t, ctx, db)
	seedTargetFactsDifferentialFixture(t, ctx, db)

	targets := []string{"repo:alpha", "repo:beta", "repo:delta", "repo:gamma", "repo:semonly", "repo:none"}
	limits := []int{0, 1, 5, 10, 50}
	for _, target := range targets {
		for _, limit := range limits {
			filter := documentationFindingFilter{Repository: target, Limit: limit}
			t.Run(fmt.Sprintf("%s/limit=%d", target, limit), func(t *testing.T) {
				assertTargetFactsMatchLegacy(t, ctx, db, filter)
			})
		}
	}

	variants := map[string]documentationFindingFilter{
		"service_ref":       {ServiceID: "service:alpha", Limit: 10},
		"target_kind_id":    {TargetKind: "repository", TargetID: "repo:alpha", Limit: 10},
		"scope_a":           {Repository: "repo:alpha", ScopeID: targetFactsScopeA, Limit: 10},
		"scope_b":           {Repository: "repo:alpha", ScopeID: targetFactsScopeB, Limit: 10},
		"generation_b":      {Repository: "repo:alpha", GenerationID: targetFactsGenB, Limit: 10},
		"acl_scope_a":       {Repository: "repo:alpha", AllowedScopeIDs: []string{targetFactsScopeA}, Limit: 10},
		"acl_scope_b_only":  {Repository: "repo:alpha", AllowedScopeIDs: []string{targetFactsScopeB}, Limit: 10},
		"acl_repo_id":       {Repository: "repo:beta", AllowedRepositoryIDs: []string{"repo:beta"}, Limit: 3},
		"acl_denied":        {Repository: "repo:beta", AllowedRepositoryIDs: []string{"repo:other"}, Limit: 10},
		"source_id_matches": {Repository: "repo:alpha", SourceID: "source:runbook", Limit: 10},
	}
	for name, filter := range variants {
		t.Run(name, func(t *testing.T) {
			assertTargetFactsMatchLegacy(t, ctx, db, filter)
		})
	}

	// The production read path must also agree with the legacy row set and set
	// truncated exactly when more than limit rows exist.
	reader := NewContentReader(db)
	for _, tc := range []struct {
		target    string
		limit     int
		wantRows  int
		wantTrunc bool
	}{
		{"repo:alpha", 10, 10, true},
		{"repo:beta", 10, 10, false},
		{"repo:delta", 10, 10, true},
		{"repo:gamma", 10, 3, false},
		{"repo:alpha", 1, 1, true},
		{"repo:none", 10, 0, false},
	} {
		got, truncated, err := reader.documentationTargetFacts(ctx, documentationFindingFilter{Repository: tc.target, Limit: tc.limit})
		if err != nil {
			t.Fatalf("documentationTargetFacts(%s, %d): %v", tc.target, tc.limit, err)
		}
		if len(got) != tc.wantRows || truncated != tc.wantTrunc {
			t.Fatalf("documentationTargetFacts(%s, %d) = %d rows truncated=%v, want %d rows truncated=%v",
				tc.target, tc.limit, len(got), truncated, tc.wantRows, tc.wantTrunc)
		}
	}
}

// seedTargetFactsDifferentialFixture inserts matching and non-matching facts
// for every target the differential exercises.
func seedTargetFactsDifferentialFixture(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var rows []targetFactRow
	rows = append(rows, targetFactRows("repo:alpha", targetFactsCounts{Mentions: 6, Claims: 4, Semantics: 5})...)
	// beta: 4 + 5 here, plus the ACL row below, is exactly 10 matches.
	rows = append(rows, targetFactRows("repo:beta", targetFactsCounts{Mentions: 4, Semantics: 5})...)
	rows = append(rows, targetFactRows("repo:delta", targetFactsCounts{Mentions: 6, Semantics: 5})...)
	rows = append(rows, targetFactRows("repo:gamma", targetFactsCounts{Mentions: 3})...)
	rows = append(rows, targetFactRows("repo:semonly", targetFactsCounts{Semantics: 4})...)
	// Non-matching rows of every kind: they must never be returned.
	rows = append(rows, targetFactRows("repo:unrelated", targetFactsCounts{Mentions: 3, Claims: 2, Semantics: 3})...)
	// Tombstoned matching rows of an indexed kind and the semantic kind.
	rows = append(rows,
		targetFactRow{
			ID: "fact:alpha:tomb:mention", Kind: "documentation_entity_mention", Scope: targetFactsScopeA, Gen: targetFactsGenA,
			Offset: 9 * time.Second, Tombstone: true, Payload: refPayload(0, "repository", "repo:alpha"),
		},
		targetFactRow{
			ID: "fact:alpha:tomb:semantic", Kind: "semantic.documentation_observation", Scope: targetFactsScopeA, Gen: targetFactsGenA,
			Offset: 9 * time.Second, Tombstone: true, Payload: refPayload(1, "repository", "repo:alpha"),
		},
		// A fact kind the read never selects, matching the target's refs.
		targetFactRow{
			ID: "fact:alpha:finding", Kind: "documentation_finding", Scope: targetFactsScopeA, Gen: targetFactsGenA,
			Offset: 8 * time.Second, Payload: refPayload(0, "repository", "repo:alpha"),
		},
		// A second scope carrying matching rows of both branches, newest of all.
		targetFactRow{
			ID: "fact:alpha:scopeb:mention", Kind: "documentation_entity_mention", Scope: targetFactsScopeB, Gen: targetFactsGenB,
			Offset: 20 * time.Second, Payload: refPayload(0, "repository", "repo:alpha"),
		},
		targetFactRow{
			ID: "fact:alpha:scopeb:semantic", Kind: "semantic.documentation_observation", Scope: targetFactsScopeB, Gen: targetFactsGenB,
			Offset: 20 * time.Second, Payload: refPayload(1, "repository", "repo:alpha"),
		},
		// Service-ref rows in both branches, plus source_id-carrying rows.
		targetFactRow{
			ID: "fact:svc:mention", Kind: "documentation_entity_mention", Scope: targetFactsScopeA, Gen: targetFactsGenA,
			Offset: 3 * time.Second, Payload: refPayload(0, "service", "service:alpha"),
		},
		targetFactRow{
			ID: "fact:svc:semantic", Kind: "semantic.documentation_observation", Scope: targetFactsScopeA, Gen: targetFactsGenA,
			Offset: 3 * time.Second, Payload: refPayload(1, "service", "service:alpha"),
		},
		targetFactRow{
			ID: "fact:src:mention", Kind: "documentation_entity_mention", Scope: targetFactsScopeA, Gen: targetFactsGenA,
			Offset: 5 * time.Second, Payload: withSource(refPayload(0, "repository", "repo:alpha"), "source:runbook"),
		},
		targetFactRow{
			ID: "fact:src:semantic", Kind: "semantic.documentation_observation", Scope: targetFactsScopeA, Gen: targetFactsGenA,
			Offset: 5 * time.Second, Payload: withSource(refPayload(1, "repository", "repo:alpha"), "source:runbook"),
		},
		// Repository-id carrying rows for the repository ACL predicate.
		targetFactRow{
			ID: "fact:beta:acl:semantic", Kind: "semantic.documentation_observation", Scope: targetFactsScopeA, Gen: targetFactsGenA,
			Offset: 6 * time.Second, Payload: map[string]any{"repository_id": "repo:beta", "candidate_refs": []map[string]string{{"kind": "repository", "id": "repo:beta"}}},
		},
	)
	insertTargetFacts(t, ctx, db, rows)
	if _, err := db.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze: %v", err)
	}
}

func withSource(payload map[string]any, sourceID string) map[string]any {
	payload["source_id"] = sourceID
	return payload
}

// assertTargetFactsMatchLegacy runs both statements and compares the ordered
// result sets, including every payload byte-for-value.
func assertTargetFactsMatchLegacy(t *testing.T, ctx context.Context, db *sql.DB, filter documentationFindingFilter) {
	t.Helper()
	legacySQL, legacyArgs := legacyDocumentationTargetFactsSQL(filter)
	newSQL, newArgs := buildDocumentationTargetFactsSQL(filter)
	want := queryTargetFactPayloads(t, ctx, db, legacySQL, legacyArgs)
	got := queryTargetFactPayloads(t, ctx, db, newSQL, newArgs)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("split read differs from legacy read\n got: %v\nwant: %v", factIDs(got), factIDs(want))
	}
}

func queryTargetFactPayloads(t *testing.T, ctx context.Context, db *sql.DB, query string, args []any) []map[string]any {
	t.Helper()
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query: %v\n%s", err, query)
	}
	defer func() { _ = rows.Close() }()
	out := []map[string]any{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		out = append(out, payload)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func factIDs(payloads []map[string]any) []string {
	ids := make([]string, 0, len(payloads))
	for _, p := range payloads {
		ids = append(ids, fmt.Sprint(p["fact_id"]))
	}
	return ids
}
