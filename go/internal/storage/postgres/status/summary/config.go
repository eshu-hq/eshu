// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"fmt"
	"strings"
	"time"
)

const (
	// ReadEnabledEnv turns the status reader's use of the stored summary on.
	ReadEnabledEnv = "ESHU_STATUS_SUMMARY_READ_ENABLED"
	// StaleAfterEnv sets the oldest row age the reader serves.
	StaleAfterEnv = "ESHU_STATUS_SUMMARY_STALE_AFTER"
)

const (
	// DefaultStaleAfter is three writer intervals (10 s each) plus the 2.09 s
	// replay-lag p95 measured on the ops-qa read replica, 32.09 s, rounded up.
	// A row older than this is never served as fresh.
	DefaultStaleAfter = 33 * time.Second
	// MinStaleAfter is the smallest accepted stale_after: one default writer
	// interval. A smaller value would send most reads to the live fallback,
	// which is slower than the stored row it is meant to replace.
	MinStaleAfter = 10 * time.Second
)

// ReadConfig is the status reader's setting for the stored summary.
type ReadConfig struct {
	// Enabled makes the status snapshot read the stored row. Off by default.
	Enabled bool
	// StaleAfter is the oldest age at which a stored row is still served.
	StaleAfter time.Duration
}

// LoadReadConfig reads ESHU_STATUS_SUMMARY_READ_ENABLED and
// ESHU_STATUS_SUMMARY_STALE_AFTER. The flag parses like the writer's: an
// unset or unrecognized value is off. The stale_after value is validated only
// while the reader is on: an unparsable value or one below MinStaleAfter is an
// error then, because a typo must not quietly serve a different bound, and it
// is ignored while off so a deployment that never enabled the reader cannot
// break its status routes with it.
func LoadReadConfig(getenv func(string) string) (ReadConfig, error) {
	cfg := ReadConfig{StaleAfter: DefaultStaleAfter}
	switch strings.ToLower(strings.TrimSpace(getenv(ReadEnabledEnv))) {
	case "1", "true", "t", "yes", "y", "on":
		cfg.Enabled = true
	}
	if !cfg.Enabled {
		return cfg, nil
	}
	raw := strings.TrimSpace(getenv(StaleAfterEnv))
	if raw == "" {
		return cfg, nil
	}
	staleAfter, err := time.ParseDuration(raw)
	if err != nil {
		return ReadConfig{}, fmt.Errorf("%s=%q: %w", StaleAfterEnv, raw, err)
	}
	if staleAfter < MinStaleAfter {
		return ReadConfig{}, fmt.Errorf("%s=%s is below the %s minimum", StaleAfterEnv, staleAfter, MinStaleAfter)
	}
	cfg.StaleAfter = staleAfter
	return cfg, nil
}
