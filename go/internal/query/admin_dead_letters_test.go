// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

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

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestAdminHandler_DeadLettersQueryRequiresLimitAndTimeout(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	for _, body := range []map[string]any{
		{"timeout_ms": 5000},
		{"limit": 10},
	} {
		w := postJSON(mux, "/api/v0/admin/dead-letters/query", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
		}
	}
}

func TestAdminHandler_DeadLettersQueryEmpty(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/dead-letters/query", map[string]any{
		"limit":      10,
		"timeout_ms": 5000,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	got := decodeBody(t, w)
	if got["schema_version"] != "eshu.admin.dead_letters.v1" {
		t.Fatalf("schema_version = %v, want eshu.admin.dead_letters.v1", got["schema_version"])
	}
	if got["truncated"] != false {
		t.Fatalf("truncated = %v, want false", got["truncated"])
	}
	if got["count"].(float64) != 0 {
		t.Fatalf("count = %v, want 0", got["count"])
	}
}

func TestAdminHandler_DeadLettersQueryFiltersAndTruncates(t *testing.T) {
	updatedAfter := "2026-07-06T13:00:00Z"
	updatedBefore := "2026-07-06T14:00:00Z"
	now := time.Date(2026, 7, 6, 13, 30, 0, 0, time.UTC)
	store := &stubAdminStore{
		deadLetterRows: []AdminDeadLetterWorkItem{
			{
				WorkItemID:    "wi-1",
				ScopeID:       "scope-a",
				GenerationID:  "gen-a",
				Stage:         "reducer",
				Domain:        "runtime",
				CollectorKind: "git",
				FailureClass:  strPtr("projection_bug"),
				AttemptCount:  3,
				CreatedAt:     now.Add(-time.Hour),
				UpdatedAt:     now,
			},
			{WorkItemID: "wi-2", UpdatedAt: now.Add(-time.Minute)},
			{WorkItemID: "wi-3", UpdatedAt: now.Add(-2 * time.Minute)},
		},
	}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/dead-letters/query", map[string]any{
		"failure_class":  " projection_bug ",
		"domain":         " runtime ",
		"scope_id":       " scope-a ",
		"collector_kind": " git ",
		"updated_after":  updatedAfter,
		"updated_before": updatedBefore,
		"limit":          2,
		"timeout_ms":     7500,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got, want := store.deadLetterFilter.Limit, 3; got != want {
		t.Fatalf("store limit = %d, want %d for limit+1 truncation probe", got, want)
	}
	if got, want := store.deadLetterFilter.FailureClass, "projection_bug"; got != want {
		t.Fatalf("failure_class filter = %q, want %q", got, want)
	}
	if got, want := store.deadLetterFilter.Domain, "runtime"; got != want {
		t.Fatalf("domain filter = %q, want %q", got, want)
	}
	if got, want := store.deadLetterFilter.ScopeID, "scope-a"; got != want {
		t.Fatalf("scope_id filter = %q, want %q", got, want)
	}
	if got, want := store.deadLetterFilter.CollectorKind, "git"; got != want {
		t.Fatalf("collector_kind filter = %q, want %q", got, want)
	}
	if store.deadLetterFilter.UpdatedAfter == nil || store.deadLetterFilter.UpdatedAfter.Format(time.RFC3339) != updatedAfter {
		t.Fatalf("updated_after filter = %v, want %s", store.deadLetterFilter.UpdatedAfter, updatedAfter)
	}
	if store.deadLetterFilter.UpdatedBefore == nil || store.deadLetterFilter.UpdatedBefore.Format(time.RFC3339) != updatedBefore {
		t.Fatalf("updated_before filter = %v, want %s", store.deadLetterFilter.UpdatedBefore, updatedBefore)
	}
	if got, want := store.deadLetterFilter.Timeout, 7500*time.Millisecond; got != want {
		t.Fatalf("timeout = %s, want %s", got, want)
	}

	got := decodeBody(t, w)
	if got["truncated"] != true {
		t.Fatalf("truncated = %v, want true", got["truncated"])
	}
	if got["count"].(float64) != 2 {
		t.Fatalf("count = %v, want 2", got["count"])
	}
	items := got["items"].([]any)
	first := items[0].(map[string]any)
	if first["work_item_id"] != "wi-1" || first["collector_kind"] != "git" {
		t.Fatalf("first item = %#v, want dead-letter wi-1 with collector_kind", first)
	}
	if _, ok := first["failure_message"]; ok {
		t.Fatalf("dead-letter list leaked failure_message: %#v", first)
	}
}

func TestAdminHandler_DeadLettersQueryScopedGrants(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/dead-letters/query", strings.NewReader(`{
		"limit": 10,
		"timeout_ms": 5000
	}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	}))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got, want := store.deadLetterFilter.AllowedRepositoryIDs, []string{"repo-a"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := store.deadLetterFilter.AllowedScopeIDs, []string{"scope-a"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedScopeIDs = %#v, want %#v", got, want)
	}
}

func TestAdminHandler_DeadLettersQueryScopedEmptyGrantSkipsStore(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/dead-letters/query", strings.NewReader(`{
		"limit": 10,
		"timeout_ms": 5000
	}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if store.deadLetterCalls != 0 {
		t.Fatalf("dead-letter store calls = %d, want 0 for scoped token with no grants", store.deadLetterCalls)
	}
	got := decodeBody(t, w)
	if got["count"].(float64) != 0 || got["truncated"] != false {
		t.Fatalf("response = %#v, want empty untruncated page", got)
	}
}

func TestScopedRouteAllowsDeadLettersQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/dead-letters/query", nil)
	if !scopedHTTPRouteSupportsTenantFilter(req) {
		t.Fatal("scopedHTTPRouteSupportsTenantFilter(dead-letters query) = false, want true")
	}
}

func TestAdminHandler_DeadLettersQueryLiveSeededRow(t *testing.T) {
	if os.Getenv("ESHU_DEAD_LETTER_LIST_LIVE") != "1" {
		t.Skip("set ESHU_DEAD_LETTER_LIST_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	for _, migration := range []string{"ingestion_scopes", "scope_generations", "fact_work_items"} {
		if _, err := db.ExecContext(ctx, pgstatus.MigrationSQL(migration)); err != nil {
			t.Fatalf("apply %s migration: %v", migration, err)
		}
	}

	suffix := time.Now().UTC().UnixNano()
	scopeID := fmt.Sprintf("scope-dead-letter-live-%d", suffix)
	generationID := fmt.Sprintf("generation-dead-letter-live-%d", suffix)
	workItemID := fmt.Sprintf("work-dead-letter-live-%d", suffix)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM ingestion_scopes WHERE scope_id = $1", scopeID)
	})
	if _, err := db.ExecContext(ctx, "DELETE FROM ingestion_scopes WHERE scope_id = $1", scopeID); err != nil {
		t.Fatalf("pre-clean scope: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', 'git', 'repo-dead-letter-live', 'git',
    'repo-dead-letter-live', $2, $2, 'active', '{}'::jsonb)
`, scopeID, now); err != nil {
		t.Fatalf("insert scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES ($1, $2, 'test', $3, $3, 'active', '{}'::jsonb)
`, generationID, scopeID, now); err != nil {
		t.Fatalf("insert generation: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, failure_class, failure_message, created_at, updated_at, payload
) VALUES ($1, $2, $3, 'reducer', 'runtime', 'dead_letter',
    2, 'projection_bug', 'live proof synthetic failure', $4, $4, '{}'::jsonb)
`, workItemID, scopeID, generationID, now); err != nil {
		t.Fatalf("insert work item: %v", err)
	}

	h := &AdminHandler{Store: NewPostgresAdminStore(db)}
	mux := newAdminMux(h)
	w := postJSON(mux, "/api/v0/admin/dead-letters/query", map[string]any{
		"failure_class":  "projection_bug",
		"collector_kind": "git",
		"limit":          10,
		"timeout_ms":     5000,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["count"].(float64) != 1 || got["truncated"] != false {
		t.Fatalf("response = %#v, want one untruncated row", got)
	}
	item := got["items"].([]any)[0].(map[string]any)
	if item["work_item_id"] != workItemID || item["scope_id"] != scopeID {
		t.Fatalf("item = %#v, want seeded work_item_id/scope_id", item)
	}
	if item["failure_class"] != "projection_bug" || item["collector_kind"] != "git" {
		t.Fatalf("item = %#v, want projection_bug/git", item)
	}
}
