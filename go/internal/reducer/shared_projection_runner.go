// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
)

// The default* constants are root spellings of [worker]'s exported defaults.
const (
	defaultBatchLimit         = worker.DefaultBatchLimit
	defaultLeaseTTL           = worker.DefaultLeaseTTL
	defaultSharedPollInterval = worker.DefaultSharedPollInterval
	defaultEvidenceSource     = worker.DefaultEvidenceSource
)

// DefaultSharedProjectionLeaseOwnerPrefix is the root spelling of
// [worker.DefaultSharedProjectionLeaseOwnerPrefix].
const DefaultSharedProjectionLeaseOwnerPrefix = worker.DefaultSharedProjectionLeaseOwnerPrefix

// sharedProjectionDomains is the root spelling of the shared projection
// domain list the generic partition worker drains. It cannot be a type
// alias (Go has no var/const alias across packages), so it copies
// [worker.SharedProjectionDomains]'s result once at package init.
var sharedProjectionDomains = worker.SharedProjectionDomains()

// SharedProjectionRunnerConfig is the root spelling of
// [worker.SharedProjectionRunnerConfig].
type SharedProjectionRunnerConfig = worker.SharedProjectionRunnerConfig

// SharedProjectionRunner is the root spelling of
// [worker.SharedProjectionRunner].
type SharedProjectionRunner = worker.SharedProjectionRunner

// mergePartitionProcessResult forwards to
// [worker.MergePartitionProcessResult].
func mergePartitionProcessResult(total *PartitionProcessResult, result PartitionProcessResult) {
	worker.MergePartitionProcessResult(total, result)
}
