// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/store"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestAdminWorkItemListingExposesPriorFailureAndNote is the #7385 live proof
// (set ESHU_POSTGRES_DSN to a disposable Postgres; the test uses its own
// schema). Rows carry the failure_details the #7320 supersede fold and the
// operator-note statements write; the listing exposes the note and the prior
// failure's status, class, message and updated_at, never its details text, and
// leaves both empty for a row whose details are free text.
func TestAdminWorkItemListingExposesPriorFailureAndNote(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to a disposable Postgres to run the #7385 listing proof")
	}
	ctx := context.Background()
	root, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	schema := fmt.Sprintf("prior_failure_listing_%d", time.Now().UnixNano())
	if _, err := root.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = root.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = root.Close()
	})
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	database, err := sql.Open("pgx", dsn+sep+"search_path="+schema)
	if err != nil {
		t.Fatalf("open proof pool: %v", err)
	}
	database.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = database.Close() })
	if err := pgstatus.ApplyBootstrapWithoutContentSearchIndexes(ctx, pgstatus.SQLDB{DB: database}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	if _, err := database.Exec(`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
VALUES ('list-scope', 'repository', 'git', 'list-repo', 'git', 'list-scope', now(), now(), 'active');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('list-gen', 'list-scope', 'push', now(), now(), 'active')`); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	seed := func(id, status string, details any) {
		if _, err := database.Exec(`
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
                             failure_class, failure_message, failure_details, payload, created_at, updated_at)
VALUES ($1, 'list-scope', 'list-gen', 'projector', 'source_local', $2, 3, 'projector_superseded_by_newer_generation',
        'projector work superseded', $3, '{}'::jsonb, now(), now())`, id, status, details); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	prior := `"prior_failure":{"status":"dead_letter","failure_class":"graph_write_timeout","failure_message":"timed out",` +
		`"failure_details":"phase=semantic rows=500","updated_at":"2026-06-09T09:00:00.25+00:00"}`
	seed("superseded-row", "superseded", `{"scope_id":"list-scope",`+prior+`}`)
	seed("noted-row", "dead_letter", `{"operator_note":"triaged by hand",`+prior+`}`)
	seed("free-text-row", "dead_letter", "phase=semantic rows=500")
	seed("null-details-row", "dead_letter", nil)

	items, err := store.NewStore(database).ListWorkItems(ctx, admin.WorkItemFilter{ScopeID: "list-scope", Limit: 50})
	if err != nil {
		t.Fatalf("ListWorkItems(): %v", err)
	}
	byID := map[string]admin.WorkItem{}
	for _, item := range items {
		byID[item.WorkItemID] = item
	}
	wantPrior := admin.PriorFailure{Status: "dead_letter", FailureClass: "graph_write_timeout", FailureMessage: "timed out", UpdatedAt: "2026-06-09T09:00:00Z"}
	if p := byID["superseded-row"].PriorFailure; p == nil || *p != wantPrior || byID["superseded-row"].OperatorNote != nil {
		t.Fatalf("superseded-row = %+v, want prior_failure %+v and no note", byID["superseded-row"], wantPrior)
	}
	noted := byID["noted-row"]
	if p := noted.PriorFailure; p == nil || *p != wantPrior || noted.OperatorNote == nil || *noted.OperatorNote != "triaged by hand" {
		t.Fatalf("noted-row = %+v, want prior_failure %+v and the note", noted, wantPrior)
	}
	for _, id := range []string{"free-text-row", "null-details-row"} {
		if item := byID[id]; item.PriorFailure != nil || item.OperatorNote != nil {
			t.Fatalf("%s = %+v, want neither field", id, item)
		}
	}
}
