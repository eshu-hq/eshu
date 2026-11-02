// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestWinnerDataColumnsMatchTableAndSelect pins that the write-on-change column
// list is the table's column set. A column the select projects but the list
// omits would be written on insert yet never compared, so a change to it alone
// would be skipped as "unchanged" and the read model would go stale.
func TestWinnerDataColumnsMatchTableAndSelect(t *testing.T) {
	t.Parallel()

	ddl, err := os.ReadFile("migrations/033_supply_chain_impact_canonical_winners.sql")
	if err != nil {
		t.Fatalf("read winners DDL: %v", err)
	}
	_, body, ok := strings.Cut(string(ddl), "CREATE TABLE")
	if !ok {
		t.Fatal("winners DDL has no CREATE TABLE")
	}
	body, _, _ = strings.Cut(body, "\n);")
	var tableCols []string
	for _, line := range strings.Split(body, "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) > 1 && !strings.HasPrefix(fields[0], "--") && fields[0] != "PRIMARY" {
			tableCols = append(tableCols, fields[0])
		}
	}
	want := append(append([]string{"canonical_key"}, supplyChainImpactWinnerDataColumns...), "materialized_at")
	if !slices.Equal(tableCols, want) {
		t.Fatalf("winners table columns = %v\n want canonical_key + data columns + materialized_at = %v", tableCols, want)
	}

	// The select projects canonical_key and every data column, in table order.
	_, final, ok := strings.Cut(supplyChainImpactWinnerSelectSQL[strings.LastIndex(supplyChainImpactWinnerSelectSQL, "\nSELECT\n"):], "\nSELECT\n")
	if !ok {
		t.Fatal("winner select has no final SELECT")
	}
	final, _, _ = strings.Cut(final, "FROM source_winners")
	alias := regexp.MustCompile(`(?m)(?:\bAS\s+|\bsource\.)(\w+),?\s*$`)
	var selected []string
	for _, m := range alias.FindAllStringSubmatch(final, -1) {
		selected = append(selected, m[1])
	}
	if !slices.Equal(selected, want[:len(want)-1]) {
		t.Fatalf("winner select columns = %v\n want %v", selected, want[:len(want)-1])
	}
}

// TestRebuildSQLWritesOnlyChangedRows pins the write-on-change shape (#9774):
// the insert reads from the changed set, the change test covers every data
// column and not materialized_at, and the form is the anti-join, not a bare
// DO UPDATE ... WHERE, which still locks every conflicting row.
func TestRebuildSQLWritesOnlyChangedRows(t *testing.T) {
	t.Parallel()

	sqlText := rebuildSupplyChainImpactWinnersSQL
	for _, want := range []string{
		"changed AS MATERIALIZED (",
		"LEFT JOIN supply_chain_impact_canonical_winners AS w",
		"WHERE w.canonical_key IS NULL",
		") IS DISTINCT FROM ROW(",
		"FROM changed\n    ON CONFLICT (canonical_key) DO UPDATE SET",
		"materialized_at = EXCLUDED.materialized_at",
		"RETURNING\n    (SELECT count(*) FROM changed WHERE is_new),",
	} {
		if !strings.Contains(sqlText, want) {
			t.Errorf("rebuild SQL missing %q", want)
		}
	}
	if strings.Contains(sqlText, "FROM winners_now\n    ON CONFLICT") {
		t.Error("rebuild SQL inserts every winner; it must insert only the changed set")
	}
	if strings.Contains(sqlText, "DO UPDATE SET materialized_at = EXCLUDED.materialized_at\n    WHERE") ||
		regexp.MustCompile(`(?s)environments = EXCLUDED\.environments,\s+materialized_at = EXCLUDED\.materialized_at\s+WHERE`).MatchString(sqlText) {
		t.Error("rebuild SQL filters with ON CONFLICT ... WHERE, which still locks every row")
	}
	_, afterChanged, _ := strings.Cut(sqlText, "changed AS MATERIALIZED")
	compare, _, _ := strings.Cut(afterChanged, "upserted AS")
	if strings.Contains(compare, "n.materialized_at") || strings.Contains(compare, "w.materialized_at") {
		t.Error("the change test compares materialized_at, which differs every tick")
	}
	for _, col := range supplyChainImpactWinnerDataColumns {
		if !strings.Contains(compare, "n."+col+",") && !strings.Contains(compare, "n."+col+")") {
			t.Errorf("change test omits column %s", col)
		}
		if !strings.Contains(compare, "w."+col+",") && !strings.Contains(compare, "w."+col+")") {
			t.Errorf("change test omits stored column %s", col)
		}
	}
}

// winnersCountRows is a scripted cursor. err surfaces from Err once the rows run
// out, the point where a real cursor reports a failed commit. closeErr surfaces
// from Close.
type winnersCountRows struct {
	rows     [][]any
	i        int
	err      error
	closeErr error
	closed   bool
}

func (r *winnersCountRows) Next() bool {
	r.i++
	return r.i <= len(r.rows)
}

func (r *winnersCountRows) Scan(dest ...any) error {
	for i, v := range r.rows[r.i-1] {
		switch d := dest[i].(type) {
		case *int64:
			*d = v.(int64)
		case *bool:
			*d = v.(bool)
		case *sql.NullTime:
			*d = v.(sql.NullTime)
		}
	}
	return nil
}

func (r *winnersCountRows) Err() error {
	if r.i > len(r.rows) {
		return r.err
	}
	return nil
}

func (r *winnersCountRows) Close() error {
	r.closed = true
	return r.closeErr
}

// winnersFakeTx scripts the resweep transaction: the single-flight claim, then
// the rebuild statement.
type winnersFakeTx struct {
	claim      *winnersCountRows
	counts     *winnersCountRows
	claimErr   error
	rebuildErr error
	commitErr  error

	queries    []string
	args       [][]any
	committed  bool
	rolledBack bool
	openAtEnd  bool
}

func (tx *winnersFakeTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	tx.queries, tx.args = append(tx.queries, query), append(tx.args, args)
	if query == tryClaimSupplyChainImpactWinnersResweepSQL {
		return tx.claim, tx.claimErr
	}
	return tx.counts, tx.rebuildErr
}

func (tx *winnersFakeTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("the resweep must not use ExecContext")
}

func (tx *winnersFakeTx) Commit() error {
	tx.committed = true
	tx.openAtEnd = !tx.counts.closed
	return tx.commitErr
}

func (tx *winnersFakeTx) Rollback() error {
	if !tx.committed {
		tx.rolledBack = true
	}
	return nil
}

// winnersFakeDB is the store's database: a Queryer that can begin winnersFakeTx.
type winnersFakeDB struct {
	tx       *winnersFakeTx
	beginErr error
}

func (d *winnersFakeDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("the resweep must run inside its transaction")
}

func (d *winnersFakeDB) Begin(context.Context) (db.Transaction, error) {
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	return d.tx, nil
}

func winnersClaimed(got bool) *winnersCountRows {
	return &winnersCountRows{rows: [][]any{{got}}}
}

func winnersCounts(values ...any) *winnersCountRows {
	return &winnersCountRows{rows: [][]any{values}}
}

func TestResweepWinnersReportsCounts(t *testing.T) {
	t.Parallel()

	previous := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tx := &winnersFakeTx{
		claim:  winnersClaimed(true),
		counts: winnersCounts(int64(1), int64(2), int64(3), int64(4), sql.NullTime{Time: previous, Valid: true}),
	}
	stamp := previous.Add(30 * time.Second)

	got, err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.ResweepWinners(context.Background(), stamp)
	if err != nil {
		t.Fatalf("ResweepWinners() error = %v", err)
	}
	if got.Inserted != 1 || got.Updated != 2 || got.Deleted != 3 || got.Unchanged != 4 || got.Skipped || !got.PreviousWatermark.Equal(previous) {
		t.Fatalf("ResweepWinners() = %+v, want 1/2/3/4 and previous watermark %v", got, previous)
	}
	if len(tx.queries) != 2 || tx.queries[0] != tryClaimSupplyChainImpactWinnersResweepSQL || tx.queries[1] != rebuildSupplyChainImpactWinnersSQL {
		t.Fatalf("queries = %q, want the claim, then the rebuild: the claim must precede the rebuild's snapshot", tx.queries)
	}
	if len(tx.args[1]) != 1 || tx.args[1][0] != stamp {
		t.Fatalf("rebuild args = %v, want the stamp", tx.args[1])
	}
	if !tx.committed || tx.openAtEnd {
		t.Fatalf("committed = %v with the cursor open = %v, want a commit after the cursor closed", tx.committed, tx.openAtEnd)
	}

	tx = &winnersFakeTx{claim: winnersClaimed(true), counts: winnersCounts(int64(0), int64(0), int64(0), int64(0), sql.NullTime{})}
	first, err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.ResweepWinners(context.Background(), stamp)
	if err != nil || !first.PreviousWatermark.IsZero() {
		t.Fatalf("first resweep = %+v, %v; want a zero previous watermark", first, err)
	}
}

// TestResweepWinnersSkipsWhileAnotherResweepRuns pins the single-flight rule: a
// resweep that loses the claim runs no statement and commits nothing, and that
// is a normal outcome, not an error.
func TestResweepWinnersSkipsWhileAnotherResweepRuns(t *testing.T) {
	t.Parallel()

	tx := &winnersFakeTx{claim: winnersClaimed(false), counts: &winnersCountRows{}}
	got, err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.ResweepWinners(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("ResweepWinners() error = %v", err)
	}
	if !got.Skipped || got.Inserted+got.Updated+got.Deleted+got.Unchanged != 0 {
		t.Fatalf("ResweepWinners() = %+v, want only Skipped", got)
	}
	if len(tx.queries) != 1 {
		t.Fatalf("queries = %q, want only the claim", tx.queries)
	}
	if tx.committed || !tx.rolledBack {
		t.Fatalf("committed = %v rolledBack = %v, want a rollback", tx.committed, tx.rolledBack)
	}
}

// TestRebuildAllWinnersFailsWhenAnotherResweepRuns pins that the reconcile
// wrapper never reports success for a skipped resweep: a caller that asked for
// the table to be reconciled must learn that nothing was reconciled.
func TestRebuildAllWinnersFailsWhenAnotherResweepRuns(t *testing.T) {
	t.Parallel()

	tx := &winnersFakeTx{claim: winnersClaimed(false), counts: &winnersCountRows{}}
	err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.RebuildAllWinners(context.Background(), time.Now())
	if err == nil {
		t.Fatal("RebuildAllWinners() error = nil on a skipped resweep, want an error")
	}
	if !strings.Contains(err.Error(), "another resweep is in flight") {
		t.Fatalf("RebuildAllWinners() error = %q, want it to name the in-flight resweep", err)
	}
	if len(tx.queries) != 1 || tx.queries[0] != tryClaimSupplyChainImpactWinnersResweepSQL {
		t.Fatalf("queries = %q, want only the claim and no rebuild statement", tx.queries)
	}
	if tx.committed || !tx.rolledBack {
		t.Fatalf("committed = %v rolledBack = %v, want a rollback", tx.committed, tx.rolledBack)
	}
}

func TestResweepWinnersFailsLoudly(t *testing.T) {
	t.Parallel()

	good := func() *winnersCountRows {
		return winnersCounts(int64(1), int64(0), int64(0), int64(0), sql.NullTime{})
	}
	afterRow := good()
	afterRow.err = errors.New("commit failed after the row")
	closeFails := good()
	closeFails.closeErr = errors.New("close failed")
	twoRows := good()
	twoRows.rows = append(twoRows.rows, twoRows.rows[0])

	for name, tx := range map[string]*winnersFakeTx{
		"claim error":        {claimErr: errors.New("boom"), claim: &winnersCountRows{}, counts: good()},
		"no claim row":       {claim: &winnersCountRows{}, counts: good()},
		"claim cursor error": {claim: &winnersCountRows{err: errors.New("cursor")}, counts: good()},
		"rebuild error":      {claim: winnersClaimed(true), rebuildErr: context.DeadlineExceeded, counts: good()},
		"no counts row":      {claim: winnersClaimed(true), counts: &winnersCountRows{}},
		"cursor error":       {claim: winnersClaimed(true), counts: &winnersCountRows{err: errors.New("cursor")}},
		"error after row":    {claim: winnersClaimed(true), counts: afterRow},
		"close error":        {claim: winnersClaimed(true), counts: closeFails},
		"extra counts row":   {claim: winnersClaimed(true), counts: twoRows},
		"commit error":       {claim: winnersClaimed(true), counts: good(), commitErr: errors.New("commit")},
	} {
		got, err := (SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}).ResweepWinners(context.Background(), time.Now())
		if err == nil {
			t.Errorf("%s: ResweepWinners() = %+v, error = nil, want an error", name, got)
		}
		if got != (supplychaincore.SupplyChainImpactWinnersResweep{}) {
			t.Errorf("%s: ResweepWinners() = %+v on failure, want the zero value", name, got)
		}
		if name != "commit error" && tx.committed {
			t.Errorf("%s: committed a failed resweep", name)
		}
	}
	if _, err := (SupplyChainImpactWinnersStore{DB: &winnersFakeDB{beginErr: errors.New("begin")}}).ResweepWinners(context.Background(), time.Now()); err == nil {
		t.Error("begin error: ResweepWinners() error = nil, want an error")
	}
	if _, err := (SupplyChainImpactWinnersStore{DB: winnersReadOnlyDB{}}).ResweepWinners(context.Background(), time.Now()); err == nil {
		t.Error("no transactions: ResweepWinners() error = nil, want an error")
	}
	if err := (SupplyChainImpactWinnersStore{}).RebuildAllWinners(context.Background(), nil); err == nil {
		t.Error("RebuildAllWinners() with no database error = nil, want an error")
	}
}

// winnersReadOnlyDB cannot begin a transaction.
type winnersReadOnlyDB struct{}

func (winnersReadOnlyDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("not reached")
}
