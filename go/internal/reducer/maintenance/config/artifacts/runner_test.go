// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/testutil"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fakeLister answers ListDirtyRepos from a script, one entry per call, and
// the skip check from a set.
type fakeLister struct {
	mu      sync.Mutex
	lists   [][]DirtyRepo
	errs    []error
	calls   []int
	skipped map[string]bool
	skipErr error
}

func (f *fakeLister) ListDirtyRepos(_ context.Context, limit int) ([]DirtyRepo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := len(f.calls)
	f.calls = append(f.calls, limit)
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	if i < len(f.lists) {
		return f.lists[i], nil
	}
	return nil, nil
}

func (f *fakeLister) SkippedRepos(_ context.Context, repoIDs []string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.skipErr != nil {
		return nil, f.skipErr
	}
	var out []string
	for _, repoID := range repoIDs {
		if f.skipped[repoID] {
			out = append(out, repoID)
		}
	}
	return out, nil
}

// fakeClaimer answers ClaimPage from a script of pages.
type fakeClaimer struct {
	mu    sync.Mutex
	pages [][]string
	err   error
	calls []int
}

func (f *fakeClaimer) ClaimPage(_ context.Context, budget int) ([]string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, budget)
	if f.err != nil {
		return nil, "", f.err
	}
	i := len(f.calls) - 1
	if i < len(f.pages) {
		page := f.pages[i]
		next := ""
		if len(page) == budget {
			next = page[len(page)-1]
		}
		return page, next, nil
	}
	return nil, "", nil
}

type deriveCall struct {
	repoID  string
	trigger string
}

// fakeDeriver answers Derive from outcomes keyed by repo id, recording calls.
type fakeDeriver struct {
	mu       sync.Mutex
	outcomes map[string]string
	err      error
	calls    []deriveCall
	onCall   func()
}

func (f *fakeDeriver) Derive(_ context.Context, repoID string, trigger string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, deriveCall{repoID: repoID, trigger: trigger})
	if f.onCall != nil {
		f.onCall()
	}
	if f.err != nil {
		return "", f.err
	}
	if outcome, ok := f.outcomes[repoID]; ok {
		return outcome, nil
	}
	return "ok", nil
}

// fakeMarker holds the backfill marker state in memory.
type fakeMarker struct {
	mu       sync.Mutex
	complete bool
	recorded int
	err      error
}

func (f *fakeMarker) BackfillRecorded(_ context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.complete, f.err
}

func (f *fakeMarker) RecordBackfill(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.complete = true
	f.recorded++
	return nil
}

func newWalkerTestInstruments(t *testing.T) (*telemetry.Instruments, *metric.ManualReader) {
	t.Helper()
	reader := metric.NewManualReader()
	inst, err := telemetry.NewInstruments(
		metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	return inst, reader
}

func collectMetrics(t *testing.T, reader *metric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

// TestRunnerDerivesDirtyAsRepairAndWalkAsBackfill pins the cycle order and the
// trigger labels: the dirty join's repositories derive with trigger repair, a
// claimed walk page derives with trigger backfill, a repository on both lists
// derives once as repair, and a skipped repository is counted and never
// derived. Per-derive spans, derives_total, the duration histogram and the
// stale gauge are emitted.
func TestRunnerDerivesDirtyAsRepairAndWalkAsBackfill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inst, reader := newWalkerTestInstruments(t)
	spans := tracetest.NewSpanRecorder()
	lister := &fakeLister{lists: [][]DirtyRepo{
		{
			{RepoID: "repo-repair", Reason: "stale_version"},
			{RepoID: "repo-skipped", Reason: "failed", Skipped: true},
		},
		{},
	}}
	claimer := &fakeClaimer{pages: [][]string{{"repo-repair", "repo-walk"}}}
	deriver := &fakeDeriver{}
	marker := &fakeMarker{}
	runner := &Runner{
		Lister:      lister,
		Claimer:     claimer,
		Deriver:     deriver,
		Marker:      marker,
		Config:      Config{Workers: 2, PageBudget: 10, SkipAfterFailures: 5, PollInterval: time.Minute},
		Tracer:      sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test"),
		Instruments: inst,
	}

	batch, err := runner.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, batch.Derived)
	require.Equal(t, 1, batch.Skipped)

	got := map[string]string{}
	for _, call := range deriver.calls {
		if prev, ok := got[call.repoID]; ok {
			t.Fatalf("repository %s derived twice (triggers %s and %s)", call.repoID, prev, call.trigger)
		}
		got[call.repoID] = call.trigger
	}
	require.Equal(t, map[string]string{
		"repo-repair": "repair",
		"repo-walk":   "backfill",
	}, got)

	rm := collectMetrics(t, reader)
	require.Equal(t, int64(1), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_derives_total",
		map[string]string{"trigger": "repair", "outcome": "ok"}))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_derives_total",
		map[string]string{"trigger": "backfill", "outcome": "ok"}))
	// Both derives wrote rows (the walk page held a repository the dirty
	// list did not name, so it derived as backfill and wrote), hence two
	// repaired points: reconcile counts what happened, triggers count why.
	require.Equal(t, int64(2), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_reconcile_total",
		map[string]string{"outcome": "repaired"}))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_reconcile_total",
		map[string]string{"outcome": "skipped"}))
	require.Equal(t, int64(1), testutil.GaugeValue(t, rm,
		"eshu_dp_config_artifact_stale_repos",
		map[string]string{"reason": "stale_version"}))
	require.Equal(t, int64(1), testutil.GaugeValue(t, rm,
		"eshu_dp_config_artifact_stale_repos",
		map[string]string{"reason": "failed"}))
	require.Len(t, spans.Ended(), 2)
	for _, span := range spans.Ended() {
		require.Equal(t, "config_artifacts.derive", span.Name())
	}
}

// TestRunnerWalkSkipsFailedRepositories pins the walk skip filter (protocol
// section 4): a repository over the failure limit is never derived, whether
// the dirty list names it skipped or only the walk page carries it. The
// dirty-skipped repository is counted once, the walk-only skipped repository
// is counted once, and a failing skip check fails the cycle.
func TestRunnerWalkSkipsFailedRepositories(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inst, reader := newWalkerTestInstruments(t)
	lister := &fakeLister{
		lists: [][]DirtyRepo{
			{
				{RepoID: "repo-repair", Reason: "stale_version"},
				{RepoID: "repo-dirty-skip", Reason: "failed", Skipped: true},
			},
			{},
		},
		skipped: map[string]bool{
			"repo-dirty-skip": true,
			"repo-walk-skip":  true,
		},
	}
	claimer := &fakeClaimer{pages: [][]string{
		{"repo-dirty-skip", "repo-walk-skip", "repo-walk"},
		{"repo-x"},
	}}
	deriver := &fakeDeriver{}
	marker := &fakeMarker{}
	runner := &Runner{
		Lister:      lister,
		Claimer:     claimer,
		Deriver:     deriver,
		Marker:      marker,
		Config:      Config{Workers: 2, PageBudget: 10, SkipAfterFailures: 5, PollInterval: time.Minute},
		Instruments: inst,
	}

	batch, err := runner.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, batch.Derived)
	require.Equal(t, 2, batch.Skipped)

	got := map[string]string{}
	for _, call := range deriver.calls {
		if prev, ok := got[call.repoID]; ok {
			t.Fatalf("repository %s derived twice (triggers %s and %s)", call.repoID, prev, call.trigger)
		}
		got[call.repoID] = call.trigger
	}
	require.Equal(t, map[string]string{
		"repo-repair": "repair",
		"repo-walk":   "backfill",
	}, got)

	rm := collectMetrics(t, reader)
	require.Equal(t, int64(2), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_reconcile_total",
		map[string]string{"outcome": "skipped"}))

	lister.skipErr = errors.New("connection refused")
	if _, err := runner.RunOnce(ctx); err == nil {
		t.Fatal("RunOnce with a failing skip check = nil, want the check error")
	}
}

// TestRunnerMapsOutcomesToReconcileLabels pins the derive-to-reconcile
// mapping: ok repairs, a repair superseded by a racing Write stays
// superseded, a backfill walk that finds a repository already current reports
// match, failures report error, and a transport error reports error too.
func TestRunnerMapsOutcomesToReconcileLabels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inst, reader := newWalkerTestInstruments(t)
	lister := &fakeLister{lists: [][]DirtyRepo{
		{
			{RepoID: "repo-ok", Reason: "stale_version"},
			{RepoID: "repo-raced", Reason: "stale_version"},
			{RepoID: "repo-broken", Reason: "failed"},
		},
		{},
	}}
	claimer := &fakeClaimer{pages: [][]string{{"repo-current", "repo-transport-broken"}}}
	deriverWithTransport := &selectiveDeriver{
		outcomes: map[string]string{
			"repo-raced":   "superseded",
			"repo-broken":  "timeout",
			"repo-current": "superseded",
		},
		transportFor: map[string]bool{
			"repo-transport-broken": true,
		},
	}
	marker := &fakeMarker{}
	runner := &Runner{
		Lister:      lister,
		Claimer:     claimer,
		Deriver:     deriverWithTransport,
		Marker:      marker,
		Config:      Config{Workers: 4, PageBudget: 10, SkipAfterFailures: 5, PollInterval: time.Minute},
		Instruments: inst,
	}

	batch, err := runner.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 5, batch.Derived)
	require.Equal(t, 1, batch.Repaired)
	require.Equal(t, 1, batch.Matched)
	require.Equal(t, 1, batch.Superseded)
	require.Equal(t, 2, batch.Failed)

	rm := collectMetrics(t, reader)
	require.Equal(t, int64(1), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_reconcile_total",
		map[string]string{"outcome": "repaired"}))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_reconcile_total",
		map[string]string{"outcome": "match"}))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_reconcile_total",
		map[string]string{"outcome": "superseded"}))
	require.Equal(t, int64(2), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_reconcile_total",
		map[string]string{"outcome": "error"}))
}

// selectiveDeriver fails the transport for named repositories and answers
// outcomes for the rest.
type selectiveDeriver struct {
	mu           sync.Mutex
	outcomes     map[string]string
	transportFor map[string]bool
	calls        []deriveCall
}

func (f *selectiveDeriver) Derive(_ context.Context, repoID string, trigger string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, deriveCall{repoID: repoID, trigger: trigger})
	if f.transportFor[repoID] {
		return "", errors.New("pool closed")
	}
	if outcome, ok := f.outcomes[repoID]; ok {
		return outcome, nil
	}
	return "ok", nil
}

// TestRunnerRecordsTheMarkerOnlyOnACleanWrap pins the backfill completion:
// the marker records when the walk reaches the end with nothing dirty left,
// and never records while a failed repository is still dirty or while the
// walk has more pages.
func TestRunnerRecordsTheMarkerOnlyOnACleanWrap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Clean wrap: dirty, then empty; one short page. Marker records once.
	lister := &fakeLister{lists: [][]DirtyRepo{
		{{RepoID: "repo-a", Reason: "missing"}},
		{},
	}}
	claimer := &fakeClaimer{pages: [][]string{{"repo-b"}}}
	marker := &fakeMarker{}
	runner := &Runner{
		Lister:  lister,
		Claimer: claimer,
		Deriver: &fakeDeriver{},
		Marker:  marker,
		Config:  Config{Workers: 1, PageBudget: 10, SkipAfterFailures: 5, PollInterval: time.Minute},
	}
	batch, err := runner.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, batch.MarkerRecorded)
	require.Equal(t, 1, marker.recorded)

	// Still dirty after the cycle (the repair failed): no marker.
	lister = &fakeLister{lists: [][]DirtyRepo{
		{{RepoID: "repo-a", Reason: "missing"}},
		{{RepoID: "repo-a", Reason: "failed"}},
	}}
	claimer = &fakeClaimer{pages: [][]string{{}}}
	marker = &fakeMarker{}
	runner = &Runner{
		Lister:  lister,
		Claimer: claimer,
		Deriver: &fakeDeriver{outcomes: map[string]string{"repo-a": "error"}},
		Marker:  marker,
		Config:  Config{Workers: 1, PageBudget: 10, SkipAfterFailures: 5, PollInterval: time.Minute},
	}
	batch, err = runner.RunOnce(ctx)
	require.NoError(t, err)
	require.False(t, batch.MarkerRecorded)
	require.Equal(t, 0, marker.recorded)

	// Clean but the walk has more pages (full page): no marker yet.
	lister = &fakeLister{lists: [][]DirtyRepo{nil, {}}}
	claimer = &fakeClaimer{pages: [][]string{{"repo-a", "repo-b"}}}
	marker = &fakeMarker{}
	runner = &Runner{
		Lister:  lister,
		Claimer: claimer,
		Deriver: &fakeDeriver{},
		Marker:  marker,
		Config:  Config{Workers: 1, PageBudget: 2, SkipAfterFailures: 5, PollInterval: time.Minute},
	}
	batch, err = runner.RunOnce(ctx)
	require.NoError(t, err)
	require.False(t, batch.WalkWrapped)
	require.False(t, batch.MarkerRecorded)
	require.Equal(t, 0, marker.recorded)
}

// TestRunnerRefusalAbortsTheCycle pins the read-model-off short-circuit: when
// the start-up checks failed (encoding or fence), the first derive refuses
// without a statement, so the walker counts it and stops the cycle instead of
// refusing every repository one by one. No marker, no progress.
func TestRunnerRefusalAbortsTheCycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inst, reader := newWalkerTestInstruments(t)
	lister := &fakeLister{lists: [][]DirtyRepo{
		{
			{RepoID: "repo-a", Reason: "missing"},
			{RepoID: "repo-b", Reason: "missing"},
		},
	}}
	claimer := &fakeClaimer{pages: [][]string{{"repo-c"}}}
	deriver := &fakeDeriver{outcomes: map[string]string{"repo-a": "fence_missing"}}
	marker := &fakeMarker{}
	runner := &Runner{
		Lister:      lister,
		Claimer:     claimer,
		Deriver:     deriver,
		Marker:      marker,
		Config:      Config{Workers: 1, PageBudget: 10, SkipAfterFailures: 5, PollInterval: time.Minute},
		Instruments: inst,
	}

	batch, err := runner.RunOnce(ctx)
	require.NoError(t, err)
	// One worker keeps the abort deterministic: repo-a refuses, repo-b never
	// derives. With more workers, shares already handed out still finish;
	// the guarantee is that no new work starts: no walk claim, no marker.
	require.Equal(t, 1, len(deriver.calls))
	require.Equal(t, 1, batch.Derived)
	require.True(t, batch.Refused)
	require.False(t, batch.MarkerRecorded)

	rm := collectMetrics(t, reader)
	require.Equal(t, int64(1), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_derives_total",
		map[string]string{"trigger": "repair", "outcome": "fence_missing"}))
	require.Equal(t, int64(1), testutil.CounterValue(t, rm,
		"eshu_dp_config_artifact_reconcile_total",
		map[string]string{"outcome": "error"}))
	require.Equal(t, 0, len(claimer.calls))

	// With several workers the shares already handed out still finish, but
	// no new work starts however the goroutines interleave: the walk is
	// never claimed and the marker never records.
	lister = &fakeLister{lists: [][]DirtyRepo{
		{
			{RepoID: "repo-a", Reason: "missing"},
			{RepoID: "repo-b", Reason: "missing"},
			{RepoID: "repo-c", Reason: "missing"},
			{RepoID: "repo-d", Reason: "missing"},
		},
	}}
	claimer = &fakeClaimer{pages: [][]string{{"repo-e"}}}
	deriver = &fakeDeriver{outcomes: map[string]string{
		"repo-a": "fence_missing",
		"repo-b": "fence_missing",
		"repo-c": "fence_missing",
		"repo-d": "fence_missing",
	}}
	marker = &fakeMarker{}
	runner = &Runner{
		Lister:      lister,
		Claimer:     claimer,
		Deriver:     deriver,
		Marker:      marker,
		Config:      Config{Workers: 4, PageBudget: 10, SkipAfterFailures: 5, PollInterval: time.Minute},
		Instruments: inst,
	}
	// One run is enough: the abort skips the claim and the marker on every
	// path out of deriveGroup, so their absence is deterministic however
	// many shares finished. Only the derive count itself varies.
	batch, err = runner.RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, batch.Refused)
	require.GreaterOrEqual(t, len(deriver.calls), 1)
	require.LessOrEqual(t, len(deriver.calls), 4)
	require.False(t, batch.MarkerRecorded)
	require.Equal(t, 0, len(claimer.calls))
}

// TestRunnerSkipsEmissionOnShutdown pins the silent-shutdown rule: a derive
// the shutdown cancels records nothing, so a shutdown wave never reads as a
// derive-error wave.
func TestRunnerSkipsEmissionOnShutdown(t *testing.T) {
	t.Parallel()
	inst, reader := newWalkerTestInstruments(t)
	canceled, cancel := context.WithCancel(context.Background())
	lister := &fakeLister{lists: [][]DirtyRepo{
		{{RepoID: "repo-a", Reason: "missing"}},
	}}
	claimer := &fakeClaimer{pages: [][]string{{}}}
	deriver := &fakeDeriver{outcomes: map[string]string{"repo-a": "canceled"}}
	deriver.onCall = cancel
	marker := &fakeMarker{}
	runner := &Runner{
		Lister:      lister,
		Claimer:     claimer,
		Deriver:     deriver,
		Marker:      marker,
		Config:      Config{Workers: 1, PageBudget: 10, SkipAfterFailures: 5, PollInterval: time.Minute},
		Instruments: inst,
	}

	_, err := runner.RunOnce(canceled)
	require.NoError(t, err)

	rm := collectMetrics(t, reader)
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name == "eshu_dp_config_artifact_derives_total" ||
				m.Name == "eshu_dp_config_artifact_reconcile_total" {
				sum, ok := m.Data.(metricdata.Sum[int64])
				require.True(t, ok)
				require.Empty(t, sum.DataPoints)
			}
		}
	}
}

// TestRunnerRequiresItsDependencies pins startup validation: a missing port
// fails the cycle and the loop instead of deriving against nil.
func TestRunnerRequiresItsDependencies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	runner := &Runner{}
	if _, err := runner.RunOnce(ctx); err == nil {
		t.Fatal("RunOnce with no ports = nil, want a missing-dependency error")
	}
	if err := runner.Run(ctx); err == nil {
		t.Fatal("Run with no ports = nil, want a missing-dependency error")
	}
	runner = &Runner{Lister: &fakeLister{}, Claimer: &fakeClaimer{}}
	if _, err := runner.RunOnce(ctx); err == nil {
		t.Fatal("RunOnce with no deriver = nil, want a missing-dependency error")
	}
}

// TestRunnerDrainsThenWaits pins the maintenance loop shape: a productive
// cycle loops immediately so a backlog drains, an empty cycle waits the poll
// interval, and a canceled context ends the run silently.
func TestRunnerDrainsThenWaits(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var waits atomic.Int32
	lister := &fakeLister{lists: [][]DirtyRepo{
		{{RepoID: "repo-a", Reason: "missing"}},
		nil,
	}}
	runner := &Runner{
		Lister:  lister,
		Claimer: &fakeClaimer{pages: [][]string{nil}},
		Deriver: &fakeDeriver{},
		Marker:  &fakeMarker{complete: true},
		Config:  Config{Workers: 1, PageBudget: 10, SkipAfterFailures: 5, PollInterval: time.Hour},
		Wait: func(ctx context.Context, _ time.Duration) error {
			waits.Add(1)
			cancel()
			<-ctx.Done()
			return ctx.Err()
		},
	}
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop after the wait")
	}
	// Cycle 1 derives (productive, loops immediately); cycle 2 is empty
	// (waits once, then the canceled context ends the run).
	require.Equal(t, 2, len(lister.calls))
	require.Equal(t, int32(1), waits.Load())
}

// TestRunnerErrorWaitsPollInterval pins the failure backoff: a failed cycle
// is logged and waits the poll interval before the retry, so a persistent
// failure (a database down) cannot spin the loop at full speed.
func TestRunnerErrorWaitsPollInterval(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var waits atomic.Int32
	lister := &fakeLister{
		lists: [][]DirtyRepo{nil},
		errs:  []error{errors.New("connection refused")},
	}
	runner := &Runner{
		Lister:  lister,
		Claimer: &fakeClaimer{pages: [][]string{nil}},
		Deriver: &fakeDeriver{},
		Marker:  &fakeMarker{complete: true},
		Config:  Config{Workers: 1, PageBudget: 10, SkipAfterFailures: 5, PollInterval: time.Hour},
		Wait: func(ctx context.Context, _ time.Duration) error {
			if waits.Add(1) == 2 {
				cancel()
				<-ctx.Done()
				return ctx.Err()
			}
			return nil
		},
	}
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop after the waits")
	}
	// Cycle 1 fails on the dirty list (waits); cycle 2 is empty (waits,
	// then the canceled context ends the run).
	require.Equal(t, 2, len(lister.calls))
	require.Equal(t, int32(2), waits.Load())
}
