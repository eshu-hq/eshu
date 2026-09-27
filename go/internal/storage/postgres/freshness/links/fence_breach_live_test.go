// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// breachFence updates one to-be-deleted state row of scopeID in a second
// session and holds it until run's link statement waits on it, then commits:
// the EvalPlanQual shape of a broken per-scope fence (arbiter ruling
// arb-7127-g8, section 3). run executes the link and returns its error.
func (l *ledgerDB) breachFence(t *testing.T, scopeID string, run func() error) error {
	t.Helper()
	holder, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(l.ctx, `UPDATE changed_since_key_state SET fact_kind = fact_kind
WHERE scope_id = $1 AND fact_category = 'content_entities' AND stable_fact_key = 'ent:1'`, scopeID); err != nil {
		t.Fatalf("hold a to-be-deleted row: %v", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- run() }()
	deadline := time.Now().Add(60 * time.Second)
	for {
		var waiting int64
		_ = l.raw.QueryRowContext(l.ctx, `SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%cur AS MATERIALIZED%'`).Scan(&waiting)
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the link never waited on the held row")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := holder.Commit(); err != nil {
		t.Fatalf("commit holder: %v", err)
	}
	return <-errCh
}

// TestLinkFailsWhenAFenceBreachSkipsADelete is P4 (live) of arbiter ruling
// arb-7127-g8. Under a broken fence the ctid delete skips a row whose version
// changed (999 of 1,000); the row-count invariant must turn that into a
// counting failure with nothing persisted, and the rerun must equal an
// undisturbed reference. RED: the statement without the Go check commits
// and leaves state that differs from the aggregate.
func TestLinkFailsWhenAFenceBreachSkipsADelete(t *testing.T) {
	l := openLedgerDB(t)
	clock := time.Now().UTC()
	w := linksfreshnessstore.NewLinkWriter(l.store)
	w.Now = func() time.Time { return clock }
	for _, scopeID := range []string{"breach", "reference", "planted"} {
		l.seedRekeyScope(t, w, scopeID, 2000, 1000)
	}
	mustLink(t, w, l, "reference")

	_, seqBefore := l.cursor(t, "breach")
	before := l.ledgerRows(t, "breach")
	err := l.breachFence(t, "breach", func() error {
		_, err := w.LinkNext(l.ctx, "breach")
		return err
	})
	var failure *linksfreshnessstore.FailureError
	if !errors.As(err, &failure) || failure.Class != linksfreshnessstore.FailureInternal ||
		!strings.Contains(err.Error(), "row-count invariant") {
		t.Fatalf("link under a fence breach = %v; want a counting internal row-count invariant failure", err)
	}
	if got := l.ledgerRows(t, "breach"); got != before {
		t.Fatal("the failed link persisted ledger rows")
	}
	if _, seq := l.cursor(t, "breach"); seq != seqBefore {
		t.Fatalf("the failed link moved the cursor from %d to %d", seqBefore, seq)
	}
	record, err := w.RecordFailure(l.ctx, failure, linksfreshnessstore.DefaultMaxAttempts)
	if err != nil || !record.Counted || record.Attempts != 1 {
		t.Fatalf("RecordFailure = %+v, %v; want one counted attempt", record, err)
	}
	clock = record.NextAttemptAt.Add(time.Second)
	if res := mustLink(t, w, l, "breach"); res.Kind != linksfreshnessstore.LinkKindIncremental {
		t.Fatalf("rerun = %+v, want incremental", res)
	}
	if got, want := l.ledgerRows(t, "breach"), l.ledgerRows(t, "reference"); got != want {
		t.Fatal("the rerun differs from the undisturbed reference")
	}

	// RED: the same breach against the statement without the Go check.
	err = l.breachFence(t, "planted", func() error {
		tx, err := l.raw.BeginTx(l.ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(l.ctx, linksfreshnessstore.IncrementalLinkSQL, "planted", "planted-g1", "planted-g0",
			linksfreshnessstore.DigestVersion, clock); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		t.Fatalf("planted writer: %v", err)
	}
	if got, _ := l.stateDigest(t, "planted"); got == hashLinesOf(t, l, "planted") {
		t.Fatal("the planted writer without the check left correct state; the breach did not skip a delete")
	}
}

// hashLinesOf is the aggregate digest of scopeID's g1, the state a correct
// link reaches.
func hashLinesOf(t *testing.T, l *ledgerDB, scopeID string) string {
	t.Helper()
	d, _ := l.aggregateDigest(t, scopeID, scopeID+"-g1")
	return d
}
