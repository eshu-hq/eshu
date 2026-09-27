// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// seedBulkGeneration inserts n content_entity facts for one generation in one
// statement. variant changes every payload whose index is divisible by 97.
func (l *ledgerDB) seedBulkGeneration(t *testing.T, scopeID, generationID string, n, variant int) {
	t.Helper()
	l.exec(t, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, source_uri, observed_at, ingested_at, is_tombstone, payload)
SELECT $2 || '/' || i, $1, $2, 'content_entity', 'ent:' || i, 'git', 'ent:' || i, 'f' || (i / 50) || '.go',
       $4, $4, FALSE,
       jsonb_build_object('n', i, 'v', CASE WHEN i % 97 = 0 THEN $5 ELSE 0 END, 'indexed_at', $2,
                          'body', repeat('x', 200))
FROM generate_series(1, $3) AS i`, scopeID, generationID, n, fixtureEpoch, variant)
}

// TestLinkFenceOneWinnerPerActivation is gate G9: concurrent writers on one
// scope produce one link per activation, advance the cursor once, and every
// loser returns cursor_locked (or idle, after the winner committed) within
// one second.
func TestLinkFenceOneWinnerPerActivation(t *testing.T) {
	l := openLedgerDB(t)
	const rounds, writers = 24, 4
	for round := range rounds {
		scopeID := fmt.Sprintf("fence-%02d", round)
		l.seedScope(t, scopeID)
		l.seedGeneration(t, scopeID, scopeID+"-g0", false, "active", fixtureEpoch, time.Time{})
		l.seedBulkGeneration(t, scopeID, scopeID+"-g0", 3000, 0)
		l.journal(t, scopeID, scopeID+"-g0", "")

		type outcome struct {
			result  linksfreshnessstore.LinkResult
			err     error
			elapsed time.Duration
		}
		results := make([]outcome, writers)
		var ready, done sync.WaitGroup
		start := make(chan struct{})
		for w := range writers {
			ready.Add(1)
			done.Add(1)
			go func() {
				defer done.Done()
				writer := linksfreshnessstore.NewLinkWriter(l.store)
				ready.Done()
				<-start
				began := time.Now()
				res, err := writer.LinkNext(l.ctx, scopeID)
				results[w] = outcome{res, err, time.Since(began)}
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()

		winners := 0
		for w, got := range results {
			switch {
			case got.err == nil && got.result.Kind == linksfreshnessstore.LinkKindRoot:
				winners++
			case got.err == nil && got.result.Idle:
			case got.err != nil:
				reason, ok := linksfreshnessstore.RetryReasonOf(got.err)
				if !ok || reason != linksfreshnessstore.RetryCursorLocked {
					t.Fatalf("round %d writer %d: %v, want cursor_locked", round, w, got.err)
				}
				if got.elapsed > time.Second {
					t.Fatalf("round %d writer %d: loser took %s, want under 1s", round, w, got.elapsed)
				}
			default:
				t.Fatalf("round %d writer %d: unexpected result %+v", round, w, got.result)
			}
		}
		if winners != 1 {
			t.Fatalf("round %d: %d writers linked, want exactly 1", round, winners)
		}
		if n := l.queryInt(t, `SELECT count(*) FROM changed_since_links WHERE scope_id = $1`, scopeID); n != 1 {
			t.Fatalf("round %d: %d link rows, want 1", round, n)
		}
		if _, seq := l.cursor(t, scopeID); seq != l.queryInt(t,
			`SELECT activation_seq FROM changed_since_activations WHERE scope_id = $1`, scopeID) {
			t.Fatalf("round %d: cursor seq %d is not the activation's", round, seq)
		}
	}
}

// ledgerRows is every ledger row of a scope with the scope id removed, so two
// scopes built from the same fixture compare equal.
func (l *ledgerDB) ledgerRows(t *testing.T, scopeID string) string {
	t.Helper()
	var all []string
	for _, query := range []string{
		`SELECT replace(generation_id, $1, '') || '|' || replace(prior_generation_id, $1, '') || '|' || fact_category
		   || '|' || classification || '|' || stable_fact_key || '|' || COALESCE(prior_fact_kind, '') || '|'
		   || COALESCE(current_fact_kind, '') || '|' || COALESCE(encode(prior_state, 'hex'), '') || '|'
		   || COALESCE(encode(current_state, 'hex'), '') || '|' || current_tombstoned
		 FROM changed_since_link_deltas WHERE scope_id = $1 ORDER BY 1`,
		`SELECT replace(generation_id, $1, '') || '|' || replace(prior_generation_id, $1, '') || '|' || fact_category
		   || '|' || classification || '|' || key_count
		 FROM changed_since_link_bucket_counts WHERE scope_id = $1 ORDER BY 1`,
		`SELECT replace(generation_id, $1, '') || '|' || replace(prior_generation_id, $1, '') || '|' || link_kind
		   || '|' || delta_rows || '|' || files_keys || '|' || content_entities_keys || '|' || facts_keys
		 FROM changed_since_links WHERE scope_id = $1 ORDER BY 1`,
		`SELECT fact_category || '|' || stable_fact_key || '|' || fact_kind || '|' || encode(state, 'hex')
		 FROM changed_since_key_state WHERE scope_id = $1 ORDER BY 1`,
		`SELECT replace(COALESCE(state_generation_id, ''), $1, '') FROM changed_since_scope_cursor WHERE scope_id = $1`,
	} {
		all = append(all, l.queryStrings(t, query, scopeID)...)
	}
	return hashLines(all)
}

// TestLinkKilledMidStatementRerunsToIdenticalRows is gate G10.
func TestLinkKilledMidStatementRerunsToIdenticalRows(t *testing.T) {
	l := openLedgerDB(t)
	const n = 150000
	for _, scopeID := range []string{"killed", "reference"} {
		l.seedScope(t, scopeID)
		l.seedGeneration(t, scopeID, scopeID+"-g0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
		l.seedGeneration(t, scopeID, scopeID+"-g1", false, "active", fixtureEpoch.Add(time.Hour), time.Time{})
		l.seedBulkGeneration(t, scopeID, scopeID+"-g0", n, 0)
		l.seedBulkGeneration(t, scopeID, scopeID+"-g1", n, 1)
		l.journal(t, scopeID, scopeID+"-g0", "")
		l.journal(t, scopeID, scopeID+"-g1", scopeID+"-g0")
		mustLink(t, linksfreshnessstore.NewLinkWriter(l.store), l, scopeID)
	}
	mustLink(t, linksfreshnessstore.NewLinkWriter(l.store), l, "reference")
	before := l.ledgerRows(t, "killed")
	_, seqBefore := l.cursor(t, "killed")

	// Hold a row lock on a state row the incremental statement must update
	// (ent:97 changes under variant 1), so the statement blocks mid-way, then
	// kill its backend. Without the lock the statement can finish before a
	// poller sees it. pg_stat_activity truncates the query text at
	// track_activity_query_size, so match the leading CTE.
	holder, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(l.ctx, `SELECT 1 FROM changed_since_key_state
WHERE scope_id = 'killed' AND fact_category = 'content_entities' AND stable_fact_key = 'ent:97' FOR UPDATE`); err != nil {
		t.Fatalf("hold state row: %v", err)
	}
	errCh := make(chan error, 1)
	var raced linksfreshnessstore.LinkResult
	go func() {
		var err error
		raced, err = linksfreshnessstore.NewLinkWriter(l.store).LinkNext(l.ctx, "killed")
		errCh <- err
	}()
	killed := false
	deadline := time.Now().Add(60 * time.Second)
	for !killed && time.Now().Before(deadline) {
		var pid sql.NullInt64
		_ = l.raw.QueryRowContext(l.ctx, `
SELECT pid FROM pg_stat_activity
WHERE state = 'active' AND wait_event_type = 'Lock'
  AND query LIKE '%cur AS MATERIALIZED%' AND datname = current_database() AND pid <> pg_backend_pid()
LIMIT 1`).Scan(&pid)
		if pid.Valid {
			var ok bool
			if err := l.raw.QueryRowContext(l.ctx, `SELECT pg_terminate_backend($1)`, pid.Int64).Scan(&ok); err == nil && ok {
				killed = true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = holder.Rollback()
	err = <-errCh
	if !killed {
		t.Fatalf("never caught the incremental statement running (LinkNext = %+v, err=%v)", raced, err)
	}
	var failure *linksfreshnessstore.FailureError
	if !errors.As(err, &failure) || failure.Class != linksfreshnessstore.FailureConnectionLost {
		t.Fatalf("killed LinkNext = %v, want a counting connection_lost failure", err)
	}
	if got := l.ledgerRows(t, "killed"); got != before {
		t.Fatalf("killed link left partial rows")
	}
	if _, seq := l.cursor(t, "killed"); seq != seqBefore {
		t.Fatalf("killed link moved the cursor from %d to %d", seqBefore, seq)
	}
	// One counted connection_lost; the rerun waits out the backoff.
	clock := time.Now().UTC()
	writer := linksfreshnessstore.NewLinkWriter(l.store)
	writer.Now = func() time.Time { return clock }
	record, err := writer.RecordFailure(l.ctx, failure, linksfreshnessstore.DefaultMaxAttempts)
	if err != nil || !record.Counted || record.Attempts != 1 || record.Poisoned {
		t.Fatalf("RecordFailure after the kill = %+v, %v; want one counted attempt", record, err)
	}
	if res, err := writer.LinkNext(l.ctx, "killed"); err != nil || !res.Deferred {
		t.Fatalf("LinkNext inside the backoff = %+v, %v; want deferred", res, err)
	}
	clock = record.NextAttemptAt.Add(time.Second)
	if res := mustLink(t, writer, l, "killed"); res.Kind != linksfreshnessstore.LinkKindIncremental {
		t.Fatalf("rerun = %+v, want incremental", res)
	}
	if got, want := l.ledgerRows(t, "killed"), l.ledgerRows(t, "reference"); got != want {
		t.Fatalf("rerun after kill differs from the uninterrupted reference")
	}
}

// ledgerSchemaViolations lists every foreign key on the six ledger tables and
// every nullable column the chain read's NOT IN depends on.
func ledgerSchemaViolations(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
},
) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
SELECT 'foreign key ' || conname || ' on ' || conrelid::regclass::text
FROM pg_constraint
WHERE contype = 'f'
  AND (conrelid::regclass::text IN ('changed_since_activations', 'changed_since_key_state',
        'changed_since_scope_cursor', 'changed_since_links', 'changed_since_link_deltas',
        'changed_since_link_bucket_counts')
    OR confrelid::regclass::text IN ('changed_since_activations', 'changed_since_key_state',
        'changed_since_scope_cursor', 'changed_since_links', 'changed_since_link_deltas',
        'changed_since_link_bucket_counts'))
UNION ALL
SELECT 'nullable ' || attname || ' on changed_since_link_deltas'
FROM pg_attribute
WHERE attrelid = 'changed_since_link_deltas'::regclass
  AND attname IN ('fact_category', 'stable_fact_key') AND NOT attnotnull
ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// TestLedgerSchemaHasNoForeignKeys is gate G13, with its RED case: a foreign
// key or a nullable NOT IN column added inside a rolled-back transaction must
// be reported.
func TestLedgerSchemaHasNoForeignKeys(t *testing.T) {
	l := openLedgerDB(t)
	if got, err := ledgerSchemaViolations(l.ctx, l.raw); err != nil || len(got) != 0 {
		t.Fatalf("shipped ledger schema violations = %v, %v; want none", got, err)
	}
	tx, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range []string{
		`ALTER TABLE changed_since_key_state ADD CONSTRAINT planted_fk
		   FOREIGN KEY (scope_id) REFERENCES ingestion_scopes (scope_id)`,
		`ALTER TABLE changed_since_link_deltas DROP CONSTRAINT changed_since_link_deltas_pkey`,
		`ALTER TABLE changed_since_link_deltas ALTER COLUMN stable_fact_key DROP NOT NULL`,
	} {
		if _, err := tx.ExecContext(l.ctx, statement); err != nil {
			t.Fatalf("plant %q: %v", firstLine(statement), err)
		}
	}
	got, err := ledgerSchemaViolations(l.ctx, tx)
	if err != nil {
		t.Fatalf("violations in planted schema: %v", err)
	}
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "planted_fk") || !strings.Contains(joined, "nullable stable_fact_key") {
		t.Fatalf("planted violations not reported: %v", got)
	}
}

// TestCursorHeldReturnsRetryWithoutWaiting pins the non-blocking half of G9:
// a writer that finds the scope's cursor row locked returns cursor_locked at
// once. The race test above accepts an idle loser, so on its own it would not
// notice a blocking lock that waits for the winner and then finds no work.
func TestCursorHeldReturnsRetryWithoutWaiting(t *testing.T) {
	l := openLedgerDB(t)
	l.seedScope(t, "held")
	l.seedGeneration(t, "held", "h0", false, "active", fixtureEpoch, time.Time{})
	l.journal(t, "held", "h0", "")
	l.exec(t, `INSERT INTO changed_since_scope_cursor (scope_id, digest_version, updated_at) VALUES ('held', $1, now())`,
		linksfreshnessstore.DigestVersion)
	holder, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(l.ctx, `SELECT 1 FROM changed_since_scope_cursor WHERE scope_id = 'held' FOR UPDATE`); err != nil {
		t.Fatalf("hold cursor: %v", err)
	}
	ctx, cancel := context.WithTimeout(l.ctx, 3*time.Second)
	defer cancel()
	began := time.Now()
	_, err = linksfreshnessstore.NewLinkWriter(l.store).LinkNext(ctx, "held")
	if reason, ok := linksfreshnessstore.RetryReasonOf(err); !ok || reason != linksfreshnessstore.RetryCursorLocked {
		t.Fatalf("LinkNext with the cursor held = %v after %s, want cursor_locked", err, time.Since(began))
	}
	if elapsed := time.Since(began); elapsed > time.Second {
		t.Fatalf("cursor_locked took %s, want under 1s", elapsed)
	}
}

// TestTransactionDeadlineEndsAStalledLink is review F4 (ruling 8.10 item 6):
// with a statement timeout far away, only the transaction deadline can end a
// link stalled on a held state row. It must end within the deadline, as a
// counting statement_timeout failure, and leave the cursor where it was.
func TestTransactionDeadlineEndsAStalledLink(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	l.seedScope(t, "stall")
	l.seedGeneration(t, "stall", "stall-g0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
	l.seedGeneration(t, "stall", "stall-g1", false, "active", fixtureEpoch.Add(time.Hour), time.Time{})
	l.seedBulkGeneration(t, "stall", "stall-g0", 500, 0)
	l.seedBulkGeneration(t, "stall", "stall-g1", 500, 1)
	l.journal(t, "stall", "stall-g0", "")
	l.journal(t, "stall", "stall-g1", "stall-g0")
	mustLink(t, w, l, "stall")
	_, seqBefore := l.cursor(t, "stall")

	holder, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(l.ctx, `SELECT 1 FROM changed_since_key_state
WHERE scope_id = 'stall' AND stable_fact_key = 'ent:97' FOR UPDATE`); err != nil {
		t.Fatalf("hold state row: %v", err)
	}
	release := time.AfterFunc(15*time.Second, func() { _ = holder.Rollback() })
	defer release.Stop()

	w.StatementTimeout = time.Hour
	w.TransactionDeadline = 2 * time.Second
	began := time.Now()
	_, err = w.LinkNext(l.ctx, "stall")
	elapsed := time.Since(began)
	var failure *linksfreshnessstore.FailureError
	if !errors.As(err, &failure) || failure.Class != linksfreshnessstore.FailureStatementTimeout {
		t.Fatalf("stalled link = %v after %s, want a counting statement_timeout", err, elapsed)
	}
	if elapsed > 6*time.Second {
		t.Fatalf("stalled link ended after %s, want about the 2s transaction deadline", elapsed)
	}
	if _, seq := l.cursor(t, "stall"); seq != seqBefore {
		t.Fatalf("deadline moved the cursor from %d to %d", seqBefore, seq)
	}
}
