// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "time"

// RepoDependencyProjectionRunnerConfig configures the controlled repo-dependency lane.
type RepoDependencyProjectionRunnerConfig struct {
	LeaseOwner   string
	PollInterval time.Duration
	LeaseTTL     time.Duration
	BatchLimit   int
	Workers      int
}

func (c RepoDependencyProjectionRunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultSharedPollInterval
	}
	return c.PollInterval
}

func (c RepoDependencyProjectionRunnerConfig) leaseTTL() time.Duration {
	if c.LeaseTTL <= 0 {
		return defaultLeaseTTL
	}
	return c.LeaseTTL
}

func (c RepoDependencyProjectionRunnerConfig) batchLimit() int {
	if c.BatchLimit <= 0 {
		return defaultBatchLimit
	}
	return c.BatchLimit
}

func (c RepoDependencyProjectionRunnerConfig) leaseOwner() string {
	if c.LeaseOwner == "" {
		return defaultRepoDependencyLeaseOwner
	}
	return c.LeaseOwner
}

func (c RepoDependencyProjectionRunnerConfig) workerCount() int {
	if c.Workers <= 0 {
		return 1
	}
	return c.Workers
}
