// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "fmt"

// TruthLevel describes how directly evidence supports an answer.
type TruthLevel string

// Truth levels form the ordered evidence-authority scale.
const (
	// TruthLevelExact means authoritative evidence directly supports the answer.
	TruthLevelExact    TruthLevel = "exact"
	TruthLevelDerived  TruthLevel = "derived"
	TruthLevelFallback TruthLevel = "fallback"
)

// TruthBasis names the evidence source used to produce an answer.
type TruthBasis string

// Truth bases identify the evidence source used for an answer.
const (
	// TruthBasisAuthoritativeGraph identifies canonical graph evidence.
	TruthBasisAuthoritativeGraph TruthBasis = "authoritative_graph"
	TruthBasisSemanticFacts      TruthBasis = "semantic_facts"
	TruthBasisContentIndex       TruthBasis = "content_index"
	TruthBasisHybrid             TruthBasis = "hybrid"
	TruthBasisRuntimeState       TruthBasis = "runtime_state"
)

// FreshnessState reports whether the evidence is current and available.
type FreshnessState string

// Freshness states describe current evidence availability.
const (
	// FreshnessFresh means the served evidence is current.
	FreshnessFresh       FreshnessState = "fresh"
	FreshnessStale       FreshnessState = "stale"
	FreshnessBuilding    FreshnessState = "building"
	FreshnessUnavailable FreshnessState = "unavailable"
)

// TruthFreshness carries the freshness state and a proven cause when known.
type TruthFreshness struct {
	State      FreshnessState      `json:"state"`
	ObservedAt string              `json:"observed_at,omitempty"`
	Detail     string              `json:"detail,omitempty"`
	Cause      FreshnessCause      `json:"cause,omitempty"`
	NextCheck  *FreshnessNextCheck `json:"next_check,omitempty"`
}

// TruthEnvelope carries query capability, evidence, and freshness metadata.
type TruthEnvelope struct {
	Level      TruthLevel     `json:"level"`
	Capability string         `json:"capability,omitempty"`
	Profile    QueryProfile   `json:"profile,omitempty"`
	Basis      TruthBasis     `json:"basis,omitempty"`
	Backend    GraphBackend   `json:"backend,omitempty"`
	Freshness  TruthFreshness `json:"freshness"`
	Reason     string         `json:"reason,omitempty"`
}

// ErrorProfiles names the active and minimum required profiles.
type ErrorProfiles struct {
	Current  QueryProfile `json:"current,omitempty"`
	Required QueryProfile `json:"required,omitempty"`
}

// ErrorCode is a stable machine-readable query error code.
type ErrorCode string

// Error codes form the stable machine-readable query error vocabulary.
const (
	// ErrorCodeUnsupportedCapability reports a profile-incompatible capability.
	ErrorCodeUnsupportedCapability        ErrorCode = "unsupported_capability"
	ErrorCodeAmbiguous                    ErrorCode = "ambiguous"
	ErrorCodeUnauthenticated              ErrorCode = "unauthenticated"
	ErrorCodeInvalidArgument              ErrorCode = "invalid_argument"
	ErrorCodeNotFound                     ErrorCode = "not_found"
	ErrorCodePermissionDenied             ErrorCode = "permission_denied"
	ErrorCodeBackendUnavailable           ErrorCode = "backend_unavailable"
	ErrorCodeBackendTimeout               ErrorCode = "backend_timeout"
	ErrorCodeIndexBuilding                ErrorCode = "index_building"
	ErrorCodeScopeNotFound                ErrorCode = "scope_not_found"
	ErrorCodeServiceNotFound              ErrorCode = "service_not_found"
	ErrorCodeCapabilityDegraded           ErrorCode = "capability_degraded"
	ErrorCodeOverloaded                   ErrorCode = "overloaded"
	ErrorCodeInternalError                ErrorCode = "internal_error"
	ErrorCodeReadModelUnavailable         ErrorCode = "documentation_read_model_unavailable"
	ErrorCodeComponentRegistryUnavailable ErrorCode = "component_registry_unavailable"
)

// ErrorEnvelope carries a stable query error and optional profile detail.
type ErrorEnvelope struct {
	Code          ErrorCode      `json:"code"`
	Message       string         `json:"message"`
	Capability    string         `json:"capability,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Profiles      *ErrorProfiles `json:"profiles,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
}

// ResponseEnvelope is the negotiated query response wire contract.
type ResponseEnvelope struct {
	Data  any            `json:"data"`
	Truth *TruthEnvelope `json:"truth"`
	Error *ErrorEnvelope `json:"error"`
}

// AnswerTruthClass is the prompt-facing classification of an answer's truth.
//
// It folds the two existing truth axes — TruthLevel (exact, derived, fallback)
// and TruthBasis (authoritative_graph, semantic_facts, content_index, hybrid)
// — into a single label so prompt surfaces can choose presentation and caution
// without re-implementing the capability matrix. It does not introduce a new
// truth source; it is derived entirely from an existing TruthEnvelope. The
// mapping is documented in docs/public/reference/answer-packets.md.
type AnswerTruthClass string

const (
	// AnswerTruthDeterministic marks authoritative graph truth: an exact
	// TruthLevel with an authoritative_graph basis. Safe to present as fact.
	AnswerTruthDeterministic AnswerTruthClass = "deterministic"
	// AnswerTruthDerived marks a deterministic result computed from indexed
	// entities, content, or relational state rather than authoritative graph
	// topology.
	AnswerTruthDerived AnswerTruthClass = "derived"
	// AnswerTruthFallback marks an exploratory result that is useful but not
	// authoritative for the capability.
	AnswerTruthFallback AnswerTruthClass = "fallback"
	// AnswerTruthSemanticObservation marks durable semantic truth from facts
	// (an exact TruthLevel with a semantic_facts basis) rather than graph
	// topology.
	AnswerTruthSemanticObservation AnswerTruthClass = "semantic_observation"
	// AnswerTruthCodeHint marks a content-index or search signal: a hint, not a
	// verified relationship.
	AnswerTruthCodeHint AnswerTruthClass = "code_hint"
	// AnswerTruthUnsupported marks an answer with no truth to classify, built
	// from an ErrorEnvelope or missing required evidence.
	AnswerTruthUnsupported AnswerTruthClass = "unsupported"
)

// BuildTruthEnvelope builds truth metadata from the registered capability ceiling.
func BuildTruthEnvelope(profile QueryProfile, capability string, basis TruthBasis, reason string) *TruthEnvelope {
	if _, ok := CapabilitySupportFor(capability); !ok {
		panic(fmt.Sprintf("query capability %q missing from capability matrix", capability))
	}
	basis = normalizeTruthBasis(profile, basis)
	maxLevel := maxTruthLevel(capability, profile)
	level := basisLevel(basis)
	if maxLevel != nil {
		level = minTruthLevel(level, *maxLevel)
	}
	return &TruthEnvelope{
		Level:      level,
		Capability: capability,
		Profile:    profile,
		Basis:      basis,
		Freshness:  TruthFreshness{State: FreshnessFresh},
		Reason:     reason,
	}
}

func basisLevel(basis TruthBasis) TruthLevel {
	switch basis {
	case TruthBasisAuthoritativeGraph, TruthBasisSemanticFacts, TruthBasisRuntimeState:
		return TruthLevelExact
	case TruthBasisContentIndex, TruthBasisHybrid:
		return TruthLevelDerived
	default:
		return TruthLevelFallback
	}
}

func minTruthLevel(a, b TruthLevel) TruthLevel {
	rank := map[TruthLevel]int{TruthLevelExact: 3, TruthLevelDerived: 2, TruthLevelFallback: 1}
	if rank[a] <= rank[b] {
		return a
	}
	return b
}

func normalizeTruthBasis(profile QueryProfile, basis TruthBasis) TruthBasis {
	if profile == ProfileLocalLightweight && basis == TruthBasisAuthoritativeGraph {
		return TruthBasisHybrid
	}
	return basis
}
