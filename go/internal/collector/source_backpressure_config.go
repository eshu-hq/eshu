// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Per-source reducer backpressure env contract (#10882 decision 3). These
// are the canonical names: go/cmd/ingester and the collector entrypoint
// template load through this package so the two cannot drift. Every
// threshold is operator-set: unset disables the leg, because the design
// record's numbers stay Proposed until S0/S1 measurement confirms them
// and no enforcement default ships unmeasured.
const (
	SourceBackpressureEngageAgeEnv     = "ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE"
	SourceBackpressureReleaseAgeEnv    = "ESHU_SOURCE_BACKPRESSURE_RELEASE_AGE"
	SourceBackpressureDepthFloorEnv    = "ESHU_SOURCE_BACKPRESSURE_DEPTH_FLOOR"
	SourceBackpressureGrantDelayEnv    = "ESHU_SOURCE_BACKPRESSURE_GRANT_DELAY"
	SourceBackpressureMaxEngagementEnv = "ESHU_SOURCE_BACKPRESSURE_MAX_ENGAGEMENT"
	SourceBackpressureSignalCacheEnv   = "ESHU_SOURCE_BACKPRESSURE_SIGNAL_CACHE_TTL"
)

// LoadSourceBackpressureConfig parses the per-source backpressure env
// contract. All thresholds unset is the disabled zero value; a partial
// set fails startup through the config validation, because a
// half-configured throttle is a misconfiguration, not a conservative
// default.
func LoadSourceBackpressureConfig(getenv func(string) string) (workflow.SourceBackpressureConfig, error) {
	var config workflow.SourceBackpressureConfig
	var err error
	if config.EngageAge, err = parseSourceBackpressureDuration(getenv, SourceBackpressureEngageAgeEnv); err != nil {
		return workflow.SourceBackpressureConfig{}, err
	}
	if config.ReleaseAge, err = parseSourceBackpressureDuration(getenv, SourceBackpressureReleaseAgeEnv); err != nil {
		return workflow.SourceBackpressureConfig{}, err
	}
	if config.GrantDelay, err = parseSourceBackpressureDuration(getenv, SourceBackpressureGrantDelayEnv); err != nil {
		return workflow.SourceBackpressureConfig{}, err
	}
	if config.MaxEngagement, err = parseSourceBackpressureDuration(getenv, SourceBackpressureMaxEngagementEnv); err != nil {
		return workflow.SourceBackpressureConfig{}, err
	}
	if config.SignalCacheTTL, err = parseSourceBackpressureDuration(getenv, SourceBackpressureSignalCacheEnv); err != nil {
		return workflow.SourceBackpressureConfig{}, err
	}
	rawFloor := strings.TrimSpace(getenv(SourceBackpressureDepthFloorEnv))
	if rawFloor != "" {
		floor, parseErr := strconv.ParseInt(rawFloor, 10, 64)
		if parseErr != nil {
			return workflow.SourceBackpressureConfig{}, fmt.Errorf("parse %s: %w", SourceBackpressureDepthFloorEnv, parseErr)
		}
		config.DepthFloor = floor
	}
	if err := config.Validate(); err != nil {
		return workflow.SourceBackpressureConfig{}, fmt.Errorf("source backpressure config: %w", err)
	}
	return config, nil
}

// parseSourceBackpressureDuration parses one optional duration env var; an
// unset value is zero and the whole-config validation decides whether the
// set is complete.
func parseSourceBackpressureDuration(getenv func(string) string, name string) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}
