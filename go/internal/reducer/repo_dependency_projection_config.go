// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"
)

const (
	defaultRepoDependencyProjectionLeaseTTL     = 5 * time.Minute
	defaultRepoDependencyProjectionCycleTimeout = 45 * time.Second
	defaultRepoDependencyGraphQuiescenceBudget  = 2 * time.Minute
	repoDependencyProjectionLeaseSafetyMargin   = 30 * time.Second
)

// CanonicalCodeQuiescenceChecker reports whether any active code generation
// still lacks its canonical-nodes phase.
type CanonicalCodeQuiescenceChecker interface {
	HasUncommittedCanonicalCodeScopes(ctx context.Context) (bool, error)
}

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

// WorkloadMaterializationReplayOutcome is the closed result of one workload
// materialization replay request.
type WorkloadMaterializationReplayOutcome string

const (
	// WorkloadMaterializationReplayScheduled means replayable work is pending,
	// claimed, running, retrying, or was just enqueued or reopened.
	WorkloadMaterializationReplayScheduled WorkloadMaterializationReplayOutcome = "scheduled"
	// WorkloadMaterializationReplaySuperseded means the stable work item is
	// terminally superseded and the queue never revives it. The repo-dependency
	// runner reaches this outcome only after its freshness check read the
	// generation as current, so an owed materialization cannot run: it
	// re-checks freshness, skips only if the generation has since retired, and
	// otherwise fails the cycle closed and counts the anomaly.
	WorkloadMaterializationReplaySuperseded WorkloadMaterializationReplayOutcome = "superseded"
	// WorkloadMaterializationReplayNotScheduled means the replay could not be
	// scheduled for any other reason, such as a dead-lettered stable item.
	WorkloadMaterializationReplayNotScheduled WorkloadMaterializationReplayOutcome = "not_scheduled"
)

// WorkloadMaterializationOutcomeReplayer is the optional replayer extension
// that distinguishes a superseded stable work item from other unscheduled
// outcomes. A replayer without it reports only a boolean.
type WorkloadMaterializationOutcomeReplayer interface {
	ReplayWorkloadMaterializationOutcome(
		ctx context.Context,
		scopeID string,
		generationID string,
		entityKey string,
	) (WorkloadMaterializationReplayOutcome, error)
}
