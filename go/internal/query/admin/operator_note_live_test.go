// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

import (
	"context"
	"database/sql"
	"encoding/json"
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

// noteRow is a work row as an admin mutation left it.
type noteRow struct {
	status                        string
	class, message, detailsRaw    sql.NullString
	operatorNote, priorStatus     string
	priorClass, priorMessage      sql.NullString
	priorDetails                  sql.NullString
	details                       map[string]any
	hasPrior, detailsIsJSONObject bool
}

func readNoteRow(t *testing.T, database *sql.DB, id string) noteRow {
	t.Helper()
	var r noteRow
	if err := database.QueryRow(`SELECT status, failure_class, failure_message, failure_details
FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&r.status, &r.class, &r.message, &r.detailsRaw); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	if r.detailsRaw.Valid && json.Unmarshal([]byte(r.detailsRaw.String), &r.details) == nil {
		r.detailsIsJSONObject = true
		r.operatorNote, _ = r.details["operator_note"].(string)
		if prior, ok := r.details["prior_failure"].(map[string]any); ok {
			r.hasPrior = true
			r.priorStatus, _ = prior["status"].(string)
			str := func(k string) sql.NullString {
				v, ok := prior[k].(string)
				return sql.NullString{String: v, Valid: ok}
			}
			r.priorClass, r.priorMessage, r.priorDetails = str("failure_class"), str("failure_message"), str("failure_details")
		}
	}
	return r
}

// TestOperatorNoteKeepsTheFailureItReplaces runs the two admin mutations that
// take an operator note against the real schema (set ESHU_POSTGRES_DSN to a
// disposable Postgres; the test uses its own schema). A terminal row that fails
// with triage details keeps them under prior_failure when a note is supplied,
// keeps them untouched when it is not, and Skip treats an empty failure_class
// like a missing one.
func TestOperatorNoteKeepsTheFailureItReplaces(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to a disposable Postgres to run the #7388 operator-note proof")
	}
	ctx := context.Background()
	admin0, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	schema := fmt.Sprintf("operator_note_proof_%d", time.Now().UnixNano())
	if _, err := admin0.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin0.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = admin0.Close()
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

	for _, scope := range []string{"note-scope", "note-scope-2"} {
		if _, err := database.Exec(`
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
VALUES ($1, 'repository', 'git', $1 || '-repo', 'git', $1, now(), now(), 'active')`, scope); err != nil {
			t.Fatalf("seed scope %s: %v", scope, err)
		}
		if _, err := database.Exec(`
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1 || '-gen', $1, 'push', now(), now(), 'active')`, scope); err != nil {
			t.Fatalf("seed generation %s: %v", scope, err)
		}
	}
	const (
		triageDetails = "stage=project_work_item triage=input_invalid class=input_invalid message=fact payload rejected"
		retryDetails  = "dial tcp 10.0.0.4:7687: connection reset by peer"
	)
	seed := func(id, scope, status string, class, message, details any) {
		if _, err := database.Exec(`
INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
                             failure_class, failure_message, failure_details, payload, created_at, updated_at)
VALUES ($1, $2, $2 || '-gen', 'projector', 'source_local', $3, 3, $4, $5, $6, '{}'::jsonb, now(), now())`,
			id, scope, status, class, message, details); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("dl-noted", "note-scope", "failed", "input_invalid", "fact payload rejected", triageDetails)
	seed("dl-plain", "note-scope", "failed", "input_invalid", "fact payload rejected", triageDetails)
	seed("skip-retrying", "note-scope", "retrying", "projection_retryable", "write canonical nodes: connection reset", retryDetails)
	seed("skip-empty-class", "note-scope", "pending", "", nil, nil)
	seed("skip-empty-note", "note-scope-2", "failed", "graph_write_timeout", "timed out", "phase=semantic rows=500")

	adminStore := store.NewStore(database)

	// Dead-letter with a note: the note is the details, the old details ride
	// under prior_failure, and the class and message are untouched.
	if _, err := adminStore.DeadLetterWorkItems(ctx, admin.DeadLetterFilter{WorkItemIDs: []string{"dl-noted"}, OperatorNote: "triaged: bad upstream payload"}); err != nil {
		t.Fatalf("DeadLetterWorkItems(note): %v", err)
	}
	r := readNoteRow(t, database, "dl-noted")
	if r.status != "dead_letter" || r.class.String != "input_invalid" || r.message.String != "fact payload rejected" {
		t.Fatalf("dl-noted = (%s, %q, %q), want (dead_letter, input_invalid, fact payload rejected)", r.status, r.class.String, r.message.String)
	}
	if !r.detailsIsJSONObject || r.operatorNote != "triaged: bad upstream payload" {
		t.Fatalf("dl-noted details = %q, want an object carrying operator_note", r.detailsRaw.String)
	}
	if !r.hasPrior || r.priorStatus != "failed" || r.priorClass.String != "input_invalid" ||
		r.priorMessage.String != "fact payload rejected" || r.priorDetails.String != triageDetails {
		t.Fatalf("dl-noted prior_failure = (%s, %q, %q, %q), want the failed row's triage evidence", r.priorStatus, r.priorClass.String, r.priorMessage.String, r.priorDetails.String)
	}

	// Dead-letter without a note leaves the details untouched.
	if _, err := adminStore.DeadLetterWorkItems(ctx, admin.DeadLetterFilter{WorkItemIDs: []string{"dl-plain"}}); err != nil {
		t.Fatalf("DeadLetterWorkItems(no note): %v", err)
	}
	if r := readNoteRow(t, database, "dl-plain"); r.status != "dead_letter" || r.detailsRaw.String != triageDetails {
		t.Fatalf("dl-plain = (%s, %q), want (dead_letter, the original details)", r.status, r.detailsRaw.String)
	}

	// Skip with a note: the retrying row keeps its class, its evidence rides
	// under prior_failure, and a row whose failure_class is the empty string
	// takes the operator_skipped class.
	if _, err := adminStore.SkipRepositoryWorkItems(ctx, "note-scope-repo", "skipped: repository retired"); err != nil {
		t.Fatalf("SkipRepositoryWorkItems(note): %v", err)
	}
	r = readNoteRow(t, database, "skip-retrying")
	if r.status != "dead_letter" || r.class.String != "projection_retryable" || r.operatorNote != "skipped: repository retired" {
		t.Fatalf("skip-retrying = (%s, %q, note %q), want (dead_letter, projection_retryable, the note)", r.status, r.class.String, r.operatorNote)
	}
	if !r.hasPrior || r.priorStatus != "retrying" || r.priorDetails.String != retryDetails {
		t.Fatalf("skip-retrying prior_failure = (%s, %q), want (retrying, %q)", r.priorStatus, r.priorDetails.String, retryDetails)
	}
	r = readNoteRow(t, database, "skip-empty-class")
	if r.status != "dead_letter" || r.class.String != "operator_skipped" {
		t.Fatalf("skip-empty-class = (%s, %q), want (dead_letter, operator_skipped): an empty class must not survive a skip", r.status, r.class.String)
	}
	if r.hasPrior {
		t.Fatalf("skip-empty-class has prior_failure %v, want none for a row that never failed", r.details["prior_failure"])
	}

	// Skip without a note leaves the details untouched.
	if _, err := adminStore.SkipRepositoryWorkItems(ctx, "note-scope-2-repo", ""); err != nil {
		t.Fatalf("SkipRepositoryWorkItems(no note): %v", err)
	}
	if r := readNoteRow(t, database, "skip-empty-note"); r.status != "dead_letter" || r.detailsRaw.String != "phase=semantic rows=500" {
		t.Fatalf("skip-empty-note = (%s, %q), want (dead_letter, the original details)", r.status, r.detailsRaw.String)
	}
}
