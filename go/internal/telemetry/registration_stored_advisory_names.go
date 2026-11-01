// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "github.com/eshu-hq/eshu/go/internal/telemetry/contract"

// registerStoredAdvisoryNames appends the stored advisory package name read
// span (#9777), unconditionally at the end and independent of every earlier
// step.
func registerStoredAdvisoryNames() {
	spanNames = append(spanNames, contract.SpanStoredAdvisoryNamesRead)
}
