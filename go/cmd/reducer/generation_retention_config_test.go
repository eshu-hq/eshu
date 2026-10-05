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
	require.Equal(t, 90*24*time.Hour, cfg.Runner.Policy.HardMaxSupersededAge)
	require.False(t, cfg.HardMaxSupersededAgeLifted)
	require.Equal(t, 100, cfg.Runner.Policy.BatchGenerationLimit)
	require.Equal(t, 100_000, cfg.Runner.Policy.BatchRowLimit)
	require.Equal(t, "global", cfg.Runner.Policy.PolicyScope)
	require.Equal(t, "global-default-v1", cfg.Runner.Policy.PolicyRevision)
	require.NoError(t, validateGenerationRetentionConfig(func(string) string { return "" }, cfg))
}

func TestLoadGenerationRetentionConfigOverrides(t *testing.T) {
	env := map[string]string{
		generationRetentionEnabledEnv:                  "true",
		generationRetentionPollIntervalEnv:             "15m",
		generationRetentionMinSupersededGenerationsEnv: "12",
		generationRetentionMaxSupersededAgeEnv:         "72h",
		generationRetentionHardMaxSupersededAgeEnv:     "720h",
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
	require.Equal(t, 720*time.Hour, cfg.Runner.Policy.HardMaxSupersededAge)
	require.Equal(t, 25, cfg.Runner.Policy.BatchGenerationLimit)
	require.Equal(t, 5000, cfg.Runner.Policy.BatchRowLimit)
	require.Equal(t, "collector-kind", cfg.Runner.Policy.PolicyScope)
	require.Equal(t, "revision-2", cfg.Runner.Policy.PolicyRevision)
	require.NoError(t, validateGenerationRetentionConfig(func(key string) string { return env[key] }, cfg))
}

func TestLoadGenerationRetentionConfigRejectsHardCeilingBelowSoftAge(t *testing.T) {
	env := map[string]string{
		generationRetentionMaxSupersededAgeEnv:     "168h",
		generationRetentionHardMaxSupersededAgeEnv: "24h",
	}
	cfg := loadGenerationRetentionConfig(func(key string) string { return env[key] })

	require.ErrorContains(t,
		validateGenerationRetentionConfig(func(key string) string { return env[key] }, cfg),
		"ESHU_GENERATION_RETENTION_HARD_MAX_SUPERSEDED_AGE")
}

// TestLoadGenerationRetentionConfigDerivesHardCeilingFromLongSoftAge pins
// #7611: an unset hard ceiling resolves to the soft window when that is longer
// than 2160h, so a deployment that holds retention with a long soft window and
// never sets the hard ceiling still starts. The check runs before the Enabled
// early return, so the disabled local path must pass too.
func TestLoadGenerationRetentionConfigDerivesHardCeilingFromLongSoftAge(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{name: "enabled", env: map[string]string{
			generationRetentionMaxSupersededAgeEnv: "87600h",
		}},
		{name: "disabled local profile", env: map[string]string{
			generationRetentionEnabledEnv:          "false",
			generationRetentionMaxSupersededAgeEnv: "87600h",
			queryProfileEnv:                        "local_authoritative",
		}},
		{name: "invalid hard value falls back to derived", env: map[string]string{
			generationRetentionMaxSupersededAgeEnv:     "87600h",
			generationRetentionHardMaxSupersededAgeEnv: "abc",
		}},
		{name: "non-positive hard value falls back to derived", env: map[string]string{
			generationRetentionMaxSupersededAgeEnv:     "87600h",
			generationRetentionHardMaxSupersededAgeEnv: "-1h",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string { return tc.env[key] }
			cfg := loadGenerationRetentionConfig(getenv)

			require.Equal(t, 87600*time.Hour, cfg.Runner.Policy.HardMaxSupersededAge)
			require.True(t, cfg.HardMaxSupersededAgeLifted)
			require.NoError(t, validateGenerationRetentionConfig(getenv, cfg))
		})
	}
}

// TestLoadGenerationRetentionConfigKeepsExplicitHardCeilingBelowSoftAgeFatal
// proves #7611 only changes the unset case: an explicit hard ceiling below a
// long soft window still fails closed.
func TestLoadGenerationRetentionConfigKeepsExplicitHardCeilingBelowSoftAgeFatal(t *testing.T) {
	env := map[string]string{
		generationRetentionMaxSupersededAgeEnv:     "87600h",
		generationRetentionHardMaxSupersededAgeEnv: "2160h",
	}
	getenv := func(key string) string { return env[key] }
	cfg := loadGenerationRetentionConfig(getenv)

	require.Equal(t, 2160*time.Hour, cfg.Runner.Policy.HardMaxSupersededAge)
	require.False(t, cfg.HardMaxSupersededAgeLifted)
	require.ErrorContains(t, validateGenerationRetentionConfig(getenv, cfg),
		"ESHU_GENERATION_RETENTION_HARD_MAX_SUPERSEDED_AGE")
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
