// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deltareader

// The delta generation inserts this comment, which moves the function below
// it down by three lines (#10022, #10049).

// deltaReaderMovedCaller calls a helper in a file the delta never touches.
// The delta inserts lines above it, so the function moves (#10049).
func deltaReaderMovedCaller() int {
	return deltaReaderLedgerTotal()
}
