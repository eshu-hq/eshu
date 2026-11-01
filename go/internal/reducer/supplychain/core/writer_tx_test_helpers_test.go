// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"database/sql"
	"errors"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

// fakeImpactBeginner adapts a testutil.FakeExecer into the writer's
// transaction surface. Batched fact inserts land on inserts, so the existing
// batched-insert assertions keep reading exactly the insert statements; every
// other statement (the conflict-domain lock and the retraction) lands on
// control. Every statement is also appended to all, in order.
type fakeImpactBeginner struct {
	inserts *testutil.FakeExecer
	state   *fakeImpactTxState
}

type fakeImpactTxState struct {
	control   testutil.FakeExecer
	all       []testutil.ExecCall
	commits   int
	rollbacks int
	// failOn fails the first statement whose query equals it.
	failOn string
	// rowsAffected overrides the RowsAffected the fake reports for a query, so a
	// test can make the fencing admission report a rejection (0 rows).
	rowsAffected map[string]int64
	// stored feeds the changed-row read inside the write transaction; cveRows
	// feeds the recovery CVE read. A nil queryErr means the reads succeed.
	stored   []fakeStoredFinding
	cveRows  []string
	queryErr error
}

// fakeStoredFinding is one ACTIVE fact_records row the changed-row read returns;
// the production query excludes tombstoned rows.
type fakeStoredFinding struct {
	factID       string
	generationID string
	stableKey    string
	digest       string
	cveID        string
	fencingToken int64
}

// fakeImpactRows is a SupplyChainImpactRows over fixed rows of scan values.
type fakeImpactRows struct {
	rows [][]any
	at   int
}

func (r *fakeImpactRows) Next() bool   { r.at++; return r.at <= len(r.rows) }
func (r *fakeImpactRows) Err() error   { return nil }
func (r *fakeImpactRows) Close() error { return nil }
func (r *fakeImpactRows) Scan(dest ...any) error {
	for i, value := range r.rows[r.at-1] {
		switch d := dest[i].(type) {
		case *string:
			*d = value.(string)
		case *bool:
			*d = value.(bool)
		case *int64:
			*d = value.(int64)
		}
	}
	return nil
}

// fakeImpactResult is a sql.Result reporting a fixed affected-row count.
type fakeImpactResult int64

func (fakeImpactResult) LastInsertId() (int64, error)   { return 0, nil }
func (r fakeImpactResult) RowsAffected() (int64, error) { return int64(r), nil }

func newFakeImpactBeginner(inserts *testutil.FakeExecer) fakeImpactBeginner {
	return fakeImpactBeginner{inserts: inserts, state: &fakeImpactTxState{}}
}

func (b fakeImpactBeginner) BeginSupplyChainImpactTx(context.Context) (SupplyChainImpactTx, error) {
	return fakeImpactTx(b), nil
}

type fakeImpactTx fakeImpactBeginner

var errFakeImpactStatement = errors.New("injected statement failure")

func (t fakeImpactTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	t.state.all = append(t.state.all, testutil.ExecCall{Query: query, Args: args})
	if t.state.failOn != "" && query == t.state.failOn {
		return nil, errFakeImpactStatement
	}
	if n, ok := t.state.rowsAffected[query]; ok {
		return fakeImpactResult(n), nil
	}
	if query == factwrite.BatchInsertVersionedQuery {
		return t.inserts.ExecContext(ctx, query, args...)
	}
	return t.state.control.ExecContext(ctx, query, args...)
}

func (t fakeImpactTx) QueryContext(_ context.Context, query string, args ...any) (SupplyChainImpactRows, error) {
	t.state.all = append(t.state.all, testutil.ExecCall{Query: query, Args: args})
	if t.state.queryErr != nil {
		return nil, t.state.queryErr
	}
	switch query {
	case listStoredSupplyChainImpactFindingsQuery:
		rows := make([][]any, 0, len(t.state.stored))
		for _, row := range t.state.stored {
			rows = append(rows, []any{row.factID, row.generationID, row.stableKey, row.digest, row.cveID, row.fencingToken})
		}
		return &fakeImpactRows{rows: rows}, nil
	case listSupplyChainImpactCVEIDsQuery:
		rows := make([][]any, 0, len(t.state.cveRows))
		for _, id := range t.state.cveRows {
			rows = append(rows, []any{id})
		}
		return &fakeImpactRows{rows: rows}, nil
	}
	return nil, errors.New("unexpected query " + query)
}

func (t fakeImpactTx) Commit() error   { t.state.commits++; return nil }
func (t fakeImpactTx) Rollback() error { t.state.rollbacks++; return nil }
