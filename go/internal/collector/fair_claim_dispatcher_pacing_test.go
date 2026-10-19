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

// pacingDispatcherChecker is a scripted ClaimPacingChecker: pacedTargets name
// the kind/instance pairs whose grants are delayed, and checkErr, when set,
// fails every pacing consult to prove fail-open.
type pacingDispatcherChecker struct {
	mu           sync.Mutex
	pacedTargets map[string]workflow.ClaimPacingDecision
	checkErr     error
	calls        int
}

func (c *pacingDispatcherChecker) CheckClaimPacing(
	_ context.Context,
	selector workflow.ClaimSelector,
	_ workflow.ClaimPacingConfig,
	_ time.Time,
) (workflow.ClaimPacingDecision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.checkErr != nil {
		return workflow.ClaimPacingDecision{}, c.checkErr
	}
	key := string(selector.CollectorKind) + ":" + selector.CollectorInstanceID
	if decision, ok := c.pacedTargets[key]; ok {
		return decision, nil
	}
	return workflow.ClaimPacingDecision{}, nil
}

// TestFairClaimDispatcherSkipsPacedTargets proves pacing throttles only the
// affected domain: a paced family is skipped while an unpaced family still
// grants, preserving the work-conserving rotation.
func TestFairClaimDispatcherSkipsPacedTargets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 22, 30, 0, 0, time.UTC)
	gitItem := testClaimedWorkItem(now)
	gitItem.CollectorKind = scope.CollectorGit
	gitItem.CollectorInstanceID = "git-primary"
	store := &fairDispatcherStore{
		results: map[string][]fairDispatcherClaimResult{
			"aws:aws-primary": {},
			"git:git-primary": {{
				item:  gitItem,
				claim: testWorkflowClaim(gitItem.WorkItemID, now),
				found: true,
			}},
		},
	}
	checker := &pacingDispatcherChecker{
		pacedTargets: map[string]workflow.ClaimPacingDecision{
			"aws:aws-primary": {
				Paced:      true,
				Signal:     workflow.ClaimPacingSignalDomain,
				Domain:     "aws/aws-primary",
				RetryAfter: 2 * time.Second,
			},
		},
	}
	dispatcher, err := NewFairClaimDispatcher(store, []workflow.FairnessCandidate{
		{CollectorKind: scope.CollectorAWS, CollectorInstanceID: "aws-primary", Weight: 1},
		{CollectorKind: scope.CollectorGit, CollectorInstanceID: "git-primary", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewFairClaimDispatcher() error = %v, want nil", err)
	}
	dispatcher.WithClaimPacing(checker, pacingDispatcherConfig())

	item, _, target, found, err := dispatcher.ClaimNext(
		context.Background(),
		"owner-1",
		"claim-1",
		now,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("ClaimNext() error = %v, want nil (paced target skipped, not failed)", err)
	}
	if !found {
		t.Fatal("ClaimNext() found = false, want true from the unpaced target")
	}
	if got, want := target.CollectorKind, scope.CollectorGit; got != want {
		t.Fatalf("target collector kind = %q, want %q", got, want)
	}
	if got, want := item.WorkItemID, gitItem.WorkItemID; got != want {
		t.Fatalf("WorkItemID = %q, want %q", got, want)
	}
}

// TestFairClaimDispatcherDefersWhenAllTargetsPaced proves the grant-delay
// channel: with every family paced, no claim is issued and the dispatcher
// returns the pacing deferral carrying the longest grant delay.
func TestFairClaimDispatcherDefersWhenAllTargetsPaced(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 22, 30, 0, 0, time.UTC)
	store := &fairDispatcherStore{
		results: map[string][]fairDispatcherClaimResult{},
	}
	checker := &pacingDispatcherChecker{
		pacedTargets: map[string]workflow.ClaimPacingDecision{
			"aws:aws-primary": {
				Paced:      true,
				Signal:     workflow.ClaimPacingSignalDomain,
				Domain:     "aws/aws-primary",
				RetryAfter: time.Second,
			},
			"git:git-primary": {
				Paced:      true,
				Signal:     workflow.ClaimPacingSignalGlobal,
				RetryAfter: 3 * time.Second,
			},
		},
	}
	dispatcher, err := NewFairClaimDispatcher(store, []workflow.FairnessCandidate{
		{CollectorKind: scope.CollectorAWS, CollectorInstanceID: "aws-primary", Weight: 1},
		{CollectorKind: scope.CollectorGit, CollectorInstanceID: "git-primary", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewFairClaimDispatcher() error = %v, want nil", err)
	}
	dispatcher.WithClaimPacing(checker, pacingDispatcherConfig())

	_, _, _, found, err := dispatcher.ClaimNext(
		context.Background(),
		"owner-1",
		"claim-1",
		now,
		time.Minute,
	)
	if err == nil {
		t.Fatal("ClaimNext() error = nil, want the pacing deferral")
	}
	if found {
		t.Fatal("ClaimNext() found = true, want false (paced grants issue no claim)")
	}
	var deferral *workflow.ClaimPacingDeferral
	if !errors.As(err, &deferral) {
		t.Fatalf("ClaimNext() error = %v, want *workflow.ClaimPacingDeferral", err)
	}
	if got, want := deferral.RetryAfterDelay(), 3*time.Second; got != want {
		t.Fatalf("deferral delay = %s, want longest grant delay %s", got, want)
	}
	if got := store.selectorCount(); got != 0 {
		t.Fatalf("claim selector count = %d, want 0 (no claim issued while paced)", got)
	}
}

// TestFairClaimDispatcherFailsOpenOnPacingError proves a dead pacing signal
// never blocks claims: a failing pacing consult claims normally.
func TestFairClaimDispatcherFailsOpenOnPacingError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 22, 30, 0, 0, time.UTC)
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
	checker := &pacingDispatcherChecker{checkErr: errors.New("depth unavailable")}
	dispatcher, err := NewFairClaimDispatcher(store, []workflow.FairnessCandidate{
		{CollectorKind: scope.CollectorGit, CollectorInstanceID: "git-primary", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewFairClaimDispatcher() error = %v, want nil", err)
	}
	dispatcher.WithClaimPacing(checker, pacingDispatcherConfig())

	_, _, _, found, err := dispatcher.ClaimNext(
		context.Background(),
		"owner-1",
		"claim-1",
		now,
		time.Minute,
	)
	if err != nil {
		t.Fatalf("ClaimNext() error = %v, want nil (pacing failure fails open)", err)
	}
	if !found {
		t.Fatal("ClaimNext() found = false, want true (pacing failure fails open)")
	}
}

// pacingDispatcherConfig returns tight pacing thresholds for dispatcher
// proofs. The scripted checker answers verdicts directly, so only the delay
// shape matters here; the watermarks travel with the checker for realism.
func pacingDispatcherConfig() workflow.ClaimPacingConfig {
	return workflow.ClaimPacingConfig{
		DomainHigh:    50,
		DomainLow:     20,
		GlobalHigh:    500,
		GlobalLow:     200,
		GrantDelay:    time.Second,
		MaxEngagement: time.Minute,
	}
}

// pacingOnceChecker paces the first consult with delay, then claims normally.
type pacingOnceChecker struct {
	mu    sync.Mutex
	delay time.Duration
	paced bool
}

func (c *pacingOnceChecker) CheckClaimPacing(
	_ context.Context,
	_ workflow.ClaimSelector,
	_ workflow.ClaimPacingConfig,
	_ time.Time,
) (workflow.ClaimPacingDecision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.paced {
		c.paced = true
		return workflow.ClaimPacingDecision{
			Paced:      true,
			Signal:     workflow.ClaimPacingSignalDomain,
			Domain:     "aws/aws-primary",
			RetryAfter: c.delay,
		}, nil
	}
	return workflow.ClaimPacingDecision{}, nil
}

// TestClaimedServiceBacksOffGrantDelayOnPacingDeferral proves the runner
// honors the grant-delay channel: a paced grant issues no claim and the
// runner waits out the grant delay (not just the poll interval) before the
// next grant succeeds.
func TestClaimedServiceBacksOffGrantDelayOnPacingDeferral(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.June, 16, 22, 45, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.CollectorKind = scope.CollectorAWS
	item.CollectorInstanceID = "aws-primary"
	item.SourceSystem = "aws"
	item.ScopeID = "aws-scope-1"
	item.AcceptanceUnitID = "aws-account-1"
	item.SourceRunID = "aws-generation-1"
	item.GenerationID = "aws-generation-1"
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &fairDispatcherStore{
		results: map[string][]fairDispatcherClaimResult{
			"aws:aws-primary": {{item: item, claim: claim, found: true}},
		},
		heartbeat: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	checker := &pacingOnceChecker{delay: 100 * time.Millisecond}
	dispatcher, err := NewFairClaimDispatcher(store, []workflow.FairnessCandidate{
		{CollectorKind: scope.CollectorAWS, CollectorInstanceID: "aws-primary", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewFairClaimDispatcher() error = %v, want nil", err)
	}
	dispatcher.WithClaimPacing(checker, pacingDispatcherConfig())
	sourceScope := testScope()
	sourceScope.CollectorKind = scope.CollectorAWS
	sourceScope.SourceSystem = "aws"
	sourceScope.ScopeID = item.ScopeID
	generation := testGeneration(now)
	generation.ScopeID = item.ScopeID
	generation.GenerationID = item.GenerationID
	source := &stubClaimedSource{
		collected: FactsFromSlice(sourceScope, generation, testFacts(now)),
		ok:        true,
	}
	committer := &stubClaimedCommitter{}
	service := ClaimedService{
		ControlStore:      store,
		ClaimDispatcher:   dispatcher,
		Source:            source,
		Committer:         committer,
		OwnerID:           "collector-owner-1",
		ClaimIDFunc:       func() string { return claim.ClaimID },
		PollInterval:      5 * time.Millisecond,
		ClaimLeaseTTL:     time.Minute,
		HeartbeatInterval: time.Second,
		Clock:             func() time.Time { return now },
	}

	started := time.Now()
	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if elapsed := time.Since(started); elapsed < 100*time.Millisecond {
		t.Fatalf("Run() elapsed = %s, want >= 100ms (the paced grant delay, not the 5ms poll)", elapsed)
	}
	if got, want := store.completeCalls, 1; got != want {
		t.Fatalf("complete calls = %d, want %d (the delayed grant still completes)", got, want)
	}
	if got := store.selectorCount(); got != 1 {
		t.Fatalf("claim selector count = %d, want 1 (paced attempt issues no claim)", got)
	}
}
