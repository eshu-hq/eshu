// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"time"
)

const (
	targetClassConfiguredDirect = "configured_direct"
	targetClassOwnedPackage     = "owned_package"
	targetClassInstalledOS      = "installed_os_package"
	targetClassSBOMComponent    = "sbom_component"
	targetClassBroad            = "broad"

	packageRegistryTargetClassConfiguredDirect = targetClassConfiguredDirect
	packageRegistryTargetClassOwnedPackage     = targetClassOwnedPackage
	packageRegistryTargetClassBroad            = targetClassBroad

	vulnerabilityTargetClassConfiguredDirect   = targetClassConfiguredDirect
	vulnerabilityTargetClassOwnedPackage       = targetClassOwnedPackage
	vulnerabilityTargetClassInstalledOSPackage = targetClassInstalledOS
	vulnerabilityTargetClassSBOMComponent      = targetClassSBOMComponent
)

func targetClassRank(targetClass string) int {
	switch strings.TrimSpace(targetClass) {
	case targetClassConfiguredDirect:
		return 0
	case targetClassOwnedPackage:
		return 1
	case targetClassInstalledOS, targetClassSBOMComponent:
		return 2
	case targetClassBroad:
		return 3
	default:
		return 4
	}
}

func targetCreatedAt(observedAt time.Time, ordinal int) time.Time {
	if ordinal < 0 {
		ordinal = 0
	}
	// Postgres TIMESTAMPTZ stores microseconds, so priority spacing must
	// survive that precision before FIFO falls back to work_item_id.
	return observedAt.UTC().Add(time.Duration(ordinal) * time.Microsecond)
}

func packageRegistryConfiguredTargetClass(target packageRegistryTargetConfiguration) string {
	for _, pkg := range target.Packages {
		if strings.TrimSpace(pkg) != "" {
			return packageRegistryTargetClassConfiguredDirect
		}
	}
	return packageRegistryTargetClassBroad
}

func packageRegistryTargetClass(target packageRegistryTargetConfiguration) string {
	if target.TargetClass != "" {
		return strings.TrimSpace(target.TargetClass)
	}
	if target.Derived {
		return packageRegistryTargetClassOwnedPackage
	}
	return packageRegistryConfiguredTargetClass(target)
}

func vulnerabilityTargetClass(target vulnerabilityTargetConfiguration) string {
	if target.TargetClass != "" {
		return strings.TrimSpace(target.TargetClass)
	}
	if target.Derived {
		return vulnerabilityTargetClassOwnedPackage
	}
	return vulnerabilityTargetClassConfiguredDirect
}
