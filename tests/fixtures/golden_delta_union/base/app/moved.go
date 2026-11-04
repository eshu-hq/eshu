// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deltareader

// deltaReaderMovedCaller calls a helper in a file the delta never touches.
// The delta inserts lines above it, so the function moves (#10049).
func deltaReaderMovedCaller() int {
	return deltaReaderLedgerTotal()
}
