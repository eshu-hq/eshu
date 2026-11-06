// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// errAuditSinkDown stands in for a durable sink that rejects or is
// unreachable for an append. The wrapper must surface it unchanged (the
// caller's best-effort `_ =` decides the primary op survives) while still
// counting the drop through the observer hook.
var errAuditSinkDown = errors.New("governance audit sink down")

type failingAuditSink struct{ err error }

func (f *failingAuditSink) Append(_ context.Context, _ []governanceaudit.Event) error {
	return f.err
}

type recordingAuditSink struct{ got [][]governanceaudit.Event }

func (f *recordingAuditSink) Append(_ context.Context, events []governanceaudit.Event) error {
	f.got = append(f.got, events)
	return nil
}

func testAuditEvents() []governanceaudit.Event {
	return []governanceaudit.Event{{
		Type:       governanceaudit.EventTypeBootstrap,
		ActorClass: governanceaudit.ActorClassSystem,
		ScopeClass: governanceaudit.ScopeClassAdmin,
		Decision:   governanceaudit.DecisionAllowed,
		ReasonCode: "bootstrap_mode_generated",
		OccurredAt: time.Now().UTC(),
	}}
}

// TestObservedGovernanceAuditAppenderCountsDrops is the drop-hook contract:
// a failed inner append still returns the error (failure semantics
// preserved: the caller decides the primary op survives) and the observer
// fires exactly once with that error and the dropped event count, which is
// what a satellite-native counter increments.
func TestObservedGovernanceAuditAppenderCountsDrops(t *testing.T) {
	t.Parallel()

	var calls int
	var gotErr error
	var gotCount int
	appender := ObservedGovernanceAuditAppender{
		Inner: &failingAuditSink{err: errAuditSinkDown},
		OnDrop: func(_ context.Context, err error, eventCount int) {
			calls++
			gotErr = err
			gotCount = eventCount
		},
	}

	err := appender.Append(context.Background(), testAuditEvents())

	if !errors.Is(err, errAuditSinkDown) {
		t.Fatalf("Append error = %v, want %v: the wrapper must not convert or swallow the sink error", err, errAuditSinkDown)
	}
	if calls != 1 {
		t.Fatalf("OnDrop calls = %d, want 1", calls)
	}
	if !errors.Is(gotErr, errAuditSinkDown) {
		t.Errorf("OnDrop error = %v, want %v", gotErr, errAuditSinkDown)
	}
	if gotCount != 1 {
		t.Errorf("OnDrop eventCount = %d, want 1", gotCount)
	}
}

// TestObservedGovernanceAuditAppenderSilentOnSuccess proves a healthy append
// forwards the events untouched, returns nil, and never fires the observer,
// so the satellite counter only moves on real loss.
func TestObservedGovernanceAuditAppenderSilentOnSuccess(t *testing.T) {
	t.Parallel()

	sink := &recordingAuditSink{}
	observed := 0
	appender := ObservedGovernanceAuditAppender{
		Inner:  sink,
		OnDrop: func(_ context.Context, _ error, _ int) { observed++ },
	}

	events := testAuditEvents()
	if err := appender.Append(context.Background(), events); err != nil {
		t.Fatalf("Append error = %v, want nil", err)
	}
	if len(sink.got) != 1 || len(sink.got[0]) != len(events) {
		t.Fatalf("sink received %d batches, want 1 batch of %d events", len(sink.got), len(events))
	}
	if observed != 0 {
		t.Errorf("OnDrop calls = %d, want 0 on success", observed)
	}
}

// TestObservedGovernanceAuditAppenderNilObserverIsNoOp proves the hook is
// opt-in: without an observer a failed append still returns the sink error
// and never panics, so in-tree call sites can adopt the wrapper before any
// satellite counter exists.
func TestObservedGovernanceAuditAppenderNilObserverIsNoOp(t *testing.T) {
	t.Parallel()

	appender := ObservedGovernanceAuditAppender{Inner: &failingAuditSink{err: errAuditSinkDown}}

	if err := appender.Append(context.Background(), testAuditEvents()); !errors.Is(err, errAuditSinkDown) {
		t.Fatalf("Append error = %v, want %v", err, errAuditSinkDown)
	}
}

// TestObservedGovernanceAuditAppenderZeroValueIsUsable proves the zero value
// (nil inner, e.g. audit store unavailable) is a silent skip returning nil,
// matching the existing nil-appender convention at every call site: the
// primary op proceeds and no drop is reported because nothing was attempted.
func TestObservedGovernanceAuditAppenderZeroValueIsUsable(t *testing.T) {
	t.Parallel()

	var appender ObservedGovernanceAuditAppender
	called := false
	appender.OnDrop = func(_ context.Context, _ error, _ int) { called = true }

	if err := appender.Append(context.Background(), testAuditEvents()); err != nil {
		t.Fatalf("zero-value Append error = %v, want nil", err)
	}
	if called {
		t.Error("OnDrop fired on zero-value skip, want no call: nothing was attempted")
	}
}

// TestObservedGovernanceAuditAppenderSatisfiesAdmission keeps the wrapper on
// the versioned seam: it must always satisfy GovernanceAuditAdmission so a
// satellite can wrap its sink without leaving the frozen contract.
func TestObservedGovernanceAuditAppenderSatisfiesAdmission(t *testing.T) {
	t.Parallel()

	var _ GovernanceAuditAdmission = ObservedGovernanceAuditAppender{}
	var _ GovernanceAuditAdmission = &ObservedGovernanceAuditAppender{}
}
