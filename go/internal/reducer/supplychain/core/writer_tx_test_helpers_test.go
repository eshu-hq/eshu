// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"database/sql"
	"errors"
	"time"

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
	// candidates feeds the suppression diff baseline read; diffPairs feeds
	// one pair read, keyed by (prior, current) generation id; wholeGen feeds
	// the no-baseline whole-generation read (#10019).
	candidates []suppressionGeneration
	diffPairs  map[[2]string][]suppressionDiffRow
	wholeGen   []suppressionDiffRow
	// passStateFenced feeds the scope pass-state fence decision read and
	// passStateRecorded its write-outcome column (#10422). Zero values
	// report fence open, preserving every existing test's behavior.
	passStateFenced   bool
	passStateRecorded int64
}

// fakeStoredFinding is one ACTIVE fact_records row the changed-row read returns;
// the production query excludes tombstoned rows.
type fakeStoredFinding struct {
	factID       string
	generationID string
	stableKey    string
	digest       string
	cveID        string
	advisoryID   string
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
		case *time.Time:
			*d = value.(time.Time)
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
	if t.state.failOn != "" && query == t.state.failOn {
		return nil, errFakeImpactStatement
	}
	switch query {
	case listStoredSupplyChainImpactFindingsQuery:
		rows := make([][]any, 0, len(t.state.stored))
		for _, row := range t.state.stored {
			rows = append(rows, []any{row.factID, row.generationID, row.stableKey, row.digest, row.cveID, row.advisoryID, row.fencingToken})
		}
		return &fakeImpactRows{rows: rows}, nil
	case listSupplyChainImpactCVEIDsQuery:
		rows := make([][]any, 0, len(t.state.cveRows))
		for _, id := range t.state.cveRows {
			rows = append(rows, []any{id})
		}
		return &fakeImpactRows{rows: rows}, nil
	case listSupplyChainImpactGroupIDsQuery:
		rows := make([][]any, 0, len(t.state.stored)+len(t.state.cveRows))
		for _, row := range t.state.stored {
			rows = append(rows, []any{row.cveID, row.advisoryID})
		}
		for _, id := range t.state.cveRows {
			rows = append(rows, []any{id, ""})
		}
		return &fakeImpactRows{rows: rows}, nil
	case selectSuppressionCandidatesQuery:
		rows := make([][]any, 0, len(t.state.candidates))
		for _, g := range t.state.candidates {
			rows = append(rows, []any{
				g.GenerationID, g.Status, !g.ActivatedAt.IsZero(),
				g.IngestedAt, g.Admitted, g.Owed,
			})
		}
		return &fakeImpactRows{rows: rows}, nil
	case selectSuppressionDiffPairQuery:
		var key [2]string
		if len(args) == 3 {
			if prior, ok := args[2].(string); ok {
				key[0] = prior
			}
			if current, ok := args[1].(string); ok {
				key[1] = current
			}
		}
		return &fakeImpactRows{rows: suppressionDiffFakeRows(t.state.diffPairs[key])}, nil
	case selectSuppressionWholeGenerationQuery:
		return &fakeImpactRows{rows: suppressionDiffFakeRows(t.state.wholeGen)}, nil
	case setSuppressionDiffStatementTimeoutQuery:
		return &fakeImpactRows{}, nil
	case supplyChainImpactScopePassStateQuery:
		// The pass-state upsert is a control statement (not a batched fact
		// insert), so it lands on control like the lock, admission, and
		// retraction, keeping the statement-stream assertions whole.
		t.state.control.Execs = append(t.state.control.Execs, testutil.ExecCall{Query: query, Args: args})
		return &fakeImpactRows{rows: [][]any{{t.state.passStateFenced, t.state.passStateRecorded}}}, nil
	}
	return nil, errors.New("unexpected query " + query)
}

func (t fakeImpactTx) Commit() error   { t.state.commits++; return nil }
func (t fakeImpactTx) Rollback() error { t.state.rollbacks++; return nil }

// suppressionDiffFakeRows renders diff rows as the pair and whole-generation
// reads return them: suppression id, current CVE id, prior CVE id.
func suppressionDiffFakeRows(rows []suppressionDiffRow) [][]any {
	out := make([][]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, []any{row.SuppressionID, row.CurrentCVEID, row.PriorCVEID})
	}
	return out
}
