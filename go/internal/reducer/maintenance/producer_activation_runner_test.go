// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fakeProducerActivationStore serves a queue of obligations and scripted
// settle results, recording every call.
type fakeProducerActivationStore struct {
	mu       sync.Mutex
	queue    []ProducerActivation
	settles  map[string][]ProducerActivationSettleResult
	settleEr map[string][]error
	calls    []string
	pruned   int
	stats    ProducerActivationStats
}

func (f *fakeProducerActivationStore) ClaimProducerActivation(_ context.Context, owner string, _ time.Duration) (*ProducerActivation, error) {
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

func (f *fakeProducerActivationStore) SettleProducerActivation(_ context.Context, work ProducerActivation) (ProducerActivationSettleResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "settle:"+work.GenerationID)
	if errs := f.settleEr[work.GenerationID]; len(errs) > 0 {
		err := errs[0]
		f.settleEr[work.GenerationID] = errs[1:]
		if err != nil {
			return ProducerActivationSettleResult{}, err
		}
	}
	results := f.settles[work.GenerationID]
	if len(results) == 0 {
		return ProducerActivationSettleResult{Outcome: ProducerActivationOutcomeNotOwner}, nil
	}
	result := results[0]
	f.settles[work.GenerationID] = results[1:]
	return result, nil
}

func (f *fakeProducerActivationStore) PruneProducerActivations(context.Context, time.Duration, int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pruned, nil
}

func (f *fakeProducerActivationStore) ProducerActivationStats(context.Context) (ProducerActivationStats, error) {
	return f.stats, nil
}

func newProducerActivationRunnerForTest(
	t *testing.T, store *fakeProducerActivationStore,
) (*ProducerActivationRunner, *sdkmetric.ManualReader, *bytes.Buffer) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	var logs bytes.Buffer
	return &ProducerActivationRunner{
		Store:       store,
		Config:      ProducerActivationRunnerConfig{Owner: "consumer-1", MaxPerCycle: 8},
		Instruments: instruments,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
	}, reader, &logs
}

func collectProducerActivationMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

func TestProducerActivationRunnerSettlesOneCycleAndCountsReopenedByDomain(t *testing.T) {
	t.Parallel()
	store := &fakeProducerActivationStore{
		queue: []ProducerActivation{{ScopeID: "s", GenerationID: "g1", CreatedAt: time.Now().Add(-time.Minute)}},
		settles: map[string][]ProducerActivationSettleResult{
			"g1": {{
				Outcome:  ProducerActivationOutcomeCompleted,
				Reopened: map[string]int{"kubernetes_correlation_materialization": 2},
			}},
		},
	}
	runner, reader, logs := newProducerActivationRunnerForTest(t, store)
	processed, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, []string{"claim:consumer-1", "settle:g1", "claim:consumer-1"}, store.calls)
	rm := collectProducerActivationMetrics(t, reader)
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_producer_activation_settle_total", map[string]string{"outcome": "completed"}))
	require.EqualValues(t, 2, reducerCounterValue(t, rm, "eshu_dp_producer_activation_reopened_total", map[string]string{"domain": "kubernetes_correlation_materialization"}))
	require.Contains(t, logs.String(), `"scope_id":"s"`)
	require.Contains(t, logs.String(), `"generation_id":"g1"`)
	require.Contains(t, logs.String(), `"outcome":"completed"`)
}

func TestProducerActivationRunnerClassifiesLeaseLossLockTimeoutAndErrors(t *testing.T) {
	t.Parallel()
	store := &fakeProducerActivationStore{
		queue: []ProducerActivation{
			{ScopeID: "s", GenerationID: "g1"},
			{ScopeID: "s", GenerationID: "g2"},
			{ScopeID: "s", GenerationID: "g3"},
		},
		settleEr: map[string][]error{
			"g1": {errors.Join(errors.New("partial reopen"), ErrProducerActivationLeaseLost)},
			"g2": {errors.Join(errors.New("lock wait"), ErrProducerActivationSettleLockTimeout)},
			"g3": {errors.New("connection reset")},
		},
		settles: map[string][]ProducerActivationSettleResult{},
	}
	runner, reader, logs := newProducerActivationRunnerForTest(t, store)
	processed, err := runner.RunOnce(context.Background())
	require.NoError(t, err, "one obligation's failure must not stop the cycle")
	require.Equal(t, 3, processed)
	rm := collectProducerActivationMetrics(t, reader)
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_producer_activation_settle_total", map[string]string{"outcome": "lease_lost"}))
	require.EqualValues(t, 2, reducerCounterValue(t, rm, "eshu_dp_producer_activation_settle_total", map[string]string{"outcome": "error"}))
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_producer_activation_failures_total", map[string]string{"reason": "settle_lock_timeout"}))
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_producer_activation_failures_total", map[string]string{"reason": "settle"}))
	require.Contains(t, logs.String(), "connection reset")
}

func TestProducerActivationRunnerBoundsOneCycleAndRunsHousekeeping(t *testing.T) {
	t.Parallel()
	store := &fakeProducerActivationStore{settles: map[string][]ProducerActivationSettleResult{}}
	for i := 0; i < 5; i++ {
		store.queue = append(store.queue, ProducerActivation{ScopeID: "s", GenerationID: "g"})
	}
	store.pruned = 4
	store.stats = ProducerActivationStats{
		ByState:       map[string]int64{"pending": 7, "leased": 1, "completed": 2, "obsolete": 0, "inapplicable": 0},
		OldestOpenAge: 90 * time.Second,
	}
	runner, reader, _ := newProducerActivationRunnerForTest(t, store)
	runner.Config.MaxPerCycle = 2
	processed, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, processed, "one cycle claims at most MaxPerCycle obligations")
	require.Len(t, store.queue, 3)
	rm := collectProducerActivationMetrics(t, reader)
	require.EqualValues(t, 4, reducerCounterValue(t, rm, "eshu_dp_producer_activation_pruned_total", map[string]string{}))
	require.EqualValues(t, 7, activationGaugeValue(t, rm, "eshu_dp_producer_activations", map[string]string{"status": "pending"}))
}

func TestProducerActivationRunnerRefusesAnIncompleteConfiguration(t *testing.T) {
	t.Parallel()
	store := &fakeProducerActivationStore{}
	for name, runner := range map[string]*ProducerActivationRunner{
		"missing store": {Config: ProducerActivationRunnerConfig{Owner: "consumer-1"}},
		"missing owner": {Store: store},
		"missing both":  {},
	} {
		_, err := runner.RunOnce(context.Background())
		require.Error(t, err, name)
		require.Error(t, runner.Run(context.Background()), name)
	}
}
