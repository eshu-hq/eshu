// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// recordingConfigStateDriftRedriveScheduler captures every EnsureScheduled
// call for assertion.
type recordingConfigStateDriftRedriveScheduler struct {
	calls []struct {
		scopeID        string
		generationID   string
		firstAttemptAt time.Time
	}
	err error
}

func (r *recordingConfigStateDriftRedriveScheduler) EnsureScheduled(_ context.Context, scopeID, generationID string, firstAttemptAt time.Time) error {
	r.calls = append(r.calls, struct {
		scopeID        string
		generationID   string
		firstAttemptAt time.Time
	}{scopeID, generationID, firstAttemptAt})
	return r.err
}

// TestDriftHandlerNoOwnerSchedulesRedrive proves the issue #5593 P1-A fix:
// the handler schedules a redrive ONLY when it actually observes
// tfstatebackend.ErrNoConfigRepoOwnsBackend -- not unconditionally on every
// intent -- and schedules it for the exact (scope, generation) the intent
// carries, at now + RedriveDelay.
func TestDriftHandlerNoOwnerSchedulesRedrive(t *testing.T) {
	t.Parallel()

	resolver := tfstatebackend.NewResolver(&stubBackendQuery{}) // empty rows -> no owner
	scheduler := &recordingConfigStateDriftRedriveScheduler{}
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	h := TerraformConfigStateDriftHandler{
		Resolver:       resolver,
		EvidenceLoader: &stubDriftLoader{},
		Redrive:        scheduler,
		RedriveDelay:   5 * time.Minute,
		Now:            func() time.Time { return now },
	}

	intent := validIntent()
	res, err := h.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() err = %v, want nil", err)
	}
	if res.Status != ResultStatusSucceeded {
		t.Fatalf("res.Status = %q, want Succeeded", res.Status)
	}

	if len(scheduler.calls) != 1 {
		t.Fatalf("EnsureScheduled call count = %d, want 1", len(scheduler.calls))
	}
	call := scheduler.calls[0]
	if call.scopeID != intent.ScopeID || call.generationID != intent.GenerationID {
		t.Fatalf("EnsureScheduled args = %+v, want scope=%q generation=%q", call, intent.ScopeID, intent.GenerationID)
	}
	if want := now.Add(5 * time.Minute); !call.firstAttemptAt.Equal(want) {
		t.Fatalf("EnsureScheduled firstAttemptAt = %v, want %v (now + RedriveDelay)", call.firstAttemptAt, want)
	}
}

// TestDriftHandlerAmbiguousOwnerDoesNotScheduleRedrive proves the redrive is
// scoped strictly to the no-owner rejection: an ambiguous-owner rejection is
// a DIFFERENT, more definitive outcome (multiple config repos currently
// claim the same backend) that a bounded redrive would not resolve on its
// own, so scheduling one would only waste attempts.
func TestDriftHandlerAmbiguousOwnerDoesNotScheduleRedrive(t *testing.T) {
	t.Parallel()

	rows := []tfstatebackend.TerraformBackendRow{
		{RepoID: "repo-a", ScopeID: "repo:repo-a@1", CommitID: "aaa", CommitObservedAt: time.Now(), BackendKind: "s3", LocatorHash: "hash-1"},
		{RepoID: "repo-b", ScopeID: "repo:repo-b@1", CommitID: "bbb", CommitObservedAt: time.Now(), BackendKind: "s3", LocatorHash: "hash-1"},
	}
	resolver := tfstatebackend.NewResolver(&stubBackendQuery{rows: rows})
	scheduler := &recordingConfigStateDriftRedriveScheduler{}
	h := TerraformConfigStateDriftHandler{
		Resolver:       resolver,
		EvidenceLoader: &stubDriftLoader{},
		Redrive:        scheduler,
	}

	if _, err := h.Handle(context.Background(), validIntent()); err != nil {
		t.Fatalf("Handle() err = %v, want nil", err)
	}
	if len(scheduler.calls) != 0 {
		t.Fatalf("EnsureScheduled call count = %d, want 0 (ambiguous owner must not schedule a redrive)", len(scheduler.calls))
	}
}

// TestDriftHandlerSingleOwnerDoesNotScheduleRedrive proves a successful
// resolution (single owner found, evaluation proceeds) never schedules a
// redrive -- the P1-A regression this whole fix exists to close was
// scheduling unconditionally on EVERY runtime-triggered activation,
// including ones that resolve correctly on the first attempt.
func TestDriftHandlerSingleOwnerDoesNotScheduleRedrive(t *testing.T) {
	t.Parallel()

	rows := []tfstatebackend.TerraformBackendRow{
		{RepoID: "repo-a", ScopeID: "repo:repo-a@1", CommitID: "aaa", CommitObservedAt: time.Now(), BackendKind: "s3", LocatorHash: "hash-1"},
	}
	resolver := tfstatebackend.NewResolver(&stubBackendQuery{rows: rows})
	scheduler := &recordingConfigStateDriftRedriveScheduler{}
	h := TerraformConfigStateDriftHandler{
		Resolver:       resolver,
		EvidenceLoader: &stubDriftLoader{}, // no drift rows -> evaluated, no drift
		Redrive:        scheduler,
	}

	if _, err := h.Handle(context.Background(), validIntent()); err != nil {
		t.Fatalf("Handle() err = %v, want nil", err)
	}
	if len(scheduler.calls) != 0 {
		t.Fatalf("EnsureScheduled call count = %d, want 0 (a resolved evaluation must not schedule a redrive)", len(scheduler.calls))
	}
}

// TestDriftHandlerNonStateSnapshotScopeDoesNotScheduleRedrive proves a
// structural scope mismatch (a different rejection class entirely) does not
// schedule a redrive either -- only tfstatebackend.ErrNoConfigRepoOwnsBackend
// does.
func TestDriftHandlerNonStateSnapshotScopeDoesNotScheduleRedrive(t *testing.T) {
	t.Parallel()

	scheduler := &recordingConfigStateDriftRedriveScheduler{}
	h := TerraformConfigStateDriftHandler{
		Resolver: tfstatebackend.NewResolver(nil),
		Redrive:  scheduler,
	}
	intent := validIntent()
	intent.ScopeID = "repo:repo-1@abc"

	if _, err := h.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() err = %v, want nil", err)
	}
	if len(scheduler.calls) != 0 {
		t.Fatalf("EnsureScheduled call count = %d, want 0 (structural mismatch is not the no-owner rejection)", len(scheduler.calls))
	}
}

// TestDriftHandlerSkipsRedriveSchedulingWhenNil proves scheduling is a pure
// no-op (never panics) when Redrive is unwired -- every caller before issue
// #5593 P1-A's fix, and the durable-terminal-rejection behavior that
// predates it.
func TestDriftHandlerSkipsRedriveSchedulingWhenNil(t *testing.T) {
	t.Parallel()

	resolver := tfstatebackend.NewResolver(&stubBackendQuery{}) // no owner
	h := TerraformConfigStateDriftHandler{
		Resolver:       resolver,
		EvidenceLoader: &stubDriftLoader{},
		// Redrive intentionally left nil.
	}
	res, err := h.Handle(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("Handle() err = %v, want nil", err)
	}
	if res.Status != ResultStatusSucceeded {
		t.Fatalf("res.Status = %q, want Succeeded", res.Status)
	}
}

// TestDriftHandlerLogsRedriveSchedulingFailureWithoutFailingHandle proves a
// redrive-scheduling error is logged, not propagated: the "no owner"
// rejection is itself a valid, already-recorded Handle() outcome, and a
// best-effort recovery aid failing to schedule must not turn that into a
// runtime error that would dead-letter the intent.
func TestDriftHandlerLogsRedriveSchedulingFailureWithoutFailingHandle(t *testing.T) {
	t.Parallel()

	resolver := tfstatebackend.NewResolver(&stubBackendQuery{})
	scheduler := &recordingConfigStateDriftRedriveScheduler{err: errors.New("ledger write failed")}
	h := TerraformConfigStateDriftHandler{
		Resolver:       resolver,
		EvidenceLoader: &stubDriftLoader{},
		Redrive:        scheduler,
		Logger:         slog.Default(),
	}

	res, err := h.Handle(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("Handle() err = %v, want nil (scheduling failure must not fail the intent)", err)
	}
	if res.Status != ResultStatusSucceeded {
		t.Fatalf("res.Status = %q, want Succeeded", res.Status)
	}
	if len(scheduler.calls) != 1 {
		t.Fatalf("EnsureScheduled call count = %d, want 1 (the attempt must still happen)", len(scheduler.calls))
	}
}
