// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// fakeClock is the only clock the searcher sees in tests. The fake database
// advances it by the simulated cost of each statement, so a budget decision is
// a pure function of the corpus and never of the host.
type fakeClock struct{ now time.Time }

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1_700_000_000, 0)} }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// fakeRow is one content_files row in key order.
type fakeRow struct {
	repo, path string
	match      bool
	cost       time.Duration // cost of testing this row in a key-ordered walk
}

// fakeCorpus is an ordered table plus the simulated cost of the trigram tail.
type fakeCorpus struct {
	rows []fakeRow
	// tailCost is the simulated wall of the trigram tail from row index from
	// (the first row strictly after the cursor).
	tailCost func(from int) time.Duration
	// tailLag is extra uninterruptible time a cancelled tail runs past its
	// statement timeout.
	tailLag time.Duration
}

// newCorpus builds n rows with keys that sort in index order. isMatch picks
// the matching rows; rowCost is the per-row walk cost.
func newCorpus(n int, rowCost time.Duration, isMatch func(i int) bool) *fakeCorpus {
	corpus := &fakeCorpus{tailCost: func(int) time.Duration { return 300 * time.Millisecond }}
	for i := 0; i < n; i++ {
		corpus.rows = append(corpus.rows, fakeRow{
			repo:  fmt.Sprintf("repo-%03d", i/100),
			path:  fmt.Sprintf("file-%05d.go", i),
			match: isMatch(i),
			cost:  rowCost,
		})
	}
	return corpus
}

func (c *fakeCorpus) indexAfter(repo, path string) int {
	if repo == "" && path == "" {
		return 0
	}
	for i, row := range c.rows {
		if row.repo > repo || (row.repo == repo && row.path > path) {
			return i
		}
	}
	return len(c.rows)
}

// fakeDB is the Beginner the searcher opens its transaction on.
type fakeDB struct {
	corpus   *fakeCorpus
	clock    *fakeClock
	readyErr error
	// readyCost is simulated time the readiness statement consumes, used to
	// put a call near the end of its budget before the walk starts.
	readyCost time.Duration
	begun     int
	tx        *fakeTx
}

func newFakeDB(corpus *fakeCorpus, clock *fakeClock) *fakeDB {
	return &fakeDB{corpus: corpus, clock: clock}
}

func (f *fakeDB) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	f.begun++
	f.tx = &fakeTx{db: f, timeout: 0}
	return f.tx, nil
}

// fakeTx simulates one PostgreSQL transaction: a cancelled statement aborts it
// until ROLLBACK TO SAVEPOINT, and SET LOCAL values roll back with the
// savepoint, as on the server.
type fakeTx struct {
	db         *fakeDB
	timeout    time.Duration
	indexOff   bool
	aborted    bool
	savedTO    time.Duration
	savedIdx   bool
	hasSave    bool
	log        []string // one label per statement, in order
	execMode   []bool   // whether each statement carried pgx.QueryExecModeExec
	args       [][]any
	rolledBack bool
	cancels    int
}

func (tx *fakeTx) Commit() error { return nil }

func (tx *fakeTx) Rollback() error { tx.rolledBack = true; return nil }

func (tx *fakeTx) QueryRowContext(context.Context, string, ...any) db.Row { return nil }

func (tx *fakeTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	sawExecMode := false
	if len(args) > 0 {
		if _, ok := args[0].(pgx.QueryExecMode); ok {
			args = args[1:]
			sawExecMode = true
		}
	}
	label := classify(query)
	tx.log = append(tx.log, label)
	tx.execMode = append(tx.execMode, sawExecMode)
	tx.args = append(tx.args, args)
	if tx.aborted && label != "rollback_to" {
		return nil, &pgconn.PgError{Code: "25P02", Message: "current transaction is aborted"}
	}
	switch label {
	case "readiness":
		if tx.db.readyErr != nil {
			return nil, tx.db.readyErr
		}
		tx.db.clock.advance(tx.db.readyCost)
		return newFakeRows(nil), nil
	case "savepoint":
		tx.savedTO, tx.savedIdx, tx.hasSave = tx.timeout, tx.indexOff, true
		return newFakeRows(nil), nil
	case "release":
		tx.hasSave = false
		return newFakeRows(nil), nil
	case "rollback_to":
		tx.aborted = false
		tx.timeout, tx.indexOff = tx.savedTO, tx.savedIdx
		return newFakeRows(nil), nil
	case "set_timeout":
		text := args[0].(string)
		millis, err := strconv.Atoi(strings.TrimSuffix(text, "ms"))
		if err != nil {
			return nil, fmt.Errorf("fake: bad timeout %q", text)
		}
		tx.timeout = time.Duration(millis) * time.Millisecond
		return newFakeRows(nil), nil
	case "index_off":
		tx.indexOff = true
		return newFakeRows(nil), nil
	case "index_reset":
		tx.indexOff = false
		return newFakeRows(nil), nil
	case "step":
		return tx.runStep(args)
	case "tail":
		return tx.runTail(args)
	}
	return nil, fmt.Errorf("fake: unclassified statement %q", query)
}

func classify(query string) string {
	q := strings.TrimSpace(query)
	switch {
	case strings.Contains(q, "eshu_require_content_substring_indexes_ready"):
		return "readiness"
	case strings.HasPrefix(q, "SAVEPOINT"):
		return "savepoint"
	case strings.HasPrefix(q, "RELEASE SAVEPOINT"):
		return "release"
	case strings.HasPrefix(q, "ROLLBACK TO SAVEPOINT"):
		return "rollback_to"
	case strings.Contains(q, "set_config('statement_timeout'"):
		return "set_timeout"
	case strings.HasPrefix(q, "SET LOCAL enable_indexscan = off"):
		return "index_off"
	case strings.HasPrefix(q, "RESET enable_indexscan"):
		return "index_reset"
	case strings.Contains(q, "UNION ALL"):
		return "step"
	case strings.Contains(q, "FROM content_files"):
		return "tail"
	}
	return "unknown"
}

func (tx *fakeTx) cancel(lag time.Duration) (db.Rows, error) {
	tx.cancels++
	tx.db.clock.advance(tx.timeout + lag)
	tx.aborted = true
	return nil, &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}
}

// runStep simulates the key-ordered window statement: $1 pattern, $2 window
// rows, $3 matches wanted, optional $4/$5 cursor.
func (tx *fakeTx) runStep(args []any) (db.Rows, error) {
	window := int(args[1].(int64))
	want := int(args[2].(int64))
	from := 0
	if len(args) >= 5 {
		from = tx.db.corpus.indexAfter(args[3].(string), args[4].(string))
	}
	rows := tx.db.corpus.rows
	var cost time.Duration
	var out [][]any
	hits := 0
	for i := from; i < from+window && i < len(rows) && hits < want; i++ {
		cost += rows[i].cost
		if rows[i].match {
			hits++
			out = append(out, fileRow(kindHit, rows[i]))
		}
	}
	if tx.timeout > 0 && cost > tx.timeout {
		return tx.cancel(0)
	}
	tx.db.clock.advance(cost)
	if edge := from + window - 1; edge < len(rows) {
		out = append(out, fileRow(kindEdge, rows[edge]))
	}
	return newFakeRows(out), nil
}

// runTail simulates the trigram bitmap statement: $1 pattern, $2 matches
// wanted, optional $3/$4 cursor. It refuses to run with index scans enabled,
// because the contract is that the tail is planned as the trigram bitmap.
func (tx *fakeTx) runTail(args []any) (db.Rows, error) {
	if !tx.indexOff {
		return nil, fmt.Errorf("fake: tail ran with enable_indexscan on")
	}
	want := int(args[1].(int64))
	from := 0
	if len(args) >= 4 {
		from = tx.db.corpus.indexAfter(args[2].(string), args[3].(string))
	}
	cost := tx.db.corpus.tailCost(from)
	if tx.timeout > 0 && cost > tx.timeout {
		return tx.cancel(tx.db.corpus.tailLag)
	}
	tx.db.clock.advance(cost)
	var out [][]any
	for i := from; i < len(tx.db.corpus.rows) && len(out) < want; i++ {
		if tx.db.corpus.rows[i].match {
			out = append(out, fileRow(kindHit, tx.db.corpus.rows[i]))
		}
	}
	return newFakeRows(out), nil
}

const (
	kindHit  = "hit"
	kindEdge = "edge"
)

func fileRow(kind string, row fakeRow) []any {
	return []any{kind, row.repo, row.path, "sha", "", "hash-" + row.path, int64(10), "go", "source"}
}

type fakeRows struct {
	data [][]any
	pos  int
}

func newFakeRows(data [][]any) *fakeRows { return &fakeRows{data: data, pos: -1} }

func (r *fakeRows) Next() bool { r.pos++; return r.pos < len(r.data) }

func (r *fakeRows) Err() error { return nil }

func (r *fakeRows) Close() error { return nil }

func (r *fakeRows) Scan(dest ...any) error {
	row := r.data[r.pos]
	if len(dest) != len(row) {
		return fmt.Errorf("fake: scan %d columns into %d destinations", len(row), len(dest))
	}
	for i, d := range dest {
		switch ptr := d.(type) {
		case *string:
			*ptr = row[i].(string)
		case *int:
			*ptr = int(row[i].(int64))
		default:
			return fmt.Errorf("fake: unsupported scan destination %T", d)
		}
	}
	return nil
}

// labels returns the statement labels the transaction saw, without the
// savepoint bookkeeping, so a test can assert phase order.
func (tx *fakeTx) labels(t *testing.T, keep ...string) []string {
	t.Helper()
	want := make(map[string]bool, len(keep))
	for _, k := range keep {
		want[k] = true
	}
	var out []string
	for _, l := range tx.log {
		if want[l] {
			out = append(out, l)
		}
	}
	return out
}
