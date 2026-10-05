// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// fleetStageConnector answers the member-fleet identity check (six columns,
// including the postmaster incarnation and server address), the replica replay
// fence, snapshot export, SET TRANSACTION SNAPSHOT, and one business row, so a
// fleet-routed read pays every guarded-reader stage without a database.
type fleetStageConnector struct{}

func (fleetStageConnector) Connect(context.Context) (driver.Conn, error) {
	return &fleetStageConn{}, nil
}
func (fleetStageConnector) Driver() driver.Driver { return terminalErrorDriver{} }

type fleetStageConn struct{}

func (*fleetStageConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (*fleetStageConn) Close() error                        { return nil }
func (*fleetStageConn) Begin() (driver.Tx, error)           { return queryIdentityTx{}, nil }
func (*fleetStageConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return queryIdentityTx{}, nil
}

func (*fleetStageConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.ResultNoRows, nil
}

func (*fleetStageConn) QueryContext(_ context.Context, statement string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(statement, "pg_control_system()"):
		return &queryIdentityRows{
			columns: []string{"read_only", "recovery", "system_id", "database", "incarnation", "server_address"},
			values:  []driver.Value{"on", true, "7", "eshu", "inc-1", "127.0.0.1"},
		}, nil
	case strings.Contains(statement, "pg_last_wal_replay_lsn()"):
		return &queryIdentityRows{columns: []string{"caught_up"}, values: []driver.Value{true}}, nil
	case statement == "SELECT pg_export_snapshot()":
		return &queryIdentityRows{columns: []string{"snapshot"}, values: []driver.Value{"00000003-0000001B-1"}}, nil
	}
	return &queryIdentityRows{columns: []string{"value"}, values: []driver.Value{int64(42)}}, nil
}

// newFleetStageFixtureAccess returns a two-member fleet Access over the fake
// driver plus a ctx carrying a checkpoint owned by it.
func newFleetStageFixtureAccess(tb testing.TB, observer Observer) (*Access, context.Context) {
	tb.Helper()
	members := make([]physicalReaderMember, 2)
	for i := range members {
		pool := sql.OpenDB(fleetStageConnector{})
		tb.Cleanup(func() { _ = pool.Close() })
		members[i] = physicalReaderMember{
			ordinal: i, pool: pool, maxOpen: 4, incarnation: "inc-1",
			addresses: []net.IP{net.ParseIP("127.0.0.1")},
		}
	}
	access := &Access{
		observer: observer, reader: members[0].pool, readerMembers: members, allocator: newReaderAllocator([]int{4, 4}, 8),
		replayTimeout: time.Second,
		lineage:       newWriterLineage(physicalIdentity{systemID: "7", database: "eshu", incarnation: "inc-1"}, lineageObservation{}, nil),
	}
	ctx := context.WithValue(context.Background(), checkpointKey{}, checkpoint{
		owner: access, systemID: "7", database: "eshu", incarnation: "inc-1", lsn: "0/10",
	})
	return access, ctx
}

// fleetStageReads lists the two fleet-routed reads and the exact number of
// observations each guarded stage receives. A plain query borrows once (one
// allocator reservation plus one connection). A two-connection snapshot set
// reserves once, borrows two connections, fences both, opens two transactions,
// runs pg_export_snapshot (observed by the read transaction, not the fleet),
// and runs SET TRANSACTION SNAPSHOT on the second, so every fleet call site in
// reader_fleet.go is hit and counted separately: business_query is two
// BeginTx, one export, and one SET.
var fleetStageReads = []struct {
	name string
	run  func(testing.TB, *Access, context.Context)
	want map[Stage]int
}{
	{
		name: "plain query",
		run:  runStageFixtureQuery,
		want: map[Stage]int{StageReaderBorrow: 2, StageReaderIdentity: 1, StageReaderReplay: 1, StageBusinessQuery: 1},
	},
	{
		name: "snapshot set",
		run: func(tb testing.TB, access *Access, ctx context.Context) {
			tb.Helper()
			set, err := access.Reader().(db.ReadSnapshotSetBeginner).BeginReadOnlySnapshotSet(ctx, 2)
			if err != nil {
				tb.Fatalf("begin snapshot set: %v (cause: %v)", err, errors.Unwrap(err))
			}
			if err := set.Close(); err != nil {
				tb.Fatalf("close snapshot set: %v", err)
			}
		},
		want: map[Stage]int{StageReaderBorrow: 3, StageReaderIdentity: 2, StageReaderReplay: 2, StageBusinessQuery: 4},
	},
}

// TestFleetReaderStagesAreChildrenOfTheRequestSpan is the #7545 fleet
// regression: the member-fleet borrow and business-query observations were
// recorded with no request context, so their postgres.reader_access spans were
// orphan roots even though the single-endpoint path joined the request trace.
func TestFleetReaderStagesAreChildrenOfTheRequestSpan(t *testing.T) {
	for _, tt := range fleetStageReads {
		t.Run(tt.name, func(t *testing.T) {
			spans := tracetest.NewSpanRecorder()
			traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
			observer, err := NewObserver(sdkmetric.NewMeterProvider().Meter("test"), traces.Tracer("test"))
			if err != nil {
				t.Fatal(err)
			}
			access, ctx := newFleetStageFixtureAccess(t, observer)
			ctx, request := traces.Tracer("test").Start(ctx, "request")
			tt.run(t, access, ctx)
			request.End()

			got := map[Stage]int{}
			for _, span := range spans.Ended() {
				if span.Name() != readerAccessSpanName {
					continue
				}
				var stage Stage
				for _, kv := range span.Attributes() {
					if kv.Key == attribute.Key(readerAttributeStage) {
						stage = Stage(kv.Value.AsString())
					}
				}
				got[stage]++
				parent := span.Parent()
				if !parent.IsValid() || parent.SpanID() != request.SpanContext().SpanID() ||
					span.SpanContext().TraceID() != request.SpanContext().TraceID() {
					t.Errorf("fleet stage %s span parent=%v, want child of request span %v (orphan root)",
						stage, parent.SpanID(), request.SpanContext().SpanID())
				}
			}
			for stage, want := range tt.want {
				if got[stage] != want {
					t.Errorf("%s spans for stage %s = %d, want %d (all=%v)", readerAccessSpanName, stage, got[stage], want, got)
				}
			}
		})
	}
}

// TestFleetReaderFillsRequestStageTimings proves a fleet-routed read under a
// ctx carrying db.WithStageTimings accounts every observation of the four
// guarded stages in the request accumulator, with a nil observer, so the
// per-request stage log line is also true on the member-fleet path.
func TestFleetReaderFillsRequestStageTimings(t *testing.T) {
	slots := map[Stage]db.ReaderStage{
		StageReaderBorrow: db.ReaderStageBorrow, StageReaderIdentity: db.ReaderStageIdentity,
		StageReaderReplay: db.ReaderStageReplay, StageBusinessQuery: db.ReaderStageBusinessQuery,
	}
	for _, tt := range fleetStageReads {
		t.Run(tt.name, func(t *testing.T) {
			access, ctx := newFleetStageFixtureAccess(t, nil)
			ctx, timings := db.WithStageTimings(ctx)
			tt.run(t, access, ctx)
			for stage, want := range tt.want {
				if got := timings.Count(slots[stage]); got != int64(want) {
					t.Errorf("accumulator Count(%s) = %d, want %d", stage, got, want)
				}
			}
		})
	}
}
