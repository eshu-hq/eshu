// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10023 code_call_materialization fails closed on an unusable delta file scope through the real ReducerQueue claim/Fail SQL: the handler's non-retryable self-classified error dead-letters the work item on attempt 1 with failure_class code_call_delta_scope_unusable and a triage disposition that agrees with the retry authority, and the dead-lettered row is never re-claimed; isolated schema with the full production bootstrap, 9 to 42 s across three local runs (the bootstrap dominates); enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorfailure "github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/materialization"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// codeCallDeltaScopeFactLoader serves a delta-generation repository with no
// local path plus one file whose call resolves, so the handler would emit a
// whole-repository refresh and a delta-only per-edge row without the #10023
// fence.
type codeCallDeltaScopeFactLoader struct{}

func (codeCallDeltaScopeFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-a", "source_run_id": "run-a", "graph_id": "repo-a",
			"graph_kind": "repository", "name": "repo-a",
			"delta_generation": true, "delta_relative_paths": []string{"caller.py"},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id": "repo-a", "relative_path": "caller.py",
			"parsed_file_data": map[string]any{
				"path": "caller.py",
				"functions": []any{
					map[string]any{"name": "handle", "line_number": 3, "uid": "entity:handle"},
					map[string]any{"name": "helper", "line_number": 9, "uid": "entity:helper"},
				},
				"function_calls": []any{
					map[string]any{"name": "helper", "full_name": "helper", "lang": "python", "line_number": 4},
				},
			},
		}},
	}, nil
}

// countingCodeCallIntentWriter counts the shared intents the handler tries to
// persist; the fence must leave it at zero.
type countingCodeCallIntentWriter struct{ rows int }

func (w *countingCodeCallIntentWriter) UpsertIntents(_ context.Context, rows []sharedintent.Row) error {
	w.rows += len(rows)
	return nil
}

var codeCallDeltaScopeDispositionPattern = regexp.MustCompile(`disposition=([a-z_]+)`)

// TestCodeCallDeltaScopeUnusableDeadLettersOnFirstAttemptLive is the #10023
// queue proof: the fenced handler error goes through the real ReducerQueue
// Fail SQL and lands as a dead letter on attempt 1, with the error's own
// failure class, and the row is not claimable afterwards.
func TestCodeCallDeltaScopeUnusableDeadLettersOnFirstAttemptLive(t *testing.T) {
	ctx, db := openCodeCallDeltaScopeLiveDB(t)
	clock := time.Now().UTC()
	const scope, gen = "git-repository-scope:acme/app", "gen-delta"
	seedOwnGateScope(t, ctx, db, scope, gen, clock.Add(-time.Hour))
	addOwnGateItem(t, ctx, db, "code-call-1", scope, gen, reducercontract.DomainCodeCallMaterialization, clock.Add(-time.Minute))

	q := newOwnGateQueue(db, &clock, reducercontract.DomainCodeCallMaterialization)
	intent, ok, err := q.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	writer := &countingCodeCallIntentWriter{}
	handler := materialization.Handler{FactLoader: codeCallDeltaScopeFactLoader{}, IntentWriter: writer}
	_, handleErr := handler.Handle(ctx, intent)
	if handleErr == nil {
		t.Fatal("Handle() error = nil, want the unusable delta scope failure")
	}
	if writer.rows != 0 {
		t.Fatalf("handler wrote %d shared intents, want 0", writer.rows)
	}
	if err := q.Fail(ctx, intent, handleErr); err != nil {
		t.Fatalf("fail: %v", err)
	}

	var status, class, message, details string
	var attempts int
	if err := db.QueryRowContext(ctx, `
SELECT status, COALESCE(failure_class, ''), COALESCE(failure_message, ''), COALESCE(failure_details, ''), attempt_count
FROM fact_work_items WHERE work_item_id = 'code-call-1'`).Scan(&status, &class, &message, &details, &attempts); err != nil {
		t.Fatalf("read work item: %v", err)
	}
	t.Logf("status=%s class=%s attempts=%d message=%q details=%q", status, class, attempts, message, details)
	if status != "dead_letter" {
		t.Fatalf("status = %q, want dead_letter on the first attempt", status)
	}
	if class != materialization.DeltaScopeUnusableFailureClass {
		t.Fatalf("failure_class = %q, want %q", class, materialization.DeltaScopeUnusableFailureClass)
	}
	if attempts != 1 {
		t.Fatalf("attempt_count = %d, want 1", attempts)
	}
	if !strings.Contains(message, "repo-a=missing_local_path") {
		t.Fatalf("failure_message = %q, want it to name repo-a and its reason", message)
	}
	match := codeCallDeltaScopeDispositionPattern.FindStringSubmatch(details)
	if match == nil {
		t.Fatalf("failure_details = %q, want a triage disposition", details)
	}
	if projectorfailure.TriageDispositionConflicts(reducercontract.IsRetryable(handleErr), projectorfailure.RetryDisposition(match[1])) {
		t.Fatalf("triage disposition %q conflicts with retryable=%v", match[1], reducercontract.IsRetryable(handleErr))
	}

	clock = clock.Add(24 * time.Hour)
	if _, ok, err := q.Claim(ctx); err != nil || ok {
		t.Fatalf("re-claim after dead letter: ok=%v err=%v, want no claimable work", ok, err)
	}
}

// openCodeCallDeltaScopeLiveDB applies the production bootstrap into an
// isolated schema of ESHU_POSTGRES_TEST_DSN and drops it on cleanup. The
// readiness runner provides ESHU_GENERATION_RETENTION_PROOF_DSN instead, so it
// is bridged onto ESHU_POSTGRES_TEST_DSN the way delta_union_live_test.go does.
func openCodeCallDeltaScopeLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_GENERATION_RETENTION_PROOF_DSN is set without ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #10023 delta scope dead-letter proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres admin connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	postgresproof.InstallTrigramExtension(ctx, t, adminDB)

	schemaName := fmt.Sprintf("code_call_delta_scope_10023_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+array.QuoteIdentifier(schemaName)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = adminDB.ExecContext(cleanupCtx, "DROP SCHEMA "+array.QuoteIdentifier(schemaName)+" CASCADE")
	})

	targetURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse ESHU_POSTGRES_TEST_DSN: %v", err)
	}
	params := targetURL.Query()
	// public stays on the path so the pg_trgm operator classes resolve (#7869).
	params.Set("search_path", schemaName+",public")
	targetURL.RawQuery = params.Encode()
	db, err := sql.Open("pgx", targetURL.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap to isolated schema: %v", err)
	}
	return ctx, db
}
