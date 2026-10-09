// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package liveness

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestGenerationLivenessRunnerLogsEachRedrive proves the #7265 operator
// signal: every re-driven generation emits one Info log carrying its scope,
// generation, durable attempt count, the bounded reason, and the effective
// progress window, and the per-row list survives into the RunOnce result.
func TestGenerationLivenessRunnerLogsEachRedrive(t *testing.T) {
	recoveries := []Recovery{
		{ScopeID: "scope-a", GenerationID: "gen-a", LivenessRecoveryAttempts: 1},
		{ScopeID: "scope-b", GenerationID: "gen-b", LivenessRecoveryAttempts: 3},
	}
	var buf bytes.Buffer
	runner := &Runner{
		Recoverer: &fakeGenerationLivenessRecoverer{
			results: []Result{{Recovered: 2, Recoveries: recoveries}},
		},
		Config: Config{
			Policy: Policy{ProgressWindow: 12 * time.Minute},
		},
		Logger: slog.New(slog.NewJSONHandler(&buf, nil)),
	}

	result, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, recoveries, result.Recoveries)

	var redrives []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		if entry["msg"] == generationLivenessRedriveLogMessage {
			redrives = append(redrives, entry)
		}
	}
	require.Len(t, redrives, 2)
	for i, entry := range redrives {
		require.Equal(t, "INFO", entry["level"])
		require.Equal(t, recoveries[i].ScopeID, entry["scope_id"])
		require.Equal(t, recoveries[i].GenerationID, entry["generation_id"])
		require.EqualValues(t, recoveries[i].LivenessRecoveryAttempts, entry["liveness_recovery_attempts"])
		require.Equal(t, "no_intent_progress_within_window", entry["reason"])
		require.Equal(t, "12m0s", entry["progress_window"])
	}
}

// TestGenerationLivenessRunnerSkipsAreNotLoggedPerGeneration pins that a
// cycle which re-drives nothing (every blocked generation is draining) emits
// no per-generation log; the draining gauge bucket is the skip signal.
func TestGenerationLivenessRunnerSkipsAreNotLoggedPerGeneration(t *testing.T) {
	var buf bytes.Buffer
	runner := &Runner{
		Recoverer: &fakeGenerationLivenessRecoverer{results: []Result{{}}},
		Logger:    slog.New(slog.NewJSONHandler(&buf, nil)),
	}
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	require.Empty(t, buf.String())
}

func TestGenerationLivenessPolicyEffectiveProgressWindowDefault(t *testing.T) {
	require.Equal(t, 10*time.Minute, Policy{}.effectiveProgressWindow())
	require.Equal(t, 10*time.Minute, Policy{ProgressWindow: -time.Second}.effectiveProgressWindow())
	require.Equal(t, time.Hour, Policy{ProgressWindow: time.Hour}.effectiveProgressWindow())
}
