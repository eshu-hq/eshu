// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8830 disposable PostgreSQL 18 proof of the collector terminal-failure acknowledgement write: it records the failure the operator read (class, failed_terminal updated_at), refuses an unknown, disabled, or deactivated instance, a scope with no unresolved failure, a scope a later completed item already resolved, and a failure whose class changed since it was read; a repeat of a covering acknowledgement is Unchanged and keeps the first reason and actor; the status read then reports the failure as acknowledged and reports a later different-class failure again; 16 concurrent acknowledgements of one failure all succeed and leave one row, and an acknowledgement racing a new failure never suppresses a failure class it did not name; isolated bootstrapped schema per test; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func (f *terminalFailureLiveFixture) ackStore() *TerminalFailureAckStore {
	store := NewTerminalFailureAckStore(SQLDB{DB: f.database})
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
	f.item("disabled-f", sa, "disabled", "scope-a", "failed_terminal", "auth_denied", true, 10*time.Minute)
	f.item("deactivated-f", sa, "deactivated", "scope-a", "failed_terminal", "auth_denied", true, 10*time.Minute)
	store := f.ackStore()

	base := TerminalFailureAckRequest{
		InstanceID: "live", ScopeID: "scope-a", FailureClass: "auth_denied",
		Reason: "credential revoked on purpose", ActorClass: "admin_token", ActorIDHash: "h1",
	}
	cases := []struct {
		name   string
		mutate func(*TerminalFailureAckRequest)
		want   error
	}{
		{"unknown instance", func(r *TerminalFailureAckRequest) { r.InstanceID = "nope" }, ErrTerminalFailureAckInstanceNotFound},
		{"disabled instance", func(r *TerminalFailureAckRequest) { r.InstanceID = "disabled" }, ErrTerminalFailureAckInstanceNotFound},
		{"deactivated instance", func(r *TerminalFailureAckRequest) { r.InstanceID = "deactivated" }, ErrTerminalFailureAckInstanceNotFound},
		{"scope that never failed", func(r *TerminalFailureAckRequest) { r.ScopeID = "scope-x" }, ErrTerminalFailureAckNoFailure},
		{"scope a completed item resolved", func(r *TerminalFailureAckRequest) { r.ScopeID = "scope-r" }, ErrTerminalFailureAckNoFailure},
		{"class changed since read", func(r *TerminalFailureAckRequest) { r.FailureClass = "not_found" }, ErrTerminalFailureAckClassChanged},
		{"class not named", func(r *TerminalFailureAckRequest) { r.FailureClass = "" }, ErrTerminalFailureAckClassChanged},
	}
	for _, tc := range cases {
		req := base
		tc.mutate(&req)
		if _, err := store.Acknowledge(f.ctx, req); !errors.Is(err, tc.want) {
			t.Fatalf("%s: error = %v, want %v", tc.name, err, tc.want)
		}
	}
	for _, bad := range []TerminalFailureAckRequest{
		{InstanceID: "live", ScopeID: "scope-a", FailureClass: "auth_denied", Reason: "  "},
		{InstanceID: "", ScopeID: "scope-a", FailureClass: "auth_denied", Reason: "r"},
		{InstanceID: "live", ScopeID: "", FailureClass: "auth_denied", Reason: "r"},
	} {
		if _, err := store.Acknowledge(f.ctx, bad); err == nil {
			t.Fatalf("Acknowledge(%+v) error = nil, want a validation error", bad)
		}
	}
	tooLong := base
	tooLong.Reason = strings.Repeat("x", MaxTerminalFailureAckReasonLength+1)
	if _, err := store.Acknowledge(f.ctx, tooLong); err == nil {
		t.Fatal("Acknowledge(reason of 513 characters) error = nil, want a validation error")
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
	req := TerminalFailureAckRequest{
		InstanceID: "live", ScopeID: "scope-a", FailureClass: "auth_denied",
		Reason: "credential revoked on purpose", ActorClass: "admin_token", ActorIDHash: "h1",
	}

	if before := f.read()["live"]; before.Acknowledged {
		t.Fatalf("before acknowledging, failure = %+v, want unacknowledged", before)
	}
	got, err := store.Acknowledge(f.ctx, req)
	if err != nil {
		t.Fatalf("Acknowledge() error = %v", err)
	}
	if got.Unchanged || got.CollectorKind != sa || got.FailureClass != "auth_denied" ||
		!got.FailureAt.Equal(f.asOf.Add(-10*time.Minute)) || !got.AcknowledgedAt.Equal(f.asOf) {
		t.Fatalf("Acknowledge() = %+v, want a recorded acknowledgement of the 10 minute old failure", got)
	}
	read := f.read()["live"]
	if !read.Acknowledged || read.FailureClass != "auth_denied" || read.AcknowledgedReason != req.Reason ||
		!read.AcknowledgedAt.Equal(f.asOf) {
		t.Fatalf("after acknowledging, failure = %+v, want the acknowledged failure with reason and time", read)
	}

	// A repeat, even from another actor, is a no-op that keeps the first
	// reason and actor.
	second := req
	second.Reason, second.ActorIDHash = "second operator", "h2"
	repeat, err := store.Acknowledge(f.ctx, second)
	if err != nil || !repeat.Unchanged || repeat.Reason != req.Reason {
		t.Fatalf("repeat Acknowledge() = %+v, %v; want Unchanged keeping %q", repeat, err, req.Reason)
	}
	var actor string
	if err := f.database.QueryRowContext(f.ctx,
		`SELECT actor_id_hash FROM collector_terminal_failure_acknowledgements`).Scan(&actor); err != nil || actor != "h1" {
		t.Fatalf("stored actor = %q, %v; want the first actor h1", actor, err)
	}

	// The same episode keeps failing: still acknowledged.
	f.item("f2", sa, "live", "scope-a", "failed_terminal", "auth_denied", true, time.Minute)
	if again := f.read()["live"]; !again.Acknowledged {
		t.Fatalf("same-class failure after the acknowledgement = %+v, want still acknowledged", again)
	}
	// A different class is a new failure and reports again.
	f.item("f3", sa, "live", "scope-a", "failed_terminal", "not_found", true, 30*time.Second)
	if changed := f.read()["live"]; changed.Acknowledged || changed.FailureClass != "not_found" {
		t.Fatalf("new-class failure = %+v, want an unacknowledged not_found", changed)
	}
	// Acknowledging the new failure replaces the earlier acknowledgement.
	req.FailureClass = "not_found"
	if _, err := store.Acknowledge(f.ctx, req); err != nil {
		t.Fatalf("Acknowledge(not_found) error = %v", err)
	}
	if rows := f.ackRows(); rows != 1 {
		t.Fatalf("acknowledgement rows = %d, want 1 per instance and scope", rows)
	}
	if now := f.read()["live"]; !now.Acknowledged || now.FailureClass != "not_found" {
		t.Fatalf("after the second acknowledgement = %+v, want an acknowledged not_found", now)
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
			_, err := store.Acknowledge(f.ctx, TerminalFailureAckRequest{
				InstanceID: "live", ScopeID: "scope-a", FailureClass: "auth_denied",
				Reason: "concurrent operators", ActorClass: "admin_token", ActorIDHash: string(rune('a' + i)),
			})
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
	if got := f.read()["live"]; !got.Acknowledged {
		t.Fatalf("failure after concurrent acknowledgements = %+v, want acknowledged", got)
	}
}

// TestTerminalFailureAckStoreNeverSuppressesUnseenClassLive races an
// acknowledgement of auth_denied against a failure of another class landing on
// the same scope. Whichever order wins, the not_found failure is never
// reported as acknowledged: the acknowledgement stores only the class it read.
func TestTerminalFailureAckStoreNeverSuppressesUnseenClassLive(t *testing.T) {
	f := newTerminalFailureLiveFixture(t, "terminal_failure_ack_race")
	const sa = "security_alert"
	f.instance(sa, "live", true, false, `{"scan_interval": "15m"}`)
	f.item("f1", sa, "live", "scope-a", "failed_terminal", "auth_denied", true, 10*time.Minute)
	store := f.ackStore()

	for round := range 8 {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := store.Acknowledge(f.ctx, TerminalFailureAckRequest{
				InstanceID: "live", ScopeID: "scope-a", FailureClass: "auth_denied",
				Reason: "race", ActorClass: "admin_token",
			})
			// Losing the race to the new class is the other legal outcome.
			if err != nil && !errors.Is(err, ErrTerminalFailureAckClassChanged) {
				t.Errorf("round %d: Acknowledge() error = %v", round, err)
			}
		}()
		go func() {
			defer wg.Done()
			f.item("race-nf-"+string(rune('a'+round)), sa, "live", "scope-a", "failed_terminal", "not_found", true, time.Duration(8-round)*time.Second)
		}()
		wg.Wait()
		got := f.read()["live"]
		if got.FailureClass != "not_found" || got.Acknowledged {
			t.Fatalf("round %d: failure = %+v, want the unseen not_found reported unacknowledged", round, got)
		}
		f.exec(`DELETE FROM collector_terminal_failure_acknowledgements`)
		f.exec(`DELETE FROM workflow_work_items WHERE work_item_id LIKE 'race-nf-%'`)
	}
}
