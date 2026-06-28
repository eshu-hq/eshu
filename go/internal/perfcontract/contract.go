// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perfcontract

// Enforcement classifies how a threshold is checked.
type Enforcement string

const (
	// EnforcementHermeticGate marks a threshold whose runtime value is measured by
	// a hermetic, credential-free gate that already runs in CI (the hybrid
	// retrieval local-deterministic bars, measured in go/internal/searchbench).
	EnforcementHermeticGate Enforcement = "hermetic_gate"
	// EnforcementOperatorGated marks a threshold whose runtime measurement needs a
	// controlled environment (real corpus, live backend, consistent hardware) and
	// therefore the operator/remote validation run, not hermetic CI. The contract
	// lockstep below still keeps its documented number honest.
	EnforcementOperatorGated Enforcement = "operator_gated"
)

// Threshold is one published performance number bound to the document that
// states it. Doc is repo-relative; Phrase is the exact substring that must
// appear verbatim in Doc; Token is the value as it is written inside Phrase
// (e.g. "15s", "1.10x", "50 ms"); Value is the numeric form for programmatic
// consumers, in the stated Unit.
type Threshold struct {
	Name        string
	Doc         string
	Phrase      string
	Token       string
	Value       float64
	Unit        string
	Enforcement Enforcement
}

// Doc paths for the three published performance contracts, repo-relative.
const (
	DocLocalEnvelope   = "docs/public/reference/local-performance-envelope.md"
	DocClaimLatency    = "docs/public/reference/reducer-claim-latency-gate.md"
	DocHybridRetrieval = "docs/public/reference/hybrid-retrieval-production-gate.md"
)

// ContractThresholds returns every documented performance threshold across the
// three contracts. The lockstep test asserts each is still present in its doc and
// consistent with its in-code value.
func ContractThresholds() []Threshold {
	var out []Threshold
	out = append(out, localEnvelopeThresholds()...)
	out = append(out, reducerClaimLatencyThresholds()...)
	out = append(out, reducerHandlerBudgetThresholds()...)
	out = append(out, hybridRetrievalThresholds()...)
	return out
}
