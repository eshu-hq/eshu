// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packages

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
)

// Target classes a package-registry instance can plan. Derived targets carry
// the owned-package class; a configured target is direct when it names
// packages and broad when it sweeps a whole registry.
const (
	targetClassConfiguredDirect = schedule.TargetClassConfiguredDirect
	targetClassOwnedPackage     = schedule.TargetClassOwnedPackage
	targetClassBroad            = schedule.TargetClassBroad
)

// configuredTargetClass classifies a configured (non-derived) target by
// whether it names at least one package.
func configuredTargetClass(target packageRegistryTargetConfiguration) string {
	for _, pkg := range target.Packages {
		if strings.TrimSpace(pkg) != "" {
			return targetClassConfiguredDirect
		}
	}
	return targetClassBroad
}

// targetClass resolves the priority class of one planned target, preferring an
// explicitly configured class over the derived-or-configured inference.
func targetClass(target packageRegistryTargetConfiguration) string {
	if target.TargetClass != "" {
		return strings.TrimSpace(target.TargetClass)
	}
	if target.Derived {
		return targetClassOwnedPackage
	}
	return configuredTargetClass(target)
}
