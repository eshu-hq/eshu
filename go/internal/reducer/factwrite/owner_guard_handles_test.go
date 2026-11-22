// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factwrite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// heldRows is a db.Rows over (fact_id, stored scope) pairs, the owner probe's
// result shape.
type heldRows struct {
	rows [][2]string
	next int
	err  error
}

func (r *heldRows) Next() bool { r.next++; return r.next <= len(r.rows) }
func (r *heldRows) Err() error { return r.err }
func (r *heldRows) Close() error {
	return nil
}

func (r *heldRows) Scan(dest ...any) error {
	*(dest[0].(*string)), *(dest[1].(*string)) = r.rows[r.next-1][0], r.rows[r.next-1][1]
	return nil
}

// shortExecer reports a fixed rows-affected count and, through query, the probe's
// held rows. Its type decides which probe interface it offers.
type shortExecer struct {
	affected int64
	execErr  error
	held     [][2]string
	queryErr error
	queries  int
}

func (s *shortExecer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	if s.execErr != nil {
		return nil, s.execErr
	}
	return countResult{s.affected}, nil
}

func (s *shortExecer) probe() (db.Rows, error) {
	s.queries++
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	return &heldRows{rows: s.held}, nil
}

// queryerExecer offers the probe as a db.Queryer.
type queryerExecer struct{ shortExecer }

func (q *queryerExecer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return q.probe()
}

// lenderExecer offers it as an OwnerProbeQueryer only, like a transaction
// wrapper whose own QueryContext returns another row type.
type lenderExecer struct{ shortExecer }

func (l *lenderExecer) QueryFactOwners(context.Context, string, ...any) (db.Rows, error) {
	return l.probe()
}

func ownerTestRow(id, scope string) Row {
	at := time.Date(2026, time.November, 22, 0, 0, 0, 0, time.UTC)
	return Row{FactID: id, ScopeID: scope, GenerationID: "gen", FactKind: "k", ObservedAt: at, IngestedAt: at, Payload: "{}"}
}

func ownerTestVersioned(id, scope string) VersionedRow {
	r := ownerTestRow(id, scope)
	return VersionedRow{
		FactID: r.FactID, ScopeID: r.ScopeID, GenerationID: r.GenerationID, FactKind: r.FactKind,
		SchemaVersion: "1.0.0", ObservedAt: r.ObservedAt, IngestedAt: r.IngestedAt, Payload: r.Payload,
	}
}

func TestCanProbeOwnersByHandleShape(t *testing.T) {
	t.Parallel()
	if CanProbeOwners(&shortExecer{}) {
		t.Error("a handle with no query surface cannot probe")
	}
	if !CanProbeOwners(&queryerExecer{}) || !CanProbeOwners(&lenderExecer{}) {
		t.Error("a db.Queryer and an OwnerProbeQueryer must both be able to probe")
	}
}

// TestBatchWritersReportForeignRowsThroughEveryProbeShape drives the plain and
// the versioned batch writers end to end: a short write whose probe names a
// foreign-held id is a typed conflict through either probe interface, a short
// write with no foreign row is clean, and a failing probe or exec is an error.
func TestBatchWritersReportForeignRowsThroughEveryProbeShape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	writers := map[string]struct {
		write func(Execer) error
		label string
	}{
		"batch": {func(e Execer) error {
			return BatchInsertFacts(ctx, e, []Row{ownerTestRow("f1", "scope-b"), ownerTestRow("f2", "scope-b")})
		}, WriterBatch},
		"versioned": {func(e Execer) error {
			return BatchInsertVersionedFacts(ctx, e, []VersionedRow{ownerTestVersioned("f1", "scope-b"), ownerTestVersioned("f2", "scope-b")})
		}, WriterVersioned},
	}
	for name, w := range writers {
		for shape, build := range map[string]func(shortExecer) Execer{
			"queryer": func(s shortExecer) Execer { return &queryerExecer{s} },
			"lender":  func(s shortExecer) Execer { return &lenderExecer{s} },
		} {
			t.Run(name+"/"+shape, func(t *testing.T) {
				var conflict *OwnerConflictError
				err := w.write(build(shortExecer{affected: 1, held: [][2]string{{"f2", "scope-a"}}}))
				if !errors.As(err, &conflict) || conflict.Writer != w.label || conflict.Count != 1 || conflict.Samples[0].StoredScopeID != "scope-a" {
					t.Fatalf("foreign row error = %v, want one %s conflict held by scope-a", err, w.label)
				}
				if msg := conflict.Error(); !strings.Contains(msg, "fact_id_owner_conflict") || !strings.Contains(msg, `"f2"`) {
					t.Fatalf("error text %q does not name the class and the fact", msg)
				}
				if err := w.write(build(shortExecer{affected: 1})); err != nil {
					t.Fatalf("a short write with no foreign row (fenced stale) reported %v", err)
				}
				if err := w.write(build(shortExecer{affected: 2})); err != nil {
					t.Fatalf("a full write reported %v", err)
				}
				if err := w.write(build(shortExecer{affected: 1, queryErr: errors.New("boom")})); err == nil {
					t.Fatal("a failing probe must surface")
				}
				if err := w.write(build(shortExecer{execErr: errors.New("exec boom")})); err == nil {
					t.Fatal("a failing exec must surface")
				}
			})
		}
	}
}

// TestCheckOwnedLooksUpTheStoredOwnerThroughEveryProbeShape: the single-row
// conflict names the stored scope when the handle can probe, and still reports
// the conflict when the lookup fails.
func TestCheckOwnedLooksUpTheStoredOwnerThroughEveryProbeShape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, handle := range map[string]func(shortExecer) Execer{
		"queryer": func(s shortExecer) Execer { return &queryerExecer{s} },
		"lender":  func(s shortExecer) Execer { return &lenderExecer{s} },
	} {
		var conflict *OwnerConflictError
		err := CheckOwned(ctx, handle(shortExecer{held: [][2]string{{"f1", "scope-a"}}}), countResult{0}, WriterSingle, "f1", "scope-b", "gen", "k")
		if !errors.As(err, &conflict) || conflict.Samples[0].StoredScopeID != "scope-a" {
			t.Fatalf("%s: error = %v, want a conflict held by scope-a", name, err)
		}
		err = CheckOwned(ctx, handle(shortExecer{queryErr: errors.New("boom")}), countResult{0}, WriterSingle, "f1", "scope-b", "gen", "k")
		if !errors.As(err, &conflict) || conflict.Samples[0].StoredScopeID != "" {
			t.Fatalf("%s: a failed lookup must still report the conflict, got %v", name, err)
		}
	}
}
