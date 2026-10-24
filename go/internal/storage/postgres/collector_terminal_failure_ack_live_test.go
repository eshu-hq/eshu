// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8830 disposable PostgreSQL 18 proof of the collector terminal-failure acknowledgement write: it records the newest unresolved failure inside the instance's window (its updated_at, raw class, and work item id), refuses an unknown, kind-mismatched, disabled, or deactivated instance, a scope with no unresolved failure, a scope a completed item resolved, a failure outside the window, and a failure whose class differs from the one named; a second acknowledgement of a newer episode replaces the row; the status read then reports the failure as acknowledged and a later different-class failure again; 16 concurrent acknowledgements leave one row, and an acknowledgement racing failed_terminal writes on another connection never deadlocks and never suppresses a failure class it did not record; the admin list returns the reason; isolated bootstrapped schema per test; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func (f *terminalFailureLiveFixture) ackStore() *TerminalFailureAckStore {
	store := NewTerminalFailureAckStore(SQLDB{DB: f.database}, SQLDB{DB: f.database})
	store.now = func() time.Time { return f.asOf }
	return store
}

func (f *terminalFailureLiveFixture) ackRows() int {
	f.t.Helper()
	var count int
	if err := f.database.QueryRowContext(f.ctx,
		`SELECT count(*) FROM collector_terminal_failure_acknowledgements`).Scan(&count); err != nil {
		f.t.Fatalf("count acknowledgements: %v", err)
	}
	return count
}

// insertFailedRaceSafe inserts one failed_terminal row for the race test from a
// goroutine: unlike item it touches no shared fixture counter and reports with
// Errorf, so it is safe off the test goroutine.
func (f *terminalFailureLiveFixture) insertFailedRaceSafe(id, class string, age time.Duration) {
	updatedAt := f.asOf.Add(-age)
	if _, err := f.database.ExecContext(f.ctx, `INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind,
    collector_instance_id, scope_id, generation_id, status, last_failure_class, last_failure_operation,
    created_at, updated_at)
VALUES ($1, 'run-8803', 'security_alert', 'live', 'scope-a', $1, 'failed_terminal', $2, 'list_org_alerts', $3, $3)`,
		id, class, updatedAt); err != nil {
		f.t.Errorf("insert %s: %v", id, err)
	}
}

func ackRequest(instance, scope string) TerminalFailureAckRequest {
	return TerminalFailureAckRequest{
		CollectorKind: "security_alert", InstanceID: instance, ScopeID: scope,
		Reason: "credential revoked on purpose", ActorClass: "admin_token", ActorIDHash: "h1", CorrelationID: "corr-1",
	}
}

func TestTerminalFailureAckStoreRefusalsLive(t *testing.T) {
	f := newTerminalFailureLiveFixture(t, "terminal_failure_ack_refusals")
	const sa = "security_alert"
	const fifteen = `{"scan_interval": "15m"}`
	f.instance(sa, "live", true, false, fifteen)
	f.instance(sa, "disabled", false, false, fifteen)
	f.instance(sa, "deactivated", true, true, fifteen)
	f.item("live-f", sa, "live", "scope-a", "failed_terminal", "auth_denied", true, 10*time.Minute)
	f.item("resolved-f", sa, "live", "scope-r", "failed_terminal", "auth_denied", true, 10*time.Minute)
	f.item("resolved-c", sa, "live", "scope-r", "completed", "", false, 5*time.Minute)
	f.item("aged-f", sa, "live", "scope-aged", "failed_terminal", "auth_denied", true, 3*time.Hour)
	f.item("disabled-f", sa, "disabled", "scope-a", "failed_terminal", "auth_denied", true, 10*time.Minute)
	f.item("deactivated-f", sa, "deactivated", "scope-a", "failed_terminal", "auth_denied", true, 10*time.Minute)
	store := f.ackStore()

	cases := []struct {
		name   string
		mutate func(*TerminalFailureAckRequest)
		want   error
	}{
		{"unknown instance", func(r *TerminalFailureAckRequest) { r.InstanceID = "nope" }, ErrTerminalFailureAckInstanceNotFound},
		{"kind mismatch", func(r *TerminalFailureAckRequest) { r.CollectorKind = "git" }, ErrTerminalFailureAckInstanceNotFound},
		{"disabled instance", func(r *TerminalFailureAckRequest) { r.InstanceID = "disabled" }, ErrTerminalFailureAckInstanceNotFound},
		{"deactivated instance", func(r *TerminalFailureAckRequest) { r.InstanceID = "deactivated" }, ErrTerminalFailureAckInstanceNotFound},
		{"scope that never failed", func(r *TerminalFailureAckRequest) { r.ScopeID = "scope-x" }, ErrTerminalFailureAckNoFailure},
		{"scope a completed item resolved", func(r *TerminalFailureAckRequest) { r.ScopeID = "scope-r" }, ErrTerminalFailureAckNoFailure},
		{"failure outside the window", func(r *TerminalFailureAckRequest) { r.ScopeID = "scope-aged" }, ErrTerminalFailureAckNoFailure},
		{"class changed since read", func(r *TerminalFailureAckRequest) { r.FailureClass = "not_found" }, ErrTerminalFailureAckClassChanged},
	}
	for _, tc := range cases {
		req := ackRequest("live", "scope-a")
		tc.mutate(&req)
		if _, err := store.Acknowledge(f.ctx, req); !errors.Is(err, tc.want) {
			t.Fatalf("%s: Acknowledge error = %v, want %v", tc.name, err, tc.want)
		}
		if _, err := store.ResolveTarget(f.ctx, req); !errors.Is(err, tc.want) {
			t.Fatalf("%s: ResolveTarget error = %v, want %v", tc.name, err, tc.want)
		}
	}
	for _, bad := range []TerminalFailureAckRequest{
		{CollectorKind: sa, InstanceID: "live", ScopeID: "scope-a", Reason: "  "},
		{CollectorKind: sa, InstanceID: "", ScopeID: "scope-a", Reason: "r"},
		{CollectorKind: sa, InstanceID: "live", ScopeID: "", Reason: "r"},
		{CollectorKind: "", InstanceID: "live", ScopeID: "scope-a", Reason: "r"},
		{CollectorKind: sa, InstanceID: "live", ScopeID: "scope-a", Reason: strings.Repeat("x", MaxTerminalFailureAckReasonLength+1)},
	} {
		if _, err := store.Acknowledge(f.ctx, bad); err == nil {
			t.Fatalf("Acknowledge(%+v) error = nil, want a validation error", bad)
		}
	}
	if rows := f.ackRows(); rows != 0 {
		t.Fatalf("refused acknowledgements wrote %d rows, want 0", rows)
	}
}

func TestTerminalFailureAckStoreRecordsAndSuppressesLive(t *testing.T) {
	started := time.Now()
	f := newTerminalFailureLiveFixture(t, "terminal_failure_ack_records")
	const sa = "security_alert"
	f.instance(sa, "live", true, false, `{"scan_interval": "15m"}`)
	f.item("f1", sa, "live", "scope-a", "failed_terminal", "auth_denied", true, 10*time.Minute)
	store := f.ackStore()
	req := ackRequest("live", "scope-a")

	if _, acked := split(t, f.readAll()["live"]); acked != nil {
		t.Fatalf("before acknowledging, acknowledged row = %+v, want none", acked)
	}
	req.FailureClass = "auth_denied"
	got, err := store.Acknowledge(f.ctx, req)
	if err != nil {
		t.Fatalf("Acknowledge() error = %v", err)
	}
	if got.CollectorKind != sa || got.WorkItemID != "f1" || got.FailureClass != "auth_denied" ||
		got.FailureOperation != "list_org_alerts" || !got.FailureAt.Equal(f.asOf.Add(-10*time.Minute)) || !got.AcknowledgedAt.Equal(f.asOf) {
		t.Fatalf("Acknowledge() = %+v, want the 10 minute old failure f1 acknowledged now", got)
	}
	open, acked := split(t, f.readAll()["live"])
	if open != nil || acked == nil || acked.WorkItemID != "f1" || !acked.AcknowledgedAt.Equal(f.asOf) {
		t.Fatalf("after acknowledging: unacknowledged=%+v acknowledged=%+v, want only f1 acknowledged", open, acked)
	}
	var reason, actor, corr string
	if err := f.database.QueryRowContext(f.ctx, `SELECT reason, actor_id_hash, correlation_id FROM collector_terminal_failure_acknowledgements`).
		Scan(&reason, &actor, &corr); err != nil || reason != req.Reason || actor != "h1" || corr != "corr-1" {
		t.Fatalf("stored row = %q/%q/%q, %v; want the reason, actor hash, and correlation id", reason, actor, corr, err)
	}

	// The same episode keeps failing: still acknowledged, now through f2.
	f.item("f2", sa, "live", "scope-a", "failed_terminal", "auth_denied", true, time.Minute)
	if open, acked := split(t, f.readAll()["live"]); open != nil || acked == nil || acked.WorkItemID != "f2" {
		t.Fatalf("same-class failure after the acknowledgement: unacknowledged=%+v acknowledged=%+v, want f2 acknowledged", open, acked)
	}
	// A different class is a new failure and reports again, beside the old one.
	f.item("f3", sa, "live", "scope-a", "failed_terminal", "not_found", true, 30*time.Second)
	if open, _ := split(t, f.readAll()["live"]); open == nil || open.WorkItemID != "f3" || open.FailureClass != "not_found" {
		t.Fatalf("new-class failure: unacknowledged=%+v, want f3 not_found", open)
	}
	// Acknowledging the new failure replaces the earlier row (one row per target).
	next := ackRequest("live", "scope-a")
	next.Reason = "second operator"
	if _, err := store.Acknowledge(f.ctx, next); err != nil {
		t.Fatalf("Acknowledge(not_found) error = %v", err)
	}
	if rows := f.ackRows(); rows != 1 {
		t.Fatalf("acknowledgement rows = %d, want 1 per instance and scope", rows)
	}
	if open, acked := split(t, f.readAll()["live"]); open != nil || acked == nil || acked.WorkItemID != "f3" {
		t.Fatalf("after the second acknowledgement: unacknowledged=%+v acknowledged=%+v, want only f3 acknowledged", open, acked)
	}

	// The admin read returns the reason, newest first, with the bounded class.
	records, err := store.ListTerminalFailureAcks(f.ctx, TerminalFailureAckListFilter{InstanceID: "live"})
	if err != nil || len(records) != 1 || records[0].Reason != "second operator" || records[0].FailureClass != "not_found" ||
		records[0].WorkItemID != "f3" || records[0].ScopeID != "scope-a" {
		t.Fatalf("ListTerminalFailureAcks() = %+v, %v; want the replaced row with its reason", records, err)
	}
	if none, err := store.ListTerminalFailureAcks(f.ctx, TerminalFailureAckListFilter{InstanceID: "other"}); err != nil || len(none) != 0 {
		t.Fatalf("ListTerminalFailureAcks(other) = %+v, %v; want none", none, err)
	}
	t.Logf("acknowledgement record and suppress proof: %s", time.Since(started))
}

func TestTerminalFailureAckStoreConcurrentLive(t *testing.T) {
	f := newTerminalFailureLiveFixture(t, "terminal_failure_ack_concurrent")
	const sa = "security_alert"
	f.instance(sa, "live", true, false, `{"scan_interval": "15m"}`)
	f.item("f1", sa, "live", "scope-a", "failed_terminal", "auth_denied", true, 10*time.Minute)
	store := f.ackStore()

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	start := make(chan struct{})
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := ackRequest("live", "scope-a")
			req.ActorIDHash = fmt.Sprintf("actor-%d", i)
			_, err := store.Acknowledge(f.ctx, req)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Acknowledge() error = %v, want every call to succeed", err)
		}
	}
	if rows := f.ackRows(); rows != 1 {
		t.Fatalf("acknowledgement rows = %d after %d concurrent calls, want 1", rows, workers)
	}
	if _, acked := split(t, f.readAll()["live"]); acked == nil {
		t.Fatal("failure after concurrent acknowledgements is not acknowledged")
	}
}

// checkAgainstOracle compares the status read with an independent Go oracle of
// the suppression predicate over the rows now in the table. The scope holds only
// failed_terminal rows, so a row t is covered by the stored acknowledgement iff
// t.updated_at <= failure_at or t has the acknowledged raw class. The newest
// uncovered row must be the unacknowledged failure, and the newest covered row
// the acknowledged one, whichever order the writes landed in.
func (f *terminalFailureLiveFixture) checkAgainstOracle(round int) {
	f.t.Helper()
	type row struct {
		id, class string
		at        time.Time
	}
	rows, err := f.database.QueryContext(f.ctx, `SELECT work_item_id, COALESCE(last_failure_class, ''), updated_at
FROM workflow_work_items WHERE collector_instance_id = 'live' AND scope_id = 'scope-a' AND status = 'failed_terminal'
ORDER BY updated_at DESC, work_item_id ASC`)
	if err != nil {
		f.t.Fatalf("round %d: read rows: %v", round, err)
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.class, &r.at); err != nil {
			f.t.Fatalf("round %d: scan: %v", round, err)
		}
		all = append(all, r)
	}
	_ = rows.Close()
	var ackClass string
	var ackAt time.Time
	hasAck := true
	if err := f.database.QueryRowContext(f.ctx, `SELECT failure_class, failure_at FROM collector_terminal_failure_acknowledgements`).Scan(&ackClass, &ackAt); err != nil {
		hasAck = false
	}
	wantOpen, wantAcked := "", ""
	for _, r := range all {
		covered := hasAck && (!r.at.After(ackAt) || r.class == ackClass)
		switch {
		case covered && wantAcked == "":
			wantAcked = r.id
		case !covered && wantOpen == "":
			wantOpen = r.id
		}
	}
	open, acked := split(f.t, f.readAll()["live"])
	gotOpen, gotAcked := "", ""
	if open != nil {
		gotOpen = open.WorkItemID
	}
	if acked != nil {
		gotAcked = acked.WorkItemID
	}
	if gotOpen != wantOpen || gotAcked != wantAcked {
		f.t.Fatalf("round %d: read unacknowledged=%q acknowledged=%q, oracle %q and %q (ack %q at %s, rows %+v)",
			round, gotOpen, gotAcked, wantOpen, wantAcked, ackClass, ackAt, all)
	}
}

// TestTerminalFailureAckStoreRacesFailureWritesLive runs acknowledgements
// against failed_terminal writes for the same scope on other connections, with
// both classes in flight. Whichever order wins, there is no deadlock, the
// acknowledgement stores the failure it read (its class and updated_at), and the
// status read agrees with an independent oracle of the predicate: a failure of
// another class newer than the acknowledged one is reported, never suppressed.
func TestTerminalFailureAckStoreRacesFailureWritesLive(t *testing.T) {
	f := newTerminalFailureLiveFixture(t, "terminal_failure_ack_race")
	const sa = "security_alert"
	f.instance(sa, "live", true, false, `{"scan_interval": "15m"}`)
	f.item("f1", sa, "live", "scope-a", "failed_terminal", "auth_denied", true, 10*time.Minute)
	store := f.ackStore()

	for round := range 8 {
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			if _, err := store.Acknowledge(f.ctx, ackRequest("live", "scope-a")); err != nil &&
				!errors.Is(err, ErrTerminalFailureAckNoFailure) {
				t.Errorf("round %d: Acknowledge() error = %v", round, err)
			}
		}()
		go func() {
			defer wg.Done()
			f.insertFailedRaceSafe(fmt.Sprintf("race-same-%d", round), "auth_denied", time.Duration(30-round)*time.Second)
		}()
		go func() {
			defer wg.Done()
			f.insertFailedRaceSafe(fmt.Sprintf("race-nf-%d", round), "not_found", time.Duration(20-round)*time.Second)
		}()
		wg.Wait()
		f.checkAgainstOracle(round)
		f.exec(`DELETE FROM collector_terminal_failure_acknowledgements`)
		f.exec(`DELETE FROM workflow_work_items WHERE work_item_id LIKE 'race-%'`)
	}
}
