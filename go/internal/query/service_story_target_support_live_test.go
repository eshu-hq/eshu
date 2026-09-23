// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// TestServiceStoryTargetSupportSQLSemanticsLive is the committed #6794 parity
// proof for the story target-support rewrite (review finding F-08). It runs the
// shipped statements against a seeded Postgres fixture and checks rows derived
// by hand from the fixture intent, so it pins the contract the pre-change
// JOIN-plus-ANY statements had:
//
//   - only facts of the requested kinds on each scope's own active generation
//     count; superseded, pending, tombstoned, other-kind, and foreign-pointer
//     facts are excluded;
//   - the target read orders by observed_at DESC then fact_id DESC, so a tie on
//     observed_at is broken deterministically;
//   - a kind listed twice counts its facts once (fact_kind = ANY semantics);
//   - the source-only rollup keeps its pre-existing three-valued-logic
//     behavior: a fact missing a ref key is not counted (tracked in #6807).
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestServiceStoryTargetSupportSQLSemanticsLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #6794 story target-support semantics proof")
	}
	ctx := context.Background()
	conn := openStorySupportSchema(ctx, t, dsn)
	seedStorySupportFixture(ctx, t, conn)

	targetSQL, targetArgs := buildServiceStoryTargetSupportSQL(serviceStoryTargetSupportFilter{Repository: "repo-x", Limit: 20})
	rows, err := conn.QueryContext(ctx, targetSQL, targetArgs...)
	if err != nil {
		t.Fatalf("target support query: %v", err)
	}
	var gotIDs []string
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatalf("scan target row: %v", err)
		}
		start := strings.Index(payload, `"fact_id": "`) + len(`"fact_id": "`)
		gotIDs = append(gotIDs, payload[start:start+strings.Index(payload[start:], `"`)])
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate target rows: %v", err)
	}
	_ = rows.Close()
	if got, want := strings.Join(gotIDs, ","), "f-incident,f-transition,f-record"; got != want {
		t.Fatalf("target support facts = %s, want %s", got, want)
	}

	// A kind listed twice must not double-count its facts.
	kinds := []string{"work_item.record", "work_item.record", "incident_routing.coverage_warning"}
	sourceSQL, _ := buildServiceStoryTargetSupportSourceOnlySQL(kinds)
	var total, workItems, incidents int64
	if err := conn.QueryRowContext(ctx, sourceSQL, array.Array(kinds)).Scan(&total, &workItems, &incidents); err != nil {
		t.Fatalf("source-only query: %v", err)
	}
	if got, want := fmt.Sprintf("%d|%d|%d", total, workItems, incidents), "2|1|1"; got != want {
		t.Fatalf("source-only counts = %s, want %s", got, want)
	}
}

func openStorySupportSchema(ctx context.Context, t *testing.T, dsn string) *sql.Conn {
	t.Helper()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open postgres connection: %v", err)
	}
	schema := fmt.Sprintf("story_support_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = conn.Close()
	})
	for _, stmt := range []string{"CREATE SCHEMA " + schema, "SET search_path TO " + schema + ", public"} {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	for _, def := range storagepostgres.BootstrapDefinitions() {
		if _, err := conn.ExecContext(ctx, def.SQL); err != nil {
			t.Fatalf("apply migration %s: %v", def.Name, err)
		}
	}
	return conn
}

// seedStorySupportFixture seeds one scope per exclusion branch plus the
// target-linked and source-only facts the expected rows name.
func seedStorySupportFixture(ctx context.Context, t *testing.T, conn *sql.Conn) {
	t.Helper()
	base := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, query)
		}
	}
	for _, scope := range []struct{ id, active string }{
		{"s-a", "g-a"},          // active generation with target-linked facts
		{"s-b", "g-b"},          // active generation with exclusions and source-only facts
		{"s-none", ""},          // owns g-none but has no active pointer
		{"s-foreign", "g-none"}, // active pointer names another scope's generation
		{"s-pending", "g-p"},    // active pointer to a generation whose status is pending
	} {
		exec(`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $2, $2, 'active', NULLIF($3, ''), '{}'::jsonb)`, scope.id, base, scope.active)
	}
	for _, gen := range []struct{ id, scope, status string }{
		{"g-a", "s-a", "active"},
		{"g-a-old", "s-a", "superseded"},
		{"g-b", "s-b", "active"},
		{"g-none", "s-none", "active"},
		{"g-p", "s-pending", "pending"},
	} {
		exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ($1, $2, 'snapshot', $3, $3, $4, '{}'::jsonb)`, gen.id, gen.scope, base, gen.status)
	}
	linked := `{"candidate_refs":[{"id":"repo-x","kind":"repository"}]}`
	allEmpty := `{"candidate_refs":[],"evidence_refs":[],"linked_entities":[]}`
	for _, fact := range []struct {
		id, scope, gen, kind, payload string
		observed                      time.Duration
		tombstone                     bool
	}{
		{"f-record", "s-a", "g-a", "work_item.record", linked, -time.Hour, false},
		{"f-transition", "s-a", "g-a", "work_item.transition", `{"evidence_refs":[{"id":"repo-x","kind":"repository"}]}`, -time.Hour, false},
		{"f-incident", "s-b", "g-b", "incident_routing.coverage_warning", `{"linked_entities":[{"entity_id":"repo-x","entity_type":"repository"}]}`, 0, false},
		{"f-superseded", "s-a", "g-a-old", "work_item.record", linked, 0, false},
		{"f-tombstone", "s-b", "g-b", "work_item.record", linked, 0, true},
		{"f-other-kind", "s-b", "g-b", "content_entity", linked, 0, false},
		{"f-other-repo", "s-b", "g-b", "work_item.record", `{"candidate_refs":[{"id":"repo-y","kind":"repository"}]}`, 0, false},
		{"f-foreign", "s-foreign", "g-none", "work_item.record", linked, 0, false},
		{"f-pending", "s-pending", "g-p", "work_item.record", linked, 0, false},
		// Source-only candidates on the active generation of s-b.
		{"src-work", "s-b", "g-b", "work_item.record", allEmpty, 0, false},
		{"src-incident", "s-b", "g-b", "incident_routing.coverage_warning", allEmpty, 0, false},
		{"src-missing-keys", "s-b", "g-b", "work_item.record", `{}`, 0, false},
	} {
		exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, 'jira', $1, $5, $5, $6, $7::jsonb)`,
			fact.id, fact.scope, fact.gen, fact.kind, base.Add(fact.observed), fact.tombstone, fact.payload)
	}
}
