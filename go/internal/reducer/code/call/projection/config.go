// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
)

// RunnerConfig configures the controlled code-calls lane.
type RunnerConfig struct {
	LeaseOwner          string
	PollInterval        time.Duration
	LeaseTTL            time.Duration
	BatchLimit          int
	AcceptanceScanLimit int
	PartitionCount      int
	Workers             int
}

func (c RunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return worker.DefaultPollInterval
	}
	return c.PollInterval
}

func (c RunnerConfig) leaseTTL() time.Duration {
	if c.LeaseTTL <= 0 {
		return worker.DefaultLeaseTTL
	}
	return c.LeaseTTL
}

func (c RunnerConfig) batchLimit() int {
	if c.BatchLimit <= 0 {
		return worker.DefaultBatchLimit
	}
	return c.BatchLimit
}

func (c RunnerConfig) partitionCount() int {
	if c.PartitionCount <= 0 {
		return 1
	}
	return c.PartitionCount
}

func (c RunnerConfig) workers() int {
	if c.Workers <= 0 {
		return 1
	}
	if c.Workers > c.partitionCount() {
		return c.partitionCount()
	}
	return c.Workers
}

func (c RunnerConfig) acceptanceScanLimit() int {
	if c.AcceptanceScanLimit <= 0 {
		return DefaultAcceptanceScanLimit
	}
	if c.AcceptanceScanLimit < c.batchLimit() {
		return c.batchLimit()
	}
	return c.AcceptanceScanLimit
}

func (c RunnerConfig) leaseOwner() string {
	if c.LeaseOwner == "" {
		return DefaultLeaseOwnerPrefix
	}
	return c.LeaseOwner
}
