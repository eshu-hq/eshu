// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"
	"time"
)

type snapshotEventSpy struct {
	queryStartSpy
	businessObservations int
	failedObservations   int
	sequence             int64
}

func (s *snapshotEventSpy) Observe(_ string, stage Stage, outcome Outcome, _ time.Duration) {
	if stage == StageBusinessQuery {
		s.businessObservations++
		if outcome != OutcomeOK {
			s.failedObservations++
		}
	}
}

func (s *snapshotEventSpy) recordReaderQueryStart(ctx context.Context, sequence int64, identity readerBackendIdentity) {
	s.queryStartSpy.recordReaderQueryStart(ctx, sequence, identity)
	s.sequence = sequence
}

type snapshotEventConnector struct {
	spy         *snapshotEventSpy
	exportError error
}

func (c snapshotEventConnector) Connect(context.Context) (driver.Conn, error) {
	return &snapshotEventConn{queryIdentityConn: queryIdentityConn{spy: &c.spy.queryStartSpy}, exportError: c.exportError}, nil
}
func (snapshotEventConnector) Driver() driver.Driver { return terminalErrorDriver{} }

type snapshotEventConn struct {
	queryIdentityConn
	exportError error
}

func (c *snapshotEventConn) QueryContext(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
	if statement == "SELECT pg_export_snapshot()" {
		if c.exportError != nil {
			return nil, c.exportError
		}
		return &queryIdentityRows{columns: []string{"snapshot"}, values: []driver.Value{"00000003-0000001B-1"}}, nil
	}
	return c.queryIdentityConn.QueryContext(ctx, statement, args)
}

func (*snapshotEventConn) ExecContext(_ context.Context, statement string, args []driver.NamedValue) (driver.Result, error) {
	if statement != "SET TRANSACTION SNAPSHOT '00000003-0000001B-1'" || len(args) != 0 {
		return nil, fmt.Errorf("unexpected snapshot import: %q", statement)
	}
	return driver.RowsAffected(0), nil
}

func TestSnapshotSetControlStatementsDoNotEmitBusinessStart(t *testing.T) {
	for _, failExport := range []bool{false, true} {
		t.Run(fmt.Sprint("exportFailure=", failExport), func(t *testing.T) {
			spy := &snapshotEventSpy{}
			var exportError error
			if failExport {
				exportError = errors.New("seeded export failure")
			}
			pool := sql.OpenDB(snapshotEventConnector{spy: spy, exportError: exportError})
			defer pool.Close()
			pool.SetMaxOpenConns(2)
			access := &Access{
				reader: pool, observer: spy, samePrimary: true, replayTimeout: time.Second,
				identity:        physicalIdentity{systemID: "7", database: "eshu"},
				snapshotSetGate: make(chan struct{}, 1), readerPermits: make(chan struct{}, 2),
			}
			access.snapshotSetGate <- struct{}{}
			for range 2 {
				access.readerPermits <- struct{}{}
			}
			ctx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{owner: access, systemID: "7", database: "eshu"})
			set, err := (fencedQueryer{access: access}).BeginReadOnlySnapshotSet(ctx, 2)
			if failExport {
				if !errors.Is(err, exportError) || set != nil {
					t.Fatalf("export failure: set=%v err=%v", set, err)
				}
				if spy.businessObservations != 2 || spy.failedObservations != 1 {
					t.Fatalf("export observations: %+v", spy)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer set.Close()
				if spy.starts != 0 || access.querySequence.Load() != 0 {
					t.Fatalf("control statements emitted %d events; sequence=%d", spy.starts, access.querySequence.Load())
				}
				if spy.businessObservations != 4 || spy.failedObservations != 0 {
					t.Fatalf("setup observations: %+v", spy)
				}
				queryer, err := set.Reader(1)
				if err != nil {
					t.Fatal(err)
				}
				var value int
				rows, err := queryer.QueryContext(ctx, "SELECT $1::int", 42)
				if err != nil {
					t.Fatal(err)
				}
				if err := (&fencedRow{rows: rows}).Scan(&value); err != nil {
					t.Fatal(err)
				}
				if value != 42 || spy.starts != 1 || spy.sequence != 1 || spy.businessObservations != 5 {
					t.Fatalf("business result=%d observations=%+v", value, spy)
				}
				if err := set.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if spy.starts != 0 && failExport {
				t.Fatalf("failed control statement emitted %d events", spy.starts)
			}
			if pool.Stats().InUse != 0 || len(access.readerPermits) != 2 || len(access.snapshotSetGate) != 1 {
				t.Fatalf("leaked snapshot resources: in-use=%d permits=%d gate=%d", pool.Stats().InUse, len(access.readerPermits), len(access.snapshotSetGate))
			}
		})
	}
}
