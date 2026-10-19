// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// pacingStubClaimStore is a ClaimControlStore that also serves scripted
// pacing consults, so the undispatched grant path proves its fail-open
// contract against a stub instead of a live store.
type pacingStubClaimStore struct {
	stubClaimStore
	decision     workflow.ClaimPacingDecision
	consultErr   error
	consultCalls int
}

func (s *pacingStubClaimStore) CheckClaimPacing(
	_ context.Context,
	_ workflow.ClaimSelector,
	_ workflow.ClaimPacingConfig,
	_ time.Time,
) (workflow.ClaimPacingDecision, error) {
	s.consultCalls++
	if s.consultErr != nil {
		return workflow.ClaimPacingDecision{}, s.consultErr
	}
	return s.decision, nil
}

// consultLogSink returns a debug-level logger writing JSON records into the
// returned buffer, so tests prove the fail-open consult stays diagnosable.
func consultLogSink() (*bytes.Buffer, *slog.Logger) {
	var buf bytes.Buffer
	return &buf, slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func pacingConsultSelector() workflow.ClaimSelector {
	return workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "git-primary",
		OwnerID:             "owner-1",
		ClaimID:             "claim-1",
	}
}

// TestPacingGrantDeferralPacedVerdict proves the undispatched grant path
// carries a paced verdict onto the grant-delay channel with no claim issued.
func TestPacingGrantDeferralPacedVerdict(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 23, 0, 0, 0, time.UTC)
	store := &pacingStubClaimStore{
		decision: workflow.ClaimPacingDecision{
			Paced:      true,
			Signal:     workflow.ClaimPacingSignalDomain,
			Domain:     "git/git-primary",
			RetryAfter: 3 * time.Second,
		},
	}
	svc := ClaimedService{
		ControlStore: store,
		ClaimPacing:  pacingPtr(pacingDispatcherConfig()),
		Clock:        func() time.Time { return now },
	}

	deferral := svc.pacingGrantDeferral(context.Background(), pacingConsultSelector())
	if deferral == nil {
		t.Fatal("pacingGrantDeferral() = nil, want grant-delay deferral (no claim issued)")
	}
	if deferral.Signal != workflow.ClaimPacingSignalDomain || deferral.Domain != "git/git-primary" {
		t.Fatalf("deferral = %s/%q, want domain git/git-primary", deferral.Signal, deferral.Domain)
	}
	if deferral.RetryAfterDelay() != 3*time.Second {
		t.Fatalf("deferral delay = %s, want 3s", deferral.RetryAfterDelay())
	}
	if store.consultCalls != 1 {
		t.Fatalf("consult calls = %d, want 1", store.consultCalls)
	}
}

// TestPacingGrantDeferralConsultErrorFailsOpenAndLogs proves a dead pacing
// signal never blocks the undispatched grant path, and the silence stays
// diagnosable: the fail-open consult emits a debug record naming the error.
func TestPacingGrantDeferralConsultErrorFailsOpenAndLogs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 23, 5, 0, 0, time.UTC)
	store := &pacingStubClaimStore{consultErr: errors.New("depth unavailable")}
	buf, logger := consultLogSink()
	svc := ClaimedService{
		ControlStore: store,
		ClaimPacing:  pacingPtr(pacingDispatcherConfig()),
		Clock:        func() time.Time { return now },
		Logger:       logger,
	}

	if deferral := svc.pacingGrantDeferral(context.Background(), pacingConsultSelector()); deferral != nil {
		t.Fatalf("pacingGrantDeferral() = %+v, want nil (consult failure fails open)", deferral)
	}
	if store.consultCalls != 1 {
		t.Fatalf("consult calls = %d, want 1 (the consult ran and failed)", store.consultCalls)
	}
	if logged := buf.String(); !strings.Contains(logged, "pacing consult failed open") ||
		!strings.Contains(logged, "depth unavailable") {
		t.Fatalf("consult log = %q, want fail-open record naming the error", logged)
	}
}

// TestPacingGrantDeferralSkipsStoreWithoutPacing proves a control store that
// does not serve pacing consults claims unpaced without touching pacing.
func TestPacingGrantDeferralSkipsStoreWithoutPacing(t *testing.T) {
	t.Parallel()

	svc := ClaimedService{
		ControlStore: &stubClaimStore{},
		ClaimPacing:  pacingPtr(pacingDispatcherConfig()),
		Clock:        func() time.Time { return time.Now().UTC() },
	}

	if deferral := svc.pacingGrantDeferral(context.Background(), pacingConsultSelector()); deferral != nil {
		t.Fatalf("pacingGrantDeferral() = %+v, want nil (store serves no pacing consult)", deferral)
	}
}

// TestPacingGrantDeferralSkipsDisabledPacing proves pacing off means no
// consult: nil config and disabled config both claim with no pacing check.
func TestPacingGrantDeferralSkipsDisabledPacing(t *testing.T) {
	t.Parallel()

	selector := pacingConsultSelector()
	for name, cfg := range map[string]*workflow.ClaimPacingConfig{
		"nil config":      nil,
		"disabled config": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := &pacingStubClaimStore{
				decision: workflow.ClaimPacingDecision{Paced: true, RetryAfter: time.Second},
			}
			svc := ClaimedService{ControlStore: store, ClaimPacing: cfg}

			if deferral := svc.pacingGrantDeferral(context.Background(), selector); deferral != nil {
				t.Fatalf("pacingGrantDeferral() = %+v, want nil (pacing off)", deferral)
			}
			if store.consultCalls != 0 {
				t.Fatalf("consult calls = %d, want 0 (pacing off consults nothing)", store.consultCalls)
			}
		})
	}
}

// TestPacingGrantDeferralUnpacedVerdict proves an unpaced verdict claims
// normally: no deferral, no log noise.
func TestPacingGrantDeferralUnpacedVerdict(t *testing.T) {
	t.Parallel()

	store := &pacingStubClaimStore{}
	buf, logger := consultLogSink()
	svc := ClaimedService{
		ControlStore: store,
		ClaimPacing:  pacingPtr(pacingDispatcherConfig()),
		Logger:       logger,
	}

	if deferral := svc.pacingGrantDeferral(context.Background(), pacingConsultSelector()); deferral != nil {
		t.Fatalf("pacingGrantDeferral() = %+v, want nil (unpaced verdict)", deferral)
	}
	if logged := buf.String(); logged != "" {
		t.Fatalf("consult log = %q, want empty (unpaced verdict is not a failure)", logged)
	}
}

// TestFairClaimDispatcherConsultErrorLogsFailOpen proves the dispatched path
// keeps its fail-open silence diagnosable too: the skipped target claims
// normally and the consult failure is logged with its error.
func TestFairClaimDispatcherConsultErrorLogsFailOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 23, 10, 0, 0, time.UTC)
	gitItem := testClaimedWorkItem(now)
	gitItem.CollectorKind = scope.CollectorGit
	gitItem.CollectorInstanceID = "git-primary"
	store := &fairDispatcherStore{
		results: map[string][]fairDispatcherClaimResult{
			"git:git-primary": {{
				item:  gitItem,
				claim: testWorkflowClaim(gitItem.WorkItemID, now),
				found: true,
			}},
		},
	}
	buf, logger := consultLogSink()
	dispatcher, err := NewFairClaimDispatcher(store, []workflow.FairnessCandidate{
		{CollectorKind: scope.CollectorGit, CollectorInstanceID: "git-primary", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewFairClaimDispatcher() error = %v, want nil", err)
	}
	dispatcher.WithClaimPacing(&pacingDispatcherChecker{checkErr: errors.New("depth unavailable")}, pacingDispatcherConfig())
	dispatcher.WithPacingLogger(logger)

	_, _, _, found, err := dispatcher.ClaimNext(context.Background(), "owner-1", "claim-1", now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNext() error = %v, want nil (pacing failure fails open)", err)
	}
	if !found {
		t.Fatal("ClaimNext() found = false, want true (pacing failure fails open)")
	}
	if logged := buf.String(); !strings.Contains(logged, "pacing consult failed open") ||
		!strings.Contains(logged, "depth unavailable") {
		t.Fatalf("consult log = %q, want fail-open record naming the error", logged)
	}
}

func pacingPtr(cfg workflow.ClaimPacingConfig) *workflow.ClaimPacingConfig {
	return &cfg
}
