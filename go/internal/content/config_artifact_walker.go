// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// ConfigArtifactWalkerEnabledEnv is the walker's own kill switch (#8155).
	// The content Write's ConfigArtifactDeriveEnabledEnv does not gate the
	// reducer walker. Unset means enabled: the chain needs the walker running
	// to backfill before the PR3c read cutover, and rollback stays a flag
	// flip to false (readers fall back live for repositories the walker has
	// not derived yet). Read once at reducer start.
	ConfigArtifactWalkerEnabledEnv = "ESHU_CONFIG_ARTIFACT_WALKER_ENABLED"
	// ConfigArtifactWalkerWorkersEnv bounds the walker's derive workers per
	// process (K in the record, default 4). Read once at reducer start.
	ConfigArtifactWalkerWorkersEnv = "ESHU_CONFIG_ARTIFACT_WALKER_WORKERS"
	// DefaultConfigArtifactWalkerWorkers is K, the walker's derive workers
	// per process (Proposed in the record).
	DefaultConfigArtifactWalkerWorkers = 4
)

// ConfigArtifactWalkerConfig holds the reducer start-time settings of the
// config-artifact walker (#8155).
type ConfigArtifactWalkerConfig struct {
	// Enabled reports whether the reducer runs the walker. When false the
	// reducer builds no walker and the stored rows fall behind until the
	// content Writes' own derives or a later walker run repairs them.
	Enabled bool
	// Workers is the walker's derive workers per process. It is always
	// positive.
	Workers int
}

// LoadConfigArtifactWalkerConfig reads ConfigArtifactWalkerEnabledEnv and
// ConfigArtifactWalkerWorkersEnv through getenv. The reducer calls it once at
// start and keeps the result for the process lifetime; the walker never reads
// the environment per cycle.
//
// An unset or blank variable takes its default (enabled, 4 workers). A flag
// value strconv.ParseBool rejects, a worker count strconv.Atoi rejects, or a
// worker count below 1 returns an error, so the process fails at start
// instead of walking with a setting it did not ask for. The worker count is
// validated even when the walker is disabled, so turning the switch back on
// cannot surface a latent startup error. A nil getenv returns the defaults.
func LoadConfigArtifactWalkerConfig(getenv func(string) string) (ConfigArtifactWalkerConfig, error) {
	cfg := ConfigArtifactWalkerConfig{Enabled: true, Workers: DefaultConfigArtifactWalkerWorkers}
	if getenv == nil {
		return cfg, nil
	}

	if raw := strings.TrimSpace(getenv(ConfigArtifactWalkerEnabledEnv)); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return ConfigArtifactWalkerConfig{}, fmt.Errorf(
				"parse %s=%q: must be true or false: %w", ConfigArtifactWalkerEnabledEnv, raw, err)
		}
		cfg.Enabled = enabled
	}

	if raw := strings.TrimSpace(getenv(ConfigArtifactWalkerWorkersEnv)); raw != "" {
		workers, err := strconv.Atoi(raw)
		if err != nil {
			return ConfigArtifactWalkerConfig{}, fmt.Errorf(
				"parse %s=%q: must be a whole number of workers: %w", ConfigArtifactWalkerWorkersEnv, raw, err)
		}
		if workers < 1 {
			return ConfigArtifactWalkerConfig{}, fmt.Errorf(
				"parse %s=%q: must be at least 1", ConfigArtifactWalkerWorkersEnv, raw)
		}
		cfg.Workers = workers
	}
	return cfg, nil
}
