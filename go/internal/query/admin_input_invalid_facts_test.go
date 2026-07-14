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

	metricnoop "go.opentelemetry.io/otel/metric/noop"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestListInputInvalidFactsRequiresScopeGenerationLimitAndTimeout(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	for _, body := range []map[string]any{
		{"generation_id": "gen-a", "limit": 10, "timeout_ms": 5000},
		{"scope_id": "scope-a", "limit": 10, "timeout_ms": 5000},
		{"scope_id": "scope-a", "generation_id": "gen-a", "timeout_ms": 5000},
		{"scope_id": "scope-a", "generation_id": "gen-a", "limit": 10},
	} {
		w := postJSON(mux, "/api/v0/admin/input-invalid-facts/query", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d for body %#v; body: %s", w.Code, http.StatusBadRequest, body, w.Body.String())
		}
	}
}

func TestListInputInvalidFactsEmpty(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/input-invalid-facts/query", map[string]any{
		"scope_id":      "scope-a",
		"generation_id": "gen-a",
		"limit":         10,
		"timeout_ms":    5000,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	got := decodeBody(t, w)
	if got["schema_version"] != "eshu.admin.input_invalid_facts.v1" {
		t.Fatalf("schema_version = %v, want eshu.admin.input_invalid_facts.v1", got["schema_version"])
	}
	if got["truncated"] != false {
		t.Fatalf("truncated = %v, want false", got["truncated"])
	}
	if got["count"].(float64) != 0 {
		t.Fatalf("count = %v, want 0", got["count"])
	}
	if store.inputInvalidFactFilter.ScopeID != "scope-a" || store.inputInvalidFactFilter.GenerationID != "gen-a" {
		t.Fatalf("filter = %#v, want scope-a/gen-a", store.inputInvalidFactFilter)
	}
}

func TestListInputInvalidFactsFiltersAndTruncates(t *testing.T) {
	now := time.Date(2026, 7, 14, 13, 0, 0, 0, time.UTC)
	store := &stubAdminStore{
		inputInvalidFactRows: []AdminReducerInputInvalidFact{
			{
				FactID: "fact-1", FactKind: "aws_resource", MissingField: "account_id",
				FailureClass: "input_invalid", Domain: "aws_resource_materialization",
				ScopeID: "scope-a", GenerationID: "gen-a", DecidedAt: now,
			},
			{FactID: "fact-2", DecidedAt: now.Add(-time.Minute)},
			{FactID: "fact-3", DecidedAt: now.Add(-2 * time.Minute)},
		},
	}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/input-invalid-facts/query", map[string]any{
		"scope_id":      " scope-a ",
		"generation_id": " gen-a ",
		"domain":        " aws_resource_materialization ",
		"fact_kind":     " aws_resource ",
		"limit":         2,
		"timeout_ms":    7500,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got, want := store.inputInvalidFactFilter.Limit, 3; got != want {
		t.Fatalf("store limit = %d, want %d for limit+1 truncation probe", got, want)
	}
	if got, want := store.inputInvalidFactFilter.Domain, "aws_resource_materialization"; got != want {
		t.Fatalf("domain filter = %q, want %q", got, want)
	}
	if got, want := store.inputInvalidFactFilter.FactKind, "aws_resource"; got != want {
		t.Fatalf("fact_kind filter = %q, want %q", got, want)
	}
	if got, want := store.inputInvalidFactFilter.Timeout, 7500*time.Millisecond; got != want {
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
	if first["fact_id"] != "fact-1" || first["missing_field"] != "account_id" {
		t.Fatalf("first item = %#v, want fact-1 with missing_field account_id", first)
	}
}

func TestListInputInvalidFactsScopedGrants(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/input-invalid-facts/query", strings.NewReader(`{
		"scope_id": "scope-a",
		"generation_id": "gen-a",
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
	if store.inputInvalidFactCalls != 1 {
		t.Fatalf("store calls = %d, want 1 for a granted scope_id", store.inputInvalidFactCalls)
	}
	if got, want := store.inputInvalidFactFilter.AllowedRepositoryIDs, []string{"repo-a"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := store.inputInvalidFactFilter.AllowedScopeIDs, []string{"scope-a"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedScopeIDs = %#v, want %#v", got, want)
	}
}

// TestListInputInvalidFactsScopedRepositoryOnlyGrantReachesStore proves the
// codex P2 fix on PR #5252 (issue #4630): a scoped token that requests a
// scope_id NOT in its combined allowed-IDs map must still reach the store
// (and thread AllowedRepositoryIDs/AllowedScopeIDs) rather than being
// rejected by an in-memory pre-check. Authorization for a repository grant
// against a scope_id it does not literally match (because the token grants
// the REPOSITORY identifier, not the raw ingestion scope_id) is delegated to
// the store's SQL join against ingestion_scopes
// (buildListReducerInputInvalidFactsQuery, proven in
// TestBuildListReducerInputInvalidFactsQuery_AuthorizesViaIngestionScopes and
// against real Postgres by TestAdminHandler_InputInvalidFactsQueryLiveRepositoryScopedGrant).
// Before the fix, this scope_id would never reach the store: the handler's
// own allowsRepositoryID(scopeID) pre-check compared scope_id against the
// combined allowed-IDs map directly and rejected it, always returning an
// empty page for a repository-scoped token reading its own quarantine rows.
func TestListInputInvalidFactsScopedRepositoryOnlyGrantReachesStore(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/input-invalid-facts/query", strings.NewReader(`{
		"scope_id": "scope-b",
		"generation_id": "gen-a",
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
	if store.inputInvalidFactCalls != 1 {
		t.Fatalf("store calls = %d, want 1: authorization is delegated to the store's SQL join, not an in-memory pre-check", store.inputInvalidFactCalls)
	}
	if got, want := store.inputInvalidFactFilter.ScopeID, "scope-b"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := store.inputInvalidFactFilter.AllowedRepositoryIDs, []string{"repo-a"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := store.inputInvalidFactFilter.AllowedScopeIDs, []string{"scope-a"}; !equalStringSlices(got, want) {
		t.Fatalf("AllowedScopeIDs = %#v, want %#v", got, want)
	}
}

// TestListInputInvalidFactsScopedEmptyGrantSkipsStore proves the ONE
// remaining in-memory short-circuit: a scoped token with NO grants at all
// (repositoryAccessFilter.empty()) still skips the store entirely, mirroring
// AdminHandler.listDeadLetters' access.empty() check.
func TestListInputInvalidFactsScopedEmptyGrantSkipsStore(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/input-invalid-facts/query", strings.NewReader(`{
		"scope_id": "scope-b",
		"generation_id": "gen-a",
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
	if store.inputInvalidFactCalls != 0 {
		t.Fatalf("store calls = %d, want 0 for a scoped token with no grants at all", store.inputInvalidFactCalls)
	}
	got := decodeBody(t, w)
	if got["count"].(float64) != 0 || got["truncated"] != false {
		t.Fatalf("response = %#v, want empty untruncated page", got)
	}
}

func TestListInputInvalidFactsRecordsTelemetry(t *testing.T) {
	meter := metricnoop.NewMeterProvider().Meter("test")
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store, Instruments: instruments}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/input-invalid-facts/query", map[string]any{
		"scope_id":      "scope-a",
		"generation_id": "gen-a",
		"limit":         10,
		"timeout_ms":    5000,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

// TestAdminHandler_InputInvalidFactsQueryLiveRepositoryScopedGrant is the
// real-Postgres proof for the codex P2 fix on PR #5252 (issue #4630): a
// repository-scoped token that grants ONLY the repository identifier
// (ingestion_scopes.source_key), never the raw ingestion scope_id, can read
// its own reducer_input_invalid_facts rows. Before the fix, this request
// always returned an empty page: the handler's in-memory
// access.allowsRepositoryID(scopeID) pre-check compared the requested raw
// scope_id against the combined allowed-IDs map, which contains repository
// identifiers, not scope ids, so it could never match and the store was
// never even called.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/query -run TestAdminHandler_InputInvalidFactsQueryLiveRepositoryScopedGrant -count=1
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

	h := &AdminHandler{Store: NewPostgresAdminStore(db)}
	mux := newAdminMux(h)
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
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-4630-live",
		WorkspaceID:          "workspace-4630-live",
		AllowedRepositoryIDs: []string{repoID},
	}))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["count"].(float64) != 1 || got["truncated"] != false {
		t.Fatalf("response = %#v, want one untruncated row (repository grant must authorize this scope_id via ingestion_scopes.source_key)", got)
	}
	item := got["items"].([]any)[0].(map[string]any)
	if item["fact_id"] != factID || item["scope_id"] != scopeID {
		t.Fatalf("item = %#v, want seeded fact_id/scope_id", item)
	}
}
