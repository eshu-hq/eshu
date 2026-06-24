// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestClaimedServiceClaimsHeartbeatsCommitsAndCompletes(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.April, 20, 22, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.TenantID = "tenant-a"
	item.WorkspaceID = "workspace-a"
	item.SubjectClass = "collector"
	item.PolicyRevisionHash = "policy-a"
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		heartbeat: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{
		collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)),
		ok:        true,
	}
	committer := &stubClaimedCommitter{}
	sink := &stubGenerationDeadLetterSink{}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)
	service.DeadLetters = sink

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := store.claimCalls, 1; got != want {
		t.Fatalf("claim calls = %d, want %d", got, want)
	}
	if got, want := store.completeCalls, 1; got != want {
		t.Fatalf("complete calls = %d, want %d", got, want)
	}
	if got, want := committer.claimedCalls, 1; got != want {
		t.Fatalf("claimed commit calls = %d, want %d", got, want)
	}
	if got := store.lastComplete; got.WorkItemID != item.WorkItemID || got.ClaimID != claim.ClaimID || got.FencingToken != claim.FencingToken {
		t.Fatalf("complete mutation = %#v, want item/claim/fence from claim", got)
	}
	if got := committer.lastClaimMutation; got.WorkItemID != item.WorkItemID || got.ClaimID != claim.ClaimID || got.FencingToken != claim.FencingToken {
		t.Fatalf("claimed commit mutation = %#v, want item/claim/fence from claim", got)
	}
	if got := committer.lastClaimMutation; got.TenantID != item.TenantID ||
		got.WorkspaceID != item.WorkspaceID ||
		got.SubjectClass != item.SubjectClass ||
		got.PolicyRevisionHash != item.PolicyRevisionHash {
		t.Fatalf("claimed commit tenant boundary = %#v, want boundary from work item", got)
	}
	if got, want := len(sink.completions), 1; got != want {
		t.Fatalf("dead-letter replay completions = %d, want %d", got, want)
	}
	if got := sink.completionContextErrs[0]; got != nil {
		t.Fatalf("dead-letter replay completion context error = %v, want nil", got)
	}
}

func TestClaimedServiceReleasesWhenClaimHasNoGeneration(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.April, 20, 22, 10, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		release: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	service := testClaimedService(now, claim, scope.CollectorGit, store, &stubClaimedSource{}, &stubClaimedCommitter{})

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := store.releaseCalls, 1; got != want {
		t.Fatalf("release calls = %d, want %d", got, want)
	}
	if got, want := store.completeCalls, 0; got != want {
		t.Fatalf("complete calls = %d, want %d", got, want)
	}
}

// TestClaimedServiceTerminalFailsClassifiedCommitFailure pins the
// symmetric-with-NextClaimed terminal classification on the commit path. A
// commit-side stale-fence (issue #612) returns a terminal-classified error
// so the runner routes the claim through FailClaimTerminal with that class,
// not FailClaimRetryable.
func TestClaimedServiceTerminalFailsClassifiedCommitFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.May, 25, 16, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		terminalFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)), ok: true}
	committer := &stubClaimedCommitter{
		claimedCommit: func(context.Context, workflow.ClaimMutation, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error {
			return terminalCollectFailure{err: errors.New("commit stale fence")}
		},
	}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after terminal commit failure", err)
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	if got, want := store.retryableFailCalls, 0; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got := store.lastTerminalFail.FailureClass; got != "non_retryable" {
		t.Fatalf("FailureClass = %q, want non_retryable", got)
	}
}

func TestClaimedServiceFailsClaimWhenCommitFails(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.April, 20, 22, 20, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	wantErr := errors.New("commit failed")
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		retryableFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{collected: FactsFromSlice(testScope(), testGeneration(now), testFacts(now)), ok: true}
	committer := &stubClaimedCommitter{
		claimedCommit: func(context.Context, workflow.ClaimMutation, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error {
			return wantErr
		},
	}
	sink := &stubGenerationDeadLetterSink{}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)
	service.DeadLetters = sink

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after retryable commit failure", err)
	}
	if got, want := store.retryableFailCalls, 1; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got := store.lastRetryableFail; got.FailureClass != "commit_failure" {
		t.Fatalf("FailureClass = %q, want commit_failure", got.FailureClass)
	}
	if got, want := len(sink.records), 1; got != want {
		t.Fatalf("dead-letter records = %d, want %d", got, want)
	}
	if got := sink.records[0].Generation.GenerationID; got != "generation-claim-1" {
		t.Fatalf("dead-letter generation_id = %q, want generation-claim-1", got)
	}
}

func TestClaimedServiceContinuesAfterRetryableCollectFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.May, 18, 16, 45, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	wantErr := errors.New("temporary throttle")
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		retryableFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{err: wantErr}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after retryable claim failure", err)
	}
	if got, want := store.retryableFailCalls, 1; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got := store.lastRetryableFail.FailureClass; got != "collect_failure" {
		t.Fatalf("FailureClass = %q, want collect_failure", got)
	}
	if got, want := store.lastRetryableFail.VisibleAt, now.Add(service.PollInterval); !got.Equal(want) {
		t.Fatalf("VisibleAt = %s, want %s", got, want)
	}
}

func TestClaimedServiceHonorsRetryAfterOnRetryableCollectFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.May, 18, 16, 50, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	wantDelay := 45 * time.Second
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		retryableFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{err: retryAfterCollectFailure{delay: wantDelay}}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after retryable claim failure", err)
	}
	if got, want := store.retryableFailCalls, 1; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got, want := store.lastRetryableFail.VisibleAt, now.Add(wantDelay); !got.Equal(want) {
		t.Fatalf("VisibleAt = %s, want %s", got, want)
	}
}

func TestClaimedServiceHonorsSDKProviderFailureRetryAfter(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.May, 18, 16, 55, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	wantDelay := 55 * time.Second
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		retryableFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	httpErr := sdk.HTTPError{Provider: "test", StatusCode: 429, RetryAfter: wantDelay}
	source := &stubClaimedSource{err: sdk.ClassifyProviderFailure("test", httpErr, sdk.StatusPolicy{}, sdk.FailureRetryable)}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after retryable claim failure", err)
	}
	if got, want := store.retryableFailCalls, 1; got != want {
		t.Fatalf("retryable fail calls = %d, want %d", got, want)
	}
	if got, want := store.lastRetryableFail.VisibleAt, now.Add(wantDelay); !got.Equal(want) {
		t.Fatalf("VisibleAt = %s, want %s", got, want)
	}
}

func TestClaimedServiceTerminalFailsClassifiedCollectFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	now := time.Date(2026, time.May, 24, 17, 30, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
		terminalFail: func(context.Context, workflow.ClaimMutation) error {
			cancel()
			return nil
		},
	}
	source := &stubClaimedSource{err: terminalCollectFailure{err: errors.New("nvd request returned status 404")}}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after terminal collect failure is recorded", err)
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	if got := store.retryableFailCalls; got != 0 {
		t.Fatalf("retryable fail calls = %d, want 0", got)
	}
	if got := store.lastTerminalFail.FailureClass; got != "non_retryable" {
		t.Fatalf("FailureClass = %q, want non_retryable", got)
	}
}

func TestClaimedServiceRejectsGenericCommitter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 8, 0, 0, 0, time.UTC)
	claim := testWorkflowClaim("item-claim-1", now)
	service := testClaimedService(now, claim, scope.CollectorGit, &stubClaimStore{}, &stubClaimedSource{}, &stubCommitter{})

	err := service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if got, want := err.Error(), "claim-aware collector committer must implement ClaimedCommitter"; got != want {
		t.Fatalf("Run() error = %q, want %q", got, want)
	}
}

func TestClaimedServiceTerminalFailsIdentityMismatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 22, 30, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	claim := testWorkflowClaim(item.WorkItemID, now)
	collectedScope := testScope()
	collectedScope.ScopeID = "scope-other"
	store := &stubClaimStore{
		item:  item,
		claim: claim,
		found: true,
	}
	source := &stubClaimedSource{collected: FactsFromSlice(collectedScope, testGeneration(now), testFacts(now)), ok: true}
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, &stubClaimedCommitter{})

	err := service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want identity mismatch")
	}
	if got, want := store.terminalFailCalls, 1; got != want {
		t.Fatalf("terminal fail calls = %d, want %d", got, want)
	}
	if got := store.lastTerminalFail; got.FailureClass != "identity_mismatch" {
		t.Fatalf("FailureClass = %q, want identity_mismatch", got.FailureClass)
	}
}

func TestClaimedServiceErrorUsesConfiguredCollectorKind(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 22, 40, 0, 0, time.UTC)
	wantErr := errors.New("claim store unavailable")
	claim := testWorkflowClaim("item-claim-1", now)
	store := &stubClaimStore{claimErr: wantErr}
	service := testClaimedService(now, claim, scope.CollectorTerraformState, store, &stubClaimedSource{}, &stubClaimedCommitter{})

	err := service.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, wantErr)
	}
	if got := err.Error(); !strings.Contains(got, "terraform_state") || strings.Contains(got, "git work item") {
		t.Fatalf("Run() error = %q, want configured collector kind without git wording", got)
	}
}

type retryAfterCollectFailure struct {
	delay time.Duration
}

func (e retryAfterCollectFailure) Error() string {
	return "provider retry-after"
}

func (e retryAfterCollectFailure) RetryAfterDelay() time.Duration {
	return e.delay
}
