// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"
)

// stageFixtureBusinessDelay makes the business_query stage measurably
// positive without a real database.
const stageFixtureBusinessDelay = 2 * time.Millisecond

// stageFixtureConnector is a fake driver that answers all four guarded-reader
// stages: the identity check, the replica replay fence, and one business row.
type stageFixtureConnector struct{ businessDelay time.Duration }

func (c stageFixtureConnector) Connect(context.Context) (driver.Conn, error) {
	return &stageFixtureConn{businessDelay: c.businessDelay}, nil
}
func (stageFixtureConnector) Driver() driver.Driver { return terminalErrorDriver{} }

type stageFixtureConn struct{ businessDelay time.Duration }

func (*stageFixtureConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (*stageFixtureConn) Close() error                        { return nil }
func (*stageFixtureConn) Begin() (driver.Tx, error)           { return queryIdentityTx{}, nil }

func (c *stageFixtureConn) QueryContext(_ context.Context, statement string, _ []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(statement, "pg_control_system()"):
		return &queryIdentityRows{columns: []string{"read_only", "recovery", "system_id", "database"}, values: []driver.Value{"on", true, "7", "eshu"}}, nil
	case strings.Contains(statement, "pg_last_wal_replay_lsn()"):
		return &queryIdentityRows{columns: []string{"caught_up"}, values: []driver.Value{true}}, nil
	}
	if c.businessDelay > 0 {
		time.Sleep(c.businessDelay)
	}
	return &queryIdentityRows{columns: []string{"value"}, values: []driver.Value{int64(42)}}, nil
}

// newStageFixtureAccess returns an Access over the fake replica driver. With
// samePrimary false the guarded reader pays all four stages per query.
func newStageFixtureAccess(tb testing.TB, observer Observer, businessDelay time.Duration) (*Access, context.Context) {
	tb.Helper()
	pool := sql.OpenDB(stageFixtureConnector{businessDelay: businessDelay})
	tb.Cleanup(func() { _ = pool.Close() })
	access := &Access{
		reader: pool, observer: observer, replayTimeout: time.Second,
		identity: physicalIdentity{systemID: "7", database: "eshu"},
	}
	ctx := context.WithValue(context.Background(), checkpointKey{}, checkpoint{owner: access, systemID: "7", database: "eshu", lsn: "0/10"})
	return access, ctx
}

// runStageFixtureQuery runs one fenced business query on ctx and drains it.
func runStageFixtureQuery(tb testing.TB, access *Access, ctx context.Context) {
	tb.Helper()
	rows, err := access.Reader().QueryContext(ctx, "SELECT $1::int", 42)
	if err != nil {
		tb.Fatalf("fenced query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var value int
		if err := rows.Scan(&value); err != nil {
			tb.Fatalf("scan: %v", err)
		}
	}
	if err := rows.Err(); err != nil {
		tb.Fatalf("rows: %v", err)
	}
}
