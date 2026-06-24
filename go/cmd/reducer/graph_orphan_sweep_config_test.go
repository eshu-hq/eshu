// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadGraphOrphanSweepConfigDefaults(t *testing.T) {
	cfg := loadGraphOrphanSweepConfig(func(string) string { return "" })

	require.True(t, cfg.Enabled)
	require.Equal(t, time.Hour, cfg.Runner.PollInterval)
	require.True(t, strings.HasPrefix(cfg.Runner.LeaseOwner, "graph-orphan-sweep-runner:"), cfg.Runner.LeaseOwner)
	require.Equal(t, 5*time.Minute, cfg.Runner.LeaseTTL)
	require.Equal(t, 7*24*time.Hour, cfg.Runner.Policy.OrphanTTL)
	require.Equal(t, 100, cfg.Runner.Policy.BatchLimit)
	require.Equal(t, 10_000, cfg.Runner.Policy.CountLimit)
	require.Equal(t, defaultGraphOrphanSweepLabels(), cfg.Runner.Policy.Labels)
}

func TestLoadGraphOrphanSweepConfigOverrides(t *testing.T) {
	env := map[string]string{
		graphOrphanSweepEnabledEnv:      "true",
		graphOrphanSweepPollIntervalEnv: "30m",
		graphOrphanSweepTTLEnv:          "72h",
		graphOrphanSweepBatchLimitEnv:   "25",
		graphOrphanSweepCountLimitEnv:   "500",
		graphOrphanSweepLeaseOwnerEnv:   "sweep-owner-a",
		graphOrphanSweepLeaseTTLEnv:     "2m",
	}

	cfg := loadGraphOrphanSweepConfig(func(key string) string { return env[key] })

	require.True(t, cfg.Enabled)
	require.Equal(t, 30*time.Minute, cfg.Runner.PollInterval)
	require.Equal(t, "sweep-owner-a", cfg.Runner.LeaseOwner)
	require.Equal(t, 2*time.Minute, cfg.Runner.LeaseTTL)
	require.Equal(t, 72*time.Hour, cfg.Runner.Policy.OrphanTTL)
	require.Equal(t, 25, cfg.Runner.Policy.BatchLimit)
	require.Equal(t, 500, cfg.Runner.Policy.CountLimit)
}

func TestLoadGraphOrphanSweepConfigCanDisableRunner(t *testing.T) {
	cfg := loadGraphOrphanSweepConfig(func(key string) string {
		if key == graphOrphanSweepEnabledEnv {
			return "false"
		}
		return ""
	})

	require.False(t, cfg.Enabled)
}

func TestGraphOrphanSweepRunnerForConfig(t *testing.T) {
	disabled := graphOrphanSweepRunnerFor(stubGraphExecutor{}, stubCypherReader{}, stubGraphOrphanLeaseManager{}, graphOrphanSweepConfig{})
	require.Nil(t, disabled)

	enabled := graphOrphanSweepRunnerFor(stubGraphExecutor{}, stubCypherReader{}, stubGraphOrphanLeaseManager{}, graphOrphanSweepConfig{
		Enabled: true,
		Runner:  loadGraphOrphanSweepConfig(func(string) string { return "" }).Runner,
	})
	require.NotNil(t, enabled)
	require.Equal(t, time.Hour, enabled.Config.PollInterval)
	require.NotNil(t, enabled.LeaseManager)
}

type stubGraphOrphanLeaseManager struct{}

func (stubGraphOrphanLeaseManager) ClaimPartitionLease(
	_ context.Context,
	_ string,
	_, _ int,
	_ string,
	_ time.Duration,
) (bool, error) {
	return true, nil
}

func (stubGraphOrphanLeaseManager) ReleasePartitionLease(
	_ context.Context,
	_ string,
	_, _ int,
	_ string,
) error {
	return nil
}
