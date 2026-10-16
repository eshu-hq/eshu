// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// ackStatementDB records every statement an Ack sends, in order, with its
// arguments, and answers the two reads the journal path makes: the
// superseded generation ids of the RETURNING supersede and the
// (activated, journaled, gated) row of the activate-and-journal statement.
type ackStatementDB struct {
	calls         []recordedExecCall
	supersededIDs []string
	journalRow    []any
}

func (d *ackStatementDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	d.calls = append(d.calls, recordedExecCall{query: query, args: append([]any(nil), args...)})
	return proofResult{}, nil
}

func (d *ackStatementDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	d.calls = append(d.calls, recordedExecCall{query: query, args: append([]any(nil), args...)})
	switch query {
	case supersedeProjectorActiveGenerationReturningQuery:
		rows := &valueRows{}
		for _, id := range d.supersededIDs {
			rows.rows = append(rows.rows, []any{id})
		}
		return rows, nil
	case activateAndJournalProjectorGenerationQuery:
		return &valueRows{rows: [][]any{d.journalRow}}, nil
	}
	return &valueRows{}, nil
}

func (d *ackStatementDB) Begin(context.Context) (db.Transaction, error) {
	return ackStatementTx{d}, nil
}

type ackStatementTx struct{ *ackStatementDB }

func (ackStatementTx) Commit() error   { return nil }
func (ackStatementTx) Rollback() error { return nil }

// valueRows yields fixed rows, assigning each column by its destination type.
type valueRows struct {
	rows [][]any
	next int
}

func (r *valueRows) Next() bool { r.next++; return r.next <= len(r.rows) }
func (r *valueRows) Err() error { return nil }
func (r *valueRows) Close() error {
	return nil
}

func (r *valueRows) Scan(dest ...any) error {
	row := r.rows[r.next-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan %d columns into %d destinations", len(row), len(dest))
	}
	for i, value := range row {
		switch d := dest[i].(type) {
		case *string:
			*d = value.(string)
		case *int64:
			*d = value.(int64)
		case *bool:
			*d = value.(bool)
		default:
			return fmt.Errorf("unsupported destination %T", d)
		}
	}
	return nil
}

func ackJournalQueue(database db.ExecQueryer, journal bool) ProjectorQueue {
	q := NewProjectorQueue(database, "projector-1", time.Minute)
	q.Now = func() time.Time { return time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC) }
	q.ChangedSinceJournalEnabled = journal
	return q
}

// ackFlagOffStatements is the statement sequence of a successful Ack before
// #7127 PR-3b, by shipped constant; the last two come from the activation
// package.
func ackFlagOffStatements() []string {
	return []string{
		"SELECT set_config('lock_timeout', $1, true)",
		updateProjectorScopeGenerationQuery,
		ackProjectorWorkItemQuery,
		deltaBaselineFenceQuery,
		supersedeProjectorObsoleteGenerationsQuery,
		supersedeProjectorActiveGenerationQuery,
		activateProjectorGenerationQuery,
	}
}

// TestProjectorAckWithJournalOffSendsTheShippedStatements is the dark-mode
// contract of #7127 PR-3b: with ChangedSinceJournalEnabled false, Ack sends
// exactly the statements it sent before, with the same arguments, and no
// statement that touches the ledger, an advisory lock, or a RETURNING read.
func TestProjectorAckWithJournalOffSendsTheShippedStatements(t *testing.T) {
	t.Parallel()
	database := &ackStatementDB{}
	if err := ackJournalQueue(database, false).Ack(context.Background(), ackWorkForLockTimeoutTest(), runtime.Result{}); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	want := ackFlagOffStatements()
	if len(database.calls) != len(want)+2 {
		t.Fatalf("Ack sent %d statements, want %d", len(database.calls), len(want)+2)
	}
	for i, query := range want {
		if database.calls[i].query != query {
			t.Fatalf("statement %d = %q, want the shipped %q", i, database.calls[i].query, query)
		}
	}
	if !strings.Contains(database.calls[7].query, "INSERT INTO activation_obligations") ||
		!strings.Contains(database.calls[8].query, "INSERT INTO producer_activation_obligations") {
		t.Fatalf("obligation statements = %q, %q", database.calls[7].query, database.calls[8].query)
	}
	for _, call := range database.calls {
		for _, banned := range []string{"changed_since", "advisory", "RETURNING generation_id"} {
			if strings.Contains(call.query, banned) {
				t.Fatalf("flag-off Ack sent %q, which contains %q", call.query, banned)
			}
		}
	}
	if got := len(database.calls[6].args); got != 3 {
		t.Fatalf("activate statement args = %d, want 3", got)
	}
}

// TestProjectorAckWithJournalOnReplacesOnlyTwoStatements proves the flag-on
// Ack sends the same number of statements: the active supersede gains
// RETURNING, the activate statement becomes the gated CTE, and every other
// statement is unchanged.
func TestProjectorAckWithJournalOnReplacesOnlyTwoStatements(t *testing.T) {
	t.Parallel()
	database := &ackStatementDB{supersededIDs: []string{"generation-0"}, journalRow: []any{int64(1), int64(1), true}}
	if err := ackJournalQueue(database, true).Ack(context.Background(), ackWorkForLockTimeoutTest(), runtime.Result{}); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	want := ackFlagOffStatements()
	want[5] = supersedeProjectorActiveGenerationReturningQuery
	want[6] = activateAndJournalProjectorGenerationQuery
	if len(database.calls) != len(want)+2 {
		t.Fatalf("Ack sent %d statements, want %d", len(database.calls), len(want)+2)
	}
	for i, query := range want {
		if database.calls[i].query != query {
			t.Fatalf("statement %d = %q, want %q", i, database.calls[i].query, query)
		}
	}
	args := database.calls[6].args
	if len(args) != 4 || args[1] != "scope-1" || args[2] != "generation-1" || args[3] != "generation-0" {
		t.Fatalf("activate-and-journal args = %v, want (now, scope-1, generation-1, generation-0)", args)
	}
}

// TestProjectorAckJournalPrior pins the prior rule: exactly one superseded
// active generation is the prior; none (a first activation) or several (an
// ambiguous scope) give the empty string, which the statement stores as NULL,
// as the sweeper does.
func TestProjectorAckJournalPrior(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		superseded []string
		want       string
	}{
		"superseded":       {superseded: []string{"generation-0"}, want: "generation-0"},
		"first activation": {want: ""},
		"ambiguous":        {superseded: []string{"generation-a", "generation-b"}, want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			database := &ackStatementDB{supersededIDs: tc.superseded, journalRow: []any{int64(1), int64(1), true}}
			if err := ackJournalQueue(database, true).Ack(context.Background(), ackWorkForLockTimeoutTest(), runtime.Result{}); err != nil {
				t.Fatalf("Ack() error = %v", err)
			}
			if got := database.calls[6].args[3]; got != tc.want {
				t.Fatalf("prior = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestProjectorAckJournalOutcomes maps the statement's explicit columns to
// the Ack's journal outcome.
func TestProjectorAckJournalOutcomes(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		row       []any
		activated bool
		outcome   string
		wantErr   bool
	}{
		"journaled":    {row: []any{int64(1), int64(1), true}, activated: true, outcome: ackJournalOutcomeJournaled},
		"exists":       {row: []any{int64(1), int64(0), true}, activated: true, outcome: ackJournalOutcomeExists},
		"gate skipped": {row: []any{int64(1), int64(0), false}, activated: true, outcome: ackJournalOutcomeGateSkipped},
		"superseded":   {row: []any{int64(0), int64(0), false}},
		"inconsistent": {row: []any{int64(1), int64(1), false}, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			database := &ackStatementDB{journalRow: tc.row}
			activated, outcome, err := ackJournalQueue(database, true).activateAndJournalAckGeneration(
				context.Background(), ackStatementTx{database}, ackWorkForLockTimeoutTest(), time.Now(), "")
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error %v", err, tc.wantErr)
			}
			if activated != tc.activated || outcome != tc.outcome {
				t.Fatalf("got (%v, %q), want (%v, %q)", activated, outcome, tc.activated, tc.outcome)
			}
		})
	}
}

// TestProjectorAckJournalSupersededGenerationIsRefused proves a superseded
// target takes the existing refusal path with the journal on.
func TestProjectorAckJournalSupersededGenerationIsRefused(t *testing.T) {
	t.Parallel()
	database := &ackStatementDB{journalRow: []any{int64(0), int64(0), false}}
	err := ackJournalQueue(database, true).Ack(context.Background(), ackWorkForLockTimeoutTest(), runtime.Result{})
	if !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Ack() error = %v, want ErrWorkSuperseded", err)
	}
}

// TestAckJournalStatementsDeriveFromShippedQueries is the byte-prefix guard:
// both journal statements are built from the shipped Ack statements, the
// gate is the links package's expression, and no projector queue source
// carries an advisory-lock literal of its own.
func TestAckJournalStatementsDeriveFromShippedQueries(t *testing.T) {
	t.Parallel()
	if !strings.HasPrefix(supersedeProjectorActiveGenerationReturningQuery, supersedeProjectorActiveGenerationQuery) ||
		!strings.HasSuffix(supersedeProjectorActiveGenerationReturningQuery, "RETURNING generation_id\n") {
		t.Fatalf("returning supersede is not the shipped supersede plus RETURNING:\n%s", supersedeProjectorActiveGenerationReturningQuery)
	}
	if !strings.HasPrefix(activateAndJournalProjectorGenerationQuery, "\nWITH activated AS ("+activateProjectorGenerationQuery+"RETURNING ") {
		t.Fatalf("activate-and-journal does not start with the shipped activate statement:\n%s", activateAndJournalProjectorGenerationQuery)
	}
	for _, fragment := range []string{
		"gate AS MATERIALIZED (",
		"SELECT " + linksfreshnessstore.AckJournalGateSQL + " AS acquired",
		"'" + linksfreshnessstore.SourceAck + "'",
		"ON CONFLICT (scope_id, generation_id) DO NOTHING",
		"JOIN gate ON gate.acquired",
	} {
		if strings.Count(activateAndJournalProjectorGenerationQuery, fragment) != 1 {
			t.Fatalf("activate-and-journal must contain %q exactly once:\n%s", fragment, activateAndJournalProjectorGenerationQuery)
		}
	}
	files, err := filepath.Glob("projector_queue*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob projector queue sources: %v", err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if strings.Contains(string(raw), "advisory_xact_lock") {
			t.Fatalf("%s carries an advisory-lock literal; take the gate from linksfreshnessstore.AckJournalGateSQL", file)
		}
	}
}
