// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadGenerationRetentionConfigDefaults(t *testing.T) {
	cfg := loadGenerationRetentionConfig(func(string) string { return "" })

	require.True(t, cfg.Enabled)
	require.Equal(t, time.Hour, cfg.Runner.PollInterval)
	require.Equal(t, 24, cfg.Runner.Policy.MinSupersededGenerations)
	require.Equal(t, 7*24*time.Hour, cfg.Runner.Policy.MaxSupersededAge)
	require.Equal(t, 100, cfg.Runner.Policy.BatchGenerationLimit)
	require.Equal(t, 100_000, cfg.Runner.Policy.BatchRowLimit)
	require.Equal(t, "global", cfg.Runner.Policy.PolicyScope)
	require.Equal(t, "global-default-v1", cfg.Runner.Policy.PolicyRevision)
}

func TestLoadGenerationRetentionConfigOverrides(t *testing.T) {
	env := map[string]string{
		generationRetentionEnabledEnv:                  "true",
		generationRetentionPollIntervalEnv:             "15m",
		generationRetentionMinSupersededGenerationsEnv: "12",
		generationRetentionMaxSupersededAgeEnv:         "72h",
		generationRetentionBatchGenerationLimitEnv:     "25",
		generationRetentionBatchRowLimitEnv:            "5000",
		generationRetentionPolicyScopeEnv:              "collector-kind",
		generationRetentionPolicyRevisionEnv:           "revision-2",
	}
	cfg := loadGenerationRetentionConfig(func(key string) string { return env[key] })

	require.True(t, cfg.Enabled)
	require.Equal(t, 15*time.Minute, cfg.Runner.PollInterval)
	require.Equal(t, 12, cfg.Runner.Policy.MinSupersededGenerations)
	require.Equal(t, 72*time.Hour, cfg.Runner.Policy.MaxSupersededAge)
	require.Equal(t, 25, cfg.Runner.Policy.BatchGenerationLimit)
	require.Equal(t, 5000, cfg.Runner.Policy.BatchRowLimit)
	require.Equal(t, "collector-kind", cfg.Runner.Policy.PolicyScope)
	require.Equal(t, "revision-2", cfg.Runner.Policy.PolicyRevision)
}

func TestLoadGenerationRetentionConfigAllowsLocalDisable(t *testing.T) {
	cfg := loadGenerationRetentionConfig(func(key string) string {
		if key == generationRetentionEnabledEnv {
			return "false"
		}
		return ""
	})

	require.False(t, cfg.Enabled)
}

func TestGenerationRetentionRunnerForConfig(t *testing.T) {
	disabled := generationRetentionRunnerFor(&fakeReducerDB{}, generationRetentionConfig{})
	require.Nil(t, disabled)

	enabled := generationRetentionRunnerFor(&fakeReducerDB{}, generationRetentionConfig{
		Enabled: true,
		Runner:  loadGenerationRetentionConfig(func(string) string { return "" }).Runner,
	})
	require.NotNil(t, enabled)
	require.Equal(t, time.Hour, enabled.Config.PollInterval)
}
