// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package content

import (
	"fmt"
	"strings"
)

const (
	// ConfigArtifactReadModeEnv selects the config-artifact read mode of the
	// API (#8155 PR3c): live serves the query-time extractor as today,
	// shadow serves live while comparing each related repository against its
	// stored rows, and stored serves the stored rows with a live fallback per
	// repository. Read once at API start. Unset or blank means live.
	ConfigArtifactReadModeEnv = "ESHU_CONFIG_ARTIFACT_READ_MODE"
)

// ReadMode is the config-artifact read mode of the API (#8155 PR3c). The zero
// value is live, so a loader that never sets the mode serves exactly today's
// read.
type ReadMode string

const (
	// ReadModeLive serves the query-time extractor for every repository.
	ReadModeLive ReadMode = "live"
	// ReadModeShadow serves the live answer and compares each related
	// repository against its stored rows, reporting mismatches without
	// changing the response.
	ReadModeShadow ReadMode = "shadow"
	// ReadModeStored serves the stored rows of trusted repositories and the
	// live extractor for every other repository.
	ReadModeStored ReadMode = "stored"
)

// ParseReadMode maps a raw mode name to its ReadMode. Blank means live. An
// unknown name is an error, so the process fails at start instead of serving
// a mode it did not ask for. Matching is case-insensitive after trimming
// space.
func ParseReadMode(raw string) (ReadMode, error) {
	switch ReadMode(strings.ToLower(strings.TrimSpace(raw))) {
	case "", ReadModeLive:
		return ReadModeLive, nil
	case ReadModeShadow:
		return ReadModeShadow, nil
	case ReadModeStored:
		return ReadModeStored, nil
	default:
		return "", fmt.Errorf("parse %s=%q: must be live, shadow or stored", ConfigArtifactReadModeEnv, raw)
	}
}

// ConfigArtifactReadConfig holds the API start-time settings of the
// config-artifact read path (#8155 PR3c).
type ConfigArtifactReadConfig struct {
	// Mode selects the read path. It is always one of the three modes.
	Mode ReadMode
}

// LoadConfigArtifactReadConfig reads ConfigArtifactReadModeEnv through
// getenv. A binary calls it once at start and keeps the result for the
// process lifetime; the read path never reads the environment per request.
// An unset or blank variable means live. An unknown value returns an error,
// so the process fails at start. A nil getenv returns the defaults.
func LoadConfigArtifactReadMode(getenv func(string) string) (ConfigArtifactReadConfig, error) {
	cfg := ConfigArtifactReadConfig{Mode: ReadModeLive}
	if getenv == nil {
		return cfg, nil
	}
	mode, err := ParseReadMode(getenv(ConfigArtifactReadModeEnv))
	if err != nil {
		return ConfigArtifactReadConfig{}, err
	}
	cfg.Mode = mode
	return cfg, nil
}
