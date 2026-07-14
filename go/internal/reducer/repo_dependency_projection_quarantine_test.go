// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestRepoDependencyProjectionRunnerConfigSafetyDefaults(t *testing.T) {
	t.Parallel()

	cfg := RepoDependencyProjectionRunnerConfig{}
	if got, want := cfg.leaseTTL(), 5*time.Minute; got != want {
		t.Fatalf("leaseTTL() = %v, want %v", got, want)
	}
	if got, want := cfg.cycleTimeout(), 45*time.Second; got != want {
		t.Fatalf("cycleTimeout() = %v, want %v", got, want)
	}
	if got, want := cfg.graphQuiescenceBudget(), 2*time.Minute; got != want {
		t.Fatalf("graphQuiescenceBudget() = %v, want %v", got, want)
	}
}

func TestRepoDependencyProjectionRunnerRejectsUnsafeLeaseBudget(t *testing.T) {
	t.Parallel()

	runner := validRepoDependencyQuarantineRunner(t)
	runner.Config = RepoDependencyProjectionRunnerConfig{
		LeaseTTL:              time.Minute,
		CycleTimeout:          45 * time.Second,
		GraphQuiescenceBudget: 30 * time.Second,
	}
	if err := runner.validate(); err == nil || !strings.Contains(err.Error(), "safety budget") {
		t.Fatalf("validate() error = %v, want lease safety budget failure", err)
	}
}

func TestRepoDependencyProjectionRunnerQuarantinesLeaseAfterGraphError(t *testing.T) {
	t.Parallel()

	runner := validRepoDependencyQuarantineRunner(t)
	leases := &recordingRepoDependencyLeaseManager{claimed: true}
	runner.LeaseManager = leases
	runner.EdgeWriter = &flakyCodeCallProjectionEdgeWriter{
		err:           errors.New("ambiguous graph write"),
		writeFailures: 1,
	}
	runner.Config = RepoDependencyProjectionRunnerConfig{LeaseTTL: 5 * time.Minute}

	_, err := runner.processOnce(context.Background(), time.Now().UTC())
	var quarantineErr *repoDependencyLeaseQuarantineError
	if !errors.As(err, &quarantineErr) {
		t.Fatalf("processOnce() error = %v, want lease quarantine error", err)
	}
	if got, want := quarantineErr.delay, 5*time.Minute; got != want {
		t.Fatalf("quarantine delay = %v, want %v", got, want)
	}
	if got := leases.releaseCount(); got != 0 {
		t.Fatalf("lease releases after ambiguous graph error = %d, want 0", got)
	}
}

func TestRepoDependencyProjectionRunnerWholeCycleDeadlineQuarantines(t *testing.T) {
	t.Parallel()

	runner := validRepoDependencyQuarantineRunner(t)
	leases := &recordingRepoDependencyLeaseManager{claimed: true}
	runner.LeaseManager = leases
	runner.EdgeWriter = &blockingCodeCallProjectionEdgeWriter{release: make(chan struct{})}
	runner.Config = RepoDependencyProjectionRunnerConfig{
		LeaseTTL:              5 * time.Minute,
		CycleTimeout:          5 * time.Millisecond,
		GraphQuiescenceBudget: time.Millisecond,
	}

	started := time.Now()
	_, err := runner.processOnce(context.Background(), time.Now().UTC())
	if time.Since(started) > time.Second {
		t.Fatal("whole-cycle deadline did not cancel the blocked graph write promptly")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("processOnce() error = %v, want deadline exceeded", err)
	}
	if got := leases.releaseCount(); got != 0 {
		t.Fatalf("lease releases after cycle timeout = %d, want 0", got)
	}
}

func TestRepoDependencyProjectionRunnerWholeCycleDeadlineIncludesSelection(t *testing.T) {
	t.Parallel()

	runner := validRepoDependencyQuarantineRunner(t)
	leases := &recordingRepoDependencyLeaseManager{claimed: true}
	runner.IntentReader = blockingRepoDependencyIntentReader{}
	runner.LeaseManager = leases
	runner.Config = RepoDependencyProjectionRunnerConfig{
		LeaseTTL:              5 * time.Minute,
		CycleTimeout:          5 * time.Millisecond,
		GraphQuiescenceBudget: time.Millisecond,
	}

	started := time.Now()
	_, err := runner.processOnce(context.Background(), time.Now().UTC())
	if time.Since(started) > time.Second {
		t.Fatal("whole-cycle deadline did not cancel acceptance-unit selection promptly")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("processOnce() error = %v, want deadline exceeded", err)
	}
	if got := leases.releaseCount(); got != 0 {
		t.Fatalf("lease releases after selection timeout = %d, want 0", got)
	}
}

func TestRepoDependencyProjectionRunnerReleasesLeaseAfterConfirmedCommit(t *testing.T) {
	t.Parallel()

	runner := validRepoDependencyQuarantineRunner(t)
	leases := &recordingRepoDependencyLeaseManager{claimed: true}
	runner.LeaseManager = leases
	if _, err := runner.processOnce(context.Background(), time.Now().UTC()); err != nil {
		t.Fatalf("processOnce() error = %v, want nil", err)
	}
	if got := leases.releaseCount(); got != 1 {
		t.Fatalf("lease releases after confirmed commit = %d, want 1", got)
	}
}

func TestRepoDependencyProjectionRunnerQuarantinesHeartbeatLossBeforeSuccess(t *testing.T) {
	t.Parallel()

	runner := validRepoDependencyQuarantineRunner(t)
	leases := &recordingRepoDependencyLeaseManager{claimed: true, rejectHeartbeat: true}
	runner.LeaseManager = leases
	runner.EdgeWriter = delayedSuccessRepoDependencyWriter{delay: 15 * time.Millisecond}
	runner.Config = RepoDependencyProjectionRunnerConfig{
		LeaseTTL:              9 * time.Millisecond,
		CycleTimeout:          100 * time.Millisecond,
		GraphQuiescenceBudget: time.Millisecond,
	}

	_, err := runner.processOnce(context.Background(), time.Now().UTC())
	var quarantineErr *repoDependencyLeaseQuarantineError
	if !errors.As(err, &quarantineErr) {
		t.Fatalf("processOnce() error = %v, want lease quarantine error", err)
	}
	if !strings.Contains(err.Error(), "heartbeat") {
		t.Fatalf("processOnce() error = %v, want heartbeat attribution", err)
	}
	if got := leases.releaseCount(); got != 0 {
		t.Fatalf("lease releases after heartbeat loss = %d, want 0", got)
	}
}

func TestRepoDependencyProjectionRunnerOrderlyHeartbeatStopNeverQuarantines(t *testing.T) {
	t.Parallel()

	runner := validRepoDependencyQuarantineRunner(t)
	leases := &recordingRepoDependencyLeaseManager{claimed: true}
	runner.LeaseManager = leases
	runner.EdgeWriter = delayedSuccessRepoDependencyWriter{delay: 2 * time.Millisecond}
	runner.Config = RepoDependencyProjectionRunnerConfig{
		LeaseTTL:              3 * time.Millisecond,
		CycleTimeout:          100 * time.Millisecond,
		GraphQuiescenceBudget: time.Millisecond,
	}

	for attempt := 0; attempt < 100; attempt++ {
		if _, err := runner.processOnce(context.Background(), time.Now().UTC()); err != nil {
			t.Fatalf("attempt %d processOnce() error = %v, want orderly success", attempt, err)
		}
	}
	if got, want := leases.releaseCount(), 100; got != want {
		t.Fatalf("lease releases = %d, want %d", got, want)
	}
}

func TestRepoDependencyProjectionRunnerWaitsOutQuarantineBeforeSameOwnerReclaim(t *testing.T) {
	leaseTTL := 31 * time.Second
	runner := validRepoDependencyQuarantineRunner(t)
	runner.EdgeWriter = &flakyCodeCallProjectionEdgeWriter{
		err:           errors.New("ambiguous graph write"),
		writeFailures: 1,
	}
	runner.Config = RepoDependencyProjectionRunnerConfig{
		LeaseTTL:              leaseTTL,
		CycleTimeout:          10 * time.Millisecond,
		GraphQuiescenceBudget: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.Wait = func(_ context.Context, delay time.Duration) error {
		if delay != leaseTTL {
			t.Fatalf("retry delay = %v, want quarantine lease TTL %v", delay, leaseTTL)
		}
		cancel()
		return context.Canceled
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil after cancellation", err)
	}
}

func TestRepoDependencyProjectionRunnerLogsLeaseQuarantine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	bootstrap, err := telemetry.NewBootstrap("test-reducer")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	runner := RepoDependencyProjectionRunner{
		Logger: telemetry.NewLoggerWithWriter(bootstrap, "reducer", "reducer", &buf),
	}
	runner.recordRepoDependencyCycleFailure(
		context.Background(),
		&repoDependencyLeaseQuarantineError{delay: 5 * time.Minute, cause: errors.New("ambiguous commit")},
		1.25,
	)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got, want := entry["lease_quarantined"], true; got != want {
		t.Fatalf("lease_quarantined = %v, want %v", got, want)
	}
	if got, want := entry["quarantine_duration_seconds"], 300.0; got != want {
		t.Fatalf("quarantine_duration_seconds = %v, want %v", got, want)
	}
}

func validRepoDependencyQuarantineRunner(t *testing.T) RepoDependencyProjectionRunner {
	t.Helper()
	now := time.Now().UTC()
	repoID := "repository:quarantine-proof"
	row := repoDependencyIntentRow(
		"intent-quarantine", "scope-quarantine", repoID, repoID, "run-quarantine", "gen-quarantine", now,
		map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    "repository:quarantine-target",
			"relationship_type": "DEPENDS_ON",
			"evidence_source":   crossRepoEvidenceSource,
		},
	)
	store := &fakeRepoDependencyIntentStore{
		pendingByDomain:         []SharedProjectionIntentRow{row},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: {row}},
		leaseGranted:            true,
	}
	return RepoDependencyProjectionRunner{
		IntentReader:       store,
		LeaseManager:       store,
		AcceptanceUnitGate: store,
		EdgeWriter:         &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:        acceptedGenerationFixed("gen-quarantine", true),
		Config: RepoDependencyProjectionRunnerConfig{
			LeaseTTL:              5 * time.Minute,
			CycleTimeout:          45 * time.Second,
			GraphQuiescenceBudget: 2 * time.Minute,
		},
	}
}

type recordingRepoDependencyLeaseManager struct {
	mu              sync.Mutex
	claimed         bool
	rejectHeartbeat bool
	claims          int
	releases        int
}

func (m *recordingRepoDependencyLeaseManager) ClaimPartitionLease(
	context.Context, string, int, int, string, time.Duration,
) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claims++
	if m.rejectHeartbeat && m.claims > 1 {
		return false, nil
	}
	return m.claimed, nil
}

func (m *recordingRepoDependencyLeaseManager) ReleasePartitionLease(
	context.Context, string, int, int, string,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releases++
	return nil
}

func (m *recordingRepoDependencyLeaseManager) releaseCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.releases
}

type delayedSuccessRepoDependencyWriter struct {
	delay time.Duration
}

func (w delayedSuccessRepoDependencyWriter) RetractEdges(
	context.Context, string, []SharedProjectionIntentRow, string,
) error {
	time.Sleep(w.delay)
	return nil
}

func (w delayedSuccessRepoDependencyWriter) WriteEdges(
	context.Context, string, []SharedProjectionIntentRow, string,
) error {
	time.Sleep(w.delay)
	return nil
}

type blockingRepoDependencyIntentReader struct{}

func (blockingRepoDependencyIntentReader) ListPendingDomainIntents(
	ctx context.Context, _ string, _ int,
) ([]SharedProjectionIntentRow, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingRepoDependencyIntentReader) ListAcceptanceUnitDomainIntents(
	context.Context, string, string, int,
) ([]SharedProjectionIntentRow, error) {
	return nil, errors.New("unexpected acceptance-unit load")
}

func (blockingRepoDependencyIntentReader) MarkIntentsCompleted(context.Context, []string, time.Time) error {
	return errors.New("unexpected intent completion")
}
