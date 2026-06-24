// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"runtime"
	"strconv"
	"strings"
	"time"
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

func defaultSharedProjectionWorkers() int {
	n := runtime.NumCPU()
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
