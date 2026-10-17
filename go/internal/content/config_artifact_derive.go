// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// ConfigArtifactDeriveTimeoutEnv bounds one per-repository config-artifact
	// derive (#8155). The failure record runs after it on its own 5 s bound,
	// so a caller can wait up to this value plus 5 s.
	ConfigArtifactDeriveTimeoutEnv = "ESHU_CONFIG_ARTIFACT_DERIVE_TIMEOUT"
	// ConfigArtifactDeriveEnabledEnv is the kill switch of the derive the
	// content Write runs as its last step (#8155). It does not gate the
	// reducer walker, which gets its own variable.
	ConfigArtifactDeriveEnabledEnv = "ESHU_CONFIG_ARTIFACT_DERIVE_ENABLED"
	// DefaultConfigArtifactDeriveTimeout is the derive deadline when
	// ConfigArtifactDeriveTimeoutEnv is unset.
	DefaultConfigArtifactDeriveTimeout = 10 * time.Second
)

// ConfigArtifactDeriveConfig holds the start-time settings of the
// config-artifact derive that a content-writing binary (ingester, projector,
// bootstrap-index) builds its deriver from.
type ConfigArtifactDeriveConfig struct {
	// Timeout bounds the derive steps of one repository. It is always
	// positive.
	Timeout time.Duration
	// Enabled reports whether the content Write derives. When false the
	// binary builds no deriver and logs config_artifacts.derive.disabled
	// once at start.
	Enabled bool
}

// LoadConfigArtifactDeriveConfig reads ConfigArtifactDeriveTimeoutEnv and
// ConfigArtifactDeriveEnabledEnv through getenv. A binary calls it once at
// start, beside LoadWriterConfig, and keeps the result for the process
// lifetime; the derive never reads the environment per call. Nothing calls it
// yet: PR3b-2c-2 wires it into the three content-writing binaries.
//
// An unset or blank variable takes its default (10 s, enabled). A timeout of
// zero, a negative timeout, a value time.ParseDuration rejects (a bare number
// included), or a flag value strconv.ParseBool rejects returns an error, so
// the process fails at start instead of deriving with a deadline it did not
// ask for. A timeout of zero never means "no deadline". The timeout is
// validated even when the derive is disabled, so turning the switch back on
// cannot surface a latent startup error. A nil getenv returns the defaults.
func LoadConfigArtifactDeriveConfig(getenv func(string) string) (ConfigArtifactDeriveConfig, error) {
	cfg := ConfigArtifactDeriveConfig{Timeout: DefaultConfigArtifactDeriveTimeout, Enabled: true}
	if getenv == nil {
		return cfg, nil
	}

	if raw := strings.TrimSpace(getenv(ConfigArtifactDeriveTimeoutEnv)); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return ConfigArtifactDeriveConfig{}, fmt.Errorf(
				"parse %s=%q: must be a Go duration such as 10s: %w", ConfigArtifactDeriveTimeoutEnv, raw, err)
		}
		if timeout <= 0 {
			return ConfigArtifactDeriveConfig{}, fmt.Errorf(
				"parse %s=%q: must be positive", ConfigArtifactDeriveTimeoutEnv, raw)
		}
		cfg.Timeout = timeout
	}

	if raw := strings.TrimSpace(getenv(ConfigArtifactDeriveEnabledEnv)); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return ConfigArtifactDeriveConfig{}, fmt.Errorf(
				"parse %s=%q: must be true or false: %w", ConfigArtifactDeriveEnabledEnv, raw, err)
		}
		cfg.Enabled = enabled
	}
	return cfg, nil
}
