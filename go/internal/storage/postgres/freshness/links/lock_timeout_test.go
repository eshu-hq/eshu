// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// recordingDB answers one full link of the given path (root, incremental or
// rebase) and records every statement in order. lockErr, when set, is the
// error of the generation lock; linkErr the error of the link statement.
type recordingDB struct {
	path       string
	statements []string
	lockErr    error
	linkErr    error
}

func (d *recordingDB) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	d.statements = append(d.statements, query)
	return okResult{}, nil
}

func (d *recordingDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	d.statements = append(d.statements, query)
	state := "g0"
	if d.path == "root" {
		state = ""
	}
	switch query {
	case lockCursorQuery:
		return &scriptedRows{values: []any{state, int64(1), int16(DigestVersion), int64(0), 0, nil}}, nil
	case nextActivationQuery:
		return &scriptedRows{values: []any{int64(2), "g1", "g0", false}}, nil
	case generationExistsQuery:
		if d.path == "rebase" && args[0] == "g0" {
			return &scriptedRows{}, nil
		}
		return &scriptedRows{values: []any{false}}, nil
	case lockGenerationQuery:
		if d.lockErr != nil {
			return nil, d.lockErr
		}
		return &scriptedRows{values: []any{int64(1)}}, nil
	case trySlotQuery:
		return &scriptedRows{values: []any{true}}, nil
	case RootLinkSQL:
		return &scriptedRows{values: []any{int64(0), int64(1), int64(1), int64(1)}}, d.linkErr
	case IncrementalLinkSQL:
		return &scriptedRows{values: linkRow(0, 0, 0, 0, 0)}, d.linkErr
	case RebaseLinkSQL:
		return &scriptedRows{values: []any{int64(0), int64(1), int64(1), int64(1), int64(0), int64(0), int64(0), int64(0), int64(0)}}, d.linkErr
	}
	return nil, fmt.Errorf("recording db: unexpected query %.60q", query)
}

func (d *recordingDB) Begin(context.Context) (db.Transaction, error) { return &recordingTx{db: d}, nil }

type recordingTx struct{ db *recordingDB }

func (t *recordingTx) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	return t.db.ExecContext(ctx, q, a...)
}

func (t *recordingTx) QueryContext(ctx context.Context, q string, a ...any) (db.Rows, error) {
	return t.db.QueryContext(ctx, q, a...)
}
func (t *recordingTx) Commit() error   { return nil }
func (t *recordingTx) Rollback() error { return nil }

// TestLockTimeoutIsResetBeforeTheLinkStatement is W6 of arbiter ruling
// arb-7127-3e-wait: on every full-link path the generation-lock timeout is
// set before the first generation lock, and lock_timeout is reset to 0 after
// the last generation lock and before the link statement, which runs under
// statement_timeout alone.
func TestLockTimeoutIsResetBeforeTheLinkStatement(t *testing.T) {
	for path, statement := range map[string]string{"root": RootLinkSQL, "incremental": IncrementalLinkSQL, "rebase": RebaseLinkSQL} {
		d := &recordingDB{path: path}
		if _, err := NewLinkWriter(d).LinkNext(context.Background(), "s"); err != nil {
			t.Fatalf("%s: LinkNext: %v", path, err)
		}
		set := slices.Index(d.statements, setGenerationLockTimeoutStatement)
		firstLock := slices.Index(d.statements, lockGenerationQuery)
		lastLock := len(d.statements) - 1 - slices.Index(reversed(d.statements), lockGenerationQuery)
		reset := slices.Index(d.statements, resetLockTimeoutStatement)
		link := slices.Index(d.statements, statement)
		if set < 0 || firstLock < 0 || set > firstLock {
			t.Fatalf("%s: timeout set at %d, first generation lock at %d; want the set first", path, set, firstLock)
		}
		if reset < 0 || link < 0 || reset < lastLock || reset > link {
			t.Fatalf("%s: reset at %d, last generation lock at %d, link statement at %d; want lock < reset < link",
				path, reset, lastLock, link)
		}
		// Only the incremental path locks a prior: root has none, and a
		// rebase's prior is gone (its existence read found no row).
		if path == "incremental" && slices.Index(d.statements[firstLock+1:], lockGenerationQuery) < 0 {
			t.Fatalf("%s: one generation lock recorded, want the activating generation and the prior", path)
		}
	}
}

func reversed(in []string) []string {
	out := slices.Clone(in)
	slices.Reverse(out)
	return out
}

// TestLockTimeoutMapsOnlyAtTheGenerationLock proves 55P03 is mapped where the
// ruling puts it: from a generation lock it is the non-counting
// generation_lock_timeout with its SQLSTATE; from the link statement it stays
// a counting failure, and ClassifyFailure is unchanged.
func TestLockTimeoutMapsOnlyAtTheGenerationLock(t *testing.T) {
	lockTimeout := &pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"}
	_, err := NewLinkWriter(&recordingDB{path: "incremental", lockErr: lockTimeout}).LinkNext(context.Background(), "s")
	var retry *RetryError
	if !errors.As(err, &retry) || retry.Reason != RetryGenerationLockTimeout || retry.SQLState != "55P03" || retry.ActivationSeq != 2 {
		t.Fatalf("55P03 from the generation lock = %v, want generation_lock_timeout with SQLSTATE 55P03 on activation 2", err)
	}
	_, err = NewLinkWriter(&recordingDB{path: "incremental", linkErr: lockTimeout}).LinkNext(context.Background(), "s")
	var failure *FailureError
	if !errors.As(err, &failure) || errors.As(err, &retry) {
		t.Fatalf("55P03 from the link statement = %v, want a counting *FailureError, not a retry", err)
	}
	if class := ClassifyFailure(lockTimeout); class != FailureSQLError {
		t.Fatalf("ClassifyFailure(55P03) = %q, want %q (unchanged; 55P03 is mapped only at the generation lock)", class, FailureSQLError)
	}
}
