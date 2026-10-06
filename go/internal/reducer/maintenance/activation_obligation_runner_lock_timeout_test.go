// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestActivationRunnerSeparatesFinalizeLockTimeouts (#7584 ruling P2-F): a
// Finalize that waited past its lock_timeout (the store adapter reports
// ErrActivationFinalizeLockTimeout) is expected contention, not a broken
// statement. It is counted under reason finalize_lock_timeout and logged at
// Warn; every other Finalize error stays reason finalize at Error.
func TestActivationRunnerSeparatesFinalizeLockTimeouts(t *testing.T) {
	t.Parallel()
	store := &fakeActivationStore{
		queue: []ActivationObligation{{ScopeID: "s", GenerationID: "g1"}, {ScopeID: "s", GenerationID: "g2"}},
		finalErr: map[string][]error{
			"g1": {fmt.Errorf("%w: %w", ErrActivationFinalizeLockTimeout, errors.New("SQLSTATE 55P03"))},
			"g2": {errors.New("connection reset")},
		},
		finalizes: map[string][]ActivationFinalizeResult{},
	}
	runner, reader, logs := newActivationRunnerForTest(t, store, &fakeActivationMaintainer{})
	processed, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, processed)
	rm := collectActivationMetrics(t, reader)
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_failures_total",
		map[string]string{"reason": "finalize_lock_timeout"}))
	require.EqualValues(t, 1, reducerCounterValue(t, rm, "eshu_dp_activation_obligation_failures_total",
		map[string]string{"reason": "finalize"}))

	levels := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		if class, ok := record["failure_class"].(string); ok {
			levels[class] = fmt.Sprint(record["level"])
		}
	}
	require.Equal(t, "WARN", levels["activation_obligation_finalize_lock_timeout"])
	require.Equal(t, "ERROR", levels["activation_obligation_finalize"])
}
