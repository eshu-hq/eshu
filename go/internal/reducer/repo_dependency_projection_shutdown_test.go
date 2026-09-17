// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// #6747 shape A. The Ifa determinism matrix stops the pre-maintenance reducer
// with SIGTERM; when that cancellation lands after the repo-dependency
// partition lease claim, the old runner reported a 5m lease quarantine
// ("list pending repo dependency intents: context canceled") and never
// released the lease. The next reducer process carries a different
// process-unique lease owner, so its claim was refused silently for the whole
// TTL and the post-maintenance drain timed out with repo_dependency intents
// nonterminal (runs 35165747820 and 35200501470). Shutdown is not a partition
// fault: the lease must be released through a context that outlives the
// cancellation, and the refused claim must be visible to an operator.

// cancelingRepoDependencyIntentReader cancels the runner's parent context on
// the first pending-intent list and reports that cancellation, the same
// interruption point the pre-drain reducer logged.
type cancelingRepoDependencyIntentReader struct {
	inner  RepoDependencyProjectionIntentReader
	cancel context.CancelFunc
}

func (r cancelingRepoDependencyIntentReader) ListPendingDomainIntents(
	ctx context.Context, _ string, _ int,
) ([]SharedProjectionIntentRow, error) {
	r.cancel()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (r cancelingRepoDependencyIntentReader) ListAcceptanceUnitDomainIntents(
	ctx context.Context, acceptanceUnitID, domain string, limit int,
) ([]SharedProjectionIntentRow, error) {
	return r.inner.ListAcceptanceUnitDomainIntents(ctx, acceptanceUnitID, domain, limit)
}

func (r cancelingRepoDependencyIntentReader) MarkIntentsCompleted(
	ctx context.Context, intentIDs []string, completedAt time.Time,
) error {
	return r.inner.MarkIntentsCompleted(ctx, intentIDs, completedAt)
}

// shutdownRecordingRepoDependencyLeaseManager records the context state each
// release ran under, so a release attempted through the already-cancelled
// parent context (which Postgres would reject) is distinguishable from one
// that can actually reach the database.
type shutdownRecordingRepoDependencyLeaseManager struct {
	mu             sync.Mutex
	claimed        bool
	claims         int
	releaseCtxErrs []error
}

func (m *shutdownRecordingRepoDependencyLeaseManager) ClaimPartitionLease(
	context.Context, string, int, int, string, time.Duration,
) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claims++
	return m.claimed, nil
}

func (m *shutdownRecordingRepoDependencyLeaseManager) ReleasePartitionLease(
	ctx context.Context, _ string, _, _ int, _ string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseCtxErrs = append(m.releaseCtxErrs, ctx.Err())
	return nil
}

func (m *shutdownRecordingRepoDependencyLeaseManager) releases() []error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]error(nil), m.releaseCtxErrs...)
}

func (m *shutdownRecordingRepoDependencyLeaseManager) claimCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.claims
}

func TestRepoDependencyProjectionRunnerReleasesLeaseOnShutdownCancel(t *testing.T) {
	t.Parallel()

	runner := validRepoDependencyQuarantineRunner(t)
	leases := &shutdownRecordingRepoDependencyLeaseManager{claimed: true}
	runner.LeaseManager = leases
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.IntentReader = cancelingRepoDependencyIntentReader{inner: runner.IntentReader, cancel: cancel}

	_, err := runner.processOnce(ctx, time.Now().UTC())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("processOnce() error = %v, want the shutdown cancellation", err)
	}
	var quarantineErr *repoDependencyLeaseQuarantineError
	if errors.As(err, &quarantineErr) {
		t.Fatalf("processOnce() error = %v: a shutdown must not quarantine the partition lease", err)
	}
	releases := leases.releases()
	if len(releases) != 1 {
		t.Fatalf("lease releases after shutdown cancel = %d, want 1 (the lease must not outlive the process for its TTL)", len(releases))
	}
	if releases[0] != nil {
		t.Fatalf("release ran under a context with err %v; it must use a context that survives the shutdown cancellation", releases[0])
	}
}

func TestRepoDependencyProjectionRunnerRunSerialStopsCleanlyOnShutdown(t *testing.T) {
	t.Parallel()

	runner := validRepoDependencyQuarantineRunner(t)
	leases := &shutdownRecordingRepoDependencyLeaseManager{claimed: true}
	runner.LeaseManager = leases
	var logs bytes.Buffer
	runner.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.IntentReader = cancelingRepoDependencyIntentReader{inner: runner.IntentReader, cancel: cancel}

	if err := runner.runSerial(ctx); err != nil {
		t.Fatalf("runSerial() error = %v, want nil on shutdown", err)
	}
	if got := len(leases.releases()); got != 1 {
		t.Fatalf("lease releases across shutdown = %d, want 1", got)
	}
	if strings.Contains(logs.String(), "repo dependency projection cycle failed") {
		t.Fatalf("shutdown was reported as a cycle failure (and a quarantine):\n%s", logs.String())
	}
}

// A refused claim used to return an empty cycle with no signal at all. An
// operator watching a stalled repo_dependency drain needs the reason on the
// runner's own log, once per contention episode rather than once per poll.
func TestRepoDependencyProjectionRunnerLogsContendedPartitionLease(t *testing.T) {
	t.Parallel()

	runner := validRepoDependencyQuarantineRunner(t)
	leases := &shutdownRecordingRepoDependencyLeaseManager{claimed: false}
	runner.LeaseManager = leases
	var logs bytes.Buffer
	runner.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	runner.Config.PollInterval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := 0
	runner.Wait = func(ctx context.Context, _ time.Duration) error {
		waits++
		if waits >= 3 {
			cancel()
		}
		return ctx.Err()
	}

	if err := runner.runSerial(ctx); err != nil {
		t.Fatalf("runSerial() error = %v, want nil", err)
	}
	if got := leases.claimCount(); got < 3 {
		t.Fatalf("claims = %d, want at least 3 refused claims before the test stopped the runner", got)
	}
	const want = "repo dependency partition lease held by another owner"
	if got := strings.Count(logs.String(), want); got != 1 {
		t.Fatalf("contended-lease log lines = %d, want exactly 1 per contention episode:\n%s", got, logs.String())
	}
	for _, key := range []string{`"partition_id"`, `"partition_count"`, `"lease_owner"`} {
		if !strings.Contains(logs.String(), key) {
			t.Fatalf("contended-lease log must carry %s:\n%s", key, logs.String())
		}
	}
}
