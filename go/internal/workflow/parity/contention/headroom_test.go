// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"strings"
	"testing"
	"time"
)

const exporterPage = `# HELP pg_stat_database_numbackends Number of backends
# TYPE pg_stat_database_numbackends gauge
pg_stat_database_numbackends{datid="1",datname="eshu"} 57
pg_stat_database_numbackends{datid="2",datname="workflow_claims"} 4
pg_stat_activity_count{datname="eshu",state="active",wait_event_type="Lock"} 2
pg_stat_activity_count{datname="workflow_claims",state="idle",wait_event_type=""} 4
pg_stat_database_deadlocks{datname="eshu"} 1
pg_stat_database_deadlocks{datname="workflow_claims"} 0
pg_replication_lag_seconds{application_name="r\"0"} 0.5
pg_settings_max_connections 100
`

func TestParseExporterFoldsCeilingSeries(t *testing.T) {
	s, err := parseExporter(strings.NewReader(exporterPage), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Connections != 61 || s.LockWaiters != 2 || s.DeadlocksTotal != 1 || s.ReplicationLagSec != 0.5 {
		t.Fatalf("sample = %+v", s)
	}
	if s.MaxLockWaitMS != notAvailable {
		t.Fatalf("exporter lock wait = %v, want NOT_AVAILABLE", s.MaxLockWaitMS)
	}
}

func TestParseExporterLockDatabaseFilter(t *testing.T) {
	s, err := parseExporter(strings.NewReader(exporterPage), "workflow_claims")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.LockWaiters != 0 {
		t.Fatalf("lock waiters on workflow_claims = %d, want 0 (the 2 waiters are on eshu)", s.LockWaiters)
	}
}

func TestParseExporterMissingFamiliesStayNotAvailable(t *testing.T) {
	s, err := parseExporter(strings.NewReader("pg_up 1\n"), "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Connections != notAvailable || s.LockWaiters != notAvailable || s.ReplicationLagSec != notAvailable {
		t.Fatalf("missing families read as values: %+v", s)
	}
}

func TestHeadroomCheckTriggers(t *testing.T) {
	limits := Headroom{MaxConnections: 85, MaxLagSeconds: 30, MaxLockWaiters: 5}
	clean := HeadroomSample{Connections: 61, LockWaiters: 0, MaxLockWaitMS: 10, DeadlocksTotal: 1, ReplicationLagSec: 0}
	if v := headroomCheck(clean, limits, time.Second, 1); v != nil {
		t.Fatalf("clean sample breached: %+v", v)
	}
	cases := map[Trigger]HeadroomSample{
		TriggerDeadlock: {Connections: 61, MaxLockWaitMS: 0, DeadlocksTotal: 2, ReplicationLagSec: 0},
		TriggerLockWait: {Connections: 61, MaxLockWaitMS: 1500, DeadlocksTotal: 1, ReplicationLagSec: 0},
		TriggerHeadroom: {Connections: 90, MaxLockWaitMS: 0, DeadlocksTotal: 1, ReplicationLagSec: 0},
	}
	for want, sample := range cases {
		mustTrigger(t, headroomCheck(sample, limits, time.Second, 1), want)
	}
	lag := clean
	lag.ReplicationLagSec = 31
	mustTrigger(t, headroomCheck(lag, limits, time.Second, 1), TriggerHeadroom)
}
