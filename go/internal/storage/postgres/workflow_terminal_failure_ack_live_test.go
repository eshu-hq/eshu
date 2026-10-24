// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8830 disposable PostgreSQL 18 proof of the acknowledged collector terminal-failure read: an acknowledgement suppresses the failure it names and later failures on the same scope with the same raw class (NULL and empty agree), a later different class and a failure after a completed item report again, history older than the acknowledged failure never resurfaces, an unacknowledged failure on a sibling scope is reported beside the acknowledged one (no LIMIT 1 hiding), an acknowledgement of another scope, instance, or collector kind covers nothing, an aged-out or disabled instance reports nothing, and retention deleting the acknowledged row leaves the newer same-class row suppressed; isolated bootstrapped schema per test; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// ack records one acknowledgement row directly, the shape the admin route
// writes. failureAge is how long before asOf the acknowledged failure was
// updated; class is the raw persisted class (” for NULL); the acknowledgement
// itself is stamped five minutes after the failure.
func (f *terminalFailureLiveFixture) ack(kind, instance, scope, class, workItemID string, failureAge time.Duration) {
	f.t.Helper()
	failureAt := f.asOf.Add(-failureAge)
	f.exec(`INSERT INTO collector_terminal_failure_acknowledgements (collector_kind, collector_instance_id,
    scope_id, failure_class, failure_at, work_item_id, reason, actor_class, actor_id_hash, acknowledged_at)
VALUES ($1, $2, $3, $4, $5, $6, 'target removed upstream', 'admin_token', 'hash-1', $7)`,
		kind, instance, scope, class, failureAt, workItemID, failureAt.Add(5*time.Minute))
}

// itemNullClass inserts a failed_terminal work item whose persisted class is NULL.
func (f *terminalFailureLiveFixture) itemNullClass(id, kind, instance, scope string, age time.Duration) {
	f.t.Helper()
	updatedAt := f.asOf.Add(-age)
	f.exec(`INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, scope_id,
    generation_id, status, last_failure_operation, created_at, updated_at)
VALUES ($1, 'run-8803', $2, $3, $4, $1, 'failed_terminal', 'collect_failure', $5, $5)`,
		id, kind, instance, scope, updatedAt)
}

// split separates one instance's failures into the unacknowledged and the
// acknowledged row the reader supplied.
func split(t *testing.T, failures []collector.TerminalFailure) (open, acked *collector.TerminalFailure) {
	t.Helper()
	for i := range failures {
		failure := &failures[i]
		switch {
		case failure.Acknowledged && acked == nil:
			acked = failure
		case !failure.Acknowledged && open == nil:
			open = failure
		default:
			t.Fatalf("reader supplied more than one %v row for one instance: %+v", failure.Acknowledged, failures)
		}
	}
	return open, acked
}

func TestCollectorTerminalFailureAcknowledgementSemanticsLive(t *testing.T) {
	started := time.Now()
	f := newTerminalFailureLiveFixture(t, "terminal_failure_ack")
	const sa = "security_alert"
	const fifteen = `{"scan_interval": "15m"}`

	// episode: the acknowledged failure keeps failing with the same class.
	f.instance(sa, "episode", true, false, fifteen)
	f.item("episode-f1", sa, "episode", "scope-a", "failed_terminal", "auth_denied", true, 20*time.Minute)
	f.item("episode-f2", sa, "episode", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "episode", "scope-a", "auth_denied", "episode-f1", 20*time.Minute)
	// reclass: the class changed after the acknowledgement.
	f.instance(sa, "reclass", true, false, fifteen)
	f.item("reclass-f1", sa, "reclass", "scope-a", "failed_terminal", "auth_denied", true, 20*time.Minute)
	f.item("reclass-f2", sa, "reclass", "scope-a", "failed_terminal", "not_found", true, 5*time.Minute)
	f.ack(sa, "reclass", "scope-a", "auth_denied", "reclass-f1", 20*time.Minute)
	// recovered: a completed item landed after the acknowledged failure, then the same class failed again.
	f.instance(sa, "recovered", true, false, fifteen)
	f.item("recovered-f1", sa, "recovered", "scope-a", "failed_terminal", "auth_denied", true, 25*time.Minute)
	f.item("recovered-c", sa, "recovered", "scope-a", "completed", "", false, 15*time.Minute)
	f.item("recovered-f2", sa, "recovered", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "recovered", "scope-a", "auth_denied", "recovered-f1", 25*time.Minute)
	// otherscope / twin / rekind: acknowledgements keyed elsewhere cover nothing.
	f.instance(sa, "otherscope", true, false, fifteen)
	f.item("otherscope-f", sa, "otherscope", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "otherscope", "scope-b", "auth_denied", "x", 5*time.Minute)
	f.instance(sa, "twin-a", true, false, fifteen)
	f.instance(sa, "twin-b", true, false, fifteen)
	f.item("twin-a-f", sa, "twin-a", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "twin-b", "scope-a", "auth_denied", "x", 5*time.Minute)
	f.instance(sa, "rekind", true, false, fifteen)
	f.item("rekind-f", sa, "rekind", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack("git", "rekind", "scope-a", "auth_denied", "x", 5*time.Minute)
	// siblings: an older unacknowledged scope must be reported beside the acknowledged newer one (the LIMIT 1 trap).
	f.instance(sa, "siblings", true, false, fifteen)
	f.item("siblings-acked", sa, "siblings", "scope-a", "failed_terminal", "auth_denied", true, 2*time.Minute)
	f.item("siblings-open", sa, "siblings", "scope-b", "failed_terminal", "rate_limited", true, 9*time.Minute)
	f.ack(sa, "siblings", "scope-a", "auth_denied", "siblings-acked", 2*time.Minute)
	// history: older rows of the acknowledged scope never resurface.
	f.instance(sa, "history", true, false, fifteen)
	f.item("history-f1", sa, "history", "scope-a", "failed_terminal", "rate_limited", true, 20*time.Minute)
	f.item("history-f2", sa, "history", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "history", "scope-a", "auth_denied", "history-f2", 5*time.Minute)
	// nullclass: NULL persisted class agrees with the empty acknowledged class.
	f.instance(sa, "nullclass", true, false, fifteen)
	f.itemNullClass("nullclass-f1", sa, "nullclass", "scope-a", 20*time.Minute)
	f.itemNullClass("nullclass-f2", sa, "nullclass", "scope-a", 5*time.Minute)
	f.ack(sa, "nullclass", "scope-a", "", "nullclass-f1", 20*time.Minute)
	// aged: the acknowledged failure is outside the 45 minute window.
	f.instance(sa, "aged", true, false, fifteen)
	f.item("aged-f", sa, "aged", "scope-a", "failed_terminal", "auth_denied", true, 2*time.Hour)
	f.ack(sa, "aged", "scope-a", "auth_denied", "aged-f", 2*time.Hour)
	// disabled: never read, acknowledged or not.
	f.instance(sa, "disabled", false, false, fifteen)
	f.item("disabled-f", sa, "disabled", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "disabled", "scope-a", "auth_denied", "disabled-f", 5*time.Minute)
	// retention: the acknowledged row was deleted, the newer same-class row stays suppressed.
	f.instance(sa, "retained", true, false, fifteen)
	f.item("retained-f2", sa, "retained", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "retained", "scope-a", "auth_denied", "retained-f1-deleted-by-retention", 20*time.Minute)

	got := f.readAll()
	check := func(instance string, wantOpen, wantAcked string) {
		t.Helper()
		open, acked := split(t, got[instance])
		gotOpen, gotAcked := "", ""
		if open != nil {
			gotOpen = open.WorkItemID
		}
		if acked != nil {
			gotAcked = acked.WorkItemID
		}
		if gotOpen != wantOpen || gotAcked != wantAcked {
			t.Fatalf("%s: unacknowledged=%q acknowledged=%q, want %q and %q (rows %+v)", instance, gotOpen, gotAcked, wantOpen, wantAcked, got[instance])
		}
	}
	check("episode", "", "episode-f2")
	check("reclass", "reclass-f2", "reclass-f1")
	check("recovered", "recovered-f2", "")
	check("otherscope", "otherscope-f", "")
	check("twin-a", "twin-a-f", "")
	check("twin-b", "", "")
	check("rekind", "rekind-f", "")
	check("siblings", "siblings-open", "siblings-acked")
	check("history", "", "history-f2")
	check("nullclass", "", "nullclass-f2")
	check("aged", "", "")
	check("disabled", "", "")
	check("retained", "", "retained-f2")

	// The acknowledged row carries the flag, the acknowledgement time, and the
	// failed scope; the unacknowledged row of the same instance carries none.
	open, acked := split(t, got["siblings"])
	if !acked.Acknowledged || acked.AcknowledgedAt.IsZero() || acked.ScopeID != "scope-a" || acked.FailureClass != "auth_denied" {
		t.Fatalf("siblings acknowledged row = %+v, want the flag, time, scope-a, auth_denied", acked)
	}
	if open.Acknowledged || !open.AcknowledgedAt.IsZero() || open.ScopeID != "scope-b" || open.FailureClass != "rate_limited" {
		t.Fatalf("siblings unacknowledged row = %+v, want scope-b rate_limited with no acknowledgement", open)
	}
	t.Logf("acknowledgement semantics proof: %s", time.Since(started))
}
