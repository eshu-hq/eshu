// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8830 disposable PostgreSQL 18 proof of the acknowledged collector terminal-failure read: an acknowledgement suppresses the failure episode of its own instance, scope, and class (later same-class rows stay suppressed and are returned as acknowledged), a different failure class, a completed item after the acknowledged failure, another scope, another instance, and another collector kind each report the failure again, an unacknowledged failure on a sibling scope wins over an acknowledged newer one, and the acknowledged probe never reads a row outside its own key; isolated bootstrapped schema per test; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"testing"
	"time"
)

// ack records one acknowledgement row directly, the shape the admin route
// writes. failureAge is how long before asOf the acknowledged failure was
// updated; the acknowledgement itself is stamped five minutes after it.
func (f *terminalFailureLiveFixture) ack(kind, instance, scope, class string, failureAge time.Duration) {
	f.t.Helper()
	failureAt := f.asOf.Add(-failureAge)
	f.exec(`INSERT INTO collector_terminal_failure_acknowledgements (collector_kind, collector_instance_id,
    scope_id, failure_class, failure_at, reason, actor_class, actor_id_hash, acknowledged_at)
VALUES ($1, $2, $3, $4, $5, 'target removed upstream', 'admin_token', 'hash-1', $6)`,
		kind, instance, scope, class, failureAt, failureAt.Add(5*time.Minute))
}

func TestCollectorTerminalFailureAcknowledgementSemanticsLive(t *testing.T) {
	started := time.Now()
	f := newTerminalFailureLiveFixture(t, "terminal_failure_ack")
	const sa = "security_alert"
	const fifteen = `{"scan_interval": "15m"}`

	// Suppressed: the acknowledged episode keeps failing with the same class,
	// so the newer row is still the acknowledged failure.
	f.instance(sa, "episode", true, false, fifteen)
	f.item("episode-f1", sa, "episode", "scope-a", "failed_terminal", "auth_denied", true, 20*time.Minute)
	f.item("episode-f2", sa, "episode", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "episode", "scope-a", "auth_denied", 20*time.Minute)
	// Reported again: the failure class changed after the acknowledgement.
	f.instance(sa, "reclass", true, false, fifteen)
	f.item("reclass-f1", sa, "reclass", "scope-a", "failed_terminal", "auth_denied", true, 20*time.Minute)
	f.item("reclass-f2", sa, "reclass", "scope-a", "failed_terminal", "not_found", true, 5*time.Minute)
	f.ack(sa, "reclass", "scope-a", "auth_denied", 20*time.Minute)
	// Reported again: the scope recovered (a completed item) and then failed
	// again with the same class.
	f.instance(sa, "recovered", true, false, fifteen)
	f.item("recovered-f1", sa, "recovered", "scope-a", "failed_terminal", "auth_denied", true, 25*time.Minute)
	f.item("recovered-c", sa, "recovered", "scope-a", "completed", "", false, 15*time.Minute)
	f.item("recovered-f2", sa, "recovered", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "recovered", "scope-a", "auth_denied", 25*time.Minute)
	// Reported: the acknowledgement is for another scope of the instance.
	f.instance(sa, "otherscope", true, false, fifteen)
	f.item("otherscope-f", sa, "otherscope", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "otherscope", "scope-b", "auth_denied", 5*time.Minute)
	// Reported: the acknowledgement belongs to another instance of the same
	// kind on the same scope id.
	f.instance(sa, "twin-a", true, false, fifteen)
	f.instance(sa, "twin-b", true, false, fifteen)
	f.item("twin-a-f", sa, "twin-a", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "twin-b", "scope-a", "auth_denied", 5*time.Minute)
	// Reported: the acknowledgement belongs to another collector kind that
	// reuses the instance id and scope.
	f.instance(sa, "rekind", true, false, fifteen)
	f.item("rekind-f", sa, "rekind", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack("git", "rekind", "scope-a", "auth_denied", 5*time.Minute)
	// Reported: an unacknowledged older failure on a sibling scope wins over
	// the acknowledged newer one.
	f.instance(sa, "siblings", true, false, fifteen)
	f.item("siblings-acked", sa, "siblings", "scope-a", "failed_terminal", "auth_denied", true, 2*time.Minute)
	f.item("siblings-open", sa, "siblings", "scope-b", "failed_terminal", "rate_limited", true, 9*time.Minute)
	f.ack(sa, "siblings", "scope-a", "auth_denied", 2*time.Minute)
	// Covered: failure rows older than the acknowledged failure are history it
	// superseded, so acknowledging a newer failure never resurrects them.
	f.instance(sa, "history", true, false, fifteen)
	f.item("history-f1", sa, "history", "scope-a", "failed_terminal", "rate_limited", true, 20*time.Minute)
	f.item("history-f2", sa, "history", "scope-a", "failed_terminal", "auth_denied", true, 5*time.Minute)
	f.ack(sa, "history", "scope-a", "auth_denied", 5*time.Minute)

	got := f.read()
	assertAck := func(instance string, wantReported, wantAcknowledged bool, wantClass string) {
		t.Helper()
		failure, ok := got[instance]
		if ok != wantReported {
			t.Fatalf("%s reported = %v (%+v), want %v", instance, ok, failure, wantReported)
		}
		if !ok {
			return
		}
		if failure.Acknowledged != wantAcknowledged {
			t.Fatalf("%s Acknowledged = %v, want %v (%+v)", instance, failure.Acknowledged, wantAcknowledged, failure)
		}
		if failure.FailureClass != wantClass {
			t.Fatalf("%s class = %q, want %q", instance, failure.FailureClass, wantClass)
		}
	}
	assertAck("episode", true, true, "auth_denied")
	if failure := got["episode"]; !failure.ObservedAt.Equal(f.asOf.Add(-5*time.Minute)) ||
		failure.AcknowledgedAt.IsZero() || failure.AcknowledgedReason != "target removed upstream" ||
		failure.ScopeID != "scope-a" {
		t.Fatalf("episode = %+v, want the newer row with acknowledgement time and reason", failure)
	}
	assertAck("reclass", true, false, "not_found")
	assertAck("recovered", true, false, "auth_denied")
	assertAck("otherscope", true, false, "auth_denied")
	assertAck("twin-a", true, false, "auth_denied")
	assertAck("twin-b", false, false, "")
	assertAck("rekind", true, false, "auth_denied")
	assertAck("siblings", true, false, "rate_limited")
	if failure := got["siblings"]; failure.ScopeID != "scope-b" {
		t.Fatalf("siblings ScopeID = %q, want the unacknowledged scope-b", failure.ScopeID)
	}
	assertAck("history", true, true, "auth_denied")
	t.Logf("acknowledgement semantics proof: %s", time.Since(started))
}
