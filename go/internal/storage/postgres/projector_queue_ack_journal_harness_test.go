// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// Harness of the #7127 PR-3b live proofs (the projector Ack journals its
// activation). Each proof gets its own database: the journal gate is an
// advisory lock, and advisory locks are per database, so no other proof can
// hold the key.

// ackJournalProofDSNEnv names the administrative DSN the proofs create their
// database from; the live-postgres-readiness runner sets it.
const ackJournalProofDSNEnv = "ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN"

// openAckJournalDB creates and bootstraps a disposable database and drops it
// when the test ends. The drop uses its own connection and a background
// context (docs/internal/timing-proof-rules.md, rule 3).
func openAckJournalDB(t *testing.T, conns int) *sql.DB {
	t.Helper()
	database, _ := openAckJournalDBWithDSN(t, conns)
	return database
}

// openAckJournalDBWithDSN is openAckJournalDB that also returns the
// database's DSN, for a proof that needs a second pool on it.
func openAckJournalDBWithDSN(t *testing.T, conns int) (*sql.DB, string) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(ackJournalProofDSNEnv))
	if dsn == "" {
		t.Skipf("set %s to an administrative Postgres DSN to run the #7127 Ack journal proofs", ackJournalProofDSNEnv)
	}
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("random: %v", err)
	}
	name := "ackj7127_" + hex.EncodeToString(suffix)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		_ = admin.Close()
		t.Fatalf("create database: %v", err)
	}
	_ = admin.Close()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	u.Path = "/" + name
	database, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	database.SetMaxOpenConns(conns)
	t.Cleanup(func() {
		_ = database.Close()
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dropCancel()
		dropper, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Errorf("open admin for drop: %v", err)
			return
		}
		defer func() { _ = dropper.Close() }()
		if _, err := dropper.ExecContext(dropCtx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})
	if err := ApplyBootstrapWithoutContentSearchIndexes(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	return database, u.String()
}

func ajExec(t *testing.T, d *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := d.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %.70q: %v", query, err)
	}
}

func ajCount(t *testing.T, d *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := d.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %.70q: %v", query, err)
	}
	return n
}

// ajSeedScope creates a scope whose active generation <scope>-g0 was
// activated an hour ago by an Ack with the journal off: it has no journal row.
func ajSeedScope(t *testing.T, d *sql.DB, scopeID string) {
	t.Helper()
	g0 := scopeID + "-g0"
	ajExec(t, d, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
        partition_key, observed_at, ingested_at, status, active_generation_id)
        VALUES ($1, 'repository', 'git', $1, 'git', $1, now(), now(), 'active', $2)`, scopeID, g0)
	ajExec(t, d, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at,
        ingested_at, status, activated_at) VALUES ($1, $2, 'snapshot', FALSE, now(), now(), 'active', now() - interval '1 hour')`, g0, scopeID)
	ajFacts(t, d, scopeID, g0, 0)
}

func ajFacts(t *testing.T, d *sql.DB, scopeID, generationID string, salt int) {
	t.Helper()
	for i := range 3 {
		ajExec(t, d, `INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
            source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
            VALUES ($1, $2, $3, 'file', $4, 'git', $4, now(), now(), FALSE, $5::jsonb)`,
			fmt.Sprintf("%s/%d", generationID, i), scopeID, generationID, fmt.Sprintf("k%d", i), fmt.Sprintf(`{"v":%d}`, salt*10+i))
	}
}

// ajPending adds a pending generation with facts and a running projector work
// row leased to ajLeaseOwner, and returns the work an Ack names.
func ajPending(t *testing.T, d *sql.DB, scopeID, generationID string, salt int) projector.ScopeGenerationWork {
	t.Helper()
	ajExec(t, d, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at,
        ingested_at, status) VALUES ($1, $2, 'snapshot', FALSE, now(), now(), 'pending')`, generationID, scopeID)
	ajFacts(t, d, scopeID, generationID, salt)
	ajRunning(t, d, scopeID, generationID)
	return ajWork(scopeID, generationID)
}

// ajRunning (re)creates the running projector work row of a generation.
func ajRunning(t *testing.T, d *sql.DB, scopeID, generationID string) {
	t.Helper()
	ajExec(t, d, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status,
        attempt_count, lease_owner, claim_until, visible_at, payload, created_at, updated_at)
        VALUES ($1, $2, $3, 'projector', 'source_local', 'running', 1, $4, now() + interval '10 minutes', now(), '{}'::jsonb, now(), now())
        ON CONFLICT (work_item_id) DO UPDATE SET status = 'running', lease_owner = EXCLUDED.lease_owner, attempt_count = 1`,
		projectorWorkItemID(scopeID, generationID), scopeID, generationID, ajLeaseOwner)
}

const ajLeaseOwner = "ack-journal-proof"

func ajWork(scopeID, generationID string) projector.ScopeGenerationWork {
	return projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: scopeID},
		Generation:   scope.ScopeGeneration{GenerationID: generationID},
		AttemptCount: 1,
	}
}

func ajQueue(database db.ExecQueryer, journal bool) ProjectorQueue {
	q := NewProjectorQueue(database, ajLeaseOwner, 10*time.Minute)
	q.ChangedSinceJournalEnabled = journal
	return q
}

// ajSeq is the activation_seq of a generation's journal row, or -1.
func ajSeq(t *testing.T, d *sql.DB, scopeID, generationID string) int64 {
	t.Helper()
	var seq sql.NullInt64
	err := d.QueryRow(`SELECT activation_seq FROM changed_since_activations WHERE scope_id = $1 AND generation_id = $2`,
		scopeID, generationID).Scan(&seq)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("activation_seq: %v", err)
	}
	if !seq.Valid {
		return -1
	}
	return seq.Int64
}

// ajDrainLinks links every activation of the scope above its cursor.
func ajDrainLinks(t *testing.T, d *sql.DB, scopeID string) {
	t.Helper()
	writer := linksfreshnessstore.NewLinkWriter(SQLDB{DB: d})
	for range 50 {
		result, err := writer.LinkNext(context.Background(), scopeID)
		if err != nil {
			t.Fatalf("link %s: %v", scopeID, err)
		}
		if result.Idle {
			return
		}
	}
	t.Fatalf("link %s did not reach idle", scopeID)
}

// ajLinkState is the generation the scope's link state describes.
func ajLinkState(t *testing.T, d *sql.DB, scopeID string) string {
	t.Helper()
	var state sql.NullString
	err := d.QueryRow(`SELECT state_generation_id FROM changed_since_scope_cursor WHERE scope_id = $1`, scopeID).Scan(&state)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("cursor state: %v", err)
	}
	return state.String
}

// ajHookDB wraps a pool so a test can run a hook inside an Ack transaction
// right after the statement that inserted the Ack's journal row, before the
// next statement: with the row written and not committed. It also records
// every statement. Production code is unchanged; only the transaction is
// wrapped.
type ajHookDB struct {
	SQLDB
	hook  func(ctx context.Context, tx db.Transaction) error
	mu    *sync.Mutex
	stmts *[]string
}

func newAJHookDB(d *sql.DB, hook func(ctx context.Context, tx db.Transaction) error) ajHookDB {
	return ajHookDB{SQLDB: SQLDB{DB: d}, hook: hook, mu: &sync.Mutex{}, stmts: &[]string{}}
}

func (h ajHookDB) record(query string) {
	h.mu.Lock()
	*h.stmts = append(*h.stmts, query)
	h.mu.Unlock()
}

func (h ajHookDB) statements() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), *h.stmts...)
}

func (h ajHookDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	h.record(query)
	return h.SQLDB.ExecContext(ctx, query, args...)
}

func (h ajHookDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	h.record(query)
	return h.SQLDB.QueryContext(ctx, query, args...)
}

func (h ajHookDB) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := h.SQLDB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &ajHookTx{Transaction: tx, parent: h}, nil
}

type ajHookTx struct {
	db.Transaction
	parent ajHookDB
	armed  bool
}

// before runs the hook once, before the first statement that follows the
// journal insert.
func (tx *ajHookTx) before(ctx context.Context) error {
	if !tx.armed || tx.parent.hook == nil {
		return nil
	}
	tx.armed = false
	return tx.parent.hook(ctx, tx.Transaction)
}

func (tx *ajHookTx) after(query string) {
	if strings.Contains(query, "INSERT INTO changed_since_activations") {
		tx.armed = true
	}
}

func (tx *ajHookTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	tx.parent.record(query)
	if err := tx.before(ctx); err != nil {
		return nil, err
	}
	result, err := tx.Transaction.ExecContext(ctx, query, args...)
	tx.after(query)
	return result, err
}

func (tx *ajHookTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	tx.parent.record(query)
	if err := tx.before(ctx); err != nil {
		return nil, err
	}
	rows, err := tx.Transaction.QueryContext(ctx, query, args...)
	tx.after(query)
	return rows, err
}

// ajPausedAck runs a flag-on Ack in the background and pauses it inside its
// transaction right after the journal insert. It returns the Ack's backend
// pid, a release func, and the channel that carries Ack's result. A non-empty
// activateQuery replaces the shipped statement (the ungated RED).
func ajPausedAck(t *testing.T, d *sql.DB, work projector.ScopeGenerationWork, activateQuery string) (int, func(), <-chan error) {
	t.Helper()
	paused := make(chan int, 1)
	release := make(chan struct{})
	hookDB := newAJHookDB(d, func(ctx context.Context, tx db.Transaction) error {
		var pid int
		rows, err := tx.QueryContext(ctx, `SELECT pg_backend_pid()`)
		if err != nil {
			return err
		}
		if rows.Next() {
			_ = rows.Scan(&pid)
		}
		_ = rows.Close()
		paused <- pid
		<-release
		return nil
	})
	q := ajQueue(hookDB, true)
	q.ackJournalActivateQuery = activateQuery
	done := make(chan error, 1)
	go func() { done <- q.Ack(context.Background(), work, runtime.Result{}) }()
	select {
	case pid := <-paused:
		var once sync.Once
		return pid, func() { once.Do(func() { close(release) }) }, done
	case err := <-done:
		t.Fatalf("Ack finished before it paused after the journal insert: %v", err)
	case <-time.After(time.Minute):
		t.Fatal("Ack never reached the statement after its journal insert")
	}
	return 0, nil, nil
}

// ajUngatedActivateQuery is the shipped activate-and-journal statement with
// its gate removed: the shape that inverts activation_seq against commit
// order. It is derived, never hand-copied.
func ajUngatedActivateQuery(t *testing.T) string {
	t.Helper()
	gate := "SELECT " + linksfreshnessstore.AckJournalGateSQL + " AS acquired"
	if strings.Count(activateAndJournalProjectorGenerationQuery, gate) != 1 {
		t.Fatalf("shipped statement lost its gate %q", gate)
	}
	return strings.Replace(activateAndJournalProjectorGenerationQuery, gate, "SELECT TRUE AS acquired", 1)
}

// ajUngrantedLocks counts the locks a backend of this database is waiting
// for. A transactionid wait has no database, so backends are matched through
// pg_stat_activity.
func ajUngrantedLocks(t *testing.T, d *sql.DB) int64 {
	t.Helper()
	return ajCount(t, d, `SELECT count(*) FROM pg_locks AS l JOIN pg_stat_activity AS a ON a.pid = l.pid
        WHERE NOT l.granted AND a.datname = current_database()`)
}
