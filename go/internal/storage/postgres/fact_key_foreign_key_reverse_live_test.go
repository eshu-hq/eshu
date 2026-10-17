// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7420 disposable PostgreSQL proof that the documented reverse procedure bounds its lock wait with lock_timeout beside an open fact writer, refuses with an orphan key, restores three validated cascading foreign keys after cleanup and is undone only after its step 4 forgets the 188-190 ledger rows, and that a drop migration whose key table stays busy fails the tracked bootstrap loudly with 55P03 and records nothing, enrolled in the live-postgres-readiness runner; bridges ESHU_GENERATION_RETENTION_PROOF_DSN. local run passed both tests in 7.8s of test time on PostgreSQL 18.6 on 2026-10-16
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/coordination"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/testfixtures"
)

const factKeyReverseProcedurePath = "../../../../docs/internal/evidence/7420-drop-key-sidecar-fks-reverse.sql"

// factKeyReverseProcedure is the documented reverse procedure split on its
// "-- step N:" headers: the preamble (the session lock_timeout) and steps
// 1 to 4.
type factKeyReverseProcedure struct {
	preamble string
	steps    [4]string
}

func readFactKeyReverseProcedure(t *testing.T) factKeyReverseProcedure {
	t.Helper()
	body, err := os.ReadFile(factKeyReverseProcedurePath)
	if err != nil {
		t.Fatalf("read reverse procedure: %v", err)
	}
	text := string(body)
	var procedure factKeyReverseProcedure
	starts := make([]int, 0, 5)
	for _, header := range []string{"-- step 1:", "-- step 2:", "-- step 3:", "-- step 4:"} {
		start := strings.Index(text, header)
		if start < 0 {
			t.Fatalf("reverse procedure has no %q", header)
		}
		starts = append(starts, start)
	}
	starts = append(starts, len(text))
	procedure.preamble = text[:starts[0]]
	for index := range procedure.steps {
		procedure.steps[index] = text[starts[index]:starts[index+1]]
	}
	if !strings.Contains(procedure.preamble, "SET lock_timeout = '5s';") {
		t.Fatal("reverse procedure preamble does not bound its lock waits with SET lock_timeout = '5s'")
	}
	return procedure
}

// TestFactKeyForeignKeyReverseProcedureLive runs the documented #7420
// reverse procedure, on one session as psql would, against a migrated schema
// whose foreign keys are dropped. Its lock_timeout bounds step 2 beside an
// open fact writer; the census refuses while an orphan exists and VALIDATE
// fails on it; after the cleanup the keys come back validated with ON DELETE
// CASCADE and a rerun is a no-op. A later bootstrap leaves them in place
// (188-190 are in the ledger) until step 4 forgets those ledger rows, and the
// next bootstrap drops them again.
func TestFactKeyForeignKeyReverseProcedureLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	database.SetMaxOpenConns(4)
	requireFactKeyForeignKeysDropped(t, ctx, database)
	procedure := readFactKeyReverseProcedure(t)
	steps := procedure.steps
	const scope = "keys-reverse"
	active := scope + "-active"
	testfixtures.SeedScope(t, ctx, database, scope)
	envelopes := factKeyEnvelopes(scope, active, 20)
	writeFactsWithKeys(t, ctx, SQLDB{DB: database}, envelopes)
	if _, err := database.ExecContext(ctx, `
INSERT INTO relationship_reference_candidate_keys (fact_id, scope_id, generation_id, source_repo_id, reference_key)
VALUES ('reverse-gone', $1, $2, 'repo-keys', '|gone|')`, scope, active); err != nil {
		t.Fatalf("plant orphan: %v", err)
	}
	session, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open procedure session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if _, err := session.ExecContext(ctx, procedure.preamble); err != nil {
		t.Fatalf("procedure preamble: %v", err)
	}

	// An open fact writer: step 2 must give up at the lock_timeout instead
	// of queueing every later writer behind it without bound.
	writer, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin fact writer: %v", err)
	}
	if _, err := writer.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, observed_at, ingested_at, payload)
VALUES ('reverse-writer', $1, $2, 'content', 'reverse-writer', 'git', 'reverse-writer', now(), now(), '{}'::jsonb)`, scope, active); err != nil {
		_ = writer.Rollback()
		t.Fatalf("fact writer insert: %v", err)
	}
	started := time.Now()
	_, err = session.ExecContext(ctx, steps[1])
	waited := time.Since(started)
	_ = writer.Rollback()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("step 2 beside an open fact writer: err = %v, want 55P03 from the lock_timeout", err)
	}
	if waited < 4*time.Second || waited > 15*time.Second {
		t.Fatalf("step 2 beside an open fact writer gave up after %s, want about the 5 s lock_timeout", waited)
	}

	if got := reverseCensusTables(t, ctx, session, steps[0]); strings.Join(got, ",") != "relationship_reference_candidate_keys" {
		t.Fatalf("step 1 census with an orphan = %v, want relationship_reference_candidate_keys", got)
	}
	if _, err := session.ExecContext(ctx, steps[1]); err != nil {
		t.Fatalf("step 2 add NOT VALID: %v", err)
	}
	if _, err := session.ExecContext(ctx, steps[2]); !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("step 3 validate with an orphan: err = %v, want a foreign key violation (23503)", err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM relationship_reference_candidate_keys WHERE fact_id = 'reverse-gone'`); err != nil {
		t.Fatalf("clean orphan: %v", err)
	}
	if got := reverseCensusTables(t, ctx, session, steps[0]); len(got) != 0 {
		t.Fatalf("step 1 census after cleanup = %v, want none", got)
	}
	for run := 1; run <= 2; run++ {
		for _, step := range steps[1:3] {
			if _, err := session.ExecContext(ctx, step); err != nil {
				t.Fatalf("run %d of the reverse procedure: %v", run, err)
			}
		}
	}
	if restored := validatedCascadingFactKeys(t, ctx, database); restored != len(factKeyTables) {
		t.Fatalf("validated cascading keys after the reverse procedure = %d, want %d (%v)",
			restored, len(factKeyTables), factRecordsForeignKeys(t, ctx, database))
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM fact_records WHERE scope_id = $1 AND generation_id = $2`, scope, active); err != nil {
		t.Fatalf("delete facts under the restored keys: %v", err)
	}
	for _, table := range factKeyTables {
		if got := factKeyRowsFor(t, ctx, database, table, scope, active); got != 0 {
			t.Errorf("%s rows after the fact delete = %d, want 0 by cascade", table, got)
		}
	}

	// Roll forward: the ledger still records 188-190, so a bootstrap keeps
	// the restored keys until step 4 forgets those rows.
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("bootstrap before step 4: %v", err)
	}
	if restored := validatedCascadingFactKeys(t, ctx, database); restored != len(factKeyTables) {
		t.Fatalf("keys after a bootstrap without step 4 = %d, want %d kept (ledger skips 188-190)", restored, len(factKeyTables))
	}
	if _, err := session.ExecContext(ctx, steps[3]); err != nil {
		t.Fatalf("step 4 forget 188-190: %v", err)
	}
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("bootstrap after step 4: %v", err)
	}
	requireFactKeyForeignKeysDropped(t, ctx, database)
}

func validatedCascadingFactKeys(t *testing.T, ctx context.Context, database *sql.DB) int {
	t.Helper()
	var restored int
	if err := database.QueryRowContext(ctx, `
SELECT count(*) FROM pg_constraint
WHERE contype = 'f' AND confrelid = to_regclass('fact_records') AND convalidated AND confdeltype = 'c'`).Scan(&restored); err != nil {
		t.Fatalf("count restored keys: %v", err)
	}
	return restored
}

func reverseCensusTables(t *testing.T, ctx context.Context, session *sql.Conn, census string) []string {
	t.Helper()
	rows, err := session.QueryContext(ctx, census)
	if err != nil {
		t.Fatalf("step 1 census: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var tables []string
	for rows.Next() {
		var table string
		var orphans int64
		if err := rows.Scan(&table, &orphans); err != nil {
			t.Fatalf("scan step 1 census: %v", err)
		}
		tables = append(tables, table)
	}
	return tables
}

// TestFactKeyForeignKeyDropFailsLoudlyWhenKeyTableStaysBusyLive is what an
// operator sees when a key table stays locked for the whole lock retry
// budget: the tracked migrator logs one lock_wait warning per 55P03 attempt,
// then fails the bootstrap with an error that names the budget, the attempt
// count, the migration path and the busy table. It records nothing in the
// ledger and leaves the foreign key in place, so the next run retries the
// drop instead of skipping it.
func TestFactKeyForeignKeyDropFailsLoudlyWhenKeyTableStaysBusyLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	database.SetMaxOpenConns(4)
	drop := factKeyForeignKeyDrops[2]
	var definition Definition
	for _, candidate := range BootstrapDefinitions() {
		if candidate.Name == drop.migration {
			definition = candidate
		}
	}
	if definition.Path == "" {
		t.Fatalf("no bootstrap definition named %s", drop.migration)
	}
	restoreFactKeyForeignKey(t, ctx, database, drop.table)
	if _, err := database.ExecContext(ctx, `DELETE FROM eshu_schema_migrations WHERE path = $1`, definition.Path); err != nil {
		t.Fatalf("forget %s in the ledger: %v", definition.Path, err)
	}
	holder, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin key table holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(ctx, "DELETE FROM "+drop.table+" WHERE false"); err != nil {
		t.Fatalf("hold %s: %v", drop.table, err)
	}

	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open migrator connection: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	executor := schemaConnectionExecutor{database: SQLDB{DB: database}, conn: conn, logger: logger}
	started := time.Now()
	err = executor.applyTrackedDefinitions(ctx, []Definition{definition}, schemaStatementBounds{
		lockTimeout:     200 * time.Millisecond,
		lockRetryBudget: 1500 * time.Millisecond,
	}, logger)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("drop migration with its key table held for the whole budget succeeded, want a loud failure")
	}
	for _, want := range []string{"apply " + drop.migration, "lock retry budget", definition.Path, drop.table, "55P03"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("bootstrap error %q does not name %q", err, want)
		}
	}
	if !coordination.IsLockNotAvailable(err) {
		t.Errorf("bootstrap error lost its 55P03 classification: %v", err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("exhausted bootstrap took %s, want it bounded by the 1.5 s budget plus one backoff", elapsed)
	}
	if !strings.Contains(logs.String(), "event_name=bootstrap.postgres.migration.lock_wait") {
		t.Errorf("migrator logged no lock_wait warning before failing:\n%s", logs.String())
	}
	var recorded bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM eshu_schema_migrations WHERE path = $1)`,
		definition.Path).Scan(&recorded); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if recorded {
		t.Error("failed drop migration was recorded in the ledger; the next run would skip it")
	}
	if got := factRecordsForeignKeys(t, ctx, database); len(got) != 1 {
		t.Errorf("foreign keys after the failed drop = %v, want the one held key left in place", got)
	}
	t.Logf("operator-visible error after %s: %v", elapsed.Round(time.Millisecond), err)
}
