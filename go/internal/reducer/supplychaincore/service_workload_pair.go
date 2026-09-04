// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychaincore

// SupplyChainServiceWorkloadPair records one (ServiceID, WorkloadID) pair
// exactly as it appeared together on a single reducer_service_catalog_
// correlation fact (#5466 round-8 review F-2). It exists because
// finding.ServiceIDs/WorkloadIDs are independently flattened, deduplicated
// lists with no record of which value paired with which -- WorkloadIDs in
// particular mixes this genuinely-paired source with
// reducer_workload_identity's workload IDs, which have no known service at
// all. suppressionServiceWorkloadPairMatches
// (supply_chain_suppression_scope_match.go) uses this to verify a
// suppression scoped by BOTH workload_id and service_id names a combination
// that actually co-occurred, rather than two independently-true list
// memberships that never occurred together.
type SupplyChainServiceWorkloadPair struct {
	ServiceID  string
	WorkloadID string
}
