package coordinator

import (
	"strings"
	"time"
)

const (
	targetClassConfiguredDirect = "configured_direct"
	targetClassOwnedPackage     = "owned_package"
	targetClassBroad            = "broad"

	packageRegistryTargetClassConfiguredDirect = targetClassConfiguredDirect
	packageRegistryTargetClassOwnedPackage     = targetClassOwnedPackage
	packageRegistryTargetClassBroad            = targetClassBroad

	vulnerabilityTargetClassConfiguredDirect = targetClassConfiguredDirect
	vulnerabilityTargetClassOwnedPackage     = targetClassOwnedPackage
)

func targetClassRank(targetClass string) int {
	switch strings.TrimSpace(targetClass) {
	case targetClassConfiguredDirect:
		return 0
	case targetClassOwnedPackage:
		return 1
	case targetClassBroad:
		return 2
	default:
		return 3
	}
}

func targetCreatedAt(observedAt time.Time, ordinal int) time.Time {
	if ordinal < 0 {
		ordinal = 0
	}
	return observedAt.UTC().Add(time.Duration(ordinal) * time.Nanosecond)
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
