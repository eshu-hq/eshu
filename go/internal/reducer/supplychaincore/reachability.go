// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychaincore

// SupplyChainReachabilityState is the stable cross-language reachability
// enrichment state attached to a vulnerability finding. The state is
// prioritization metadata; it never changes impact truth.
type SupplyChainReachabilityState string

const (
	// SupplyChainReachabilityReachable means evidence proves the vulnerable
	// package, image component, package import, or symbol is reachable for the
	// scoped target.
	SupplyChainReachabilityReachable SupplyChainReachabilityState = "reachable"
	// SupplyChainReachabilityNotCalled means an ecosystem-specific analyzer
	// proved the vulnerable symbol or package is not called from an entrypoint.
	SupplyChainReachabilityNotCalled SupplyChainReachabilityState = "not_called"
	// SupplyChainReachabilityUnknown means Eshu has some target evidence but
	// cannot classify call/runtime reachability.
	SupplyChainReachabilityUnknown SupplyChainReachabilityState = "unknown"
	// SupplyChainReachabilityUnavailable means no implemented reachability
	// analyzer exists for the ecosystem/target shape.
	SupplyChainReachabilityUnavailable SupplyChainReachabilityState = "unavailable"
	// SupplyChainReachabilityMissingEvidence means a supported analyzer could
	// answer, but its evidence is missing from the current finding.
	SupplyChainReachabilityMissingEvidence SupplyChainReachabilityState = "missing_evidence"
)

// SupplyChainReachability describes the evidence and confidence behind one
// finding's reachability enrichment. Impact confidence remains on the parent
// finding so callers cannot mistake reachability for affected/not-affected
// truth.
type SupplyChainReachability struct {
	State            SupplyChainReachabilityState
	Confidence       string
	Source           string
	Evidence         string
	Reason           string
	LanguageMaturity string
	MissingEvidence  []string
}
