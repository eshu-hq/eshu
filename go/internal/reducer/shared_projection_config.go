// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cpubudget"
)

func LoadSharedProjectionConfig(getenv func(string) string) SharedProjectionRunnerConfig {
	return SharedProjectionRunnerConfig{
		PartitionCount: intFromEnvDefault(getenv, "ESHU_SHARED_PROJECTION_PARTITION_COUNT", defaultPartitionCount),
		PollInterval:   durationFromEnv(getenv, "ESHU_SHARED_PROJECTION_POLL_INTERVAL", defaultSharedPollInterval),
		LeaseTTL:       durationFromEnv(getenv, "ESHU_SHARED_PROJECTION_LEASE_TTL", defaultLeaseTTL),
		BatchLimit:     intFromEnvDefault(getenv, "ESHU_SHARED_PROJECTION_BATCH_LIMIT", defaultBatchLimit),
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
