// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

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

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fakeActivationStore serves a queue of obligations and scripted finalize
// results, recording every call.
type fakeActivationStore struct {
	mu        sync.Mutex
	queue     []ActivationObligation
	finalizes map[string][]ActivationFinalizeResult
	finalErr  map[string][]error
	calls     []string
	cursors   []string
	pages     []ActivationCatchUpPage
	pruned    int
	stats     ActivationStats
}

func (f *fakeActivationStore) ClaimActivation(_ context.Context, owner string, _ time.Duration) (*ActivationObligation, error) {
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

func (f *fakeActivationStore) FinalizeActivation(_ context.Context, work ActivationObligation) (ActivationFinalizeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "finalize:"+work.GenerationID)
	if errs := f.finalErr[work.GenerationID]; len(errs) > 0 {
		err := errs[0]
		f.finalErr[work.GenerationID] = errs[1:]
		if err != nil {
			return ActivationFinalizeResult{}, err
		}
	}
	results := f.finalizes[work.GenerationID]
	if len(results) == 0 {
		return ActivationFinalizeResult{Outcome: ActivationOutcomeNotOwner}, nil
	}
	result := results[0]
	f.finalizes[work.GenerationID] = results[1:]
	return result, nil
}

func (f *fakeActivationStore) RetireActivationInapplicable(_ context.Context, work ActivationObligation) (ActivationFinalizeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "retire:"+work.GenerationID)
	return ActivationFinalizeResult{Outcome: ActivationOutcomeInapplicable}, nil
}

func (f *fakeActivationStore) CatchUpActivations(_ context.Context, cursor string, _ int) (ActivationCatchUpPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cursors = append(f.cursors, cursor)
	if len(f.pages) == 0 {
		return ActivationCatchUpPage{}, nil
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

func (f *fakeActivationStore) ActivationStats(context.Context) (ActivationStats, error) {
	return f.stats, nil
}

type fakeActivationMaintainer struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (m *fakeActivationMaintainer) MaintainActivation(_ context.Context, work ActivationObligation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, work.ScopeID+"/"+work.GenerationID)
	return m.err
}

func newActivationRunnerForTest(
	t *testing.T, store *fakeActivationStore, maintainer *fakeActivationMaintainer,
) (*ActivationObligationRunner, *sdkmetric.ManualReader, *bytes.Buffer) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	var logs bytes.Buffer
	return &ActivationObligationRunner{
		Store:       store,
		Maintainer:  maintainer,
		Config:      ActivationObligationRunnerConfig{Owner: "consumer-1", MaxPerCycle: 8},
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
		queue: []ActivationObligation{{ScopeID: "s", GenerationID: "g1", CreatedAt: time.Now().Add(-time.Minute)}},
		finalizes: map[string][]ActivationFinalizeResult{
			"g1": {{Outcome: ActivationOutcomeCompleted, Woken: 2}},
		},
	}
	maintainer := &fakeActivationMaintainer{}
	runner, reader, logs := newActivationRunnerForTest(t, store, maintainer)
	processed, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Empty(t, maintainer.calls, "an existing phase must not trigger the maintenance callback")
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_finalize_total", map[string]string{"outcome": "completed"}))
	require.EqualValues(t, 2, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_woken_total", map[string]string{}))
	require.Contains(t, logs.String(), `"scope_id":"s"`)
	require.Contains(t, logs.String(), `"generation_id":"g1"`)
}

func TestActivationRunnerMaintainsOnlyWhenThePhaseIsMissing(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{
		queue: []ActivationObligation{{ScopeID: "s", GenerationID: "g1"}},
		finalizes: map[string][]ActivationFinalizeResult{
			"g1": {{Outcome: ActivationOutcomePhaseNotReady}, {Outcome: ActivationOutcomeCompleted, Woken: 1}},
		},
	}
	maintainer := &fakeActivationMaintainer{}
	runner, reader, _ := newActivationRunnerForTest(t, store, maintainer)
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"s/g1"}, maintainer.calls)
	require.Equal(t, []string{"claim:consumer-1", "finalize:g1", "finalize:g1", "claim:consumer-1"}, store.calls)
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_finalize_total", map[string]string{"outcome": "phase_not_ready"}))
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_finalize_total", map[string]string{"outcome": "completed"}))
}

func TestActivationRunnerLeavesTheObligationLeasedWhenMaintenanceFails(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{
		queue: []ActivationObligation{{ScopeID: "s", GenerationID: "g1"}},
		finalizes: map[string][]ActivationFinalizeResult{
			"g1": {{Outcome: ActivationOutcomePhaseNotReady}},
		},
	}
	maintainer := &fakeActivationMaintainer{err: errors.New("graph unavailable")}
	runner, reader, logs := newActivationRunnerForTest(t, store, maintainer)
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err, "one obligation's failure must not stop the cycle")
	require.Equal(t, []string{"claim:consumer-1", "finalize:g1", "claim:consumer-1"}, store.calls,
		"no second finalize after a failed callback")
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_failures_total", map[string]string{"reason": "maintenance"}))
	require.Contains(t, logs.String(), "graph unavailable")
}

func TestActivationRunnerClassifiesLeaseLossAndFinalizeErrors(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{
		queue: []ActivationObligation{{ScopeID: "s", GenerationID: "g1"}, {ScopeID: "s", GenerationID: "g2"}},
		finalErr: map[string][]error{
			"g1": {errors.Join(errors.New("partial wake"), ErrActivationLeaseLost)},
			"g2": {errors.New("connection reset")},
		},
		finalizes: map[string][]ActivationFinalizeResult{},
	}
	runner, reader, _ := newActivationRunnerForTest(t, store, &fakeActivationMaintainer{})
	processed, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, processed)
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_finalize_total", map[string]string{"outcome": "lease_lost"}))
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_finalize_total", map[string]string{"outcome": "error"}))
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_failures_total", map[string]string{"reason": "finalize"}))
}

func TestActivationRunnerBoundsOneCycleAndWalksCatchUpPages(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{finalizes: map[string][]ActivationFinalizeResult{}}
	for i := 0; i < 5; i++ {
		store.queue = append(store.queue, ActivationObligation{ScopeID: "s", GenerationID: "g"})
	}
	store.pages = []ActivationCatchUpPage{{NextCursor: "m", Inserted: 3}, {NextCursor: ""}, {NextCursor: "c"}}
	store.pruned = 4
	store.stats = ActivationStats{
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
	require.EqualValues(t, 3, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_catch_up_inserted_total", map[string]string{}))
	require.EqualValues(t, 12, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_pruned_total", map[string]string{}))
	require.EqualValues(t, 7, activationGaugeValue(t, rm, "eshu_dp_activation_obligations", map[string]string{"status": "pending"}))
}

func TestActivationRunnerIdlePollNeverCallsMaintenance(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{finalizes: map[string][]ActivationFinalizeResult{}}
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
	for name, runner := range map[string]*ActivationObligationRunner{
		"no store":      {Maintainer: &fakeActivationMaintainer{}, Config: ActivationObligationRunnerConfig{Owner: "o"}},
		"no maintainer": {Store: &fakeActivationStore{}, Config: ActivationObligationRunnerConfig{Owner: "o"}},
		"no owner":      {Store: &fakeActivationStore{}, Maintainer: &fakeActivationMaintainer{}},
	} {
		if err := runner.Run(context.Background()); err == nil {
			t.Errorf("%s: Run() = nil, want refusal", name)
		}
	}
}

func TestActivationRunnerStopsPromptlyOnCancel(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{finalizes: map[string][]ActivationFinalizeResult{}}
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

// activationGaugeValue reads one int64 gauge data point by attributes.
func activationGaugeValue(t *testing.T, rm metricdata.ResourceMetrics, name string, want map[string]string) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want int64 gauge", name, m.Data)
			}
			for _, dp := range gauge.DataPoints {
				if hasAttrs(dp.Attributes.ToSlice(), want) {
					return dp.Value
				}
			}
		}
	}
	t.Fatalf("gauge %s with attrs %v not found", name, want)
	return 0
}
