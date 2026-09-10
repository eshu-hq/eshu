// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// WorkloadIdentityFactKind names the durable fact kind the workload identity
// writer publishes under. It is exported so the workload family can write it
// and the supplychain/core family can read it (active-fact-kind list,
// active-fact-kind filter, index build) without either importing the other,
// keeping the package-import direction strictly downward
// (root -> family -> shared-core -> contract). Hoisted ahead of the workload
// family move (#6061) because supplychain/core is the first family that needs
// it, exactly like PlatformMaterializationFactKind before it.
const WorkloadIdentityFactKind = "reducer_workload_identity"
