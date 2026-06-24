// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extraction

import "github.com/eshu-hq/eshu/go/internal/scope"

// Classification is the advisory verdict for whether a collector family should
// move out of the core Eshu repository. It is informational: it never moves
// code, disables a collector, or changes runtime behavior.
type Classification string

const (
	// KeepInTree marks a correlation-critical core collector that creates or
	// preserves code-to-cloud join keys. It stays in tree regardless of the SDK
	// and runtime criteria; moving it needs a separate architecture gate.
	KeepInTree Classification = "keep_in_tree"
	// ExtractionCandidate marks a vendor-API or support-source collector that is
	// eligible to move out of tree and has no unmet extraction criteria, but has
	// not completed (or has not been promoted by) a full out-of-tree proof.
	ExtractionCandidate Classification = "extraction_candidate"
	// Blocked marks a collector that is an extraction candidate by family but has
	// at least one unmet extraction criterion. The unmet criteria are reported as
	// blockers so contributors know exactly what to close.
	Blocked Classification = "blocked"
	// ExternalReady marks a collector whose out-of-tree proof is complete and that
	// runs out of tree as its default path. It is the terminal extraction state.
	ExternalReady Classification = "external_ready"
)

// Valid reports whether the classification is a recognized value.
func (c Classification) Valid() bool {
	switch c {
	case KeepInTree, ExtractionCandidate, Blocked, ExternalReady:
		return true
	default:
		return false
	}
}

// Criterion identifies one row of the collector extraction policy. The set
// mirrors the "Extraction Criteria" table in
// docs/public/reference/collector-extraction-policy.md.
type Criterion string

const (
	// SourceCoupling: the collector depends only on external source APIs or
	// artifacts plus public SDK contracts, not Eshu internal packages.
	SourceCoupling Criterion = "source_coupling"
	// FactContract: every emitted fact kind, schema version, confidence, stable
	// key, redacted payload, and downstream consumer is documented.
	FactContract Criterion = "fact_contract"
	// ScopeGeneration: the collector has durable scope and generation identity
	// supporting retry, replay, stale-state handling, and idempotent re-emission.
	ScopeGeneration Criterion = "scope_generation"
	// TrustBoundary: manifest, compatible core range, digest-pinned artifact,
	// publisher, revocation behavior, and trust mode are documented.
	TrustBoundary Criterion = "trust_boundary"
	// RuntimeBehavior: the hosted path has bounded claims, read-only credentials,
	// resource limits, retry/dead-letter behavior, health, readiness, metrics,
	// status, and logs.
	RuntimeBehavior Criterion = "runtime_behavior"
	// ReleaseCadence: vendor or source-format churn is independent enough that a
	// separate release cadence helps more than it harms correlation correctness.
	ReleaseCadence Criterion = "release_cadence"
	// ProofSurface: fixture conformance, remote Compose proof, reducer admission,
	// graph/query truth, and private-data handling all pass.
	ProofSurface Criterion = "proof_surface"
)

// orderedCriteria is the canonical, stable order criteria are reported in.
var orderedCriteria = []Criterion{
	SourceCoupling,
	FactContract,
	ScopeGeneration,
	TrustBoundary,
	RuntimeBehavior,
	ReleaseCadence,
	ProofSurface,
}

// Criteria returns the canonical extraction criteria in stable reporting order.
func Criteria() []Criterion {
	out := make([]Criterion, len(orderedCriteria))
	copy(out, orderedCriteria)
	return out
}

// Valid reports whether the criterion is a recognized policy row.
func (c Criterion) Valid() bool {
	for _, known := range orderedCriteria {
		if c == known {
			return true
		}
	}
	return false
}

// State is the evidence state of a single criterion for a collector family.
type State string

const (
	// Met means the criterion's required evidence exists.
	Met State = "met"
	// Unmet means the criterion is required for this family but its evidence does
	// not yet exist. Unmet criteria block extraction.
	Unmet State = "unmet"
	// NotApplicable means the criterion does not gate this family, for example
	// the SDK and trust criteria for a keep-in-tree core collector.
	NotApplicable State = "not_applicable"
)

// Valid reports whether the state is a recognized value.
func (s State) Valid() bool {
	switch s {
	case Met, Unmet, NotApplicable:
		return true
	default:
		return false
	}
}

// CriterionResult is the evaluated state of one extraction criterion.
type CriterionResult struct {
	// Criterion is the policy row this result describes.
	Criterion Criterion `json:"criterion"`
	// State is the evidence state of the criterion.
	State State `json:"state"`
	// Detail is a stable, operator-facing reason for the state. It never carries
	// secrets, hostnames, or machine-local paths.
	Detail string `json:"detail,omitempty"`
}

// Profile is the evidence-based input describing one collector family. Profiles
// are authored from documented repo evidence, not inferred at runtime, so the
// classification is reproducible and reviewable.
type Profile struct {
	// Family is the canonical collector kind this profile describes.
	Family scope.CollectorKind `json:"family"`
	// DisplayName is a human label for the family.
	DisplayName string `json:"display_name"`
	// CorrelationCritical marks a core collector that creates or preserves
	// code-to-cloud join keys. Such collectors classify as KeepInTree regardless
	// of the criteria below.
	CorrelationCritical bool `json:"correlation_critical"`
	// BoundaryProofComplete is true when the out-of-tree reference proof
	// (packaging, hosted claim execution, reducer admission, API/MCP readback)
	// has landed with tracked evidence.
	BoundaryProofComplete bool `json:"boundary_proof_complete"`
	// Extracted is true only when the family actually runs out of tree as its
	// default path, not merely when it is proven extractable.
	Extracted bool `json:"extracted"`
	// Criteria is the per-criterion checklist for the family. A complete profile
	// covers every criterion returned by Criteria.
	Criteria []CriterionResult `json:"criteria"`
	// Rationale is an optional stable note appended to the classification reason.
	Rationale string `json:"rationale,omitempty"`
}

// Readiness is the advisory extraction-readiness verdict for one collector
// family, including the per-criterion checklist and any blockers.
type Readiness struct {
	// Family is the canonical collector kind.
	Family scope.CollectorKind `json:"family"`
	// DisplayName is the human label for the family.
	DisplayName string `json:"display_name"`
	// Classification is the advisory verdict.
	Classification Classification `json:"classification"`
	// Criteria is the per-criterion checklist in canonical order.
	Criteria []CriterionResult `json:"criteria"`
	// Blockers lists the criteria whose state is Unmet. It is empty unless the
	// classification is Blocked.
	Blockers []CriterionResult `json:"blockers,omitempty"`
	// Rationale explains why the family received its classification.
	Rationale string `json:"rationale"`
}
