// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	codeValueFlowStaleCleanupEnabledEnv          = "ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_ENABLED"
	codeValueFlowStaleCleanupPollIntervalEnv     = "ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_POLL_INTERVAL"
	codeValueFlowStaleCleanupScopeBatchLimitEnv  = "ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_SCOPE_BATCH_LIMIT"
	codeValueFlowStaleCleanupDeleteBatchLimitEnv = "ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_DELETE_BATCH_LIMIT"
	codeValueFlowStaleCleanupLeaseOwnerEnv       = "ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_OWNER"
	codeValueFlowStaleCleanupLeaseTTLEnv         = "ESHU_CODE_VALUE_FLOW_STALE_CLEANUP_LEASE_TTL"

	defaultCodeValueFlowStaleCleanupPollInterval     = time.Hour
	defaultCodeValueFlowStaleCleanupScopeBatchLimit  = 100
	defaultCodeValueFlowStaleCleanupDeleteBatchLimit = 500
	defaultCodeValueFlowStaleCleanupLeaseTTL         = 5 * time.Minute
)

type codeValueFlowStaleCleanupConfig struct {
	Enabled bool
	Runner  reducer.CodeValueFlowStaleCleanupRunnerConfig
}

func loadCodeValueFlowStaleCleanupConfig(getenv func(string) string) codeValueFlowStaleCleanupConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return codeValueFlowStaleCleanupConfig{
		Enabled: loadBoolOrDefault(getenv, codeValueFlowStaleCleanupEnabledEnv, true),
		Runner: reducer.CodeValueFlowStaleCleanupRunnerConfig{
			PollInterval: loadDurationOrDefault(
				getenv,
				codeValueFlowStaleCleanupPollIntervalEnv,
				defaultCodeValueFlowStaleCleanupPollInterval,
			),
			LeaseOwner: loadStringOrDefault(
				getenv,
				codeValueFlowStaleCleanupLeaseOwnerEnv,
				defaultCodeValueFlowStaleCleanupLeaseOwner(),
			),
			LeaseTTL: loadDurationOrDefault(
				getenv,
				codeValueFlowStaleCleanupLeaseTTLEnv,
				defaultCodeValueFlowStaleCleanupLeaseTTL,
			),
			ScopeBatchLimit: loadPositiveIntOrDefault(
				getenv,
				codeValueFlowStaleCleanupScopeBatchLimitEnv,
				defaultCodeValueFlowStaleCleanupScopeBatchLimit,
			),
			DeleteBatchLimit: loadPositiveIntOrDefault(
				getenv,
				codeValueFlowStaleCleanupDeleteBatchLimitEnv,
				defaultCodeValueFlowStaleCleanupDeleteBatchLimit,
			),
		},
	}
}

func defaultCodeValueFlowStaleCleanupLeaseOwner() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("code-value-flow-stale-cleanup-runner:%s:%d", hostname, os.Getpid())
}
