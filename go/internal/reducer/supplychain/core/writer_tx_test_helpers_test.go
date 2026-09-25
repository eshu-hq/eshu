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
}

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
	if query == factwrite.BatchInsertVersionedQuery {
		return t.inserts.ExecContext(ctx, query, args...)
	}
	return t.state.control.ExecContext(ctx, query, args...)
}

func (t fakeImpactTx) Commit() error   { t.state.commits++; return nil }
func (t fakeImpactTx) Rollback() error { t.state.rollbacks++; return nil }
