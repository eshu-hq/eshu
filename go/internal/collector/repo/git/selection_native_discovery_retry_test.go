// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// retryableListingFailure is what fetchGitHubListingPage returns once the
// in-call retry gives up on a persistent 5xx.
func retryableListingFailure() error {
	return rest.NewRetryableError(
		rest.OperationList, "server_error", 4, 0,
		errors.New("list GitHub org repositories: status 504 after 4 attempts"),
	)
}

// discoveryRetrySelector builds a githubOrg selector whose discovery result
// is scripted per call. The nil entry is a successful discovery. Jitter is
// the identity so the between-cycle schedule is exact.
func discoveryRetrySelector(t *testing.T, script []error) (*NativeRepositorySelector, *int) {
	t.Helper()
	calls := 0
	selector := &NativeRepositorySelector{
		Config: RepoSyncConfig{
			ReposDir:   t.TempDir(),
			SourceMode: "githubOrg",
			GithubOrg:  "acme",
			RepoLimit:  100,
			GitToken:   "token-1",
		},
		Now: func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) },
		DiscoverSelection: func(context.Context, RepoSyncConfig, string) (RepositorySelection, error) {
			index := calls
			calls++
			if index < len(script) && script[index] != nil {
				return RepositorySelection{}, script[index]
			}
			return RepositorySelection{ListingComplete: true}, nil
		},
		SyncGit: func(context.Context, RepoSyncConfig, []string) (GitSyncSelection, error) {
			return GitSyncSelection{NextReconcileStartOffset: 9}, nil
		},
		discoveryFailures: rest.Failures{Jitter: func(d time.Duration) time.Duration { return d }},
	}
	return selector, &calls
}

func discoveryRetryFailure(t *testing.T, selector *NativeRepositorySelector) *rest.RetryableError {
	t.Helper()
	_, err := selector.SelectRepositories(context.Background())
	return listRetryAsDiscovery(t, err)
}

// TestSelectorOwnsTheConsecutiveFailureCounterAndBackoff pins D3: the counter
// counts retryable discovery cycles, the between-cycle wait is 5 s doubling
// to a 60 s cap, and a larger provider hint wins (capped at one hour).
func TestSelectorOwnsTheConsecutiveFailureCounterAndBackoff(t *testing.T) {
	t.Parallel()

	script := make([]error, 8)
	for i := range script {
		script[i] = retryableListingFailure()
	}
	selector, _ := discoveryRetrySelector(t, script)
	want := []time.Duration{5, 10, 20, 40, 60, 60}
	for i, seconds := range want {
		typed := discoveryRetryFailure(t, selector)
		if typed.Consecutive != i+1 {
			t.Fatalf("cycle %d: Consecutive = %d, want %d", i+1, typed.Consecutive, i+1)
		}
		if typed.RetryAfter != seconds*time.Second {
			t.Fatalf("cycle %d: RetryAfter = %v, want %v", i+1, typed.RetryAfter, seconds*time.Second)
		}
	}

	hinted := rest.NewRetryableError(rest.OperationList, "rate_limited", 1, 10*time.Minute, errors.New("limited"))
	selector, _ = discoveryRetrySelector(t, []error{hinted})
	if got := discoveryRetryFailure(t, selector).RetryAfter; got != 10*time.Minute {
		t.Fatalf("RetryAfter = %v, want the 10m provider hint", got)
	}
	huge := rest.NewRetryableError(rest.OperationList, "rate_limited", 1, 5*time.Hour, errors.New("limited"))
	selector, _ = discoveryRetrySelector(t, []error{huge})
	if got := discoveryRetryFailure(t, selector).RetryAfter; got != time.Hour {
		t.Fatalf("RetryAfter = %v, want the 1h cap", got)
	}
}

// TestSelectorCounterResetsOnlyOnASuccessfulSelection pins the D3 reset rule.
func TestSelectorCounterResetsOnlyOnASuccessfulSelection(t *testing.T) {
	t.Parallel()

	fail := retryableListingFailure
	selector, _ := discoveryRetrySelector(t, []error{fail(), fail(), nil, fail()})
	for want := 1; want <= 2; want++ {
		if got := discoveryRetryFailure(t, selector).Consecutive; got != want {
			t.Fatalf("Consecutive = %d, want %d", got, want)
		}
	}
	if _, err := selector.SelectRepositories(context.Background()); err != nil {
		t.Fatalf("successful cycle error = %v", err)
	}
	if got := discoveryRetryFailure(t, selector).Consecutive; got != 1 {
		t.Fatalf("Consecutive after a success = %d, want 1", got)
	}
}

// emptyOrBatchSelector is a webhook-style selector that answers a scripted
// batch per call.
type scriptedBatchSelector struct {
	batches []SelectionBatch
	calls   int
}

func (s *scriptedBatchSelector) SelectRepositories(context.Context) (SelectionBatch, error) {
	batch := s.batches[s.calls]
	s.calls++
	return batch, nil
}

// TestSelectorCounterIgnoresAWebhookBatch pins that a webhook batch, which
// skips the native selector, neither resets nor advances the counter: a
// webhook-fed fleet must not hide a permanently broken listing.
func TestSelectorCounterIgnoresAWebhookBatch(t *testing.T) {
	t.Parallel()

	fail := retryableListingFailure
	native, _ := discoveryRetrySelector(t, []error{fail(), fail(), fail()})
	webhook := &scriptedBatchSelector{batches: []SelectionBatch{
		{}, {}, {Repositories: []SelectedRepository{{RepoPath: "/tmp/acme/a"}}}, {},
	}}
	priority := PriorityRepositorySelector{Selectors: []RepositorySelector{webhook, native}}

	want := []int{1, 2, -1, 3}
	for cycle, consecutive := range want {
		batch, err := priority.SelectRepositories(context.Background())
		if consecutive < 0 {
			if err != nil || len(batch.Repositories) != 1 {
				t.Fatalf("cycle %d: batch %+v err %v, want the webhook batch", cycle, batch, err)
			}
			continue
		}
		typed := listRetryAsDiscovery(t, err)
		if typed.Consecutive != consecutive {
			t.Fatalf("cycle %d: Consecutive = %d, want %d", cycle, typed.Consecutive, consecutive)
		}
	}
}

// TestSelectorTurnsTheCapIntoAFatalError pins D3: at
// sdk.MaxConsecutiveTransportFailures the selector returns a non-retryable
// error that names the class and the count and still unwraps to the cause, so
// a permanent fault is a crash loop an operator can alert on.
func TestSelectorTurnsTheCapIntoAFatalError(t *testing.T) {
	t.Parallel()

	script := make([]error, sdk.MaxConsecutiveTransportFailures)
	for i := range script {
		script[i] = retryableListingFailure()
	}
	selector, _ := discoveryRetrySelector(t, script)
	for cycle := 1; cycle < sdk.MaxConsecutiveTransportFailures; cycle++ {
		if got := discoveryRetryFailure(t, selector).Consecutive; got != cycle {
			t.Fatalf("cycle %d: Consecutive = %d", cycle, got)
		}
	}
	_, err := selector.SelectRepositories(context.Background())
	if err == nil {
		t.Fatal("cycle 20: error = nil, want the fatal cap error")
	}
	if collector.IsRetryable(err) {
		t.Fatalf("cycle 20: %v is retryable, want a fatal error", err)
	}
	var typed *rest.RetryableError
	if errors.As(err, &typed) {
		t.Fatalf("cycle 20: %v still unwraps to the retryable type", err)
	}
	for _, fragment := range []string{"server_error", fmt.Sprint(sdk.MaxConsecutiveTransportFailures)} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("cap error %q does not name %q", err, fragment)
		}
	}
}

// TestSelectorLeavesNonRetryableErrorsAlone pins that a final error passes
// through unchanged and does not move the counter.
func TestSelectorLeavesNonRetryableErrorsAlone(t *testing.T) {
	t.Parallel()

	final := errors.New("list GitHub org repositories: status 401")
	selector, _ := discoveryRetrySelector(t, []error{retryableListingFailure(), final, retryableListingFailure()})
	if got := discoveryRetryFailure(t, selector).Consecutive; got != 1 {
		t.Fatalf("Consecutive = %d, want 1", got)
	}
	if _, err := selector.SelectRepositories(context.Background()); !errors.Is(err, final) || collector.IsRetryable(err) {
		t.Fatalf("error = %v, want the final error unchanged and not retryable", err)
	}
	if got := discoveryRetryFailure(t, selector).Consecutive; got != 2 {
		t.Fatalf("Consecutive = %d, want 2: a final error must not reset the counter", got)
	}
}

type countingWatermarkReader struct{ calls int }

func (c *countingWatermarkReader) ReindexWatermark(context.Context) (time.Time, error) {
	c.calls++
	return time.Time{}, nil
}

// TestFailedDiscoveryLeavesSelectorStateAlone pins arbiter proof item 3: a
// failed discovery does not advance reconcileWalkOffset, reads no reindex
// watermark, and makes no selection observation.
func TestFailedDiscoveryLeavesSelectorStateAlone(t *testing.T) {
	t.Parallel()

	selector, _ := discoveryRetrySelector(t, []error{retryableListingFailure()})
	selector.SyncGit = nil // use the real sync closure, which reads the watermark
	watermark := &countingWatermarkReader{}
	observer := &recordingSelectionObserver{}
	selector.ReindexWatermark = watermark
	selector.SelectionObserver = observer
	selector.reconcileWalkOffset = 7

	typed := discoveryRetryFailure(t, selector)
	if typed.Consecutive != 1 {
		t.Fatalf("Consecutive = %d, want 1", typed.Consecutive)
	}
	if selector.reconcileWalkOffset != 7 {
		t.Fatalf("reconcileWalkOffset = %d, want 7 unchanged", selector.reconcileWalkOffset)
	}
	if watermark.calls != 0 {
		t.Fatalf("watermark reads = %d, want 0", watermark.calls)
	}
	if len(observer.requests) != 0 {
		t.Fatalf("selection observations = %d, want 0", len(observer.requests))
	}
}

// TestSelectorCountsAMintFailureOnTheSyncPathOnce pins the second token
// resolution of a cycle (selection_cli.go): the mint succeeds for discovery,
// fails for the sync, and the selector reports one typed failure.
func TestSelectorCountsAMintFailureOnTheSyncPathOnce(t *testing.T) {
	t.Parallel()

	server, calls := mintTestServer(t, []int{http.StatusOK, http.StatusBadGateway}, nil)
	rec := newListRetryRecorder()
	selector, discoveries := discoveryRetrySelector(t, nil)
	selector.Config = mintTestConfig(t, server, rec)
	selector.Config.ReposDir = t.TempDir()
	selector.Config.SourceMode = "githubOrg"
	selector.Config.GithubOrg = "acme"
	selector.Config.RepoLimit = 100
	selector.SyncGit = nil

	typed := discoveryRetryFailure(t, selector)
	if typed.Operation != "mint" || typed.Consecutive != 1 || typed.Class != "server_error" {
		t.Fatalf("typed = %+v, want mint server_error consecutive 1", typed)
	}
	if *discoveries != 1 {
		t.Fatalf("discoveries = %d, want 1", *discoveries)
	}
	if got := calls(); got != 1+4 {
		t.Fatalf("mint calls = %d, want 1 success + 4 failed attempts", got)
	}
}

// TestSelectorCountsAFirstMintFailure pins the first token resolution.
func TestSelectorCountsAFirstMintFailure(t *testing.T) {
	t.Parallel()

	server, _ := mintTestServer(t, []int{http.StatusServiceUnavailable}, nil)
	selector, discoveries := discoveryRetrySelector(t, nil)
	selector.Config = mintTestConfig(t, server, newListRetryRecorder())
	selector.Config.ReposDir = t.TempDir()
	selector.Config.SourceMode = "githubOrg"
	selector.Config.GithubOrg = "acme"
	selector.Config.RepoLimit = 100

	typed := discoveryRetryFailure(t, selector)
	if typed.Operation != "mint" || typed.Consecutive != 1 {
		t.Fatalf("typed = %+v, want mint consecutive 1", typed)
	}
	if *discoveries != 0 {
		t.Fatalf("discoveries = %d, want 0: a failed mint must stop before discovery", *discoveries)
	}
}
