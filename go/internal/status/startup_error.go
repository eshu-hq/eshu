// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// startupReporter is implemented by a reader that can report an invalid
// configuration found when it was built, such as storage/postgres.StatusStore
// for an invalid ESHU_STATUS_SUMMARY_STALE_AFTER (#7009).
type startupReporter interface {
	StartupError() error
}

// ReaderStartupError returns the startup error reader reports, or nil. A
// decorator that wraps a Reader forwards its inner reader's error with this
// helper so a runtime fails at startup whatever wrappers sit between the store
// and the status server.
func ReaderStartupError(reader Reader) error {
	if reporter, ok := reader.(startupReporter); ok {
		return reporter.StartupError()
	}
	return nil
}
