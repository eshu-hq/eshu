// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
)

// The default* constants are root spellings of [worker]'s exported defaults.
const (
	defaultBatchLimit         = worker.DefaultBatchLimit
	defaultSharedPollInterval = worker.DefaultPollInterval
	defaultEvidenceSource     = worker.DefaultEvidenceSource
)

// DefaultSharedProjectionLeaseOwnerPrefix is the root spelling of
// [worker.DefaultLeaseOwnerPrefix].
const DefaultSharedProjectionLeaseOwnerPrefix = worker.DefaultLeaseOwnerPrefix

// sharedProjectionDomains is the root spelling of the shared projection
// domain list the generic partition worker drains. It cannot be a type
// alias (Go has no var/const alias across packages), so it copies
// [worker.Domains]'s result once at package init.
var sharedProjectionDomains = worker.Domains()

// SharedProjectionRunnerConfig is the root spelling of
// [worker.RunnerConfig].
type SharedProjectionRunnerConfig = worker.RunnerConfig

// SharedProjectionRunner is the root spelling of
// [worker.Runner].
type SharedProjectionRunner = worker.Runner
