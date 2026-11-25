// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

// Proof seams (#10880 PR-C1). The live proofs of the supports-backed reads run in
// the storage package, which cannot see this package's unexported statements.
// These functions hand them the statement text and nothing else: no connection, no
// behavior. They exist so a live equality proof runs the statements the writer runs,
// not a copy. Product code does not call them.

// ProofStoredFindingsQuery returns the supports-backed stored read the writer runs
// (arguments $1 scope id, $2 generation id).
func ProofStoredFindingsQuery() string { return listStoredSupplyChainImpactFindingsQuery }

// ProofRecoveryCVEIDsQuery returns the three-leg recovery read of the CVE ids
// (arguments $1 scope id, $2 generation id).
func ProofRecoveryCVEIDsQuery() string { return listSupplyChainImpactCVEIDsQuery }

// ProofRecoveryGroupIDsQuery returns the three-leg recovery read of the advisory
// group ids (arguments $1 scope id, $2 generation id).
func ProofRecoveryGroupIDsQuery() string { return listSupplyChainImpactGroupIDsQuery }

// ProofRecoveryCVEIDsFactsLegQuery returns the previous recovery statement, byte
// identical, which is also the third leg of ProofRecoveryCVEIDsQuery.
func ProofRecoveryCVEIDsFactsLegQuery() string { return listSupplyChainImpactCVEIDsFactsLegQuery }

// ProofRecoveryGroupIDsFactsLegQuery returns the previous group-id recovery
// statement, the third leg of ProofRecoveryGroupIDsQuery.
func ProofRecoveryGroupIDsFactsLegQuery() string { return listSupplyChainImpactGroupIDsFactsLegQuery }
