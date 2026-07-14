// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "time"

const (
	defaultRepoDependencyProjectionLeaseTTL     = 5 * time.Minute
	defaultRepoDependencyProjectionCycleTimeout = 45 * time.Second
	defaultRepoDependencyGraphQuiescenceBudget  = 2 * time.Minute
	repoDependencyProjectionLeaseSafetyMargin   = 30 * time.Second
)

// RepoDependencyProjectionRunnerConfig configures the controlled repo-dependency lane.
type RepoDependencyProjectionRunnerConfig struct {
	LeaseOwner            string
	PollInterval          time.Duration
	LeaseTTL              time.Duration
	CycleTimeout          time.Duration
	GraphQuiescenceBudget time.Duration
	BatchLimit            int
	Workers               int
	PartitionID           int
	PartitionCount        int
}

func (c RepoDependencyProjectionRunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultSharedPollInterval
	}
	return c.PollInterval
}

func (c RepoDependencyProjectionRunnerConfig) leaseTTL() time.Duration {
	if c.LeaseTTL <= 0 {
		return defaultRepoDependencyProjectionLeaseTTL
	}
	return c.LeaseTTL
}

func (c RepoDependencyProjectionRunnerConfig) cycleTimeout() time.Duration {
	if c.CycleTimeout <= 0 {
		return defaultRepoDependencyProjectionCycleTimeout
	}
	return c.CycleTimeout
}

func (c RepoDependencyProjectionRunnerConfig) graphQuiescenceBudget() time.Duration {
	if c.GraphQuiescenceBudget <= 0 {
		return defaultRepoDependencyGraphQuiescenceBudget
	}
	return c.GraphQuiescenceBudget
}

func (c RepoDependencyProjectionRunnerConfig) requiredLeaseSafetyBudget() time.Duration {
	return c.cycleTimeout() + c.graphQuiescenceBudget() + repoDependencyProjectionLeaseSafetyMargin
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
	switch c.Workers {
	case 2, 4:
		return c.Workers
	default:
		return 1
	}
}

func (c RepoDependencyProjectionRunnerConfig) partitionID() int {
	if c.PartitionID < 0 {
		return 0
	}
	return c.PartitionID
}

func (c RepoDependencyProjectionRunnerConfig) partitionCount() int {
	if c.PartitionCount <= 0 {
		return 1
	}
	return c.PartitionCount
}
