// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"strings"
)

const repoScheduledSyncEnabledEnv = "ESHU_REPO_SCHEDULED_SYNC_ENABLED"

// ScheduledSyncConfig controls whether the Git collector may run the normal
// repository selector when no higher-priority trigger work is available.
type ScheduledSyncConfig struct {
	Enabled bool
}

// LoadScheduledSyncConfig parses the scheduled repository refresh env contract.
func LoadScheduledSyncConfig(getenv func(string) string) (ScheduledSyncConfig, error) {
	if getenv == nil {
		return ScheduledSyncConfig{}, fmt.Errorf("scheduled sync getenv is required")
	}
	enabled, err := parseScheduledSyncEnabled(getenv(repoScheduledSyncEnabledEnv))
	if err != nil {
		return ScheduledSyncConfig{}, err
	}
	return ScheduledSyncConfig{Enabled: enabled}, nil
}

func parseScheduledSyncEnabled(raw string) (bool, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return true, nil
	}
	switch value {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean; accepted values: true, false, 1, 0, yes, no, on, off", repoScheduledSyncEnabledEnv)
	}
}
