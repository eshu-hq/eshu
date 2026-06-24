// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// concurrentUniqueClaimIDFunc returns a claim-id generator that is collision-free
// and safe for concurrent calls, matching the contract the host requires of
// Template.ClaimIDFunc.
func concurrentUniqueClaimIDFunc() func() string {
	var n atomic.Uint64
	return func() string {
		return fmt.Sprintf("claim-%d", n.Add(1))
	}
}

// recordingClaimedSource records how many times it resolved a claimed item so a
// test can prove which family/instance's source served a dispatched target.
type recordingClaimedSource struct {
	mu        sync.Mutex
	calls     int
	collected CollectedGeneration
	ok        bool
	err       error
}

func (r *recordingClaimedSource) NextClaimed(context.Context, workflow.WorkItem) (CollectedGeneration, bool, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return r.collected, r.ok, r.err
}

func (r *recordingClaimedSource) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func wildcardSourceRegistration(kind scope.CollectorKind, src ClaimedSource) ClaimSourceRegistration {
	return ClaimSourceRegistration{CollectorKind: kind, Source: src}
}

func enabledCollectorInstance(kind scope.CollectorKind, instanceID string) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:    instanceID,
		CollectorKind: kind,
		Enabled:       true,
		ClaimsEnabled: true,
	}
}

func disabledCollectorInstance(kind scope.CollectorKind, instanceID string) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:    instanceID,
		CollectorKind: kind,
		Enabled:       false,
		ClaimsEnabled: false,
	}
}

// TestClaimedServiceResolvesSourcePerDispatchedTarget proves the runner resolves
// the source by the dispatched target's (kind, instance), not the kind alone:
// two instances of the same kind get distinct sources, and the dispatched
// instance's source serves the work while the sibling instance's does not.
func TestClaimedServiceResolvesSourcePerDispatchedTarget(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.June, 17, 18, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.CollectorKind = scope.CollectorAWS
	item.CollectorInstanceID = "aws-primary"
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
	dispatcher, err := NewFairClaimDispatcher(store, []workflow.FairnessCandidate{
		{CollectorKind: scope.CollectorAWS, CollectorInstanceID: "aws-primary", Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewFairClaimDispatcher() error = %v", err)
	}

	primarySource := &recordingClaimedSource{ok: false}
	siblingSource := &recordingClaimedSource{ok: false}
	resolver, err := buildHostSourceResolver([]ClaimSourceRegistration{
		{CollectorKind: scope.CollectorAWS, CollectorInstanceID: "aws-primary", Source: primarySource},
		{CollectorKind: scope.CollectorAWS, CollectorInstanceID: "aws-secondary", Source: siblingSource},
	})
	if err != nil {
		t.Fatalf("buildHostSourceResolver() error = %v", err)
	}

	service := testClaimedService(now, claim, scope.CollectorGit, nil, nil, &stubClaimedCommitter{})
	service.ControlStore = store
	service.ClaimDispatcher = dispatcher
	service.SourceResolver = resolver.resolve
	service.Source = nil
	service.CollectorKind = ""
	service.CollectorInstanceID = ""

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := primarySource.callCount(); got != 1 {
		t.Fatalf("aws-primary source calls = %d, want 1 (must serve its own instance)", got)
	}
	if got := siblingSource.callCount(); got != 0 {
		t.Fatalf("aws-secondary source calls = %d, want 0 (must not serve another instance)", got)
	}
}

// TestMultiSourceCollectorHostRunsLifecycleWithoutStarvingFamilies proves the
// host runs the full claim lifecycle through ClaimedService for the family that
// has work, while skipping the empty family's lane (no starvation), and that the
// dispatcher queried both families.
func TestMultiSourceCollectorHostRunsLifecycleWithoutStarvingFamilies(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.June, 17, 18, 30, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.CollectorKind = scope.CollectorGit
	item.CollectorInstanceID = "git-primary"
	item.SourceSystem = "git"
	item.ScopeID = "git-scope-1"
	item.AcceptanceUnitID = "git-repo-1"
	item.SourceRunID = "git-generation-1"
	item.GenerationID = "git-generation-1"
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &fairDispatcherStore{
		results: map[string][]fairDispatcherClaimResult{
			"git:git-primary": {{item: item, claim: claim, found: true}},
			"aws:aws-primary": {},
		},
		heartbeat: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}

	sourceScope := testScope()
	sourceScope.CollectorKind = scope.CollectorGit
	sourceScope.SourceSystem = "git"
	sourceScope.ScopeID = item.ScopeID
	generation := testGeneration(now)
	generation.ScopeID = item.ScopeID
	generation.GenerationID = item.GenerationID
	gitSource := &recordingClaimedSource{
		collected: FactsFromSlice(sourceScope, generation, testFacts(now)),
		ok:        true,
	}
	awsSource := &recordingClaimedSource{ok: false}
	committer := &stubClaimedCommitter{}

	template := testClaimedService(now, claim, scope.CollectorGit, nil, nil, committer)
	template.ControlStore = store
	host, err := NewMultiSourceCollectorHost(MultiSourceCollectorHostConfig{
		Sources: []ClaimSourceRegistration{
			wildcardSourceRegistration(scope.CollectorGit, gitSource),
			wildcardSourceRegistration(scope.CollectorAWS, awsSource),
		},
		Instances: []workflow.CollectorInstance{
			enabledCollectorInstance(scope.CollectorGit, "git-primary"),
			enabledCollectorInstance(scope.CollectorAWS, "aws-primary"),
		},
		Template: template,
		Workers:  1,
	})
	if err != nil {
		t.Fatalf("NewMultiSourceCollectorHost() error = %v", err)
	}

	if err := host.Run(ctx); err != nil {
		t.Fatalf("host.Run() error = %v", err)
	}
	if got, want := store.completeCalls, 1; got != want {
		t.Fatalf("complete calls = %d, want %d (git lifecycle completed through host)", got, want)
	}
	if got, want := committer.claimedCalls, 1; got != want {
		t.Fatalf("claimed commit calls = %d, want %d", got, want)
	}
	if got := gitSource.callCount(); got != 1 {
		t.Fatalf("git source calls = %d, want 1", got)
	}
	sawAWS := false
	store.mu.Lock()
	for _, selector := range store.selectors {
		if selector.CollectorKind == scope.CollectorAWS {
			sawAWS = true
		}
	}
	store.mu.Unlock()
	if !sawAWS {
		t.Fatal("dispatcher never queried the aws lane; empty family starved out of fair rotation")
	}
}

// TestMultiSourceCollectorHostSharedSchedulerIsRaceFree drives many concurrent
// workers over one shared dispatcher with no available work; run under -race it
// proves the shared scheduling state has no data race and the host returns
// cleanly on cancellation.
func TestMultiSourceCollectorHostSharedSchedulerIsRaceFree(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 19, 0, 0, 0, time.UTC)
	claim := testWorkflowClaim("work-item-1", now)
	store := &fairDispatcherStore{results: map[string][]fairDispatcherClaimResult{}}

	gitSource := &recordingClaimedSource{ok: false}
	awsSource := &recordingClaimedSource{ok: false}
	template := testClaimedService(now, claim, scope.CollectorGit, nil, nil, &stubClaimedCommitter{})
	template.ControlStore = store
	// Each worker shares this func; it must hand out unique ids under concurrency.
	template.ClaimIDFunc = concurrentUniqueClaimIDFunc()
	host, err := NewMultiSourceCollectorHost(MultiSourceCollectorHostConfig{
		Sources: []ClaimSourceRegistration{
			wildcardSourceRegistration(scope.CollectorGit, gitSource),
			wildcardSourceRegistration(scope.CollectorAWS, awsSource),
		},
		Instances: []workflow.CollectorInstance{
			enabledCollectorInstance(scope.CollectorGit, "git-primary"),
			enabledCollectorInstance(scope.CollectorAWS, "aws-primary"),
		},
		Template: template,
		Workers:  8,
	})
	if err != nil {
		t.Fatalf("NewMultiSourceCollectorHost() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if err := host.Run(ctx); err != nil {
		t.Fatalf("host.Run() error = %v, want nil on cancellation", err)
	}
	if store.selectorCount() == 0 {
		t.Fatal("expected concurrent workers to query the shared dispatcher")
	}
}

// TestNewMultiSourceCollectorHostRejectsCandidateWithoutSource proves the host
// refuses a configuration where a claim-enabled dispatch candidate has no
// registered source, so the dispatcher can never select a target the host
// cannot serve.
func TestNewMultiSourceCollectorHostRejectsCandidateWithoutSource(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 19, 30, 0, 0, time.UTC)
	claim := testWorkflowClaim("work-item-1", now)
	store := &fairDispatcherStore{results: map[string][]fairDispatcherClaimResult{}}
	template := testClaimedService(now, claim, scope.CollectorGit, nil, nil, &stubClaimedCommitter{})
	template.ControlStore = store

	_, err := NewMultiSourceCollectorHost(MultiSourceCollectorHostConfig{
		Sources: []ClaimSourceRegistration{
			wildcardSourceRegistration(scope.CollectorGit, &recordingClaimedSource{}),
		},
		Instances: []workflow.CollectorInstance{
			enabledCollectorInstance(scope.CollectorGit, "git-primary"),
			enabledCollectorInstance(scope.CollectorAWS, "aws-primary"),
		},
		Template: template,
	})
	if err == nil {
		t.Fatal("NewMultiSourceCollectorHost() error = nil, want rejection for enabled aws candidate without a source")
	}
}

// TestNewMultiSourceCollectorHostIgnoresDisabledInstances proves the host
// filters disabled/claims-disabled instances out of the dispatch candidates
// before requiring sources, so a non-dispatchable registration for another
// family does not block startup.
func TestNewMultiSourceCollectorHostIgnoresDisabledInstances(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 20, 0, 0, 0, time.UTC)
	claim := testWorkflowClaim("work-item-1", now)
	store := &fairDispatcherStore{results: map[string][]fairDispatcherClaimResult{}}
	template := testClaimedService(now, claim, scope.CollectorGit, nil, nil, &stubClaimedCommitter{})
	template.ControlStore = store

	host, err := NewMultiSourceCollectorHost(MultiSourceCollectorHostConfig{
		Sources: []ClaimSourceRegistration{
			wildcardSourceRegistration(scope.CollectorGit, &recordingClaimedSource{}),
		},
		Instances: []workflow.CollectorInstance{
			enabledCollectorInstance(scope.CollectorGit, "git-primary"),
			// A disabled aws registration with no source must not block startup.
			disabledCollectorInstance(scope.CollectorAWS, "aws-primary"),
		},
		Template: template,
	})
	if err != nil {
		t.Fatalf("NewMultiSourceCollectorHost() error = %v, want nil (disabled aws instance must be ignored)", err)
	}
	if host == nil {
		t.Fatal("host = nil, want a runnable host")
	}
}
