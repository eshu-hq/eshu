// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"fmt"
	"strings"
	"testing"
)

// sessionActivity is one pg_stat_activity row that is inside a transaction.
type sessionActivity struct {
	PID             int
	BackendType     string
	ApplicationName string
	State           string
}

// clientBackendType is pg_stat_activity.backend_type of a session opened by a
// client connection. Autovacuum, parallel and other server workers carry other
// values and are not part of the derive's transaction claim.
const clientBackendType = "client backend"

// clientTransactions keeps the client sessions among rows, the ones that count
// against the derive's "no open transaction during step 2" claim.
func clientTransactions(rows []sessionActivity) []sessionActivity {
	var kept []sessionActivity
	for _, row := range rows {
		if row.BackendType == clientBackendType {
			kept = append(kept, row)
		}
	}
	return kept
}

// describeSessions names every session so a failure shows its cause.
func describeSessions(rows []sessionActivity) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, fmt.Sprintf("pid=%d backend_type=%q application_name=%q state=%q",
			row.PID, row.BackendType, row.ApplicationName, row.State))
	}
	return strings.Join(parts, "; ")
}

// TestClientTransactionsIgnoreNonClientBackends: an autovacuum or parallel
// worker inside a transaction is not a client session of the derive and must
// not count (#11227); a client session does.
func TestClientTransactionsIgnoreNonClientBackends(t *testing.T) {
	rows := []sessionActivity{
		{PID: 11, BackendType: "autovacuum worker", State: "active"},
		{PID: 12, BackendType: "parallel worker", State: "active"},
		{PID: 13, BackendType: "client backend", ApplicationName: "other-client", State: "idle in transaction"},
	}
	got := clientTransactions(rows)
	if len(got) != 1 || got[0].PID != 13 {
		t.Fatalf("clientTransactions() = %s; want only pid 13", describeSessions(got))
	}
	if only := clientTransactions(rows[:2]); len(only) != 0 {
		t.Fatalf("clientTransactions(non-client only) = %s; want none", describeSessions(only))
	}
}

// TestDescribeSessionsNamesEveryField: a failure message must name the pid,
// backend type, application name and state of each counted session.
func TestDescribeSessionsNamesEveryField(t *testing.T) {
	msg := describeSessions([]sessionActivity{{PID: 7, BackendType: "client backend", ApplicationName: "probe-x", State: "idle in transaction"}})
	for _, want := range []string{"pid=7", `backend_type="client backend"`, `application_name="probe-x"`, `state="idle in transaction"`} {
		if !strings.Contains(msg, want) {
			t.Fatalf("describeSessions() = %q, missing %q", msg, want)
		}
	}
}
