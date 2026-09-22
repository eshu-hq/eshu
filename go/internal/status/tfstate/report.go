// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstate

// Report projects the per-locator serial and recent warning
// evidence into a stable shape for the admin status surface. The Postgres
// query bounds raw inputs; this projection only sorts and groups them.
type Report struct {
	LastSerials    []LocatorSerial
	RecentWarnings []LocatorWarning
	WarningsByKind map[string]map[string][]LocatorWarning
	WarningSummary []WarningSummary
}
