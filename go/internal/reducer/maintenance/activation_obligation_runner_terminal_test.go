// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
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
