// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func spanAttributes(span sdktrace.ReadOnlySpan) map[attribute.Key]string {
	attrs := map[attribute.Key]string{}
	for _, kv := range span.Attributes() {
		attrs[kv.Key] = kv.Value.AsString()
	}
	return attrs
}

// TestSnapshotStatusReaderDisablesJITBeforeTheReadPhase is the #7009 PR-1
// contract: the status transaction issues exactly one SET LOCAL jit = off,
// after the snapshot begins and before any status statement runs, and the
// span says so.
func TestSnapshotStatusReaderDisablesJITBeforeTheReadPhase(t *testing.T) {
	if statusSnapshotJITOffSQL != "SET LOCAL jit = off" {
		t.Fatalf("statement=%q, want transaction-scoped SET LOCAL jit = off", statusSnapshotJITOffSQL)
	}
	for _, selection := range []status.SnapshotSelection{status.FullSnapshotSelection(), {}, status.SemanticOnlySnapshotSelection()} {
		recorder := tracetest.NewSpanRecorder()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
		tx := &snapshotTxStub{}
		reads := 0
		reader := NewSnapshotStatusReader(snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }},
			func(q db.Queryer) status.Reader {
				if q != tx {
					t.Fatal("factory did not receive the snapshot transaction")
				}
				return snapshotReaderStub{read: func(context.Context, status.SnapshotSelection) (status.RawSnapshot, error) {
					reads++
					if len(tx.statements) != 1 || tx.statements[0] != statusSnapshotJITOffSQL {
						t.Fatalf("statements before read=%q, want one %q", tx.statements, statusSnapshotJITOffSQL)
					}
					return status.RawSnapshot{}, nil
				}}
			}, provider.Tracer("test"))
		if _, err := reader.ReadStatusSnapshotFiltered(t.Context(), time.Now(), selection); err != nil {
			t.Fatal(err)
		}
		if reads != 1 || len(tx.statements) != 1 || tx.commits != 1 || tx.rollbacks != 0 {
			t.Fatalf("reads=%d statements=%q commits=%d rollbacks=%d", reads, tx.statements, tx.commits, tx.rollbacks)
		}
		ended := recorder.Ended()
		if len(ended) != 1 {
			t.Fatalf("spans=%d", len(ended))
		}
		if got := spanAttributes(ended[0])[attribute.Key(statusSnapshotJITKey)]; got != statusSnapshotJITOff {
			t.Fatalf("span %s=%q, want %q", statusSnapshotJITKey, got, statusSnapshotJITOff)
		}
	}
}

// TestSnapshotStatusReaderFailsClosedWhenJITCannotBeDisabled keeps the read
// from running with an unknown JIT setting: no silent fallback.
func TestSnapshotStatusReaderFailsClosedWhenJITCannotBeDisabled(t *testing.T) {
	setErr := errors.New("seeded SET failure")
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tx := &snapshotTxStub{queryErr: setErr}
	reader := NewSnapshotStatusReader(snapshotBeginStub{begin: func(context.Context) (db.ReadTransaction, error) { return tx, nil }},
		func(db.Queryer) status.Reader {
			t.Fatal("factory called after the JIT setting failed")
			return nil
		}, provider.Tracer("test"))
	if _, err := reader.ReadStatusSnapshot(t.Context(), time.Now()); !errors.Is(err, setErr) {
		t.Fatalf("error=%v, want seeded SET failure", err)
	}
	if tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("commits=%d rollbacks=%d", tx.commits, tx.rollbacks)
	}
	attrs := spanAttributes(recorder.Ended()[0])
	if attrs[attribute.Key(statusSnapshotPhaseKey)] != statusSnapshotPhaseJIT || attrs[attribute.Key(statusSnapshotOutcomeKey)] != "error" {
		t.Fatalf("span attributes=%v", attrs)
	}
	if _, present := attrs[attribute.Key(statusSnapshotJITKey)]; present {
		t.Fatalf("span claims jit=%q after the SET failed", attrs[attribute.Key(statusSnapshotJITKey)])
	}
}

// jitControlSpy counts reader query-start events, business_query stage
// observations, and transaction_control outcomes on the guarded reader.
type jitControlSpy struct {
	mu       sync.Mutex
	starts   int
	business int
	control  map[Outcome]int
}

func (s *jitControlSpy) Observe(_ string, stage Stage, outcome Outcome, _ time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch stage {
	case StageBusinessQuery:
		s.business++
	case StageTransactionControl:
		if s.control == nil {
			s.control = map[Outcome]int{}
		}
		s.control[outcome]++
	}
}
func (*jitControlSpy) recordsReaderQueryStart(context.Context) bool { return true }
func (s *jitControlSpy) recordReaderQueryStart(context.Context, int64, readerBackendIdentity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts++
}

type jitControlConnector struct {
	execs   *[]string
	execErr error
}

func (c jitControlConnector) Connect(context.Context) (driver.Conn, error) {
	return &jitControlConn{execs: c.execs, execErr: c.execErr}, nil
}
func (jitControlConnector) Driver() driver.Driver { return terminalErrorDriver{} }

type jitControlConn struct {
	execs   *[]string
	execErr error
}

func (*jitControlConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (*jitControlConn) Close() error                        { return nil }
func (*jitControlConn) Begin() (driver.Tx, error)           { return queryIdentityTx{}, nil }
func (*jitControlConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return queryIdentityTx{}, nil
}

func (c *jitControlConn) ExecContext(_ context.Context, statement string, _ []driver.NamedValue) (driver.Result, error) {
	*c.execs = append(*c.execs, statement)
	if c.execErr != nil {
		return nil, c.execErr
	}
	return driver.ResultNoRows, nil
}

func (c *jitControlConn) QueryContext(_ context.Context, statement string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(statement, "pg_control_system()"):
		return &queryIdentityRows{columns: []string{"read_only", "recovery", "system_id", "database"}, values: []driver.Value{"on", false, "7", "eshu"}}, nil
	case statement == "SELECT $1::int":
		return &queryIdentityRows{columns: []string{"value"}, values: []driver.Value{int64(42)}}, nil
	case statement == statusSnapshotJITOffSQL:
		// Answer the SET on the business path too, so a regression that
		// skips the control path fails on the counts below, not here.
		return &queryIdentityRows{columns: []string{}}, nil
	}
	return nil, fmt.Errorf("unexpected query %q", statement)
}

// TestGuardedStatusSnapshotJITSettingIsControlSQL proves the guarded reader
// path sends the SET as transaction control: it adds no reader query-start
// event and no business_query observation, and a plain guarded snapshot that
// is not a status read gets no SET at all.
func TestGuardedStatusSnapshotJITSettingIsControlSQL(t *testing.T) {
	var execs []string
	spy := &jitControlSpy{}
	pool := sql.OpenDB(jitControlConnector{execs: &execs})
	defer pool.Close()
	access := &Access{
		reader: pool, observer: spy, samePrimary: true, replayTimeout: time.Second,
		lineage: newWriterLineage(physicalIdentity{systemID: "7", database: "eshu"}, lineageObservation{}, nil),
	}
	ctx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{owner: access, systemID: "7", database: "eshu"})

	plain, err := access.Reader().BeginReadOnlySnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.Rollback(); err != nil {
		t.Fatal(err)
	}
	if len(execs) != 0 || len(spy.control) != 0 {
		t.Fatalf("plain guarded snapshot executed %q (control=%v); the SET belongs to the status reader only", execs, spy.control)
	}
	spy.business, spy.starts = 0, 0

	reader := NewSnapshotStatusReader(access.Reader(), func(q db.Queryer) status.Reader {
		return snapshotReaderStub{read: func(ctx context.Context, _ status.SnapshotSelection) (status.RawSnapshot, error) {
			rows, err := q.QueryContext(ctx, "SELECT $1::int", 42)
			if err != nil {
				return status.RawSnapshot{}, err
			}
			return status.RawSnapshot{}, rows.Close()
		}}
	}, nil)
	if _, err := reader.ReadStatusSnapshot(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(execs) != 1 || execs[0] != statusSnapshotJITOffSQL {
		t.Fatalf("control statements=%q, want one %q", execs, statusSnapshotJITOffSQL)
	}
	// One BeginTx observation plus one business query; the SET adds neither
	// and is observed once as transaction_control.
	if spy.starts != 1 || spy.business != 2 {
		t.Fatalf("query starts=%d business observations=%d, want 1 and 2", spy.starts, spy.business)
	}
	if len(spy.control) != 1 || spy.control[OutcomeOK] != 1 {
		t.Fatalf("transaction_control observations=%v, want one ok", spy.control)
	}
	if got := pool.Stats().InUse; got != 0 {
		t.Fatalf("status snapshot leaked %d connections", got)
	}
}

// TestGuardedStatusSnapshotJITFailureIsAMetricsSignal proves a failed SET is
// visible without traces: one transaction_control observation with
// outcome=error, the read fails and releases its connection, and the
// per-request business accumulator does not count the control statement.
func TestGuardedStatusSnapshotJITFailureIsAMetricsSignal(t *testing.T) {
	var execs []string
	spy := &jitControlSpy{}
	pool := sql.OpenDB(jitControlConnector{execs: &execs, execErr: errors.New("seeded SET failure")})
	defer pool.Close()
	access := &Access{
		reader: pool, observer: spy, samePrimary: true, replayTimeout: time.Second,
		lineage: newWriterLineage(physicalIdentity{systemID: "7", database: "eshu"}, lineageObservation{}, nil),
	}
	ctx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{owner: access, systemID: "7", database: "eshu"})
	ctx, timings := db.WithStageTimings(ctx)
	reader := NewSnapshotStatusReader(access.Reader(), func(db.Queryer) status.Reader {
		t.Fatal("factory called after the JIT setting failed")
		return nil
	}, nil)
	if _, err := reader.ReadStatusSnapshot(ctx, time.Now()); err == nil {
		t.Fatal("status read succeeded after the SET failed")
	}
	if len(spy.control) != 1 || spy.control[OutcomeError] != 1 {
		t.Fatalf("transaction_control observations=%v, want one error", spy.control)
	}
	if got := timings.Count(db.ReaderStageBusinessQuery); got != 1 {
		t.Fatalf("request business_query count=%d, want 1 (BeginTx only)", got)
	}
	if got := pool.Stats().InUse; got != 0 {
		t.Fatalf("failed status snapshot leaked %d connections", got)
	}
	if got := closedReaderStage(StageTransactionControl); got != string(StageTransactionControl) {
		t.Fatalf("closed stage=%q, want %q", got, StageTransactionControl)
	}
}
