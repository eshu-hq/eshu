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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// exportSpyStats counts every statement kind a fake reader received, so a test
// asserts that a refused snapshot set issued no pg_export_snapshot and no SET
// TRANSACTION SNAPSHOT, instead of inferring it from a returned error.
type exportSpyStats struct {
	dials, identity, begins, exports, imports, business atomic.Int64
}

func (s *exportSpyStats) snapshot() [6]int64 {
	return [6]int64{s.dials.Load(), s.identity.Load(), s.begins.Load(), s.exports.Load(), s.imports.Load(), s.business.Load()}
}

// exportSpyConnector answers the guarded-reader identity statement with a
// fixed build, the replica replay fence, snapshot export and import, and one
// business row. The member identity statement gets its eight columns.
type exportSpyConnector struct {
	stats      *exportSpyStats
	version    string
	versionNum int64
	recovery   bool
}

func (c exportSpyConnector) Connect(context.Context) (driver.Conn, error) {
	c.stats.dials.Add(1)
	return &exportSpyConn{connector: c}, nil
}
func (exportSpyConnector) Driver() driver.Driver { return terminalErrorDriver{} }

type exportSpyConn struct{ connector exportSpyConnector }

func (*exportSpyConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (*exportSpyConn) Close() error                        { return nil }
func (c *exportSpyConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *exportSpyConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.connector.stats.begins.Add(1)
	return queryIdentityTx{}, nil
}

func (c *exportSpyConn) ExecContext(_ context.Context, statement string, _ []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(statement, "SET TRANSACTION SNAPSHOT") {
		c.connector.stats.imports.Add(1)
	}
	return driver.RowsAffected(0), nil
}

func (c *exportSpyConn) QueryContext(_ context.Context, statement string, _ []driver.NamedValue) (driver.Rows, error) {
	stats := c.connector.stats
	switch {
	case strings.Contains(statement, "pg_control_system()"):
		stats.identity.Add(1)
		if strings.Contains(statement, "inet_server_addr") {
			return &queryIdentityRows{
				columns: []string{"read_only", "recovery", "system_id", "database", "incarnation", "server_address", "server_version", "server_version_num"},
				values:  []driver.Value{"on", c.connector.recovery, "7", "eshu", "inc-1", "127.0.0.1", c.connector.version, c.connector.versionNum},
			}, nil
		}
		return &queryIdentityRows{
			columns: []string{"read_only", "recovery", "system_id", "database", "server_version", "server_version_num"},
			values:  []driver.Value{"on", c.connector.recovery, "7", "eshu", c.connector.version, c.connector.versionNum},
		}, nil
	case strings.Contains(statement, "pg_last_wal_replay_lsn()"):
		return &queryIdentityRows{columns: []string{"caught_up"}, values: []driver.Value{true}}, nil
	case statement == "SELECT pg_export_snapshot()":
		stats.exports.Add(1)
		return &queryIdentityRows{columns: []string{"snapshot"}, values: []driver.Value{"00000003-0000001B-1"}}, nil
	}
	stats.business.Add(1)
	return &queryIdentityRows{columns: []string{"value"}, values: []driver.Value{int64(42)}}, nil
}

// exportSpyObserver counts guarded stage observations and snapshot-export
// denials by reason.
type exportSpyObserver struct {
	mu      sync.Mutex
	stages  map[Stage]int
	denials []string
}

func newExportSpyObserver() *exportSpyObserver {
	return &exportSpyObserver{stages: map[Stage]int{}}
}

func (o *exportSpyObserver) Observe(_ string, stage Stage, _ Outcome, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stages[stage]++
}

func (o *exportSpyObserver) ObserveSnapshotExportDenial(_ context.Context, reason string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.denials = append(o.denials, reason)
}

func (o *exportSpyObserver) counts() (borrows, business int, denials []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stages[StageReaderBorrow], o.stages[StageBusinessQuery], append([]string(nil), o.denials...)
}

// newExportSpySingleAccess builds a same-primary single-pool Access over the
// spy driver, with the production gate, permits, and verdict holder.
func newExportSpySingleAccess(t *testing.T, stats *exportSpyStats, version string, versionNum int64, builds []SnapshotExportBuild, observer Observer) (*Access, context.Context) {
	t.Helper()
	pool := sql.OpenDB(exportSpyConnector{stats: stats, version: version, versionNum: versionNum})
	t.Cleanup(func() { _ = pool.Close() })
	pool.SetMaxOpenConns(4)
	pool.SetMaxIdleConns(4)
	access := &Access{
		reader: pool, observer: observer, samePrimary: true, replayTimeout: time.Second,
		lineage:         newWriterLineage(physicalIdentity{systemID: "7", database: "eshu"}, lineageObservation{}, nil),
		snapshotSetGate: make(chan struct{}, 1), readerPermits: make(chan struct{}, 4),
		snapshotExport: newSnapshotExportQualification(builds),
	}
	access.snapshotSetGate <- struct{}{}
	for range 4 {
		access.readerPermits <- struct{}{}
	}
	ctx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{owner: access, systemID: "7", database: "eshu"})
	return access, ctx
}

// exportSpyMember describes one fleet member's reported build.
type exportSpyMember struct {
	stats      *exportSpyStats
	version    string
	versionNum int64
}

// newExportSpyFleetAccess builds a physical-standby fleet Access whose members
// report the given builds. Each member's verdict is seeded the way
// qualifyReaderMember seeds it at bootstrap.
func newExportSpyFleetAccess(t *testing.T, builds []SnapshotExportBuild, observer Observer, members ...exportSpyMember) (*Access, context.Context) {
	t.Helper()
	fleet := make([]physicalReaderMember, len(members))
	caps := make([]int, len(members))
	for i, spec := range members {
		pool := sql.OpenDB(exportSpyConnector{stats: spec.stats, version: spec.version, versionNum: spec.versionNum, recovery: true})
		t.Cleanup(func() { _ = pool.Close() })
		pool.SetMaxOpenConns(4)
		pool.SetMaxIdleConns(4)
		holder := newSnapshotExportQualification(builds)
		holder.observe(spec.version, int(spec.versionNum), true)
		fleet[i] = physicalReaderMember{
			ordinal: i, pool: pool, maxOpen: 4, incarnation: "inc-1",
			addresses: []net.IP{net.ParseIP("127.0.0.1")}, snapshotExport: holder,
		}
		caps[i] = 4
	}
	access := &Access{
		observer: observer, reader: fleet[0].pool, readerMembers: fleet, readerInventoryCount: len(fleet),
		allocator: newReaderAllocator(caps, 4*len(fleet)), replayTimeout: time.Second,
		lineage: newWriterLineage(physicalIdentity{systemID: "7", database: "eshu", incarnation: "inc-1"}, lineageObservation{}, nil),
	}
	ctx := context.WithValue(t.Context(), checkpointKey{}, checkpoint{
		owner: access, systemID: "7", database: "eshu", incarnation: "inc-1", lsn: "0/10",
	})
	return access, ctx
}

func assertAllocatorIdle(t *testing.T, access *Access) {
	t.Helper()
	reserved, waiting := access.allocator.pressure()
	for index := range reserved {
		if reserved[index] != 0 || waiting[index] != 0 {
			t.Fatalf("member %d leaked reservation=%d waiters=%d", index, reserved[index], waiting[index])
		}
	}
	for index, member := range access.readerMembers {
		if inUse := member.pool.Stats().InUse; inUse != 0 {
			t.Fatalf("member %d leaked %d connections", index, inUse)
		}
	}
}

// qualifySnapshotExport marks a hand-built Access and its fleet members as
// running a floor-qualified build, the state Open reaches against PostgreSQL
// 18.7 or later. Tests of snapshot-set mechanics other than #7957 use it so the
// deny-by-default holder does not refuse their sets before the code under test.
func qualifySnapshotExport(access *Access) *Access {
	if access.snapshotExport == nil {
		access.snapshotExport = newSnapshotExportQualification(nil)
	}
	access.snapshotExport.observe("18.7", 180007, !access.samePrimary)
	for i := range access.readerMembers {
		if access.readerMembers[i].snapshotExport == nil {
			access.readerMembers[i].snapshotExport = newSnapshotExportQualification(nil)
		}
		access.readerMembers[i].snapshotExport.observe("18.7", 180007, true)
	}
	return access
}
