// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	pgstore "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type emptyStatusRows struct{}

func (emptyStatusRows) Next() bool        { return false }
func (emptyStatusRows) Scan(...any) error { return errors.New("unexpected scan") }
func (emptyStatusRows) Err() error        { return nil }
func (emptyStatusRows) Close() error      { return nil }

type emptyStatusTx struct {
	*snapshotTxStub
	queries   int
	faultAt   int
	faultRows db.Rows
	faultErr  error
}

// QueryContext counts only status statements; the transaction's
// SET LOCAL jit = off goes to the embedded stub, which records it.
func (s *emptyStatusTx) QueryContext(ctx context.Context, statement string, args ...any) (db.Rows, error) {
	if statement == statusSnapshotJITOffSQL {
		return s.snapshotTxStub.QueryContext(ctx, statement, args...)
	}
	s.queries++
	if s.commits != 0 {
		return nil, errors.New("query after commit")
	}
	if s.queries == s.faultAt {
		if s.faultErr != nil {
			return nil, s.faultErr
		}
		return s.faultRows, nil
	}
	return emptyStatusRows{}, nil
}

func TestSnapshotStatusReaderRunsRealEmptyStatusStoreOnOneTransaction(t *testing.T) {
	tx := &emptyStatusTx{snapshotTxStub: &snapshotTxStub{}}
	store := snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }}
	reader := NewSnapshotStatusReader(store, func(q db.Queryer) status.Reader {
		return pgstore.NewStatusStore(q)
	}, nil)
	asOf := time.Now()
	raw, err := reader.ReadStatusSnapshotFiltered(t.Context(), asOf, status.SnapshotSelection{})
	if err != nil {
		t.Fatalf("empty filtered status: %v", err)
	}
	if !raw.AsOf.Equal(asOf) || tx.queries < 5 || tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("raw.AsOf=%v queries=%d commit=%d rollback=%d", raw.AsOf, tx.queries, tx.commits, tx.rollbacks)
	}
	if len(tx.statements) != 1 || tx.statements[0] != statusSnapshotJITOffSQL {
		t.Fatalf("control statements=%q, want one %q", tx.statements, statusSnapshotJITOffSQL)
	}
}

type oneBadStatusRow struct {
	yielded bool
	scanErr error
	decode  bool
}

func (r *oneBadStatusRow) Next() bool {
	if r.yielded {
		return false
	}
	r.yielded = true
	return true
}

func (r *oneBadStatusRow) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if r.decode {
		*dest[0].(*string) = "queue"
		*dest[1].(*int64) = 1
		*dest[2].(*string) = "not-json"
	}
	return nil
}
func (*oneBadStatusRow) Err() error   { return nil }
func (*oneBadStatusRow) Close() error { return nil }

func TestSnapshotStatusReaderRealStoreQueryScanDecodeErrorsRollback(t *testing.T) {
	queryErr := errors.New("query failed")
	scanErr := errors.New("scan failed")
	for _, tc := range []struct {
		name           string
		at             int
		rows           db.Rows
		queryErr, want error
	}{
		{"query", 1, nil, queryErr, queryErr},
		{"scan", 1, &oneBadStatusRow{scanErr: scanErr}, nil, scanErr},
		{"decode", 4, &oneBadStatusRow{decode: true}, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &emptyStatusTx{snapshotTxStub: &snapshotTxStub{}, faultAt: tc.at, faultRows: tc.rows, faultErr: tc.queryErr}
			reader := NewSnapshotStatusReader(snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }},
				func(q db.Queryer) status.Reader { return pgstore.NewStatusStore(q) }, nil)
			raw, err := reader.ReadStatusSnapshotFiltered(t.Context(), time.Now(), status.SnapshotSelection{})
			if !raw.AsOf.IsZero() || err == nil || tx.queries != tc.at || tx.commits != 0 || tx.rollbacks != 1 {
				t.Fatalf("raw=%v error=%v queries=%d commit=%d rollback=%d", raw.AsOf, err, tx.queries, tx.commits, tx.rollbacks)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error=%v, want %v", err, tc.want)
			}
		})
	}
}

func TestSnapshotStatusReaderSemanticOnlyKeepsGuardedTransaction(t *testing.T) {
	t.Parallel()
	tx := &emptyStatusTx{snapshotTxStub: &snapshotTxStub{}}
	store := snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }}
	reader := NewSnapshotStatusReader(store, func(q db.Queryer) status.Reader {
		return pgstore.NewStatusStore(q)
	}, nil)
	asOf := time.Now()
	raw, err := reader.ReadStatusSnapshotFiltered(t.Context(), asOf, status.SemanticOnlySnapshotSelection())
	if err != nil {
		t.Fatal(err)
	}
	if !raw.AsOf.Equal(asOf) || tx.queries != 1 || tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("asOf=%v queries=%d commit=%d rollback=%d", raw.AsOf, tx.queries, tx.commits, tx.rollbacks)
	}
}

func TestSnapshotStatusReaderSemanticOnlyErrorRollsBack(t *testing.T) {
	t.Parallel()
	failure := errors.New("semantic SQL failed")
	tx := &emptyStatusTx{snapshotTxStub: &snapshotTxStub{}, faultAt: 1, faultErr: failure}
	reader := NewSnapshotStatusReader(
		snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }},
		func(q db.Queryer) status.Reader { return pgstore.NewStatusStore(q) }, nil,
	)
	raw, err := reader.ReadStatusSnapshotFiltered(t.Context(), time.Now(), status.SemanticOnlySnapshotSelection())
	if !errors.Is(err, failure) || !raw.AsOf.IsZero() || tx.queries != 1 || tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("raw=%+v err=%v queries=%d commit=%d rollback=%d", raw, err, tx.queries, tx.commits, tx.rollbacks)
	}
}

func TestSnapshotStatusReaderRejectsUnknownSelectionBeforeBegin(t *testing.T) {
	t.Parallel()
	began := false
	reader := NewSnapshotStatusReader(
		snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) {
			began = true
			return nil, nil
		}},
		func(q db.Queryer) status.Reader { return pgstore.NewStatusStore(q) }, nil,
	)
	_, err := reader.ReadStatusSnapshotFiltered(t.Context(), time.Now(), status.SnapshotSelection{Mode: "unknown"})
	if err == nil || began {
		t.Fatalf("err=%v began=%v; want rejected before transaction", err, began)
	}
}

// activeWorkSummaryGatePrefix opens the gated active-work summary statement
// (#7009 S5 variant a2v): its first CTE reads pg_stats to pick a branch.
const activeWorkSummaryGatePrefix = "\nWITH fact_work_summary_mode AS MATERIALIZED ("

// orderedStatusTx logs every statement of one status snapshot in order: the
// JIT-off SET, the gated active-work summary, and every other read by its
// query-summary label.
type orderedStatusTx struct {
	*snapshotTxStub
	log []string
}

func (s *orderedStatusTx) QueryContext(ctx context.Context, statement string, args ...any) (db.Rows, error) {
	switch {
	case statement == statusSnapshotJITOffSQL:
		s.log = append(s.log, "jit-off")
		return s.snapshotTxStub.QueryContext(ctx, statement, args...)
	case db.QuerySummaryFromContext(ctx) == "active_work_summary" && strings.HasPrefix(statement, activeWorkSummaryGatePrefix):
		s.log = append(s.log, "gated-summary")
	default:
		s.log = append(s.log, db.QuerySummaryFromContext(ctx))
	}
	return emptyStatusRows{}, nil
}

// TestStatusSnapshotDisablesJITBeforeTheGatedActiveWorkSummary binds the
// #7009 PR-2 dependency in code (S5 ruling D4.4): every a2v measurement ran
// with jit=off and the gated statement costs above the default
// jit_above_cost, so the status snapshot must issue SET LOCAL jit = off once,
// before any status statement and in particular before the gated summary,
// which the real StatusStore runs on the same transaction.
func TestStatusSnapshotDisablesJITBeforeTheGatedActiveWorkSummary(t *testing.T) {
	tx := &orderedStatusTx{snapshotTxStub: &snapshotTxStub{}}
	reader := NewSnapshotStatusReader(snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }},
		func(q db.Queryer) status.Reader { return pgstore.NewStatusStore(q) }, nil)
	if _, err := reader.ReadStatusSnapshotFiltered(t.Context(), time.Now(), status.FullSnapshotSelection()); err != nil {
		t.Fatalf("status snapshot: %v", err)
	}
	jitAt, summaryAt, jitCount, summaryCount := -1, -1, 0, 0
	for i, entry := range tx.log {
		switch entry {
		case "jit-off":
			jitAt, jitCount = i, jitCount+1
		case "gated-summary":
			summaryAt, summaryCount = i, summaryCount+1
		}
	}
	if jitCount != 1 || summaryCount != 1 {
		t.Fatalf("statements %q: want one JIT-off SET and one gated active-work summary", tx.log)
	}
	if jitAt != 0 || jitAt > summaryAt {
		t.Fatalf("statements %q: SET LOCAL jit = off must run first, before the gated summary", tx.log)
	}
	if tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("commit=%d rollback=%d, want one commit", tx.commits, tx.rollbacks)
	}
}
