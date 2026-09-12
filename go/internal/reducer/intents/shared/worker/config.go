// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cpubudget"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/inheritance"
)

// RunnerConfig holds configuration for the shared projection
// partition worker.
type RunnerConfig struct {
	PartitionCount int
	PollInterval   time.Duration
	LeaseTTL       time.Duration
	LeaseOwner     string
	BatchLimit     int
	EvidenceSource string
	Workers        int // concurrent partition workers; 0 or 1 means sequential
}

func (c RunnerConfig) partitionCount() int {
	if c.PartitionCount <= 0 {
		return defaultPartitionCount
	}
	return c.PartitionCount
}

func (c RunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return DefaultPollInterval
	}
	return c.PollInterval
}

func (c RunnerConfig) leaseTTL() time.Duration {
	if c.LeaseTTL <= 0 {
		return DefaultLeaseTTL
	}
	return c.LeaseTTL
}

func (c RunnerConfig) batchLimit() int {
	if c.BatchLimit <= 0 {
		return DefaultBatchLimit
	}
	return c.BatchLimit
}

func (c RunnerConfig) evidenceSource() string {
	if c.EvidenceSource == "" {
		return DefaultEvidenceSource
	}
	return c.EvidenceSource
}

func (c RunnerConfig) leaseOwner() string {
	if c.LeaseOwner == "" {
		return DefaultLeaseOwnerPrefix
	}
	return c.LeaseOwner
}

// sharedProjectionDomainEvidenceSource returns the evidence source the worker must
// stamp on a domain's edges, falling back to the runner's global source. Domains
// promoted onto the shared-projection runner from a dedicated materialization
// handler keep the handler's original evidence source so that, across an upgrade,
// the refresh/delta retract still matches the edges the old handler wrote (a
// mismatched source would leave stale edges un-retracted and change edge
// provenance). inheritance_edges keeps reducer/inheritance (#2867),
// rationale_edges keeps reducer/rationale (#5998), and the symbol→runtime domains
// have no pre-existing edges and stay on the runner's global source.
func sharedProjectionDomainEvidenceSource(domain, fallback string) string {
	switch domain {
	case reducercontract.DomainInheritanceEdges:
		return inheritance.EvidenceSource
	case reducercontract.DomainRationaleEdges:
		return reducercontract.RationaleEvidenceSource
	default:
		return fallback
	}
}

// LoadConfig parses shared projection env vars.
func LoadConfig(getenv func(string) string) RunnerConfig {
	return RunnerConfig{
		PartitionCount: intFromEnvDefault(getenv, "ESHU_SHARED_PROJECTION_PARTITION_COUNT", defaultPartitionCount),
		PollInterval:   durationFromEnv(getenv, "ESHU_SHARED_PROJECTION_POLL_INTERVAL", DefaultPollInterval),
		LeaseTTL:       durationFromEnv(getenv, "ESHU_SHARED_PROJECTION_LEASE_TTL", DefaultLeaseTTL),
		BatchLimit:     intFromEnvDefault(getenv, "ESHU_SHARED_PROJECTION_BATCH_LIMIT", DefaultBatchLimit),
		Workers:        intFromEnvDefault(getenv, "ESHU_SHARED_PROJECTION_WORKERS", defaultSharedProjectionWorkers()),
	}
}

// defaultSharedProjectionWorkers uses cpubudget.UsableCPUs() (cgroup-aware),
// not internal/runtime's UsableCPUs(): internal/reducer cannot import
// internal/runtime without an import cycle (internal/runtime -> internal/recovery
// -> internal/projector -> internal/reducer). cpubudget has zero internal
// dependencies, so it is safe to import here. ESHU_SHARED_PROJECTION_WORKERS
// remains the operator override for cgroup-limited containers.
func defaultSharedProjectionWorkers() int {
	n := cpubudget.UsableCPUs()
	if n > 4 {
		n = 4
	}
	if n < 1 {
		n = 1
	}
	return n
}

func intFromEnvDefault(getenv func(string) string, key string, defaultValue int) int {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultValue
	}
	return value
}

func durationFromEnv(getenv func(string) string, key string, defaultValue time.Duration) time.Duration {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return defaultValue
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return defaultValue
	}
	return value
}
