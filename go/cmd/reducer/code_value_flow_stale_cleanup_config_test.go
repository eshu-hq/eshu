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

func TestLoadCodeValueFlowStaleCleanupConfigDefaults(t *testing.T) {
	cfg := loadCodeValueFlowStaleCleanupConfig(func(string) string { return "" })

	require.True(t, cfg.Enabled)
	require.Equal(t, time.Hour, cfg.Runner.PollInterval)
	require.True(t, strings.HasPrefix(cfg.Runner.LeaseOwner, "code-value-flow-stale-cleanup-runner:"), cfg.Runner.LeaseOwner)
	require.Equal(t, 5*time.Minute, cfg.Runner.LeaseTTL)
	require.Equal(t, 100, cfg.Runner.ScopeBatchLimit)
	require.Equal(t, 500, cfg.Runner.DeleteBatchLimit)
}

func TestLoadCodeValueFlowStaleCleanupConfigOverrides(t *testing.T) {
	env := map[string]string{
		codeValueFlowStaleCleanupEnabledEnv:          "true",
		codeValueFlowStaleCleanupPollIntervalEnv:     "15m",
		codeValueFlowStaleCleanupScopeBatchLimitEnv:  "25",
		codeValueFlowStaleCleanupDeleteBatchLimitEnv: "75",
		codeValueFlowStaleCleanupLeaseOwnerEnv:       "value-flow-owner-a",
		codeValueFlowStaleCleanupLeaseTTLEnv:         "90s",
	}

	cfg := loadCodeValueFlowStaleCleanupConfig(func(key string) string { return env[key] })

	require.True(t, cfg.Enabled)
	require.Equal(t, 15*time.Minute, cfg.Runner.PollInterval)
	require.Equal(t, "value-flow-owner-a", cfg.Runner.LeaseOwner)
	require.Equal(t, 90*time.Second, cfg.Runner.LeaseTTL)
	require.Equal(t, 25, cfg.Runner.ScopeBatchLimit)
	require.Equal(t, 75, cfg.Runner.DeleteBatchLimit)
}

func TestLoadCodeValueFlowStaleCleanupConfigCanDisableRunner(t *testing.T) {
	cfg := loadCodeValueFlowStaleCleanupConfig(func(key string) string {
		if key == codeValueFlowStaleCleanupEnabledEnv {
			return "false"
		}
		return ""
	})

	require.False(t, cfg.Enabled)
}

func TestCodeValueFlowStaleCleanupRunnerForConfig(t *testing.T) {
	disabled := codeValueFlowStaleCleanupRunnerFor(
		&fakeReducerDB{},
		stubCodeValueFlowStaleCleanupWriter{},
		stubCodeValueFlowStaleCleanupWriter{},
		stubCodeValueFlowStaleCleanupLeaseManager{},
		codeValueFlowStaleCleanupConfig{},
	)
	require.Nil(t, disabled)

	enabled := codeValueFlowStaleCleanupRunnerFor(
		&fakeReducerDB{},
		stubCodeValueFlowStaleCleanupWriter{},
		stubCodeValueFlowStaleCleanupWriter{},
		stubCodeValueFlowStaleCleanupLeaseManager{},
		codeValueFlowStaleCleanupConfig{
			Enabled: true,
			Runner:  loadCodeValueFlowStaleCleanupConfig(func(string) string { return "" }).Runner,
		},
	)
	require.NotNil(t, enabled)
	require.Equal(t, time.Hour, enabled.Config.PollInterval)
	require.NotNil(t, enabled.LeaseManager)
}

type stubCodeValueFlowStaleCleanupWriter struct{}

func (stubCodeValueFlowStaleCleanupWriter) RetractStaleCodeTaintEvidence(
	context.Context,
	string,
	string,
	string,
	int,
) error {
	return nil
}

func (stubCodeValueFlowStaleCleanupWriter) RetractStaleCodeInterprocEvidence(
	context.Context,
	string,
	string,
	string,
	int,
) error {
	return nil
}

type stubCodeValueFlowStaleCleanupLeaseManager struct{}

func (stubCodeValueFlowStaleCleanupLeaseManager) ClaimPartitionLease(
	_ context.Context,
	_ string,
	_, _ int,
	_ string,
	_ time.Duration,
) (bool, error) {
	return true, nil
}

func (stubCodeValueFlowStaleCleanupLeaseManager) ReleasePartitionLease(
	_ context.Context,
	_ string,
	_, _ int,
	_ string,
) error {
	return nil
}
