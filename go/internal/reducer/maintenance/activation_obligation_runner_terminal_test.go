// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// A generation that can never carry a backward-evidence phase (no repository
// fact) is retired by Finalize itself; the runner must not spend a
// maintenance callback on it (#7584 ruling D3(d)).
func TestActivationRunnerNeverMaintainsAnInapplicableObligation(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{
		queue: []ActivationObligation{{ScopeID: "gcp:project", GenerationID: "g1"}},
		finalizes: map[string][]ActivationFinalizeResult{
			"g1": {{Outcome: ActivationOutcomeInapplicable}},
		},
	}
	maintainer := &fakeActivationMaintainer{}
	runner, reader, _ := newActivationRunnerForTest(t, store, maintainer)
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Empty(t, maintainer.calls)
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_finalize_total", map[string]string{"outcome": "inapplicable"}))
}

// A maintainer that finds the owed partition maps to no repository in the
// shipped read (repo_id collision loser) returns ErrActivationInapplicable;
// the runner retires the row through the token-fenced store method and does
// not count a maintenance failure (#7584 ruling D3(c)).
func TestActivationRunnerRetiresWhenTheMaintainerReportsInapplicable(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{
		queue: []ActivationObligation{{ScopeID: "git:loser", GenerationID: "g1"}},
		finalizes: map[string][]ActivationFinalizeResult{
			"g1": {{Outcome: ActivationOutcomePhaseNotReady}},
		},
	}
	maintainer := &fakeActivationMaintainer{err: fmt.Errorf("targeted pass: %w", ErrActivationInapplicable)}
	runner, reader, _ := newActivationRunnerForTest(t, store, maintainer)
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"git:loser/g1"}, maintainer.calls)
	require.Equal(t, []string{"claim:consumer-1", "finalize:g1", "retire:g1", "claim:consumer-1"}, store.calls)
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_finalize_total", map[string]string{"outcome": "inapplicable"}))
	requireNoActivationFailure(t, rm, "maintenance")
}

// A catalog-changed refusal is held: the lease stays, the next attempt comes
// at lease cadence, no fallback pass runs, and it is counted under its own
// reason and logged at INFO, not ERROR (#7584 ruling D2).
func TestActivationRunnerHoldsACatalogChangedRefusal(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{
		queue: []ActivationObligation{{ScopeID: "git:owed", GenerationID: "g1"}},
		finalizes: map[string][]ActivationFinalizeResult{
			"g1": {{Outcome: ActivationOutcomePhaseNotReady}},
		},
	}
	maintainer := &fakeActivationMaintainer{err: fmt.Errorf("targeted pass: %w", ErrActivationCatalogChanged)}
	runner, reader, logs := newActivationRunnerForTest(t, store, maintainer)
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"git:owed/g1"}, maintainer.calls, "one callback, no fallback")
	require.Equal(t, []string{"claim:consumer-1", "finalize:g1", "claim:consumer-1"}, store.calls,
		"no second finalize and no retire after a refusal")
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_failures_total", map[string]string{"reason": "catalog_changed"}))
	requireNoActivationFailure(t, rm, "maintenance")
	var line string
	for _, l := range strings.Split(logs.String(), "\n") {
		if strings.Contains(l, "catalog") {
			line = l
		}
	}
	require.Contains(t, line, `"level":"INFO"`)
	require.Contains(t, line, `"scope_id":"git:owed"`)
	require.Contains(t, line, `"generation_id":"g1"`)
}

// requireNoActivationFailure asserts no failure was counted for reason.
func requireNoActivationFailure(t *testing.T, rm metricdata.ResourceMetrics, reason string) {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_activation_obligation_failures_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("failures metric data = %T", m.Data)
			}
			for _, dp := range sum.DataPoints {
				if hasAttrs(dp.Attributes.ToSlice(), map[string]string{"reason": reason}) && dp.Value != 0 {
					t.Fatalf("failures_total{reason=%q} = %d, want none", reason, dp.Value)
				}
			}
		}
	}
}

// Every maintainer hold carries its own closed reason label; the runner
// treats each as a held refusal (lease kept, no second finalize, Info log),
// and an unknown reason is not a hold at all.
func TestActivationRunnerLabelsEachHoldReason(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"catalog_changed", "no_memo_baseline", "closure_too_deep"} {
		store := &fakeActivationStore{
			queue:     []ActivationObligation{{ScopeID: "git:owed", GenerationID: "g1"}},
			finalizes: map[string][]ActivationFinalizeResult{"g1": {{Outcome: ActivationOutcomePhaseNotReady}}},
		}
		maintainer := &fakeActivationMaintainer{err: HoldActivation(reason, fmt.Errorf("targeted pass refused: %s", reason))}
		runner, reader, logs := newActivationRunnerForTest(t, store, maintainer)
		_, err := runner.RunOnce(context.Background())
		require.NoError(t, err)
		require.Equal(t, []string{"claim:consumer-1", "finalize:g1", "claim:consumer-1"}, store.calls, reason)
		rm := collectActivationMetrics(t, reader)
		require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_failures_total", map[string]string{"reason": reason}), reason)
		requireNoActivationFailure(t, rm, "maintenance")
		require.Contains(t, logs.String(), `"reason":"`+reason+`"`)
		require.Contains(t, logs.String(), `"level":"INFO"`)
	}
	require.Equal(t, []string{"catalog_changed", "closure_too_deep", "no_memo_baseline"}, ActivationHoldReasons())
	unknown := HoldActivation("made_up", errors.New("boom"))
	var hold *ActivationHoldError
	require.False(t, errors.As(unknown, &hold), "an unknown reason must not become a hold label")
	require.ErrorIs(t, HoldActivation("catalog_changed", errors.New("x")), ErrActivationCatalogChanged)
}

// blockingActivationMaintainer blocks until its context ends and records why.
type blockingActivationMaintainer struct {
	mu       sync.Mutex
	err      error
	deadline time.Time
}

func (b *blockingActivationMaintainer) MaintainActivation(ctx context.Context, _ ActivationObligation) error {
	deadline, _ := ctx.Deadline()
	<-ctx.Done()
	b.mu.Lock()
	b.err, b.deadline = ctx.Err(), deadline
	b.mu.Unlock()
	return ctx.Err()
}

// The maintenance callback must end before the lease does (D1R-3): it runs
// under a deadline of the lease's expiry minus a margin, a callback still
// running then is cancelled, the obligation is not finalized again (no false
// completion; it stays leased until expiry, for any claimer), and the
// timeout is counted under its own reason.
func TestActivationRunnerCancelsMaintenanceBeforeTheLeaseEnds(t *testing.T) {
	t.Parallel()
	leaseUntil := time.Now().Add(400 * time.Millisecond)
	store := &fakeActivationStore{
		queue:     []ActivationObligation{{ScopeID: "git:slow", GenerationID: "g1", LeaseUntil: leaseUntil}},
		finalizes: map[string][]ActivationFinalizeResult{"g1": {{Outcome: ActivationOutcomePhaseNotReady}}},
	}
	maintainer := &blockingActivationMaintainer{}
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)
	runner := &ActivationObligationRunner{
		Store: store, Maintainer: maintainer, Instruments: instruments,
		Config: ActivationObligationRunnerConfig{Owner: "consumer-1", Lease: 500 * time.Millisecond},
	}
	started := time.Now()
	_, err = runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Less(t, time.Since(started), 2*time.Second, "the blocked callback was not cancelled")
	maintainer.mu.Lock()
	gotErr, gotDeadline := maintainer.err, maintainer.deadline
	maintainer.mu.Unlock()
	require.ErrorIs(t, gotErr, context.DeadlineExceeded)
	require.False(t, gotDeadline.IsZero())
	require.True(t, gotDeadline.Before(leaseUntil), "deadline %s must precede the lease end %s", gotDeadline, leaseUntil)
	require.Equal(t, []string{"claim:consumer-1", "finalize:g1", "claim:consumer-1"}, store.calls,
		"no second finalize after a cancelled callback")
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_failures_total", map[string]string{"reason": "maintenance_timeout"}))
	requireNoActivationFailure(t, rm, "maintenance")
}
