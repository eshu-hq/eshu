// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/migrations"
)

func secretLinesMigrationSQLByName(t *testing.T, name string) string {
	t.Helper()
	for _, def := range migrations.BootstrapDefinitions() {
		if def.Name == name {
			return def.SQL
		}
	}
	t.Fatalf("migration %q not embedded", name)
	return ""
}

type secretLinesPolicyState struct {
	state   string
	epoch   int64
	version int64
}

func readSecretLinesPolicyState(t *testing.T, ctx context.Context, db *sql.DB) secretLinesPolicyState {
	t.Helper()
	var s secretLinesPolicyState
	if err := db.QueryRowContext(ctx, `SELECT state, epoch, policy_version FROM content_file_secret_lines_state WHERE singleton`).Scan(&s.state, &s.epoch, &s.version); err != nil {
		t.Fatal(err)
	}
	return s
}

func secretLinesFunctionIsCaseSensitive(t *testing.T, ctx context.Context, db *sql.DB) bool {
	t.Helper()
	var definition string
	if err := db.QueryRowContext(ctx, `SELECT pg_get_functiondef('eshu_secret_line_findings(text)'::regprocedure)`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	return strings.Contains(definition, "line_text ~ 'REDACTED_aws_access_key'")
}

func TestSecretLinesPolicyMigrationFlipsReadinessOnceLive(t *testing.T) {
	ctx, db := openSecretLinesDatabase(t)
	m234 := secretLinesMigrationSQLByName(t, "content_file_secret_lines_marker_policy")

	// A fresh bootstrap: 138 seeds ready at epoch 1, 234 adds the column at
	// 138, 235 replaces the function and flips readiness.
	if got := readSecretLinesPolicyState(t, ctx, db); got != (secretLinesPolicyState{"not_built", 2, 235}) {
		t.Fatalf("after bootstrap state = %+v, want not_built epoch 2 policy_version 235", got)
	}
	if !secretLinesFunctionIsCaseSensitive(t, ctx, db) {
		t.Fatal("eshu_secret_line_findings does not match the marker arm with ~")
	}
	var kind string
	if err := db.QueryRowContext(ctx, `SELECT coalesce((SELECT finding_kind FROM eshu_secret_line_findings('redacted_slack_token = abcdefghij')), '')`).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "api_token" {
		t.Fatalf("a lower-case marker spelling classified as %q, want api_token (its key arm)", kind)
	}

	// The local supervisor re-applies every migration at each start: 235 must
	// change nothing the second time.
	if _, err := db.ExecContext(ctx, m234); err != nil {
		t.Fatalf("re-apply 235: %v", err)
	}
	if got := readSecretLinesPolicyState(t, ctx, db); got != (secretLinesPolicyState{"not_built", 2, 235}) {
		t.Fatalf("after re-apply state = %+v, want it unchanged", got)
	}

	// An upgrade from each prior state of a 138-era database gains one epoch
	// and lands on not_built under policy 235.
	for _, prior := range []string{"ready", "building", "failed"} {
		if _, err := db.ExecContext(ctx, `UPDATE content_file_secret_lines_state SET state = $1, epoch = 10, policy_version = 138 WHERE singleton`, prior); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, m234); err != nil {
			t.Fatalf("apply 235 over %s: %v", prior, err)
		}
		if got := readSecretLinesPolicyState(t, ctx, db); got != (secretLinesPolicyState{"not_built", 11, 235}) {
			t.Fatalf("apply 235 over %s: state = %+v, want not_built epoch 11 policy_version 235", prior, got)
		}
	}
}

// TestSecretLinesPolicyMigrationIsOneTransactionLive holds the state row so
// 235's UPDATE blocks after its CREATE OR REPLACE FUNCTION has run, then reads
// the function from a third session: it must still be the old one, because
// the runner sends 235 as one simple-protocol query, which Postgres runs as a
// single transaction.
func TestSecretLinesPolicyMigrationIsOneTransactionLive(t *testing.T) {
	ctx, db := openSecretLinesDatabase(t)
	m234 := secretLinesMigrationSQLByName(t, "content_file_secret_lines_marker_policy")
	// Put a 138-era function and state back.
	m138 := secretLinesMigrationSQLByName(t, secretLinesMigrationName)
	if _, err := db.ExecContext(ctx, m138); err != nil {
		t.Fatalf("re-apply 138: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE content_file_secret_lines_state SET state = 'ready', epoch = 3, policy_version = 138 WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	if secretLinesFunctionIsCaseSensitive(t, ctx, db) {
		t.Fatal("setup: the 138 function is still the 235 one")
	}

	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := holder.ExecContext(ctx, `SELECT 1 FROM content_file_secret_lines_state WHERE singleton FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	applied := make(chan error, 1)
	go func() {
		_, err := db.ExecContext(ctx, m234)
		applied <- err
	}()
	// Wait until the apply is blocked on the row lock, past its function
	// replace. pg_stat_activity truncates the query text, so match the wait.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database() AND pid <> pg_backend_pid()`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("235 never blocked on the held state row")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if secretLinesFunctionIsCaseSensitive(t, ctx, db) {
		t.Fatal("a reader saw the new function while 235's UPDATE had not committed: the function replace committed alone")
	}
	if got := readSecretLinesPolicyState(t, ctx, db); got != (secretLinesPolicyState{"ready", 3, 138}) {
		t.Fatalf("state during the blocked apply = %+v, want ready epoch 3 policy_version 138", got)
	}
	if err := holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-applied; err != nil {
		t.Fatalf("apply 235: %v", err)
	}
	if !secretLinesFunctionIsCaseSensitive(t, ctx, db) {
		t.Fatal("after commit the function is not the 235 one")
	}
	if got := readSecretLinesPolicyState(t, ctx, db); got != (secretLinesPolicyState{"not_built", 4, 235}) {
		t.Fatalf("after commit state = %+v, want not_built epoch 4 policy_version 235", got)
	}
}
