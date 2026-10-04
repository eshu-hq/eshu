// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestFleetOwnedTransactionQueryIdentity pins the composition of a fleet-owned
// transaction with the reader query-start observer on its business path.
func TestFleetOwnedTransactionQueryIdentity(t *testing.T) {
	spy := &queryStartSpy{}
	pool := sql.OpenDB(queryIdentityConnector{spy: spy})
	t.Cleanup(func() { _ = pool.Close() })
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	lease := &readerConnection{Conn: conn, member: &physicalReaderMember{}}
	access := &Access{observer: spy}
	tx, err := beginReadTransactionOwned(t.Context(), lease, access)
	if err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	defer tx.Rollback()
	if spy.checks != 1 || spy.starts != 0 {
		t.Fatalf("setup observer checks=%d starts=%d, want one identity check and no business start", spy.checks, spy.starts)
	}
	rows, err := tx.QueryContext(context.Background(), "SELECT $1::int", 42)
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
		t.Fatalf("value=%d starts=%d, want value=42 and one start", value, spy.starts)
	}
}

// TestFleetOwnedNativeQueryIdentity is opt-in and reads only from an owned
// PostgreSQL fixture. It checks the exact lease used by a fleet transaction.
func TestFleetOwnedNativeQueryIdentity(t *testing.T) {
	dsn := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if dsn == "" {
		t.Skip("owned PostgreSQL reader fixture not configured")
	}
	cfg, err := parsePhysicalEndpoint(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = make(map[string]string)
	}
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	pool := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { _ = pool.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lease := &readerConnection{Conn: conn, member: &physicalReaderMember{}}
	spans := tracetest.NewSpanRecorder()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	defer traces.Shutdown(context.Background())
	observer, err := NewObserver(sdkmetric.NewMeterProvider().Meter("test"), traces.Tracer("test"))
	if err != nil {
		t.Fatal(err)
	}
	request, parent := traces.Tracer("test").Start(ctx, "request")
	access := &Access{observer: observer}
	tx, err := beginReadTransactionOwned(request, lease, access)
	if err != nil {
		parent.End()
		_ = lease.Close()
		t.Fatal(err)
	}
	var backendPID int64
	if err := tx.QueryRowContext(request, "SELECT pg_backend_pid()").Scan(&backendPID); err != nil {
		parent.End()
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		parent.End()
		t.Fatal(err)
	}
	parent.End()
	var requestEvents []sdktrace.Event
	for _, span := range spans.Ended() {
		if span.Name() == "request" {
			requestEvents = span.Events()
		}
	}
	if len(requestEvents) != 1 {
		t.Fatalf("request events=%d, want one", len(requestEvents))
	}
	event := requestEvents[0]
	if event.Name != readerQueryStartEventName {
		t.Fatalf("event=%s", event.Name)
	}
	var eventPID int64
	var remote string
	for _, attr := range event.Attributes {
		switch attr.Key {
		case readerQueryPIDKey:
			eventPID = attr.Value.AsInt64()
		case readerQueryRemoteKey:
			remote = attr.Value.AsString()
		}
	}
	if eventPID != backendPID || remote == "" || pool.Stats().InUse != 0 {
		t.Fatalf("event pid=%d backend pid=%d remote present=%t in_use=%d", eventPID, backendPID, remote != "", pool.Stats().InUse)
	}
}

type fleetEventConnector struct {
	spy         *snapshotEventSpy
	exportError error
}

func (c fleetEventConnector) Connect(context.Context) (driver.Conn, error) {
	return &fleetEventConn{snapshotEventConn: snapshotEventConn{
		queryIdentityConn: queryIdentityConn{spy: &c.spy.queryStartSpy},
		exportError:       c.exportError,
	}}, nil
}

func (fleetEventConnector) Driver() driver.Driver { return terminalErrorDriver{} }

type fleetEventConn struct{ snapshotEventConn }

func (c *fleetEventConn) QueryContext(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(statement, "pg_control_system()") {
		return &queryIdentityRows{
			columns: []string{"read_only", "recovery", "system_id", "database", "incarnation", "address"},
			values:  []driver.Value{"on", true, "7", "eshu", "123", "127.0.0.1"},
		}, nil
	}
	if strings.Contains(statement, "pg_last_wal_replay_lsn()") {
		return &queryIdentityRows{columns: []string{"caught_up"}, values: []driver.Value{true}}, nil
	}
	return c.snapshotEventConn.QueryContext(ctx, statement, args)
}

func TestFleetSnapshotSetControlStatementsDoNotEmitBusinessStart(t *testing.T) {
	for _, failExport := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "export_failure"}[failExport], func(t *testing.T) {
			spy := &snapshotEventSpy{}
			var exportError error
			if failExport {
				exportError = errors.New("seeded export failure")
			}
			pool := sql.OpenDB(fleetEventConnector{spy: spy, exportError: exportError})
			pool.SetMaxOpenConns(2)
			t.Cleanup(func() { _ = pool.Close() })
			allocator := newReaderAllocator([]int{2}, 2)
			access := &Access{
				observer: spy, replayTimeout: time.Second,
				identity:      physicalIdentity{systemID: "7", database: "eshu"},
				readerMembers: []physicalReaderMember{{pool: pool, incarnation: "123", addresses: []net.IP{net.ParseIP("127.0.0.1")}}},
				allocator:     allocator,
			}
			ctx := t.Context()
			reservation, err := allocator.reserve(ctx, []int{0}, 2)
			if err != nil {
				t.Fatal(err)
			}
			point := checkpoint{lsn: "0/1", systemID: "7", database: "eshu"}
			set, err := access.beginSnapshotSetReserved(ctx, ctx, 2, reservation, point)
			if failExport {
				if !errors.Is(err, exportError) || set != nil {
					t.Fatalf("export result: set=%v err=%v", set, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer set.Close()
			}
			if spy.starts != 0 || access.querySequence.Load() != 0 {
				t.Fatalf("fleet control emitted %d starts, sequence=%d", spy.starts, access.querySequence.Load())
			}
			if set != nil {
				queryer, err := set.Reader(1)
				if err != nil {
					t.Fatal(err)
				}
				rows, err := queryer.QueryContext(ctx, "SELECT $1::int", 42)
				if err != nil {
					t.Fatal(err)
				}
				var value int
				if err := (&fencedRow{rows: rows}).Scan(&value); err != nil {
					t.Fatal(err)
				}
				if value != 42 || spy.starts != 1 || access.querySequence.Load() != 1 {
					t.Fatalf("business value=%d starts=%d sequence=%d", value, spy.starts, access.querySequence.Load())
				}
				if err := set.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if pool.Stats().InUse != 0 || allocator.total != 0 {
				t.Fatalf("fleet resources retained: in_use=%d reserved=%d", pool.Stats().InUse, allocator.total)
			}
		})
	}
}
