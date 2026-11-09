// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"

	"github.com/jackc/pgx/v5/pgconn"
)

// stagedScriptRows answers scripted canned rows by query substring.
type stagedScriptRows struct {
	rows [][]any
	pos  int
}

func (r *stagedScriptRows) Next() bool {
	if r.pos >= len(r.rows) {
		return false
	}
	r.pos++
	return true
}

func (r *stagedScriptRows) Scan(dest ...any) error {
	row := r.rows[r.pos-1]
	if len(dest) != len(row) {
		return fmt.Errorf("staged assembly fake: want %d dests for %d columns", len(row), len(dest))
	}
	for i, column := range row {
		switch target := dest[i].(type) {
		case *string:
			value, ok := column.(string)
			if !ok {
				return fmt.Errorf("staged assembly fake: column %d is not a string", i)
			}
			*target = value
		case *int:
			value, ok := column.(int)
			if !ok {
				return fmt.Errorf("staged assembly fake: column %d is not an int", i)
			}
			*target = value
		case *sql.NullTime:
			if column == nil {
				*target = sql.NullTime{}
				continue
			}
			value, ok := column.(time.Time)
			if !ok {
				return fmt.Errorf("staged assembly fake: column %d is not a time", i)
			}
			*target = sql.NullTime{Time: value, Valid: true}
		case *[]byte:
			value, ok := column.(string)
			if !ok {
				return fmt.Errorf("staged assembly fake: column %d is not bytes", i)
			}
			*target = []byte(value)
		case *bool:
			value, ok := column.(bool)
			if !ok {
				return fmt.Errorf("staged assembly fake: column %d is not a bool", i)
			}
			*target = value
		default:
			return fmt.Errorf("staged assembly fake: dest %d has unsupported type %T", i, dest[i])
		}
	}
	return nil
}

func (r *stagedScriptRows) Err() error   { return nil }
func (r *stagedScriptRows) Close() error { return nil }

// stagedScriptReader serves canned rows per query family. Batches for the
// source and operator arms differ, so the script keys on CTE names.
type stagedScriptReader struct {
	members []string
}

func (q *stagedScriptReader) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "ingestion_scopes"):
		return &stagedScriptRows{rows: [][]any{
			{"s1", "g1"},
			{"operator:vulnerability_suppressions", "g9"},
		}}, nil
	case strings.Contains(query, "source_candidates"):
		return &stagedScriptRows{rows: [][]any{
			{"f1", "k1", "fid1", 5, 1, "active"},
			{"f2", "k1", "fid1", 5, 1, "active"},
			{"f3", "k3", "", 0, 0, "active"},
		}}, nil
	case strings.Contains(query, "operator_candidates"):
		return &stagedScriptRows{rows: [][]any{
			{"f9", "k1", "", 0, 0, "suppressed"},
		}}, nil
	case strings.Contains(query, "supply_chain_impact_canonical_winners"):
		return &stagedScriptRows{rows: [][]any{
			{"k1", "f1", "s1", "fid1", 5, 2, "", "", "", "repo1", "cve1", "adv1", "pkg1", "dig1", "", "", "", "", "", "active", nil, "[]", "[]", "[]"},
			{"k2", "f0", "s0", "fid0", 1, 1, "", "", "", "repo2", "cve2", "adv2", "pkg2", "dig2", "", "", "", "", "", "active", nil, "[]", "[]", "[]"},
		}}, nil
	case strings.Contains(query, "fact.fact_id = $1"):
		id, _ := args[0].(string)
		if id == "f3" {
			return &stagedScriptRows{rows: [][]any{{"p3", "r3", "d3", "cve3", "adv3"}}}, nil
		}
		return &stagedScriptRows{}, nil
	default:
		return nil, fmt.Errorf("staged assembly fake: unexpected query %.60q", query)
	}
}

// stagedScriptFleet pins read-0 and hands every statement the scripted reader.
type stagedScriptFleet struct {
	members []string
	pins    int
}

func (f *stagedScriptFleet) PinMember(context.Context) (string, error) {
	f.pins++
	return "read-0", nil
}

func (f *stagedScriptFleet) ReplayLag(context.Context, string) (time.Duration, error) {
	return 0, nil
}

func (f *stagedScriptFleet) RunOnMember(ctx context.Context, member string, fn func(context.Context, db.Queryer) error) error {
	f.members = append(f.members, member)
	return fn(ctx, &stagedScriptReader{members: f.members})
}

func TestStagedAuditFlowsCandidatesToIdents(t *testing.T) {
	fleet := &stagedScriptFleet{}
	auditor := StagedAuditor{Fleet: fleet}
	idents, unresolvable, _, _, err := auditor.runReadPhase(context.Background(), pgaccess.NewStagedAuditTelemetry())
	if err != nil {
		t.Fatalf("read phase: %v", err)
	}
	if unresolvable != 0 {
		t.Fatalf("unresolvable = %d, want 0", unresolvable)
	}
	if len(idents) != 3 {
		t.Fatalf("idents = %d, want 3 (override k1, stored-only k2, new k3)", len(idents))
	}
	got := map[string]string{}
	for _, ident := range idents {
		got[ident.PackageID.String] = ident.C0.String
	}
	want := map[string]string{"pkg1": "cve1", "pkg2": "cve2", "p3": "cve3"}
	if len(got) != len(want) {
		t.Fatalf("idents = %v, want %v", got, want)
	}
	for pkg, c0 := range want {
		if got[pkg] != c0 {
			t.Fatalf("idents = %v, want %v", got, want)
		}
	}
	for _, member := range fleet.members {
		if member != "read-0" {
			t.Fatalf("statement ran on %q, want every statement on read-0", member)
		}
	}
	// Census, two batches, stored fetch, one point lookup: five statements.
	if len(fleet.members) != 5 {
		t.Fatalf("statements = %d, want 5", len(fleet.members))
	}
}

// refusedPinFleet refuses the tick at member qualification.
type refusedPinFleet struct{ stagedScriptFleet }

func (f *refusedPinFleet) PinMember(context.Context) (string, error) {
	return "", errors.New("standby past replay budget")
}

func TestStagedAuditRefusedTickDefersWithoutWriter(t *testing.T) {
	auditor := StagedAuditor{Fleet: &refusedPinFleet{}}
	audit, err := auditor.AuditWinners(context.Background())
	if err != nil {
		t.Fatalf("refused tick must defer, not error: %v", err)
	}
	if !audit.Deferred {
		t.Fatal("refused tick must report Deferred")
	}
}

// conflictPointReader serves one new key whose ident lookup hits a standby
// recovery conflict.
type conflictPointReader struct{ stagedScriptReader }

func (q *conflictPointReader) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if strings.Contains(query, "fact.fact_id = $1") {
		return nil, &pgconn.PgError{Code: "40001", Message: "canceling statement due to conflict with recovery"}
	}
	return q.stagedScriptReader.QueryContext(ctx, query, args...)
}

type conflictPointFleet struct {
	stagedScriptFleet
	reader db.Queryer
}

func (f *conflictPointFleet) RunOnMember(ctx context.Context, member string, fn func(context.Context, db.Queryer) error) error {
	f.members = append(f.members, member)
	return fn(ctx, f.reader)
}

func TestStagedAuditResolveConflictDefersWithoutWriter(t *testing.T) {
	fleet := &conflictPointFleet{reader: &conflictPointReader{}}
	auditor := StagedAuditor{Fleet: fleet}
	audit, err := auditor.AuditWinners(context.Background())
	if err != nil {
		t.Fatalf("resolve-phase conflict must defer, not error: %v", err)
	}
	if !audit.Deferred {
		t.Fatal("resolve-phase conflict must report Deferred")
	}
}

// fakeWriterResult reports a canned repair count.
type fakeWriterResult struct{ repairs int64 }

func (r fakeWriterResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeWriterResult) RowsAffected() (int64, error) { return r.repairs, nil }

// fakeWriterTx answers the verify tail: claim succeeds when claim is set,
// queue reports repairs.
type fakeWriterTx struct {
	repairs int64
	claim   bool
}

func (tx *fakeWriterTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return &stagedScriptRows{rows: [][]any{{tx.claim}}}, nil
}

func (tx *fakeWriterTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return fakeWriterResult{repairs: tx.repairs}, nil
}

func (tx *fakeWriterTx) Commit() error   { return nil }
func (tx *fakeWriterTx) Rollback() error { return nil }

type fakeWriterBeginner struct{ tx *fakeWriterTx }

func (b *fakeWriterBeginner) Begin(context.Context) (db.Transaction, error) { return b.tx, nil }

func (b *fakeWriterBeginner) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, fmt.Errorf("staged assembly fake: writer must verify inside a transaction")
}

func TestStagedAuditVerifyPathRecordsDiscards(t *testing.T) {
	fleet := &stagedScriptFleet{}
	telemetry := pgaccess.NewStagedAuditTelemetry()
	var logs bytes.Buffer
	auditor := StagedAuditor{
		Fleet:     fleet,
		Store:     postgres.SupplyChainImpactWinnersStore{DB: &fakeWriterBeginner{tx: &fakeWriterTx{repairs: 2, claim: true}}},
		Telemetry: telemetry,
		Logger:    slog.New(slog.NewTextHandler(&logs, nil)),
	}
	audit, err := auditor.AuditWinners(context.Background())
	if err != nil {
		t.Fatalf("AuditWinners: %v", err)
	}
	if audit.Repairs != 2 {
		t.Fatalf("repairs = %d, want 2", audit.Repairs)
	}
	if got := telemetry.DiscardRate(); got != 1.0 {
		t.Fatalf("discard rate = %v, want 1.0 (3 candidates, 2 repairs)", got)
	}
	if telemetry.Ticks() != 1 {
		t.Fatalf("shared telemetry ticks = %d, want 1", telemetry.Ticks())
	}
	if line := logs.String(); !strings.Contains(line, "staged winners audit tick") || !strings.Contains(line, "discards=1") {
		t.Fatalf("completed-tick log line must carry discards=1, got %q", line)
	}
}

func TestStagedAuditSkippedVerifyRecordsNoVerify(t *testing.T) {
	fleet := &stagedScriptFleet{}
	telemetry := pgaccess.NewStagedAuditTelemetry()
	var logs bytes.Buffer
	auditor := StagedAuditor{
		Fleet:     fleet,
		Store:     postgres.SupplyChainImpactWinnersStore{DB: &fakeWriterBeginner{tx: &fakeWriterTx{}}},
		Telemetry: telemetry,
		Logger:    slog.New(slog.NewTextHandler(&logs, nil)),
	}
	audit, err := auditor.AuditWinners(context.Background())
	if err != nil {
		t.Fatalf("AuditWinners: %v", err)
	}
	if !audit.Skipped {
		t.Fatal("lost lock claim must report Skipped")
	}
	if got := telemetry.DiscardRate(); got != 0 {
		t.Fatalf("discard rate = %v, want 0 (no verify ran)", got)
	}
	if line := logs.String(); strings.Contains(line, "staged winners audit tick") {
		t.Fatalf("skipped pass must not log a completion tick, got %q", line)
	}
}
