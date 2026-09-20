// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"log/slog"

	"golang.org/x/sys/unix"
)

// EnableChildSubreaper registers this process as a child subreaper so
// orphaned grandchildren reparent to it instead of init. In containers this
// process normally is PID 1 already, which receives orphans regardless; the
// call is belt and braces for supervisor-nested layouts. A nil logger
// disables log output.
func EnableChildSubreaper(logger *slog.Logger) error {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return err
	}
	if logger != nil {
		logger.Info("child subreaper enabled, adopted zombies will reparent here")
	}
	return nil
}
