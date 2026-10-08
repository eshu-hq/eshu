// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// LivenessWindowEnv configures how long a selector's observation rows stay
// live after its newest evaluation. Every row a collector writes carries the
// window, and the freshness reader ignores rows older than it, so a selector
// that stops evaluating fails open to an unknown selection state instead of
// keeping stale evidence.
const LivenessWindowEnv = "ESHU_REPO_SELECTION_LIVENESS_WINDOW"

const (
	// DefaultLivenessWindow is the window when LivenessWindowEnv is unset:
	// twice the slowest full QA re-index cycle observed for #7625.
	DefaultLivenessWindow = 48 * time.Hour
	// MinimumLivenessWindow is the smallest window LivenessWindowEnv accepts.
	MinimumLivenessWindow = time.Hour
	// maximumLivenessWindow keeps liveness_window_seconds inside its INTEGER
	// column.
	maximumLivenessWindow = time.Duration(math.MaxInt32) * time.Second
)

// livenessWindow applies the DefaultLivenessWindow fallback to a configured
// window and truncates it to whole seconds, the stored precision.
func livenessWindow(configured time.Duration) time.Duration {
	if configured <= 0 {
		configured = DefaultLivenessWindow
	}
	return configured.Truncate(time.Second)
}

// ParseLivenessWindow parses a LivenessWindowEnv value as a Go duration. A
// blank value yields DefaultLivenessWindow. A value that does not parse, is
// below MinimumLivenessWindow, or does not fit the stored INT seconds column
// is an error that names the variable, so config load fails before the
// collector starts.
func ParseLivenessWindow(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultLivenessWindow, nil
	}
	window, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q is not a Go duration such as 48h: %w", LivenessWindowEnv, raw, err)
	}
	if window < MinimumLivenessWindow {
		return 0, fmt.Errorf("%s=%q is below the %s minimum", LivenessWindowEnv, raw, MinimumLivenessWindow)
	}
	if window > maximumLivenessWindow {
		return 0, fmt.Errorf("%s=%q exceeds the %s maximum", LivenessWindowEnv, raw, maximumLivenessWindow)
	}
	return window, nil
}
