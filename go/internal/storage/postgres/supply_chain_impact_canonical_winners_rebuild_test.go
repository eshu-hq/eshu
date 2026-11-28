// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
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
	// Migration 331 (#10880) appends payload and source_confidence;
	// logically they sit with the other data columns, before
	// materialized_at, which is how the data-column list orders them.
	alt, err := os.ReadFile("migrations/331_supply_chain_impact_winners_payload.sql")
	if err != nil {
		t.Fatalf("read migration 331: %v", err)
	}
	var added []string
	for _, m := range regexp.MustCompile(`ADD COLUMN IF NOT EXISTS (\w+)`).FindAllStringSubmatch(string(alt), -1) {
		added = append(added, m[1])
	}
	tableCols = append(tableCols[:len(tableCols)-1], append(added, "materialized_at")...)
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

// TestFullChunkApplyWritesOnlyChangedRows pins the write-on-change shape
// (#9774) of each full-rebuild chunk: the insert reads from the changed set,
// the change test covers every data column and not materialized_at, and the
// form is the anti-join, not a bare DO UPDATE ... WHERE, which still locks
// every conflicting row.
func TestFullChunkApplyWritesOnlyChangedRows(t *testing.T) {
	t.Parallel()

	body := applyWinnersFullChunkBodySQL
	for _, want := range []string{
		"changed AS MATERIALIZED (",
		"LEFT JOIN supply_chain_impact_canonical_winners AS w",
		"WHERE w.canonical_key IS NULL",
		") IS DISTINCT FROM ROW(",
		"FROM changed\n    ON CONFLICT (canonical_key) DO UPDATE SET",
		"materialized_at = EXCLUDED.materialized_at",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("full chunk apply SQL missing %q", want)
		}
	}
	if strings.Contains(body, "FROM winners_now\n    ON CONFLICT") {
		t.Error("full chunk apply SQL inserts every winner; it must insert only the changed set")
	}
	if strings.Contains(body, "DO UPDATE SET materialized_at = EXCLUDED.materialized_at\n    WHERE") ||
		regexp.MustCompile(`(?s)environments = EXCLUDED\.environments,\s+materialized_at = EXCLUDED\.materialized_at\s+WHERE`).MatchString(body) {
		t.Error("full chunk apply SQL filters with ON CONFLICT ... WHERE, which still locks every row")
	}
	_, afterChanged, _ := strings.Cut(body, "changed AS MATERIALIZED")
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
	// The chunk reports the keyed apply's first four counts; the watermark
	// stamps once, after every chunk, never per chunk.
	for _, want := range []string{
		"(SELECT count(*) FROM changed WHERE is_new),",
		"(SELECT count(*) FROM changed WHERE NOT is_new),",
		"(SELECT count(*) FROM deleted),",
		"(SELECT count(*) FROM winners_now) - (SELECT count(*) FROM changed)",
	} {
		if !strings.Contains(applyWinnersFullChunkSQL, want) {
			t.Errorf("full chunk apply SQL missing count %q", want)
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
		case *sql.NullInt64:
			*d = v.(sql.NullInt64)
		case *time.Time:
			// The final stamp reports its watermark for arity only: a NULL
			// reads as the zero time.
			switch t := v.(type) {
			case time.Time:
				*d = t
			case sql.NullTime:
				if t.Valid {
					*d = t.Time
				}
			default:
				return fmt.Errorf("bad time value %T", v)
			}
		case *sql.NullString:
			*d = v.(sql.NullString)
		case *string:
			*d = v.(string)
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

// winnersFakeTx scripts the resweep transaction: the single-flight claim, the
// DB clock read, the dirty-log and staging drains (#11117), the pre-stamp
// read, the supports-ready gate, the chunked walk (identifier pages and
// per-chunk applies), the final stamp, the residual counts and the committed
// watermark read.
type winnersFakeTx struct {
	claim      *winnersCountRows
	counts     *winnersCountRows
	claimErr   error
	rebuildErr error
	commitErr  error
	// clock and watermark are the DB clock and the committed watermark the
	// fake reports; zero values report a fixed default.
	clock     time.Time
	watermark time.Time
	// logDrain and stagingDrain are the (source, marks) rows each drain
	// returns; drainErr fails the log drain.
	logDrain     [][]any
	stagingDrain [][]any
	drainErr     error
	// pinnedFirst is true when the isolation pin ran before any query.
	pinnedFirst bool
	// storedContract is the (version, hash) row the guarded full reads (nil:
	// no row); contractStamps records each #11117 contract stamp's args.
	storedContract []any
	contractStamps [][]any
	// pre scripts the pre-stamp watermark the rebuild replaces (hasPre), or
	// an empty table on the first sweep (preEmpty). Without either, the
	// pre-read falls back to the committed watermark behavior below.
	pre      time.Time
	hasPre   bool
	preEmpty bool
	wmReads  int
	// supportsReady scripts the full-chunk supports-ready gate; supportsReadyErr
	// fails the gate read, which fails the full closed onto the fact_records probe.
	supportsReady    bool
	supportsReadyErr error
	// pages scripts the identifier pages the chunk walk reads, one entry per
	// page call; a missing entry reads as an empty page. Each row is one
	// five-tuple. A nil script reads one default page, so a test that only
	// needs the full to run scripts nothing.
	pages [][]winnersKeyedChunkIdent
	// pageCalls counts the page reads served.
	pageCalls int
	// applies scripts the per-chunk apply count cursors, popped in order; an
	// empty script reports one (1,0,0,0) chunk.
	applies []*winnersCountRows
	// stampValue is the watermark the final stamp reports (arity only).
	stampValue time.Time
	// issued tracks the chunk-walk cursors so Commit can prove every one
	// closed before the transaction committed.
	issued []interface{ Closed() bool }
	// execs records the Exec statements in order, so the walk setup and the
	// per-chunk probe, build and stage order is pinned alongside the queries.
	execs []string

	queries    []string
	args       [][]any
	committed  bool
	rolledBack bool
	openAtEnd  bool
}

// winnersPageRows is a scripted identifier page: five text columns per row.
type winnersPageRows struct {
	rows   [][]any
	i      int
	closed bool
}

func (r *winnersPageRows) Next() bool {
	r.i++
	return r.i <= len(r.rows)
}

func (r *winnersPageRows) Scan(dest ...any) error {
	for i, v := range r.rows[r.i-1] {
		s, ok := v.(string)
		if !ok {
			return errors.New("page row is not a string five-tuple")
		}
		p, ok := dest[i].(*string)
		if !ok {
			return errors.New("page destination is not a string")
		}
		*p = s
	}
	return nil
}

func (r *winnersPageRows) Err() error { return nil }

func (r *winnersPageRows) Close() error {
	r.closed = true
	return nil
}

// Closed reports whether the page cursor closed before commit.
func (r *winnersPageRows) Closed() bool { return r.closed }

// Closed reports whether the counts cursor closed before commit.
func (r *winnersCountRows) Closed() bool { return r.closed }

func (tx *winnersFakeTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	tx.queries, tx.args = append(tx.queries, query), append(tx.args, args)
	switch query {
	case tryClaimSupplyChainImpactWinnersResweepSQL:
		return tx.claim, tx.claimErr
	case selectWinnersKeyedTickClockSQL:
		return winnersCounts(tx.clockOrDefault()), nil
	case drainWinnersDirtyLogForFullSQL:
		return &winnersCountRows{rows: tx.logDrain}, tx.drainErr
	case drainWinnersStagingForFullSQL:
		return &winnersCountRows{rows: tx.stagingDrain}, nil
	case countWinnersKeyedRemainingSQL:
		return winnersCounts(int64(0)), nil
	case selectWinnersStagingResidualSQL:
		return winnersCounts(int64(0), sql.NullInt64{}, sql.NullInt64{}, sql.NullTime{}), nil
	case selectSupplyChainImpactWinnersContractSQL:
		if tx.storedContract == nil {
			return &winnersCountRows{}, nil
		}
		return &winnersCountRows{rows: [][]any{tx.storedContract}}, nil
	case selectWinnersWatermarkSQL:
		tx.wmReads++
		if tx.wmReads == 1 && (tx.hasPre || tx.preEmpty) {
			if tx.preEmpty {
				return &winnersCountRows{}, nil
			}
			return winnersCounts(tx.pre), nil
		}
		if tx.watermark.IsZero() {
			return winnersCounts(tx.clockOrDefault()), nil
		}
		return winnersCounts(tx.watermark), nil
	case supplyChainImpactSupportsReadySQL:
		if tx.supportsReadyErr != nil {
			return nil, tx.supportsReadyErr
		}
		return winnersCounts(tx.supportsReady), nil
	case selectWinnersFullChunkPageSQL:
		var rows [][]any
		page := []winnersKeyedChunkIdent{{pkg: "pkg", repo: "repo", digest: "digest", c0: "c0", advisory: "advisory"}}
		if tx.pages != nil {
			page = nil
			if tx.pageCalls < len(tx.pages) {
				page = tx.pages[tx.pageCalls]
			}
		}
		tx.pageCalls++
		for _, id := range page {
			rows = append(rows, []any{id.pkg, id.repo, id.digest, id.c0, id.advisory})
		}
		cur := &winnersPageRows{rows: rows}
		tx.issued = append(tx.issued, cur)
		return cur, nil
	case applyWinnersFullChunkSQL:
		var cur *winnersCountRows
		if len(tx.applies) > 0 {
			cur, tx.applies = tx.applies[0], tx.applies[1:]
		} else {
			cur = winnersCounts(int64(1), int64(0), int64(0), int64(0))
		}
		tx.issued = append(tx.issued, cur)
		return cur, tx.rebuildErr
	case stampWinnersFullWatermarkSQL:
		cur := winnersCounts(sql.NullTime{Time: tx.stampValue, Valid: !tx.stampValue.IsZero()})
		tx.issued = append(tx.issued, cur)
		return cur, nil
	}
	// #10876: the resweep commit is the providing save for the
	// winners-watermark wait kind, so it wakes parked waits in the same
	// transaction. No waits are parked in these fixtures, so the wake is a
	// no-op (0, 0). Matched by the waiting-table name plus the reopen CTE
	// so the claim and rebuild arms above keep their exact-match
	// discipline and a genuinely unknown statement still reaches the
	// scripted counts cursor below.
	if strings.Contains(query, "reducer_waiting_items") && strings.Contains(query, "reopened") {
		return winnersCounts(int64(0), int64(0)), nil
	}
	return tx.counts, tx.rebuildErr
}

// winnersFakeClock is the DB clock the fake reports when a test sets none.
var winnersFakeClock = time.Date(2026, 11, 21, 6, 0, 0, 0, time.UTC)

func (tx *winnersFakeTx) clockOrDefault() time.Time {
	if tx.clock.IsZero() {
		return winnersFakeClock
	}
	return tx.clock
}

// ExecContext accepts the #11117 isolation pin (recording whether it ran
// first), the chunked-walk DDL and probe statements, the contract stamp, and
// the #11466 enrichment first-carry claims (which return no rows back: the
// stamp table is the observable, like the pin and the stamp); any other Exec
// is a fake mismatch. The chunk walk never reads RowsAffected off these
// results, so they carry none.
func (tx *winnersFakeTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	tx.execs = append(tx.execs, query)
	switch query {
	case pinWinnersFullReadCommittedSQL:
		tx.pinnedFirst = len(tx.queries) == 0
	case stampSupplyChainImpactWinnersContractSQL:
		tx.contractStamps = append(tx.contractStamps, args)
	case claimSupplyChainImpactWinnersEnrichmentCarryFullSQL,
		claimSupplyChainImpactWinnersEnrichmentCarryKeyedSQL:
	case dropWinnersFullChunkTempSQL,
		buildWinnersFullIdentsSQL,
		analyzeWinnersFullIdentsSQL,
		createWinnersKeyedIdentsSQL,
		createWinnersKeyedNarrowSQL,
		createWinnersKeyedActiveFactsNarrowSQL,
		clearWinnersKeyedIdentsSQL,
		clearWinnersKeyedNarrowSQL,
		clearWinnersKeyedActiveFactsSQL,
		insertWinnersKeyedNarrowSupportsFullChunkSQL,
		insertWinnersKeyedNarrowPresenceFullChunkSQL,
		analyzeWinnersKeyedNarrowSQL,
		insertWinnersFullChunkActiveFactsSQL,
		analyzeWinnersKeyedActiveFactsSQL,
		addWinnersKeyedOperatorFactsNarrowFromSupportsSQL:
	default:
		if !strings.HasPrefix(query, insertWinnersKeyedIdentsPrefix) {
			return nil, errors.New("the resweep must not use ExecContext")
		}
	}
	return nil, nil
}

func (tx *winnersFakeTx) Commit() error {
	tx.committed = true
	for _, cur := range tx.issued {
		if !cur.Closed() {
			tx.openAtEnd = true
		}
	}
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
	clock := previous.Add(30 * time.Second)
	committed := clock.Add(-time.Second)
	chunk := []winnersKeyedChunkIdent{
		{pkg: "a", repo: "r", digest: "d", c0: "CVE-2026-1", advisory: ""},
		{pkg: "b", repo: "r", digest: "d", c0: "CVE-2026-2", advisory: ""},
	}
	tx := &winnersFakeTx{
		claim:         winnersClaimed(true),
		supportsReady: true,
		pages:         [][]winnersKeyedChunkIdent{chunk},
		applies:       []*winnersCountRows{winnersCounts(int64(1), int64(2), int64(3), int64(4))},
		stampValue:    committed,
		clock:         clock,
		watermark:     committed,
		pre:           previous,
		hasPre:        true,
		logDrain:      [][]any{{"fact_upsert", int64(5)}, {"activation", int64(2)}},
		stagingDrain:  [][]any{{"scanner", int64(3)}, {"fact_upsert", int64(1)}},
	}

	got, err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.ResweepWinners(context.Background())
	if err != nil {
		t.Fatalf("ResweepWinners() error = %v", err)
	}
	if got.Inserted != 1 || got.Updated != 2 || got.Deleted != 3 || got.Unchanged != 4 || got.Skipped || !got.PreviousWatermark.Equal(previous) {
		t.Fatalf("ResweepWinners() = %+v, want 1/2/3/4 and previous watermark %v", got, previous)
	}
	if got.Path != telemetry.SupplyChainImpactWinnersPathFull || !got.Watermark.Equal(committed) {
		t.Fatalf("ResweepWinners() path %q watermark %v, want full and the committed watermark %v", got.Path, got.Watermark, committed)
	}
	if got.Identifiers != 11 || got.TriggerCaptured != (supplychaincore.WinnersTriggerCaptured{Activation: 2, Scanner: 3}) {
		t.Fatalf("ResweepWinners() drained %d marks, trigger %+v; want 11 and activation 2, scanner 3", got.Identifiers, got.TriggerCaptured)
	}
	// #11117: the clock is read, the log and staging are drained, then the
	// chunk runs on a later snapshot; the final stamp, the residual counts
	// and the committed watermark follow, and the #10876 wake closes the
	// transaction. The pre-stamp watermark is read before the first chunk
	// statement, since the chunks' writes must never be visible to it. The
	// two-ident page is short of the 200-identifier bound, so one page and
	// one chunk end the walk with no second page.
	want := []string{
		tryClaimSupplyChainImpactWinnersResweepSQL,
		selectWinnersKeyedTickClockSQL,
		drainWinnersDirtyLogForFullSQL,
		drainWinnersStagingForFullSQL,
		selectWinnersWatermarkSQL,
		supplyChainImpactSupportsReadySQL,
		selectWinnersFullChunkPageSQL,
		applyWinnersFullChunkSQL,
		stampWinnersFullWatermarkSQL,
		countWinnersKeyedRemainingSQL,
		selectWinnersStagingResidualSQL,
		selectWinnersWatermarkSQL,
	}
	if len(tx.queries) != len(want)+1 || !slices.Equal(tx.queries[:len(want)], want) {
		t.Fatalf("queries = %q, want claim, clock, log drain, staging drain, pre-stamp read, gate, page, apply, stamp, residual, watermark, wake", tx.queries)
	}
	if wake := tx.queries[len(want)]; !strings.Contains(wake, "reducer_waiting_items") || !strings.Contains(wake, "reopened") {
		t.Fatalf("last query = %q, want the winners-watermark wake", wake)
	}
	// The chunk walk creates its temps once, then clears, stages, probes,
	// builds and applies one page. The gate is open, so the probe is the
	// triple-guarded presence probe over supports. The isolation pin runs
	// first; the enrichment first-carry claim and the contract stamp close
	// the walk.
	wantExecs := []string{
		pinWinnersFullReadCommittedSQL,
		dropWinnersFullChunkTempSQL,
		buildWinnersFullIdentsSQL,
		analyzeWinnersFullIdentsSQL,
		createWinnersKeyedIdentsSQL,
		createWinnersKeyedNarrowSQL,
		createWinnersKeyedActiveFactsNarrowSQL,
		clearWinnersKeyedIdentsSQL,
		"",
		clearWinnersKeyedNarrowSQL,
		insertWinnersKeyedNarrowPresenceFullChunkSQL,
		analyzeWinnersKeyedNarrowSQL,
		clearWinnersKeyedActiveFactsSQL,
		insertWinnersFullChunkActiveFactsSQL,
		analyzeWinnersKeyedActiveFactsSQL,
		addWinnersKeyedOperatorFactsNarrowFromSupportsSQL,
		claimSupplyChainImpactWinnersEnrichmentCarryFullSQL,
		stampSupplyChainImpactWinnersContractSQL,
	}
	if len(tx.execs) != len(wantExecs) {
		t.Fatalf("execs = %q, want the walk setup plus one full chunk", tx.execs)
	}
	for i, wantExec := range wantExecs {
		if i == 8 {
			if !strings.HasPrefix(tx.execs[i], insertWinnersKeyedIdentsPrefix) {
				t.Fatalf("execs[%d] = %q, want the staged chunk page", i, tx.execs[i])
			}
			continue
		}
		if tx.execs[i] != wantExec {
			t.Fatalf("execs[%d] = %q, want %q", i, tx.execs[i], wantExec)
		}
	}
	if !tx.pinnedFirst {
		t.Fatal("the full did not pin READ COMMITTED as its first statement")
	}
	stampIdx := slices.Index(tx.queries, stampWinnersFullWatermarkSQL)
	if stampIdx < 0 || len(tx.args[stampIdx]) != 1 || tx.args[stampIdx][0] != clock {
		t.Fatalf("stamp args = %v, want the DB clock %v", tx.args, clock)
	}
	if !tx.committed || tx.openAtEnd {
		t.Fatalf("committed = %v with a cursor open = %v, want a commit after every cursor closed", tx.committed, tx.openAtEnd)
	}
	if len(tx.contractStamps) != 1 || tx.contractStamps[0][0] != SupplyChainImpactWinnersContractVersion || tx.contractStamps[0][1] != supplyChainImpactWinnersContractHash {
		t.Fatalf("contract stamps = %v, want one stamp of version %d and the binary hash", tx.contractStamps, SupplyChainImpactWinnersContractVersion)
	}
	if tx.pageCalls != 1 {
		t.Fatalf("page calls = %d, want the single short page", tx.pageCalls)
	}

	tx = &winnersFakeTx{claim: winnersClaimed(true), preEmpty: true}
	first, err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.ResweepWinners(context.Background())
	if err != nil || !first.PreviousWatermark.IsZero() {
		t.Fatalf("first resweep = %+v, %v; want a zero previous watermark", first, err)
	}
}

// TestResweepWinnersFallsBackToSupportsExpressionProbeWhenSupportsNotReady pins
// the ruled gate: a closed (or unreadable) supports gate runs the chunk probe
// over supports with the payload expression instead of refusing, and an open
// gate runs the triple-guarded presence probe over supports. Both variants
// read supports; the flip is the probe shape, never the row source.
func TestResweepWinnersFallsBackToSupportsExpressionProbeWhenSupportsNotReady(t *testing.T) {
	t.Parallel()

	oneIdent := [][]winnersKeyedChunkIdent{{{pkg: "a", repo: "r", digest: "d", c0: "c", advisory: ""}}}
	for name, tx := range map[string]*winnersFakeTx{
		"closed":     {claim: winnersClaimed(true), pages: oneIdent},
		"unreadable": {claim: winnersClaimed(true), supportsReadyErr: errors.New("gate read failed"), pages: oneIdent},
	} {
		got, err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.ResweepWinners(context.Background())
		if err != nil || got.Skipped {
			t.Fatalf("%s: ResweepWinners() = %+v, %v; want the full to run on the fallback probe", name, got, err)
		}
		foundFallback, foundPresence := false, false
		for _, exec := range tx.execs {
			if exec == insertWinnersKeyedNarrowSupportsFullChunkSQL {
				foundFallback = true
			}
			if exec == insertWinnersKeyedNarrowPresenceFullChunkSQL {
				foundPresence = true
			}
		}
		if !foundFallback || foundPresence {
			t.Fatalf("%s: supports-expression probe %v, presence probe %v; want only the fallback probe", name, foundFallback, foundPresence)
		}
	}

	openTx := &winnersFakeTx{
		claim: winnersClaimed(true), supportsReady: true,
		pages: [][]winnersKeyedChunkIdent{{{pkg: "a", repo: "r", digest: "d", c0: "c", advisory: ""}}},
	}
	openStore := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: openTx}}
	if _, err := openStore.ResweepWinners(context.Background()); err != nil {
		t.Fatalf("open gate ResweepWinners() error = %v", err)
	}
	foundPresence := false
	for _, exec := range openTx.execs {
		if exec == insertWinnersKeyedNarrowPresenceFullChunkSQL {
			foundPresence = true
		}
		if exec == insertWinnersKeyedNarrowSupportsFullChunkSQL {
			t.Fatal("open gate ran the fallback probe; want the presence probe")
		}
	}
	if !foundPresence {
		t.Fatal("open gate ran no presence probe; want the triple-guarded probe over supports")
	}
}

// TestResweepWinnersUnlessCurrentSkipsACurrentContract pins the #11117 guard:
// under the resweep lock the guarded full re-reads the stored contract, and a
// version at or above the binary's runs nothing past that read; a NULL or lower
// version runs the full and stamps the binary's contract.
func TestResweepWinnersUnlessCurrentSkipsACurrentContract(t *testing.T) {
	t.Parallel()

	for _, stored := range []int64{SupplyChainImpactWinnersContractVersion, SupplyChainImpactWinnersContractVersion + 1} {
		tx := &winnersFakeTx{
			claim: winnersClaimed(true), counts: &winnersCountRows{},
			storedContract: []any{sql.NullInt64{Int64: stored, Valid: true}, sql.NullString{String: "h", Valid: true}},
		}
		got, err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.ResweepWinnersUnlessCurrent(context.Background())
		if err != nil || !got.Skipped || !got.ContractCurrent {
			t.Fatalf("stored %d: guarded full = %+v, %v; want Skipped with ContractCurrent", stored, got, err)
		}
		if want := []string{tryClaimSupplyChainImpactWinnersResweepSQL, selectSupplyChainImpactWinnersContractSQL}; !slices.Equal(tx.queries, want) {
			t.Fatalf("stored %d: queries = %q, want only the claim and the contract read", stored, tx.queries)
		}
		if tx.committed || len(tx.contractStamps) != 0 {
			t.Fatalf("stored %d: committed %v, stamps %v; want nothing written", stored, tx.committed, tx.contractStamps)
		}
	}
	for name, row := range map[string][]any{
		"no row": nil,
		"null":   {sql.NullInt64{}, sql.NullString{}},
		"lower":  {sql.NullInt64{Int64: SupplyChainImpactWinnersContractVersion - 1, Valid: true}, sql.NullString{String: "old", Valid: true}},
	} {
		tx := &winnersFakeTx{
			claim: winnersClaimed(true), counts: winnersCounts(int64(1), int64(0), int64(0), int64(0), sql.NullTime{}),
			storedContract: row,
		}
		got, err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.ResweepWinnersUnlessCurrent(context.Background())
		if err != nil || got.Skipped || got.Inserted != 1 {
			t.Fatalf("%s: guarded full = %+v, %v; want the full to run", name, got, err)
		}
		if !tx.committed || len(tx.contractStamps) != 1 {
			t.Fatalf("%s: committed %v, stamps %v; want a commit with one contract stamp", name, tx.committed, tx.contractStamps)
		}
	}
}

// TestResweepWinnersSkipsWhileAnotherResweepRuns pins the single-flight rule: a
// resweep that loses the claim runs no statement and commits nothing, and that
// is a normal outcome, not an error.
func TestResweepWinnersSkipsWhileAnotherResweepRuns(t *testing.T) {
	t.Parallel()

	tx := &winnersFakeTx{claim: winnersClaimed(false), counts: &winnersCountRows{}}
	got, err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.ResweepWinners(context.Background())
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
	err := SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}.RebuildAllWinners(context.Background())
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
	// Chunk applies scan four counts (no watermark column: the final stamp is
	// its own statement), so the apply fixtures carry four values.
	goodApply := func() *winnersCountRows {
		return winnersCounts(int64(1), int64(0), int64(0), int64(0))
	}
	afterRowApply := goodApply()
	afterRowApply.err = errors.New("commit failed after the row")
	closeFailsApply := goodApply()
	closeFailsApply.closeErr = errors.New("close failed")
	twoRowsApply := goodApply()
	twoRowsApply.rows = append(twoRowsApply.rows, twoRowsApply.rows[0])

	for name, tx := range map[string]*winnersFakeTx{
		"claim error":        {claimErr: errors.New("boom"), claim: &winnersCountRows{}, counts: good()},
		"no claim row":       {claim: &winnersCountRows{}, counts: good()},
		"claim cursor error": {claim: &winnersCountRows{err: errors.New("cursor")}, counts: good()},
		"rebuild error":      {claim: winnersClaimed(true), rebuildErr: context.DeadlineExceeded, counts: good()},
		"drain error":        {claim: winnersClaimed(true), drainErr: errors.New("drain"), counts: good()},
		"no counts row":      {claim: winnersClaimed(true), applies: []*winnersCountRows{{}}},
		"cursor error":       {claim: winnersClaimed(true), applies: []*winnersCountRows{{err: errors.New("cursor")}}},
		"error after row":    {claim: winnersClaimed(true), applies: []*winnersCountRows{afterRowApply}},
		"close error":        {claim: winnersClaimed(true), applies: []*winnersCountRows{closeFailsApply}},
		"extra counts row":   {claim: winnersClaimed(true), applies: []*winnersCountRows{twoRowsApply}},
		"commit error":       {claim: winnersClaimed(true), counts: good(), commitErr: errors.New("commit")},
	} {
		got, err := (SupplyChainImpactWinnersStore{DB: &winnersFakeDB{tx: tx}}).ResweepWinners(context.Background())
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
	if _, err := (SupplyChainImpactWinnersStore{DB: &winnersFakeDB{beginErr: errors.New("begin")}}).ResweepWinners(context.Background()); err == nil {
		t.Error("begin error: ResweepWinners() error = nil, want an error")
	}
	if _, err := (SupplyChainImpactWinnersStore{DB: winnersReadOnlyDB{}}).ResweepWinners(context.Background()); err == nil {
		t.Error("no transactions: ResweepWinners() error = nil, want an error")
	}
	if err := (SupplyChainImpactWinnersStore{}).RebuildAllWinners(context.Background()); err == nil {
		t.Error("RebuildAllWinners() with no database error = nil, want an error")
	}
}

// winnersReadOnlyDB cannot begin a transaction.
type winnersReadOnlyDB struct{}

func (winnersReadOnlyDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("not reached")
}
