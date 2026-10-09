// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package obligation

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/testutil"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fakeActivationStore serves a queue of obligations and scripted finalize
// results, recording every call.
type fakeActivationStore struct {
	mu        sync.Mutex
	queue     []Obligation
	finalizes map[string][]FinalizeResult
	finalErr  map[string][]error
	calls     []string
	cursors   []string
	pages     []CatchUpPage
	pruned    int
	stats     Stats
}

func (f *fakeActivationStore) ClaimActivation(_ context.Context, owner string, _ time.Duration) (*Obligation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "claim:"+owner)
	if len(f.queue) == 0 {
		return nil, nil
	}
	work := f.queue[0]
	f.queue = f.queue[1:]
	work.LeaseOwner = owner
	return &work, nil
}

func (f *fakeActivationStore) FinalizeActivation(_ context.Context, work Obligation) (FinalizeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "finalize:"+work.GenerationID)
	if errs := f.finalErr[work.GenerationID]; len(errs) > 0 {
		err := errs[0]
		f.finalErr[work.GenerationID] = errs[1:]
		if err != nil {
			return FinalizeResult{}, err
		}
	}
	results := f.finalizes[work.GenerationID]
	if len(results) == 0 {
		return FinalizeResult{Outcome: OutcomeNotOwner}, nil
	}
	result := results[0]
	f.finalizes[work.GenerationID] = results[1:]
	return result, nil
}

func (f *fakeActivationStore) RetireActivationInapplicable(_ context.Context, work Obligation) (FinalizeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "retire:"+work.GenerationID)
	return FinalizeResult{Outcome: OutcomeInapplicable}, nil
}

func (f *fakeActivationStore) CatchUpActivations(_ context.Context, cursor string, _ int) (CatchUpPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cursors = append(f.cursors, cursor)
	if len(f.pages) == 0 {
		return CatchUpPage{}, nil
	}
	page := f.pages[0]
	f.pages = f.pages[1:]
	return page, nil
}

func (f *fakeActivationStore) PruneActivations(context.Context, time.Duration, int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pruned, nil
}

func (f *fakeActivationStore) ActivationStats(context.Context) (Stats, error) {
	return f.stats, nil
}

type fakeActivationMaintainer struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (m *fakeActivationMaintainer) MaintainActivation(_ context.Context, work Obligation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, work.ScopeID+"/"+work.GenerationID)
	return m.err
}

func newActivationRunnerForTest(
	t *testing.T, store *fakeActivationStore, maintainer *fakeActivationMaintainer,
) (*Runner, *sdkmetric.ManualReader, *bytes.Buffer) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	var logs bytes.Buffer
	return &Runner{
		Store:       store,
		Maintainer:  maintainer,
		Config:      Config{Owner: "consumer-1", MaxPerCycle: 8},
		Instruments: instruments,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
	}, reader, &logs
}

func collectActivationMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

func TestActivationRunnerSkipsMaintenanceWhenThePhaseAlreadyExists(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{
		queue: []Obligation{{ScopeID: "s", GenerationID: "g1", CreatedAt: time.Now().Add(-time.Minute)}},
		finalizes: map[string][]FinalizeResult{
			"g1": {{Outcome: OutcomeCompleted, Woken: 2}},
		},
	}
	maintainer := &fakeActivationMaintainer{}
	runner, reader, logs := newActivationRunnerForTest(t, store, maintainer)
	processed, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Empty(t, maintainer.calls, "an existing phase must not trigger the maintenance callback")
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, testutil.CounterValue(t, rm, "eshu_dp_activation_obligation_finalize_total", map[string]string{"outcome": "completed"}))
	require.EqualValues(t, 2, testutil.CounterValue(t, rm, "eshu_dp_activation_obligation_woken_total", map[string]string{}))
	require.Contains(t, logs.String(), `"scope_id":"s"`)
	require.Contains(t, logs.String(), `"generation_id":"g1"`)
}

func TestActivationRunnerMaintainsOnlyWhenThePhaseIsMissing(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{
		queue: []Obligation{{ScopeID: "s", GenerationID: "g1"}},
		finalizes: map[string][]FinalizeResult{
			"g1": {{Outcome: OutcomePhaseNotReady}, {Outcome: OutcomeCompleted, Woken: 1}},
		},
	}
	maintainer := &fakeActivationMaintainer{}
	runner, reader, _ := newActivationRunnerForTest(t, store, maintainer)
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"s/g1"}, maintainer.calls)
	require.Equal(t, []string{"claim:consumer-1", "finalize:g1", "finalize:g1", "claim:consumer-1"}, store.calls)
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, testutil.CounterValue(t, rm, "eshu_dp_activation_obligation_finalize_total", map[string]string{"outcome": "phase_not_ready"}))
	require.EqualValues(t, 1, testutil.CounterValue(t, rm, "eshu_dp_activation_obligation_finalize_total", map[string]string{"outcome": "completed"}))
}

func TestActivationRunnerLeavesTheObligationLeasedWhenMaintenanceFails(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{
		queue: []Obligation{{ScopeID: "s", GenerationID: "g1"}},
		finalizes: map[string][]FinalizeResult{
			"g1": {{Outcome: OutcomePhaseNotReady}},
		},
	}
	maintainer := &fakeActivationMaintainer{err: errors.New("graph unavailable")}
	runner, reader, logs := newActivationRunnerForTest(t, store, maintainer)
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err, "one obligation's failure must not stop the cycle")
	require.Equal(t, []string{"claim:consumer-1", "finalize:g1", "claim:consumer-1"}, store.calls,
		"no second finalize after a failed callback")
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, testutil.CounterValue(t, rm, "eshu_dp_activation_obligation_failures_total", map[string]string{"reason": "maintenance"}))
	require.Contains(t, logs.String(), "graph unavailable")
}

func TestActivationRunnerClassifiesLeaseLossAndFinalizeErrors(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{
		queue: []Obligation{{ScopeID: "s", GenerationID: "g1"}, {ScopeID: "s", GenerationID: "g2"}},
		finalErr: map[string][]error{
			"g1": {errors.Join(errors.New("partial wake"), ErrLeaseLost)},
			"g2": {errors.New("connection reset")},
		},
		finalizes: map[string][]FinalizeResult{},
	}
	runner, reader, _ := newActivationRunnerForTest(t, store, &fakeActivationMaintainer{})
	processed, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, processed)
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, testutil.CounterValue(t, rm, "eshu_dp_activation_obligation_finalize_total", map[string]string{"outcome": "lease_lost"}))
	require.EqualValues(t, 1, testutil.CounterValue(t, rm, "eshu_dp_activation_obligation_finalize_total", map[string]string{"outcome": "error"}))
	require.EqualValues(t, 1, testutil.CounterValue(t, rm, "eshu_dp_activation_obligation_failures_total", map[string]string{"reason": "finalize"}))
}

func TestActivationRunnerBoundsOneCycleAndWalksCatchUpPages(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{finalizes: map[string][]FinalizeResult{}}
	for i := 0; i < 5; i++ {
		store.queue = append(store.queue, Obligation{ScopeID: "s", GenerationID: "g"})
	}
	store.pages = []CatchUpPage{{NextCursor: "m", Inserted: 3}, {NextCursor: ""}, {NextCursor: "c"}}
	store.pruned = 4
	store.stats = Stats{
		ByState:       map[string]int64{"pending": 7, "leased": 1, "completed": 2, "obsolete": 0},
		OldestOpenAge: 90 * time.Second,
	}
	runner, reader, _ := newActivationRunnerForTest(t, store, &fakeActivationMaintainer{})
	runner.Config.MaxPerCycle = 2
	processed, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, processed, "one cycle claims at most MaxPerCycle obligations")
	require.Len(t, store.queue, 3)
	for i := 0; i < 2; i++ {
		_, err = runner.RunOnce(context.Background())
		require.NoError(t, err)
	}
	require.Equal(t, []string{"", "m", ""}, store.cursors, "the cursor advances, then wraps after an end page")
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 3, testutil.CounterValue(t, rm, "eshu_dp_activation_obligation_catch_up_inserted_total", map[string]string{}))
	require.EqualValues(t, 12, testutil.CounterValue(t, rm, "eshu_dp_activation_obligation_pruned_total", map[string]string{}))
	require.EqualValues(t, 7, testutil.GaugeValue(t, rm, "eshu_dp_activation_obligations", map[string]string{"status": "pending"}))
}

func TestActivationRunnerIdlePollNeverCallsMaintenance(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{finalizes: map[string][]FinalizeResult{}}
	maintainer := &fakeActivationMaintainer{}
	runner, _, _ := newActivationRunnerForTest(t, store, maintainer)
	for i := 0; i < 3; i++ {
		processed, err := runner.RunOnce(context.Background())
		require.NoError(t, err)
		require.Zero(t, processed)
	}
	require.Empty(t, maintainer.calls)
}

func TestActivationRunnerRefusesAnIncompleteConfiguration(t *testing.T) {
	t.Parallel()
	for name, runner := range map[string]*Runner{
		"no store":      {Maintainer: &fakeActivationMaintainer{}, Config: Config{Owner: "o"}},
		"no maintainer": {Store: &fakeActivationStore{}, Config: Config{Owner: "o"}},
		"no owner":      {Store: &fakeActivationStore{}, Maintainer: &fakeActivationMaintainer{}},
	} {
		if err := runner.Run(context.Background()); err == nil {
			t.Errorf("%s: Run() = nil, want refusal", name)
		}
	}
}

func TestActivationRunnerStopsPromptlyOnCancel(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{finalizes: map[string][]FinalizeResult{}}
	runner, _, _ := newActivationRunnerForTest(t, store, &fakeActivationMaintainer{})
	runner.Config.PollInterval = time.Hour
	runner.Config.Workers = 3
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	claims := 0
	store.mu.Lock()
	for _, call := range store.calls {
		if strings.HasPrefix(call, "claim:") {
			claims++
		}
	}
	store.mu.Unlock()
	require.GreaterOrEqual(t, claims, 3, "every worker polls")
}
