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
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Live admin-handler proofs. These tests seed a real Postgres and drive the
// moved handlers through specAdminMux (see openapi_test.go); they skip
// without ESHU_*_LIVE=1 and ESHU_POSTGRES_DSN, exactly as they did in the
// query root. They live in the external admin_test package because an
// internal package-admin test cannot import admin/store (import cycle), and
// the live seeding needs the real store constructor.

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

	h := &admin.Handler{Store: store.NewStore(db)}
	mux := specAdminMux(h)
	w := specPostJSON(mux, "/api/v0/admin/dead-letters/query", map[string]any{
		"failure_class":  "projection_bug",
		"collector_kind": "git",
		"limit":          10,
		"timeout_ms":     5000,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	got := specDecodeBody(t, w)
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

func TestAdminHandler_InputInvalidFactsQueryLiveRepositoryScopedGrant(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres repository-scoped grant proof")
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
	for _, migration := range []string{"ingestion_scopes", "scope_generations", "reducer_input_invalid_facts"} {
		if _, err := db.ExecContext(ctx, pgstatus.MigrationSQL(migration)); err != nil {
			t.Fatalf("apply %s migration: %v", migration, err)
		}
	}

	suffix := time.Now().UTC().UnixNano()
	repoID := fmt.Sprintf("repo-4630-live-%d", suffix)
	scopeID := fmt.Sprintf("scope-4630-live-%d", suffix)
	generationID := fmt.Sprintf("generation-4630-live-%d", suffix)
	factID := fmt.Sprintf("fact-4630-live-%d", suffix)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, "DELETE FROM ingestion_scopes WHERE scope_id = $1", scopeID)
	})

	now := time.Now().UTC().Truncate(time.Second)
	// scope_id is a distinct identifier from repo_id/source_key on purpose:
	// this is exactly the shape a repository-scoped token's grant (the
	// repository identifier) cannot match against the raw scope_id, so the
	// pre-fix handler pre-check always rejected it.
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', 'git', $2, 'git', $2, $3, $3, 'active', '{}'::jsonb)
`, scopeID, repoID, now); err != nil {
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
INSERT INTO reducer_input_invalid_facts (
    fact_id, fact_kind, missing_field, failure_class, domain, scope_id, generation_id, decided_at
) VALUES ($1, 'aws_resource', 'account_id', 'input_invalid', 'aws_resource_materialization', $2, $3, $4)
`, factID, scopeID, generationID, now); err != nil {
		t.Fatalf("insert quarantine row: %v", err)
	}

	h := &admin.Handler{Store: store.NewStore(db)}
	mux := specAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/input-invalid-facts/query", strings.NewReader(fmt.Sprintf(`{
		"scope_id": %q,
		"generation_id": %q,
		"limit": 10,
		"timeout_ms": 5000
	}`, scopeID, generationID)))
	req.Header.Set("Content-Type", "application/json")
	// Grant ONLY the repository identifier — never the raw scope_id — the
	// exact shape of a repository-scoped token reading its own repo's
	// quarantine rows.
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		TenantID:             "tenant-4630-live",
		WorkspaceID:          "workspace-4630-live",
		AllowedRepositoryIDs: []string{repoID},
	}))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	got := specDecodeBody(t, w)
	if got["count"].(float64) != 1 || got["truncated"] != false {
		t.Fatalf("response = %#v, want one untruncated row (repository grant must authorize this scope_id via ingestion_scopes.source_key)", got)
	}
	item := got["items"].([]any)[0].(map[string]any)
	if item["fact_id"] != factID || item["scope_id"] != scopeID {
		t.Fatalf("item = %#v, want seeded fact_id/scope_id", item)
	}
}
