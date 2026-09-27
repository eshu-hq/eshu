// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/store"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Live proof for the changed-since poisoned/retrying link read (#7290): the
// writer's own ledger (go/internal/storage/postgres/freshness/links) is
// seeded directly (INSERT), not through the writer, because the writer ships
// dark (ESHU_CHANGED_SINCE_LINK_ENABLED default off, #7127 ruling 8.10 item
// 10) and its shape (poisoned_activation_seq set, or attempt_count > 0 with
// it NULL) is fully described by migration 136's columns. This mirrors
// live_test.go's direct-INSERT style for the sibling dead-letter and
// input-invalid-facts proofs. Skips without ESHU_POSTGRES_DSN.

// openChangedSinceLiveDB opens db, applies the ingestion_scopes and
// changed-since ledger migrations, and returns it plus a bounded context.
// Skips the test when ESHU_POSTGRES_DSN is unset.
func openChangedSinceLiveDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the changed-since poisoned-links live proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(4)
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	for _, migration := range []string{"ingestion_scopes", "changed_since_link_ledger"} {
		if _, err := db.ExecContext(ctx, pgstatus.MigrationSQL(migration)); err != nil {
			t.Fatalf("apply %s migration: %v", migration, err)
		}
	}
	return db, ctx
}

// seedChangedSinceScope inserts one ingestion_scopes row belonging to repoID
// and one changed_since_scope_cursor row in one of three states:
// poisoned (poisonedSeq > 0), retrying (attemptSeq > 0, poisonedSeq == 0), or
// healthy (both zero).
func seedChangedSinceScope(
	t *testing.T, db *sql.DB, ctx context.Context,
	scopeID, repoID string, poisonedSeq, attemptSeq int64, attemptCount int, failureClass string,
) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', 'git', $2, 'git', $2, $3, $3, 'active', '{}'::jsonb)
`, scopeID, repoID, now); err != nil {
		t.Fatalf("insert scope %s: %v", scopeID, err)
	}

	var poisonedActivationSeq, attemptActivationSeq any
	var poisonedAt, nextAttemptAt any
	var lastFailureClass any
	if poisonedSeq > 0 {
		poisonedActivationSeq = poisonedSeq
		poisonedAt = now
		lastFailureClass = failureClass
	} else if attemptSeq > 0 {
		attemptActivationSeq = attemptSeq
		nextAttemptAt = now.Add(30 * time.Second)
		lastFailureClass = failureClass
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO changed_since_scope_cursor (
    scope_id, state_generation_id, state_activation_seq, digest_version, updated_at,
    attempt_activation_seq, attempt_count, next_attempt_at, last_failure_class,
    poisoned_activation_seq, poisoned_at
) VALUES ($1, NULL, 0, 1, $2, $3, $4, $5, $6, $7, $8)
`, scopeID, now, attemptActivationSeq, attemptCount, nextAttemptAt, lastFailureClass, poisonedActivationSeq, poisonedAt); err != nil {
		t.Fatalf("insert cursor %s: %v", scopeID, err)
	}
}

// changedSincePoisonedLinksItems extracts the items array of a decoded
// response as a slice of maps, for order/content assertions.
func changedSincePoisonedLinksItems(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("body has no items array: %#v", body)
	}
	items := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		item, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("item is not an object: %#v", entry)
		}
		items = append(items, item)
	}
	return items
}

// TestAdminHandler_ChangedSincePoisonedLinksQueryLiveFiltersAndPagination
// seeds five poisoned/retrying scopes across two repositories plus one
// healthy scope, then proves against real Postgres: the healthy scope is
// excluded; status=poisoned and status=retrying each return only their
// class; scope_id filters to one row; limit is enforced and keyset
// pagination over three pages (limit=2) returns every seeded row exactly
// once, in ascending scope_id order, ending untruncated with no next_cursor.
func TestAdminHandler_ChangedSincePoisonedLinksQueryLiveFiltersAndPagination(t *testing.T) {
	db, ctx := openChangedSinceLiveDB(t)
	suffix := time.Now().UTC().UnixNano()
	base := fmt.Sprintf("chg-poison-live-%d", suffix)
	repoA := fmt.Sprintf("repo-chg-poison-live-a-%d", suffix)
	repoB := fmt.Sprintf("repo-chg-poison-live-b-%d", suffix)
	scopeIDs := []string{
		base + "-0001", base + "-0002", base + "-0003", base + "-0004", base + "-0005", base + "-0006",
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, id := range scopeIDs {
			// changed_since_scope_cursor carries no foreign key to
			// ingestion_scopes by design (#7127 ruling 7.3): deleting only
			// ingestion_scopes leaves the seeded cursor row behind.
			_, _ = db.ExecContext(cleanupCtx, "DELETE FROM changed_since_scope_cursor WHERE scope_id = $1", id)
			_, _ = db.ExecContext(cleanupCtx, "DELETE FROM ingestion_scopes WHERE scope_id = $1", id)
		}
	})

	seedChangedSinceScope(t, db, ctx, scopeIDs[0], repoA, 101, 0, 0, "statement_timeout") // poisoned
	seedChangedSinceScope(t, db, ctx, scopeIDs[1], repoA, 0, 201, 2, "sql_error")         // retrying
	seedChangedSinceScope(t, db, ctx, scopeIDs[2], repoB, 102, 0, 0, "connection_lost")   // poisoned
	seedChangedSinceScope(t, db, ctx, scopeIDs[3], repoB, 0, 202, 3, "internal")          // retrying
	seedChangedSinceScope(t, db, ctx, scopeIDs[4], repoA, 103, 0, 0, "sql_error")         // poisoned
	seedChangedSinceScope(t, db, ctx, scopeIDs[5], repoA, 0, 0, 0, "")                    // healthy: excluded

	h := &admin.Handler{Store: store.NewStore(db)}
	mux := testutil.MountAdminHandler(h)

	// Healthy rows excluded: a wide unscoped page returns exactly the five
	// poisoned/retrying rows, in ascending scope_id order, none of them the
	// healthy scope.
	w := testutil.PostJSON(mux, "/api/v0/admin/changed-since/poisoned-links/query", map[string]any{
		"limit":      50,
		"timeout_ms": 5000,
	})
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := testutil.DecodeResponseBody(t, w)
	items := changedSincePoisonedLinksItems(t, got)
	if len(items) != 5 || got["truncated"] != false {
		t.Fatalf("unscoped page = %d items, truncated=%v, want 5 untruncated; body: %s", len(items), got["truncated"], w.Body.String())
	}
	for i, want := range scopeIDs[:5] {
		if items[i]["scope_id"] != want {
			t.Fatalf("item[%d].scope_id = %v, want %s (order = %+v)", i, items[i]["scope_id"], want, items)
		}
	}
	for _, item := range items {
		if item["scope_id"] == scopeIDs[5] {
			t.Fatalf("healthy scope %s leaked into the page: %+v", scopeIDs[5], item)
		}
	}

	// status=poisoned returns only the three poisoned scopes, each reporting
	// its poisoned_activation_seq as activation_seq.
	w = testutil.PostJSON(mux, "/api/v0/admin/changed-since/poisoned-links/query", map[string]any{
		"status": "poisoned", "limit": 50, "timeout_ms": 5000,
	})
	got = testutil.DecodeResponseBody(t, w)
	items = changedSincePoisonedLinksItems(t, got)
	wantPoisoned := map[string]float64{scopeIDs[0]: 101, scopeIDs[2]: 102, scopeIDs[4]: 103}
	if len(items) != len(wantPoisoned) {
		t.Fatalf("status=poisoned returned %d items, want %d: %+v", len(items), len(wantPoisoned), items)
	}
	for _, item := range items {
		wantSeq, ok := wantPoisoned[item["scope_id"].(string)]
		if !ok {
			t.Fatalf("status=poisoned returned unexpected scope %+v", item)
		}
		if item["status"] != "poisoned" || item["activation_seq"] != wantSeq {
			t.Fatalf("poisoned item = %+v, want status=poisoned activation_seq=%v", item, wantSeq)
		}
		if item["poisoned_at"] == nil {
			t.Fatalf("poisoned item missing poisoned_at: %+v", item)
		}
	}

	// status=retrying returns only the two retrying scopes, each reporting
	// its attempt_activation_seq as activation_seq and its attempt_count.
	w = testutil.PostJSON(mux, "/api/v0/admin/changed-since/poisoned-links/query", map[string]any{
		"status": "retrying", "limit": 50, "timeout_ms": 5000,
	})
	got = testutil.DecodeResponseBody(t, w)
	items = changedSincePoisonedLinksItems(t, got)
	wantRetrying := map[string]float64{scopeIDs[1]: 201, scopeIDs[3]: 202}
	if len(items) != len(wantRetrying) {
		t.Fatalf("status=retrying returned %d items, want %d: %+v", len(items), len(wantRetrying), items)
	}
	for _, item := range items {
		wantSeq, ok := wantRetrying[item["scope_id"].(string)]
		if !ok {
			t.Fatalf("status=retrying returned unexpected scope %+v", item)
		}
		if item["status"] != "retrying" || item["activation_seq"] != wantSeq {
			t.Fatalf("retrying item = %+v, want status=retrying activation_seq=%v", item, wantSeq)
		}
		if item["next_attempt_at"] == nil {
			t.Fatalf("retrying item missing next_attempt_at: %+v", item)
		}
	}

	// scope_id filters to exactly one row.
	w = testutil.PostJSON(mux, "/api/v0/admin/changed-since/poisoned-links/query", map[string]any{
		"scope_id": scopeIDs[3], "limit": 50, "timeout_ms": 5000,
	})
	got = testutil.DecodeResponseBody(t, w)
	items = changedSincePoisonedLinksItems(t, got)
	if len(items) != 1 || items[0]["scope_id"] != scopeIDs[3] {
		t.Fatalf("scope_id filter = %+v, want exactly %s", items, scopeIDs[3])
	}

	// Keyset pagination at limit=2 walks every seeded row exactly once, in
	// order, ending untruncated with no next_cursor.
	var walked []string
	cursor := ""
	for page := 0; page < 10; page++ {
		body := map[string]any{"limit": 2, "timeout_ms": 5000}
		if cursor != "" {
			body["cursor"] = cursor
		}
		w = testutil.PostJSON(mux, "/api/v0/admin/changed-since/poisoned-links/query", body)
		got = testutil.DecodeResponseBody(t, w)
		items = changedSincePoisonedLinksItems(t, got)
		if len(items) > 2 {
			t.Fatalf("page %d returned %d items, want <= 2 (limit not enforced)", page, len(items))
		}
		for _, item := range items {
			walked = append(walked, item["scope_id"].(string))
		}
		truncated, _ := got["truncated"].(bool)
		nextCursor, hasNext := got["next_cursor"]
		if !truncated {
			if hasNext {
				t.Fatalf("page %d untruncated but carries next_cursor %v", page, nextCursor)
			}
			break
		}
		if !hasNext || nextCursor == "" {
			t.Fatalf("page %d truncated but missing next_cursor: %+v", page, got)
		}
		cursor = nextCursor.(string)
	}
	if len(walked) != 5 {
		t.Fatalf("pagination walked %d scopes, want 5: %v", len(walked), walked)
	}
	for i, want := range scopeIDs[:5] {
		if walked[i] != want {
			t.Fatalf("pagination order[%d] = %s, want %s (full walk = %v)", i, walked[i], want, walked)
		}
	}
}

// TestAdminHandler_ChangedSincePoisonedLinksQueryLiveRepositoryScopedGrant
// proves the SQL join-based authorization: a scoped token granted only
// repository A's identifier sees repository A's poisoned/retrying rows and
// none of repository B's, mirroring
// TestAdminHandler_InputInvalidFactsQueryLiveRepositoryScopedGrant for the
// sibling admin read.
func TestAdminHandler_ChangedSincePoisonedLinksQueryLiveRepositoryScopedGrant(t *testing.T) {
	db, ctx := openChangedSinceLiveDB(t)
	suffix := time.Now().UTC().UnixNano()
	base := fmt.Sprintf("chg-poison-scoped-live-%d", suffix)
	repoA := fmt.Sprintf("repo-chg-poison-scoped-a-%d", suffix)
	repoB := fmt.Sprintf("repo-chg-poison-scoped-b-%d", suffix)
	scopeA := base + "-a"
	scopeB := base + "-b"
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, id := range []string{scopeA, scopeB} {
			_, _ = db.ExecContext(cleanupCtx, "DELETE FROM changed_since_scope_cursor WHERE scope_id = $1", id)
			_, _ = db.ExecContext(cleanupCtx, "DELETE FROM ingestion_scopes WHERE scope_id = $1", id)
		}
	})

	seedChangedSinceScope(t, db, ctx, scopeA, repoA, 501, 0, 0, "statement_timeout") // granted repo, poisoned
	seedChangedSinceScope(t, db, ctx, scopeB, repoB, 0, 601, 1, "sql_error")         // ungranted repo, retrying

	h := &admin.Handler{Store: store.NewStore(db)}
	mux := testutil.MountAdminHandler(h)
	httpReq := httptest.NewRequest(http.MethodPost, "/api/v0/admin/changed-since/poisoned-links/query", strings.NewReader(`{
		"limit": 50,
		"timeout_ms": 5000
	}`))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq = httpReq.WithContext(auth.ContextWithAuthContext(httpReq.Context(), testutil.ScopedTestAuthContext("tenant-chg-poison-scoped-live", []string{repoA})))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httpReq)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	got := testutil.DecodeResponseBody(t, rec)
	items := changedSincePoisonedLinksItems(t, got)
	if len(items) != 1 || items[0]["scope_id"] != scopeA {
		t.Fatalf("repository-scoped grant returned %+v, want exactly scope %s", items, scopeA)
	}
	if items[0]["status"] != "poisoned" {
		t.Fatalf("granted scope item = %+v, want status=poisoned", items[0])
	}
}
