// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import (
	"strings"
	"time"
)

// Target classes rank how specific a scheduled target is. A collector that
// names its subject outranks one derived from owned packages, which outranks
// one derived from installed or SBOM evidence, which outranks a broad sweep.
const (
	TargetClassConfiguredDirect = "configured_direct"
	TargetClassOwnedPackage     = "owned_package"
	TargetClassInstalledOS      = "installed_os_package"
	TargetClassSBOMComponent    = "sbom_component"
	TargetClassBroad            = "broad"
)

// TargetClassRank orders target classes from most to least specific. An
// unrecognized class ranks last so it can never preempt a known one.
func TargetClassRank(targetClass string) int {
	switch strings.TrimSpace(targetClass) {
	case TargetClassConfiguredDirect:
		return 0
	case TargetClassOwnedPackage:
		return 1
	case TargetClassInstalledOS, TargetClassSBOMComponent:
		return 2
	case TargetClassBroad:
		return 3
	default:
		return 4
	}
}

// TargetCreatedAt spaces the ordinal-th target of one plan far enough apart in
// time to survive storage precision, so FIFO ordering is decided by priority
// rather than by an arbitrary tiebreak.
func TargetCreatedAt(observedAt time.Time, ordinal int) time.Time {
	if ordinal < 0 {
		ordinal = 0
	}
	// Postgres TIMESTAMPTZ stores microseconds, so priority spacing must
	// survive that precision before FIFO falls back to work_item_id.
	return observedAt.UTC().Add(time.Duration(ordinal) * time.Microsecond)
}
