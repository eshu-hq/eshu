// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !linux

package runtime

import (
	"log/slog"
)

// EnableChildSubreaper is a no-op outside Linux: only Linux supports
// PR_SET_CHILD_SUBREAPER, and only Linux exposes the /proc scan the reaper
// itself relies on. Callers keep a single wiring path across platforms.
func EnableChildSubreaper(logger *slog.Logger) error {
	if logger != nil {
		logger.Info("child subreaper unsupported on this platform, orphan reaping disabled")
	}
	return nil
}
