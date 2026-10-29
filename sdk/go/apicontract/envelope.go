// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apicontract

// TruthLevel describes how directly evidence supports an answer.
type TruthLevel string

// Truth levels form the ordered evidence-authority scale.
const (
	// TruthLevelExact means authoritative evidence directly supports the answer.
	TruthLevelExact   TruthLevel = "exact"
	TruthLevelDerived TruthLevel = "derived"
	// TruthLevelPartial means a bounded read stopped at its work budget before
	// it could prove the answer complete. The rows returned are a true
	// prefix of the exact answer in order, the response carries a resume
	// cursor, and truncated is true. It is emitted only by routes that declare
	// a work budget; it ranks between derived and fallback.
	TruthLevelPartial  TruthLevel = "partial"
	TruthLevelFallback TruthLevel = "fallback"
)

// TruthBasis names the evidence source used to produce an answer.
type TruthBasis string

// Truth bases identify the evidence source for an answer.
const (
	// TruthBasisAuthoritativeGraph identifies canonical graph evidence.
	TruthBasisAuthoritativeGraph TruthBasis = "authoritative_graph"
	TruthBasisSemanticFacts      TruthBasis = "semantic_facts"
	TruthBasisContentIndex       TruthBasis = "content_index"
	TruthBasisHybrid             TruthBasis = "hybrid"
	TruthBasisRuntimeState       TruthBasis = "runtime_state"
	// TruthBasisNoBackendRead identifies a page produced without reading any
	// backend at all -- the empty page a scoped caller whose grant admits no
	// repository receives, answered from the grant alone. It is the one basis
	// that names an absence rather than an evidence source, and it exists so
	// such a page does not have to borrow a source it never touched:
	// content_index was previously reported, which claimed a content-store read
	// that never happened. Its truth level is fixed at TruthLevelFallback by
	// basisLevel: nothing was read, so nothing can be exact or derived.
	TruthBasisNoBackendRead TruthBasis = "no_backend_read"
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
	State      FreshnessState `json:"state"`
	ObservedAt string         `json:"observed_at,omitempty"`
	Detail     string         `json:"detail,omitempty"`
	// Cause names WHY the answer is not fresh, drawn from the closed
	// FreshnessCause enumeration. It is set only when a handler holds the
	// evidence for the cause (for example a readiness verdict or a
	// generation-pending signal) and is left empty otherwise; handlers MUST NOT
	// guess. Causality is not correctness: a cause explains a known lag, it does
	// not imply the answer is wrong. Attach it through WithFreshnessCause.
	Cause FreshnessCause `json:"cause,omitempty"`
	// NextCheck is the bounded follow-up call that drills into Cause (a status,
	// generation, coverage, citation, or queue surface). It is populated
	// alongside Cause by WithFreshnessCause and nil when no cause is set.
	NextCheck *FreshnessNextCheck `json:"next_check,omitempty"`
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
	// Truncated is true when the answer is a page of the rows the route held
	// and the rest can be read by paging (the MCP response-budget page sets it
	// together with data.next_offset). A page the handler itself cut at the
	// request limit reports data.truncated and leaves this key false. It is
	// absent on the wire when false, so a response that returned every row
	// keeps its existing bytes.
	Truncated bool `json:"truncated,omitempty"`
	// Omissions lists the response sections a route withheld or reduced to
	// handle rows under the caller's selection, so a partial answer never
	// reads as a complete one. Empty (and absent on the wire) when the
	// response carries every section in full.
	Omissions []TruthOmission `json:"omissions,omitempty"`
}

// TruthOmission names one response section the route did not return in full.
// Detail is "handles" when the section's rows were projected to their
// identity keys, "omitted" when the section key is absent, and
// "response_byte_budget" when the MCP dispatch response budget dropped rows
// from the end of the section so the answer is a page (the response then
// carries data.next_offset to resume from, or, on a rank-walk page,
// data.rank_walk.next_cursor); Total is the row count the route held for
// that section before the cut.
type TruthOmission struct {
	Section string `json:"section"`
	Detail  string `json:"detail"`
	Total   int    `json:"total"`
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
//
// Dropped from the core shape: the unexported retryable marker and its
// Retryable accessor. That marker is set only by core response writers for
// the Retry-After header decision; it never crosses the wire, and a client
// copy that can only ever report false would mislead.
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
