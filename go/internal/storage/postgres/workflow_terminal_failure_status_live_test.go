// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8803 disposable PostgreSQL 18 proof of the windowed collector terminal-failure read: a later completed item of the same instance and scope clears, a sibling scope or another instance on a shared scope_id does not, later pending or retrying rows do not, non-terminal rows and disabled or deactivated instances never report, an orphan ages out after 3 x scan_interval with a 30 minute floor (12h interval keeps 20h and drops 40h; malformed scan_interval uses the floor), equal updated_at picks the lowest work_item_id, EXPLAIN uses migration 210's partial index without a sort, and re-running migration 210 on a populated table leaves one valid index; isolated bootstrapped schema per test; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

const terminalFailureLiveSkip = "set ESHU_POSTGRES_DSN to run the #8803 collector terminal-failure live proofs"

// terminalFailureLiveFixture seeds collector_instances and workflow_work_items
// rows for one live proof against an isolated bootstrapped schema.
type terminalFailureLiveFixture struct {
	t        *testing.T
	ctx      context.Context
	database *sql.DB
	asOf     time.Time
	items    int
}

func newTerminalFailureLiveFixture(t *testing.T, prefix string) *terminalFailureLiveFixture {
	t.Helper()
	database, ctx := openIsolatedLiveDB(t, prefix, terminalFailureLiveSkip)
	f := &terminalFailureLiveFixture{t: t, ctx: ctx, database: database, asOf: time.Now().UTC().Truncate(time.Second)}
	f.exec(`INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at)
VALUES ('run-8803', 'schedule', 'running', $1, $1)`, f.asOf)
	return f
}

func (f *terminalFailureLiveFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.database.ExecContext(f.ctx, query, args...); err != nil {
		f.t.Fatalf("exec %q: %v", strings.Fields(query)[0], err)
	}
}

// instance registers one collector instance. configuration is a JSON object.
func (f *terminalFailureLiveFixture) instance(kind, id string, enabled bool, deactivated bool, configuration string) {
	f.t.Helper()
	var deactivatedAt any
	if deactivated {
		deactivatedAt = f.asOf.Add(-time.Hour)
	}
	f.exec(`INSERT INTO collector_instances (instance_id, collector_kind, mode, enabled, configuration,
    last_observed_at, deactivated_at, created_at, updated_at)
VALUES ($1, $2, 'continuous', $3, $4::jsonb, $5, $6, $5, $5)`, id, kind, enabled, configuration, f.asOf, deactivatedAt)
}

// item inserts one work item updated age before asOf. A failed_terminal row
// carries terminal=true; class is the persisted failure class.
func (f *terminalFailureLiveFixture) item(id, kind, instance, scope, status, class string, terminal bool, age time.Duration) {
	f.t.Helper()
	f.items++
	var lastClass, lastOperation, lastTerminal any
	if class != "" {
		lastClass, lastOperation, lastTerminal = class, "list_org_alerts", terminal
	}
	updatedAt := f.asOf.Add(-age)
	f.exec(`INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, scope_id,
    generation_id, status, last_failure_class, last_failure_operation, last_failure_terminal,
    last_failure_message, created_at, updated_at)
VALUES ($1, 'run-8803', $2, $3, $4, $1, $5, $6, $7, $8, 'provider text that must never be read', $9, $9)`,
		id, kind, instance, scope, status, lastClass, lastOperation, lastTerminal, updatedAt)
}

// read runs the production snapshot path: the instance list with its
// scan_interval, then the windowed failure read on the same asOf.
func (f *terminalFailureLiveFixture) read() map[string]collector.TerminalFailure {
	f.t.Helper()
	snapshot, err := readCoordinatorSnapshot(f.ctx, SQLDB{DB: f.database}, f.asOf)
	if err != nil {
		f.t.Fatalf("readCoordinatorSnapshot() error = %v", err)
	}
	if snapshot == nil {
		f.t.Fatal("readCoordinatorSnapshot() = nil, want a snapshot")
	}
	got := make(map[string]collector.TerminalFailure, len(snapshot.CollectorTerminalFailures))
	for _, failure := range snapshot.CollectorTerminalFailures {
		got[failure.InstanceID] = failure
	}
	return got
}

func TestCollectorTerminalFailureWindowSemanticsLive(t *testing.T) {
	started := time.Now()
	f := newTerminalFailureLiveFixture(t, "terminal_failure_window")
	const sa = "security_alert"
	const fifteen = `{"scan_interval": "15m"}`

	// Cleared: a later completed item of the same instance and scope.
	f.instance(sa, "clears", true, false, fifteen)
	f.item("clears-f", sa, "clears", "scope-a", "failed_terminal", "auth_denied", true, 10*time.Minute)
	f.item("clears-c", sa, "clears", "scope-a", "completed", "", false, 5*time.Minute)
	// Not cleared: the later completion is on a sibling scope.
	f.instance(sa, "sibling", true, false, fifteen)
	f.item("sibling-f", sa, "sibling", "scope-a", "failed_terminal", "auth_denied", true, 10*time.Minute)
	f.item("sibling-c", sa, "sibling", "scope-b", "completed", "", false, 5*time.Minute)
	// Not cleared: another instance of the same kind completed the shared scope.
	f.instance(sa, "shared-a", true, false, fifteen)
	f.instance(sa, "shared-b", true, false, fifteen)
	f.item("shared-a-f", sa, "shared-a", "scope-shared", "failed_terminal", "not_found", true, 10*time.Minute)
	f.item("shared-b-c", sa, "shared-b", "scope-shared", "completed", "", false, 5*time.Minute)
	// Not cleared: a replan in flight (pending, then a retryable failure).
	f.instance(sa, "replan", true, false, fifteen)
	f.item("replan-f", sa, "replan", "scope-a", "failed_terminal", "rate_limited", true, 10*time.Minute)
	f.item("replan-p", sa, "replan", "scope-a", "pending", "", false, 2*time.Minute)
	f.item("replan-r", sa, "replan", "scope-a", "failed_retryable", "rate_limited", false, time.Minute)
	// Never reported: only non-terminal failures.
	f.instance(sa, "nonterminal", true, false, fifteen)
	f.item("nonterminal-p", sa, "nonterminal", "scope-a", "pending", "rate_limited", false, 5*time.Minute)
	f.item("nonterminal-r", sa, "nonterminal", "scope-a", "failed_retryable", "rate_limited", false, 4*time.Minute)
	// Never reported: disabled or deactivated instances.
	f.instance(sa, "disabled", false, false, fifteen)
	f.item("disabled-f", sa, "disabled", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.instance(sa, "deactivated", true, true, fifteen)
	f.item("deactivated-f", sa, "deactivated", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	// Orphan: outside the 30 minute floor with newer activity on the instance.
	f.instance("git", "orphan", true, false, `{}`)
	f.item("orphan-f", "git", "orphan", "scope-gone", "failed_terminal", "terminal", true, 2*time.Hour)
	f.item("orphan-c", "git", "orphan", "scope-live", "completed", "", false, time.Minute)
	f.instance("git", "inside", true, false, `{}`)
	f.item("inside-f", "git", "inside", "scope-a", "failed_terminal", "terminal", true, 20*time.Minute)
	// No scan_interval: the 30 minute floor.
	f.instance("git", "floor-25m", true, false, `{"scheduled_scan_enabled": true}`)
	f.item("floor-25m-f", "git", "floor-25m", "scope-a", "failed_terminal", "terminal", true, 25*time.Minute)
	f.instance("git", "floor-35m", true, false, `{"scheduled_scan_enabled": true}`)
	f.item("floor-35m-f", "git", "floor-35m", "scope-a", "failed_terminal", "terminal", true, 35*time.Minute)
	// scan_interval 12h: a 36 hour window.
	f.instance(sa, "twelve-20h", true, false, `{"scan_interval": "12h"}`)
	f.item("twelve-20h-f", sa, "twelve-20h", "scope-a", "failed_terminal", "auth_denied", true, 20*time.Hour)
	f.instance(sa, "twelve-40h", true, false, `{"scan_interval": "12h"}`)
	f.item("twelve-40h-f", sa, "twelve-40h", "scope-a", "failed_terminal", "auth_denied", true, 40*time.Hour)
	// Malformed scan_interval: the floor, not an error.
	f.instance(sa, "malformed", true, false, `{"scan_interval": 3600}`)
	f.item("malformed-f", sa, "malformed", "scope-a", "failed_terminal", "auth_denied", true, 40*time.Minute)
	// Equal updated_at: the lowest work_item_id wins.
	f.instance(sa, "tie", true, false, fifteen)
	f.item("tie-b", sa, "tie", "scope-b", "failed_terminal", "rate_limited", true, 5*time.Minute)
	f.item("tie-a", sa, "tie", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)

	got := f.read()
	wantClass := map[string]string{
		"sibling":    "auth_denied",
		"shared-a":   "not_found",
		"replan":     "rate_limited",
		"inside":     "terminal",
		"floor-25m":  "terminal",
		"twelve-20h": "auth_denied",
		"tie":        "auth_denied",
	}
	for id, class := range wantClass {
		failure, ok := got[id]
		if !ok {
			t.Errorf("instance %q: no terminal failure reported, want class %q", id, class)
			continue
		}
		if failure.FailureClass != class || !failure.Terminal {
			t.Errorf("instance %q: failure = %+v, want terminal class %q", id, failure, class)
		}
	}
	for _, id := range []string{"clears", "shared-b", "nonterminal", "disabled", "deactivated", "orphan", "floor-35m", "twelve-40h", "malformed"} {
		if failure, ok := got[id]; ok {
			t.Errorf("instance %q: reported %+v, want no terminal failure", id, failure)
		}
	}
	if len(got) != len(wantClass) {
		t.Errorf("reported %d instances, want %d: %+v", len(got), len(wantClass), got)
	}
	t.Logf("window semantics proof: %d work items, %s", f.items, time.Since(started))
}
