// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// sourceBackpressureCheckerStub scripts per-target source-backpressure
// verdicts: slowedTargets name the kind/instance pairs whose source is
// behind, and checkErr fails every consult to prove fail-open. flip()
// clears the slowed set to prove resume.
type sourceBackpressureCheckerStub struct {
	mu            sync.Mutex
	slowedTargets map[string]string // kind:instance -> slowed source system
	checkErr      error
	calls         int
}

func (c *sourceBackpressureCheckerStub) CheckSourceBackpressure(
	_ context.Context,
	selector workflow.ClaimSelector,
	_ workflow.SourceBackpressureConfig,
	_ time.Time,
) (workflow.ClaimPacingDecision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.checkErr != nil {
		return workflow.ClaimPacingDecision{}, c.checkErr
	}
	key := string(selector.CollectorKind) + ":" + selector.CollectorInstanceID
	if source, ok := c.slowedTargets[key]; ok {
		return workflow.ClaimPacingDecision{
			Paced:      true,
			Signal:     workflow.ClaimPacingSignalReducerSource,
			Domain:     source,
			RetryAfter: 2 * time.Second,
		}, nil
	}
	return workflow.ClaimPacingDecision{}, nil
}

// flip releases every slowed target, simulating the queue clearing.
func (c *sourceBackpressureCheckerStub) flip() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.slowedTargets = map[string]string{}
}

// sourceBackpressureDispatcherConfig returns an enabled source-backpressure
// config at test scale.
func sourceBackpressureDispatcherConfig() workflow.SourceBackpressureConfig {
	return workflow.SourceBackpressureConfig{
		EngageAge:     time.Hour,
		ReleaseAge:    15 * time.Minute,
		DepthFloor:    25,
		GrantDelay:    2 * time.Second,
		MaxEngagement: 30 * time.Minute,
	}
}

// sourceDispatcherFixture builds a two-family dispatcher: vuln (slowed
// source) and git (healthy source), each with one claimable item.
func sourceDispatcherFixture(t *testing.T, now time.Time) (*fairDispatcherStore, *FairClaimDispatcher) {
	t.Helper()

	vulnItem := testClaimedWorkItem(now)
	vulnItem.CollectorKind = scope.CollectorKind("vuln")
	vulnItem.CollectorInstanceID = "vuln-primary"
	gitItem := testClaimedWorkItem(now)
	gitItem.WorkItemID = gitItem.WorkItemID + "-git"
	gitItem.CollectorKind = scope.CollectorGit
	gitItem.CollectorInstanceID = "git-primary"
	store := &fairDispatcherStore{
		results: map[string][]fairDispatcherClaimResult{
			"vuln:vuln-primary": {{item: vulnItem, claim: testWorkflowClaim(vulnItem.WorkItemID, now), found: true}},
			"git:git-primary":   {{item: gitItem, claim: testWorkflowClaim(gitItem.WorkItemID, now), found: true}},
		},
	}
	dispatcher, err := NewFairClaimDispatcher(store, []workflow.FairnessCandidate{
		{CollectorKind: scope.CollectorKind("vuln"), CollectorInstanceID: "vuln-primary", Weight: 1},
		{CollectorKind: scope.CollectorGit, CollectorInstanceID: "git-primary", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewFairClaimDispatcher() error = %v, want nil", err)
	}
	return store, dispatcher
}

// TestFairClaimDispatcherSourceBackpressureSkipsSlowedOnly proves AC3/AC5 at
// the grant path: the slowed source's family is skipped work-conservingly
// while the healthy source still grants in the same call, and no claim is
// issued against the slowed family (AC4 no-loss at the grant boundary:
// the slowed work item is never touched, so no attempt is consumed).
func TestFairClaimDispatcherSourceBackpressureSkipsSlowedOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	store, dispatcher := sourceDispatcherFixture(t, now)
	checker := &sourceBackpressureCheckerStub{slowedTargets: map[string]string{
		"vuln:vuln-primary": "vuln-intel",
	}}
	dispatcher.WithSourceBackpressure(checker, sourceBackpressureDispatcherConfig())

	_, _, target, found, err := dispatcher.ClaimNext(context.Background(), "owner-1", "claim-1", now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNext() error = %v, want nil (healthy source grants)", err)
	}
	if !found || target.CollectorKind != scope.CollectorGit {
		t.Fatalf("ClaimNext() = found %v target %v, want git grant", found, target)
	}
	for _, selector := range store.selectors {
		if selector.CollectorInstanceID == "vuln-primary" {
			t.Fatal("claim issued against slowed vuln family, want none (no claim, no attempt)")
		}
	}
}

// TestFairClaimDispatcherSourceBackpressureDefersAndResumes proves the
// resume half of AC4: with every family slowed the grant returns the
// reducer_source deferral, and once the queue clears (checker flips) the
// previously slowed family's item is still there to claim — slowed work is
// deferred, never dropped.
func TestFairClaimDispatcherSourceBackpressureDefersAndResumes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	_, dispatcher := sourceDispatcherFixture(t, now)
	checker := &sourceBackpressureCheckerStub{slowedTargets: map[string]string{
		"vuln:vuln-primary": "vuln-intel",
		"git:git-primary":   "git",
	}}
	dispatcher.WithSourceBackpressure(checker, sourceBackpressureDispatcherConfig())

	_, _, _, found, err := dispatcher.ClaimNext(context.Background(), "owner-1", "claim-1", now, time.Minute)
	if found {
		t.Fatal("ClaimNext() found = true with all sources slowed, want false")
	}
	var deferral *workflow.ClaimPacingDeferral
	if !errors.As(err, &deferral) {
		t.Fatalf("ClaimNext() error = %v, want ClaimPacingDeferral", err)
	}
	if deferral.Signal != workflow.ClaimPacingSignalReducerSource {
		t.Fatalf("deferral signal = %q, want reducer_source", deferral.Signal)
	}
	if deferral.RetryAfterDelay() != 2*time.Second {
		t.Fatalf("deferral delay = %s, want 2s", deferral.RetryAfterDelay())
	}

	checker.flip()
	_, _, _, found, err = dispatcher.ClaimNext(context.Background(), "owner-1", "claim-2", now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNext() after release error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNext() after release found = false, want true (slowed work resumes, none lost)")
	}
}

// TestFairClaimDispatcherSourceBackpressureFailsOpen proves a dead signal
// never blocks intake: the consult error fails open and the grant proceeds.
func TestFairClaimDispatcherSourceBackpressureFailsOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	_, dispatcher := sourceDispatcherFixture(t, now)
	checker := &sourceBackpressureCheckerStub{checkErr: errors.New("signal unavailable")}
	dispatcher.WithSourceBackpressure(checker, sourceBackpressureDispatcherConfig())

	_, _, _, found, err := dispatcher.ClaimNext(context.Background(), "owner-1", "claim-1", now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNext() error = %v, want nil (fail open)", err)
	}
	if !found {
		t.Fatal("ClaimNext() found = false, want true (fail open grants)")
	}
}

// TestSourceBackpressureGrantDeferralDirectPath proves the undispatched
// grant path carries a slowed verdict onto the grant-delay channel with no
// claim issued.
func TestSourceBackpressureGrantDeferralDirectPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	store := &sourceStubClaimStore{decision: workflow.ClaimPacingDecision{
		Paced:      true,
		Signal:     workflow.ClaimPacingSignalReducerSource,
		Domain:     "git",
		RetryAfter: 2 * time.Second,
	}}
	cfg := sourceBackpressureDispatcherConfig()
	svc := ClaimedService{
		ControlStore:       store,
		SourceBackpressure: &cfg,
		Clock:              func() time.Time { return now },
	}

	deferral := svc.sourceBackpressureDeferral(context.Background(), pacingConsultSelector())
	if deferral == nil {
		t.Fatal("sourceBackpressureDeferral() = nil, want grant-delay deferral")
	}
	if deferral.Signal != workflow.ClaimPacingSignalReducerSource || deferral.Domain != "git" {
		t.Fatalf("deferral = %s/%q, want reducer_source/git", deferral.Signal, deferral.Domain)
	}
	if store.consultCalls != 1 {
		t.Fatalf("consult calls = %d, want 1", store.consultCalls)
	}
}

// sourceStubClaimStore serves scripted source-backpressure consults on the
// undispatched grant path.
type sourceStubClaimStore struct {
	stubClaimStore
	decision     workflow.ClaimPacingDecision
	consultCalls int
}

func (s *sourceStubClaimStore) CheckSourceBackpressure(
	_ context.Context,
	_ workflow.ClaimSelector,
	_ workflow.SourceBackpressureConfig,
	_ time.Time,
) (workflow.ClaimPacingDecision, error) {
	s.consultCalls++
	return s.decision, nil
}

// TestClaimPlaneClientDoesNotServeSourceBackpressure pins the #10882 AC3
// satellite boundary: a collector whose claims run through the eshu-workflow
// claim plane (ESHU_WORKFLOW_CLAIM_PLANE_URL) holds a ClaimPlaneClient, which
// has no per-source reducer-backpressure consult. The reducer queue lives in
// core Postgres, which the claim plane does not read, and workflow.v1 has no
// reducer_source pacing signal (eshu-workflow workflowv1/pacing.go knows only
// domain and global). The in-repo wiring therefore refuses startup with the
// leg enabled instead of claiming unpaced (entrypoints/service_template.go,
// cmd/collector-component-extension/service.go). If the client ever gains
// the consult, this test fails and the AC3 scope note in
// docs/internal/design/10882-source-backpressure.md must be revisited.
func TestClaimPlaneClientDoesNotServeSourceBackpressure(t *testing.T) {
	t.Parallel()

	var store any = &workflow.ClaimPlaneClient{}
	if _, ok := store.(SourceBackpressureChecker); ok {
		t.Fatal("ClaimPlaneClient serves SourceBackpressureChecker: plane-held claims are now pace-able, update the #10882 AC3 scope and satellite-parity notes")
	}
}
