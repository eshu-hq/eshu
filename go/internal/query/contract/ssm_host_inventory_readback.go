// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// This init registers the capability support row for the SSM host inventory
// readback surface (eshu-hq/eshu#9558). It MUST stay in sync with the matrix
// fragment specs/capability-matrix/ssm-host-inventory-readback.v1.yaml; the
// TestCapabilityMatrixMatchesYAMLContract gate fails if the Go row and the
// YAML row diverge. The readback joins committed graph observation nodes with
// admitted Postgres facts, so local_lightweight (no graph) and
// local_authoritative (no graph reads) are unsupported and the serving route
// returns unsupported_capability rather than a downgraded answer.
func init() {
	register(ssmHostInventoryReadbackCapability, capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: nil,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalFullStack,
	})
}
