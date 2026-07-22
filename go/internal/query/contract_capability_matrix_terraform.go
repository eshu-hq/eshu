// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// This file adds one entry to capabilityMatrix (contract_capability_matrix.go)
// via init() rather than inline: contract_capability_matrix.go is already at
// the repository's 500-line file cap (it was itself split out of contract.go
// for the same reason), so a new capability entry belongs in its own file.
// Go initializes every package-level var before any init() runs, so
// capabilityMatrix is guaranteed non-nil here regardless of file order.
func init() {
	capabilityMatrix["terraform_config_state_drift.findings.list"] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
}
