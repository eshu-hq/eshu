// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// selectorSource adapts a RepositorySelector to collector.Source the way
// GitSource does for a discovery failure: the selector error is returned from
// Next unchanged, a success is an idle poll.
type selectorSource struct{ selector RepositorySelector }

func (s selectorSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	if _, err := s.selector.SelectRepositories(ctx); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	return collector.CollectedGeneration{}, false, nil
}

type noopCommitter struct{}

func (noopCommitter) CommitScopeGeneration(context.Context, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope) error {
	return nil
}

// failingMarkStore is a trigger store whose MarkTriggersFailed write fails, as
// a Postgres outage would.
type failingMarkStore struct{ *stubWebhookTriggerStore }

func (failingMarkStore) MarkTriggersFailed(context.Context, []webhook.StoredTrigger, time.Time, string, string) error {
	return errors.New("postgres unavailable")
}

func webhookTestTriggers() []webhook.StoredTrigger {
	return []webhook.StoredTrigger{{
		TriggerID: "trigger-1",
		Trigger: webhook.Trigger{
			Provider:           webhook.ProviderGitHub,
			Decision:           webhook.DecisionAccepted,
			RepositoryFullName: "eshu-hq/eshu",
		},
	}}
}

func identityJitter(d time.Duration) time.Duration { return d }

// waitLog is a Service.Wait that records each wait and stops a runaway loop:
// after limit waits it cancels the run, which a fixed tree must never reach.
type waitLog struct {
	mu     sync.Mutex
	waits  []time.Duration
	limit  int
	cancel context.CancelFunc
}

func (w *waitLog) wait(ctx context.Context, d time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.waits = append(w.waits, d)
	if len(w.waits) >= w.limit {
		w.cancel()
	}
	return ctx.Err()
}

// TestWebhookSyncMintFailureEndsFatalWithinTheCap is the F1 repro (review r1)
// and ruling 3 test 1. With a real mint answering 502 through rest.Do and the
// real sync path, the webhook selector must not loop forever at the poll
// interval: the shared counter caps it at 20 cycles, the skip waits follow the
// jittered 5 s doubling backoff, the mint sees 4 attempts per cycle, and the
// claimed triggers are left alone for lease reaping. Both wirings.
func TestWebhookSyncMintFailureEndsFatalWithinTheCap(t *testing.T) {
	t.Parallel()

	for _, wiring := range []string{"priority webhook+native", "webhook only"} {
		t.Run(wiring, func(t *testing.T) {
			t.Parallel()
			server, mintCalls := mintTestServer(t, []int{502}, nil)
			rec := newListRetryRecorder()
			config := mintTestConfig(t, server, rec)
			config.SourceMode = "githubOrg"
			config.GithubOrg = "acme"
			config.ReposDir = t.TempDir()
			failures := &rest.Failures{Jitter: identityJitter}
			store := &stubWebhookTriggerStore{claimed: webhookTestTriggers()}
			webhookSelector := WebhookTriggerRepositorySelector{
				Config:            config,
				Store:             store,
				Owner:             "collector-git",
				Now:               func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) },
				DiscoveryFailures: failures,
			}
			var selector RepositorySelector = webhookSelector
			if wiring == "priority webhook+native" {
				native, _ := discoveryRetrySelector(t, nil)
				native.DiscoveryFailures = failures
				selector = PriorityRepositorySelector{Selectors: []RepositorySelector{webhookSelector, native}}
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			waits := &waitLog{limit: 100, cancel: cancel}
			service := collector.Service{
				Source:       selectorSource{selector: selector},
				Committer:    noopCommitter{},
				PollInterval: time.Second,
				Wait:         waits.wait,
			}
			err := service.Run(ctx)
			if err == nil {
				t.Fatalf("Run() = nil after %d skip waits, want a fatal error within the cap", len(waits.waits))
			}
			for _, fragment := range []string{"collect scope generation", "20 consecutive", "mint"} {
				if !strings.Contains(err.Error(), fragment) {
					t.Fatalf("Run() error = %q, want it to name %q", err, fragment)
				}
			}
			if got := len(waits.waits); got != 19 {
				t.Fatalf("skip waits = %d, want 19 before the fatal 20th cycle", got)
			}
			wantWait := 5 * time.Second
			for i, got := range waits.waits {
				if got != wantWait {
					t.Fatalf("skip wait %d = %v, want the 5 s doubling backoff %v", i+1, got, wantWait)
				}
				wantWait = min(wantWait*2, time.Minute)
			}
			if got, want := mintCalls(), 4*20; got != want {
				t.Fatalf("mint calls = %d, want %d (4 attempts for each of 20 cycles)", got, want)
			}
			if len(store.failedCalls) != 0 {
				t.Fatalf("MarkTriggersFailed calls = %d, want 0: a retryable failure must leave the claims for lease reaping", len(store.failedCalls))
			}
		})
	}
}

// TestSharedFailuresCountListAndMintIndependently pins ruling 3 tests 2 and 3:
// in the priority wiring a webhook mint failure and a native listing failure
// accrue separately, a good non-empty webhook cycle resets only the mint, a
// native nil return resets both, and a retryable failure never marks triggers.
func TestSharedFailuresCountListAndMintIndependently(t *testing.T) {
	t.Parallel()

	failures := &rest.Failures{Jitter: identityJitter}
	store := &stubWebhookTriggerStore{}
	var syncErr error
	webhookSelector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
		Store:  store,
		Owner:  "collector-git",
		Now:    func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) },
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			if syncErr != nil {
				return GitSyncSelection{}, syncErr
			}
			return GitSyncSelection{SelectedRepoPaths: []string{t.TempDir()}}, nil
		},
		DiscoveryFailures: failures,
	}
	native, _ := discoveryRetrySelector(t, []error{retryableListingFailure(), nil})
	native.DiscoveryFailures = failures
	priority := PriorityRepositorySelector{Selectors: []RepositorySelector{webhookSelector, native}}
	ctx := context.Background()

	// Cycle 1: no triggers, the native listing fails.
	_, err := priority.SelectRepositories(ctx)
	var typed *rest.RetryableError
	if !errors.As(err, &typed) || typed.Operation != rest.OperationList || typed.Consecutive != 1 {
		t.Fatalf("cycle 1 error = %v, want a list failure with Consecutive 1", err)
	}

	// Cycle 2: a trigger whose sync mint fails retryably.
	store.claimed = webhookTestTriggers()
	syncErr = rest.NewRetryableError(rest.OperationMint, "server_error", 4, 0, errors.New("status 502"))
	_, err = priority.SelectRepositories(ctx)
	if !errors.As(err, &typed) || typed.Operation != rest.OperationMint || typed.Consecutive != 1 {
		t.Fatalf("cycle 2 error = %v, want a mint failure with Consecutive 1", err)
	}
	if !errors.As(err, new(collector.CycleSkipper)) {
		t.Fatalf("cycle 2 error %v is not a CycleSkipper", err)
	}
	if len(store.failedCalls) != 0 {
		t.Fatalf("MarkTriggersFailed calls = %d, want 0 for a retryable failure", len(store.failedCalls))
	}
	if failures.Count(rest.OperationList) != 1 || failures.Count(rest.OperationMint) != 1 {
		t.Fatalf("counts = list %d mint %d, want 1 and 1", failures.Count(rest.OperationList), failures.Count(rest.OperationMint))
	}

	// Cycle 3: the sync recovers; only the mint count resets.
	syncErr = nil
	batch, err := priority.SelectRepositories(ctx)
	if err != nil || len(batch.Repositories) != 1 {
		t.Fatalf("cycle 3 = %d repositories, err %v, want a non-empty webhook batch", len(batch.Repositories), err)
	}
	if failures.Count(rest.OperationList) != 1 || failures.Count(rest.OperationMint) != 0 {
		t.Fatalf("counts after a webhook success = list %d mint %d, want 1 and 0", failures.Count(rest.OperationList), failures.Count(rest.OperationMint))
	}

	// Cycle 4: no triggers, the native listing succeeds; both reset.
	store.claimed = nil
	if _, err := priority.SelectRepositories(ctx); err != nil {
		t.Fatalf("cycle 4 error = %v, want nil", err)
	}
	if failures.Count(rest.OperationList) != 0 || failures.Count(rest.OperationMint) != 0 {
		t.Fatalf("counts after a native success = list %d mint %d, want 0 and 0", failures.Count(rest.OperationList), failures.Count(rest.OperationMint))
	}
}

// TestWebhookMarkFailureStaysFatal pins ruling 3 test 4: a sync failure that
// arrives together with a failing MarkTriggersFailed write is infrastructure
// breakage, never a skippable cycle, through the real loop.
func TestWebhookMarkFailureStaysFatal(t *testing.T) {
	t.Parallel()

	selector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
		Store:  failingMarkStore{&stubWebhookTriggerStore{claimed: webhookTestTriggers()}},
		Owner:  "collector-git",
		Now:    func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) },
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			return GitSyncSelection{}, errors.New("git unavailable")
		},
		DiscoveryFailures: &rest.Failures{Jitter: identityJitter},
	}
	_, err := selector.SelectRepositories(context.Background())
	if err == nil || collector.IsRetryable(err) || errors.As(err, new(collector.CycleSkipper)) {
		t.Fatalf("SelectRepositories() error = %v, want a plain fatal error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := &waitLog{limit: 5, cancel: cancel}
	service := collector.Service{
		Source:       selectorSource{selector: selector},
		Committer:    noopCommitter{},
		PollInterval: time.Second,
		Wait:         waits.wait,
	}
	runErr := service.Run(ctx)
	if runErr == nil || !strings.HasPrefix(runErr.Error(), "collect scope generation: ") || len(waits.waits) != 0 {
		t.Fatalf("Run() = %v after %d waits, want collect scope generation: ... with no wait", runErr, len(waits.waits))
	}
}

// TestWebhookSelectorWithoutACounterFailsClosed pins ruling 3 test 5: an
// unwired selector turns a retryable GitHub failure into a plain error that
// wraps the cause, so the loop can never skip without a cap. Like base, it
// then marks the claimed triggers failed.
func TestWebhookSelectorWithoutACounterFailsClosed(t *testing.T) {
	t.Parallel()

	cause := errors.New("status 502")
	store := &stubWebhookTriggerStore{claimed: webhookTestTriggers()}
	selector := WebhookTriggerRepositorySelector{
		Config: RepoSyncConfig{ReposDir: t.TempDir(), SourceMode: "explicit", CloneDepth: 1},
		Store:  store,
		Owner:  "collector-git",
		Now:    func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) },
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			return GitSyncSelection{}, fmt.Errorf("mint: %w", rest.NewRetryableError(rest.OperationMint, "server_error", 4, 0, cause))
		},
	}
	_, err := selector.SelectRepositories(context.Background())
	var typed *rest.RetryableError
	if err == nil || collector.IsRetryable(err) || errors.As(err, new(collector.CycleSkipper)) || errors.As(err, &typed) {
		t.Fatalf("SelectRepositories() error = %v, want a plain error without the skip", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("SelectRepositories() error = %v, want the cause kept", err)
	}
	if got := store.failedCall("sync_git_failed"); len(got) != 1 {
		t.Fatalf("sync_git_failed marks = %v, want the claimed trigger marked as on base", got)
	}
}
