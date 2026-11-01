// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "github.com/eshu-hq/eshu/go/internal/telemetry/contract"

// registerOwnedPackageResolver appends the owned-package name resolver span
// (#9777), unconditionally at the end and independent of every earlier step.
func registerOwnedPackageResolver() {
	spanNames = append(spanNames, contract.SpanOwnedPackageResolve)
}
