// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5/stdlib"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestReaderQueryStartEventKeepsRequestParentAndDistinctSequence(t *testing.T) {
	spans := tracetest.NewSpanRecorder()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	observer, err := NewObserver(sdkmetric.NewMeterProvider().Meter("test"), traces.Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, parent := traces.Tracer("test").Start(t.Context(), "request")
	access := &Access{observer: observer}
	identity := readerBackendIdentity{pid: 12345, remote: "10.1.2.3:5432", available: true}
	access.recordReaderQueryStart(ctx, identity)
	access.recordReaderQueryStart(ctx, identity)
	parent.End()
	ended := spans.Ended()
	if len(ended) != 1 || ended[0].Name() != "request" {
		t.Fatalf("ended spans = %v, want only request parent", ended)
	}
	events := ended[0].Events()
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	for i, event := range events {
		if event.Name != readerQueryStartEventName {
			t.Fatalf("event %d name = %q", i, event.Name)
		}
		attrs := make(map[attribute.Key]attribute.Value)
		for _, kv := range event.Attributes {
			attrs[kv.Key] = kv.Value
		}
		if attrs[readerQueryPIDKey].AsInt64() != 12345 || attrs[readerQueryRemoteKey].AsString() != "10.1.2.3:5432" || attrs[readerQueryRoleKey].AsString() != "reader" || attrs[readerQueryIdentityKey].AsString() != "available" {
			t.Fatalf("event %d attrs = %v", i, attrs)
		}
		if attrs[readerQuerySequenceKey].AsInt64() != int64(i+1) {
			t.Fatalf("event %d sequence = %d", i, attrs[readerQuerySequenceKey].AsInt64())
		}
		if len(attrs) != 5 {
			t.Fatalf("event %d has unexpected attrs: %v", i, attrs)
		}
	}
}

func TestReaderQueryIdentityUnavailableDoesNotFailOrDiscardLease(t *testing.T) {
	pool := sql.OpenDB(idleConnector{})
	defer pool.Close()
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	identity := captureReaderBackendIdentity(conn)
	if identity.available || identity.pid != 0 || identity.remote != "" {
		t.Fatalf("unsupported driver identity = %+v", identity)
	}
	if err := conn.Raw(func(any) error { return nil }); err != nil {
		t.Fatalf("healthy lease discarded: %v", err)
	}
	spans := tracetest.NewSpanRecorder()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	observer, err := NewObserver(sdkmetric.NewMeterProvider().Meter("test"), traces.Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, parent := traces.Tracer("test").Start(context.Background(), "request")
	(&Access{observer: observer}).recordReaderQueryStart(ctx, identity)
	parent.End()
	events := spans.Ended()[0].Events()
	if len(events) != 1 || events[0].Name != readerQueryStartEventName {
		t.Fatalf("unavailable event = %v", events)
	}
	attrs := make(map[attribute.Key]attribute.Value)
	for _, kv := range events[0].Attributes {
		attrs[kv.Key] = kv.Value
	}
	if len(attrs) != 3 || attrs[readerQueryRoleKey].AsString() != "reader" ||
		attrs[readerQuerySequenceKey].AsInt64() != 1 ||
		attrs[readerQueryIdentityKey].AsString() != "unavailable" {
		t.Fatalf("unavailable event attributes = %v", attrs)
	}
	if _, present := attrs[readerQueryPIDKey]; present {
		t.Fatal("unavailable event contains backend PID")
	}
	if _, present := attrs[readerQueryRemoteKey]; present {
		t.Fatal("unavailable event contains backend peer")
	}
}

type queryStartSpy struct{ starts int }

func (*queryStartSpy) Observe(string, Stage, Outcome, time.Duration) {}
func (*queryStartSpy) recordsReaderQueryStart(context.Context) bool  { return true }
func (s *queryStartSpy) recordReaderQueryStart(_ context.Context, _ uint64, identity readerBackendIdentity) {
	if identity.available {
		panic("fake driver unexpectedly reported a backend")
	}
	s.starts++
}

type queryIdentityConnector struct{ spy *queryStartSpy }

func (c queryIdentityConnector) Connect(context.Context) (driver.Conn, error) {
	return &queryIdentityConn{spy: c.spy}, nil
}
func (queryIdentityConnector) Driver() driver.Driver { return terminalErrorDriver{} }

type queryIdentityConn struct{ spy *queryStartSpy }

func (*queryIdentityConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (*queryIdentityConn) Close() error                        { return nil }
func (*queryIdentityConn) Begin() (driver.Tx, error)           { return queryIdentityTx{}, nil }
func (*queryIdentityConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return queryIdentityTx{}, nil
}

func (c *queryIdentityConn) QueryContext(_ context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(statement, "pg_control_system()") {
		if c.spy.starts != 0 {
			return nil, errors.New("identity query followed a business start event")
		}
		return &queryIdentityRows{columns: []string{"read_only", "recovery", "system_id", "database"}, values: []driver.Value{"on", false, "7", "eshu"}}, nil
	}
	if statement != "SELECT $1::int" || len(args) != 1 || args[0].Value != int64(42) || c.spy.starts != 1 {
		return nil, fmt.Errorf("business query or start order changed: statement=%q args=%v starts=%d", statement, args, c.spy.starts)
	}
	return &queryIdentityRows{columns: []string{"value"}, values: []driver.Value{int64(42)}}, nil
}

type queryIdentityTx struct{}

func (queryIdentityTx) Commit() error   { return nil }
func (queryIdentityTx) Rollback() error { return nil }

type queryIdentityRows struct {
	columns []string
	values  []driver.Value
}

func (r *queryIdentityRows) Columns() []string { return r.columns }
func (*queryIdentityRows) Close() error        { return nil }
func (r *queryIdentityRows) Next(dest []driver.Value) error {
	if r.values == nil {
		return io.EOF
	}
	copy(dest, r.values)
	r.values = nil
	return nil
}

func TestGuardedReaderEmitsStartBeforeUnchangedBusinessSQL(t *testing.T) {
	for _, snapshot := range []bool{false, true} {
		t.Run(fmt.Sprint("snapshot=", snapshot), func(t *testing.T) {
			spy := &queryStartSpy{}
			pool := sql.OpenDB(queryIdentityConnector{spy: spy})
			defer pool.Close()
			access := &Access{reader: pool, observer: spy, samePrimary: true, replayTimeout: time.Second, identity: physicalIdentity{systemID: "7", database: "eshu"}}
			ctx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{owner: access, systemID: "7", database: "eshu"})
			var rows db.Rows
			var err error
			if snapshot {
				tx, beginErr := access.Reader().BeginReadOnlySnapshot(ctx)
				if beginErr != nil {
					t.Fatal(beginErr)
				}
				defer tx.Rollback()
				rows, err = tx.QueryContext(ctx, "SELECT $1::int", 42)
			} else {
				rows, err = access.Reader().QueryContext(ctx, "SELECT $1::int", 42)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			if !rows.Next() {
				t.Fatal("missing business row")
			}
			var value int
			if err := rows.Scan(&value); err != nil {
				t.Fatal(err)
			}
			if value != 42 || spy.starts != 1 {
				t.Fatalf("value=%d starts=%d", value, spy.starts)
			}
		})
	}
}

// BenchmarkReaderQueryStartRecordedRequest measures 22 start events plus a
// synchronous in-memory exporter per request. It does not measure network
// export, a deployed binary, or the native pgx connection getter.
func BenchmarkReaderQueryStartRecordedRequest(b *testing.B) {
	for _, emitted := range []bool{false, true} {
		name := "baseline"
		if emitted {
			name = "events"
		}
		b.Run(name, func(b *testing.B) {
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
			defer provider.Shutdown(context.Background())
			observer, err := NewObserver(sdkmetric.NewMeterProvider().Meter("benchmark"), provider.Tracer("benchmark"))
			if err != nil {
				b.Fatal(err)
			}
			access := &Access{observer: observer}
			identity := readerBackendIdentity{pid: 12345, remote: "10.1.2.3:5432", available: true}
			tracer := provider.Tracer("benchmark")
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				ctx, span := tracer.Start(context.Background(), "request")
				if emitted {
					for range 22 {
						access.recordReaderQueryStart(ctx, identity)
					}
				}
				span.End()
			}
			b.StopTimer()
			if got := len(exporter.GetSpans()); got != b.N {
				b.Fatalf("exported spans=%d, want %d", got, b.N)
			}
		})
	}
}

// TestReaderQueryIdentityMatchesOneNativeLease is opt-in and read-only. Its
// DSN must already target an owned physical standby; no write-side Access or
// collector setup occurs here.
func TestReaderQueryIdentityMatchesOneNativeLease(t *testing.T) {
	dsn := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if dsn == "" {
		t.Skip("owned read-only PostgreSQL fixture not configured")
	}
	cfg, err := parsePhysicalEndpoint(dsn)
	if err != nil {
		t.Fatal("parse reader fixture endpoint")
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = make(map[string]string)
	}
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	pool := stdlib.OpenDB(*cfg)
	defer pool.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal("borrow reader fixture lease")
	}
	defer conn.Close()
	identity := captureReaderBackendIdentity(conn)
	if !identity.available || identity.pid == 0 || identity.remote == "" {
		t.Fatal("native reader lease identity unavailable")
	}
	var nativePID uint32
	var recovery bool
	var readOnly string
	if err := conn.QueryRowContext(ctx, "SELECT pg_backend_pid(), pg_is_in_recovery(), current_setting('transaction_read_only')").Scan(&nativePID, &recovery, &readOnly); err != nil {
		t.Fatal("read native reader fixture metadata")
	}
	if nativePID != identity.pid || !recovery || readOnly != "on" {
		t.Fatal("native reader fixture identity, recovery role, or read-only mode mismatch")
	}
	exporter := tracetest.NewInMemoryExporter()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer traces.Shutdown(context.Background())
	observer, err := NewObserver(sdkmetric.NewMeterProvider().Meter("test"), traces.Tracer("test"))
	if err != nil {
		t.Fatal("create reader fixture observer")
	}
	request, parent := traces.Tracer("test").Start(ctx, "request")
	(&Access{observer: observer}).startReaderQuery(request, conn)
	parent.End()
	spans := exporter.GetSpans()
	if len(spans) != 1 || len(spans[0].Events) != 1 {
		t.Fatal("native reader query event was not retained")
	}
	seenPID := false
	for _, kv := range spans[0].Events[0].Attributes {
		if kv.Key == readerQueryPIDKey && kv.Value.AsInt64() == int64(nativePID) {
			seenPID = true
		}
	}
	if !seenPID {
		t.Fatal("native reader query event PID does not match the leased backend")
	}
}
