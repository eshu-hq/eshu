// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"strings"
)

const EnvelopeMIMEType = "application/eshu.envelope+json"

// CapabilityQueryPlaybooks identifies deterministic query playbook catalog and
// resolver reads. The capability returns workflow-plan truth, not live graph
// query truth.
const CapabilityQueryPlaybooks = "query.playbooks"

const semanticSearchCapability = "semantic_search.curated_retrieval"

type QueryProfile string

const (
	ProfileLocalLightweight   QueryProfile = "local_lightweight"
	ProfileLocalAuthoritative QueryProfile = "local_authoritative"
	ProfileLocalFullStack     QueryProfile = "local_full_stack"
	ProfileProduction         QueryProfile = "production"
)

type GraphBackend string

const (
	GraphBackendNeo4j    GraphBackend = "neo4j"
	GraphBackendNornicDB GraphBackend = "nornicdb"
)

// ParseGraphBackend validates the raw value against the supported adapter
// set. Empty is treated as the NornicDB default. Invalid non-empty values are
// rejected.
func ParseGraphBackend(raw string) (GraphBackend, error) {
	switch GraphBackend(strings.TrimSpace(raw)) {
	case "":
		return GraphBackendNornicDB, nil
	case GraphBackendNeo4j:
		return GraphBackendNeo4j, nil
	case GraphBackendNornicDB:
		return GraphBackendNornicDB, nil
	default:
		return "", fmt.Errorf("invalid graph backend %q", strings.TrimSpace(raw))
	}
}

type TruthLevel string

const (
	TruthLevelExact    TruthLevel = "exact"
	TruthLevelDerived  TruthLevel = "derived"
	TruthLevelFallback TruthLevel = "fallback"
)

type TruthBasis string

const (
	TruthBasisAuthoritativeGraph TruthBasis = "authoritative_graph"
	TruthBasisSemanticFacts      TruthBasis = "semantic_facts"
	TruthBasisContentIndex       TruthBasis = "content_index"
	TruthBasisHybrid             TruthBasis = "hybrid"
	TruthBasisRuntimeState       TruthBasis = "runtime_state"
)

type FreshnessState string

const (
	FreshnessFresh       FreshnessState = "fresh"
	FreshnessStale       FreshnessState = "stale"
	FreshnessBuilding    FreshnessState = "building"
	FreshnessUnavailable FreshnessState = "unavailable"
)

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

type TruthEnvelope struct {
	Level      TruthLevel     `json:"level"`
	Capability string         `json:"capability,omitempty"`
	Profile    QueryProfile   `json:"profile,omitempty"`
	Basis      TruthBasis     `json:"basis,omitempty"`
	Backend    GraphBackend   `json:"backend,omitempty"`
	Freshness  TruthFreshness `json:"freshness"`
	Reason     string         `json:"reason,omitempty"`
}

type ErrorProfiles struct {
	Current  QueryProfile `json:"current,omitempty"`
	Required QueryProfile `json:"required,omitempty"`
}

type ErrorCode string

const (
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

type ErrorEnvelope struct {
	Code          ErrorCode      `json:"code"`
	Message       string         `json:"message"`
	Capability    string         `json:"capability,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Profiles      *ErrorProfiles `json:"profiles,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
}

type ResponseEnvelope struct {
	Data  any            `json:"data"`
	Truth *TruthEnvelope `json:"truth"`
	Error *ErrorEnvelope `json:"error"`
}

type capabilitySupport struct {
	LocalLightweightMax   *TruthLevel
	LocalAuthoritativeMax *TruthLevel
	LocalFullStackMax     *TruthLevel
	ProductionMax         *TruthLevel
	RequiredProfile       QueryProfile
}

func NormalizeQueryProfile(raw string) QueryProfile {
	profile, err := ParseQueryProfile(raw)
	if err != nil {
		return ""
	}
	return profile
}

func ParseQueryProfile(raw string) (QueryProfile, error) {
	switch QueryProfile(strings.TrimSpace(raw)) {
	case "":
		return "", nil
	case ProfileLocalLightweight:
		return ProfileLocalLightweight, nil
	case ProfileLocalAuthoritative:
		return ProfileLocalAuthoritative, nil
	case ProfileLocalFullStack:
		return ProfileLocalFullStack, nil
	case ProfileProduction:
		return ProfileProduction, nil
	default:
		return "", fmt.Errorf("invalid query profile %q", strings.TrimSpace(raw))
	}
}

func acceptsEnvelope(r *http.Request) bool {
	if r == nil {
		return false
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, EnvelopeMIMEType)
}

func maxTruthLevel(capability string, profile QueryProfile) *TruthLevel {
	support, ok := capabilityMatrix[capability]
	if !ok {
		return nil
	}
	switch profile {
	case ProfileLocalLightweight:
		return support.LocalLightweightMax
	case ProfileLocalAuthoritative:
		if support.LocalAuthoritativeMax != nil {
			return support.LocalAuthoritativeMax
		}
		// Fallback: treat authoritative-local as at least as capable as
		// lightweight when a row has not been explicitly bumped yet. This
		// keeps the Go matrix tolerant during migration; the YAML remains
		// authoritative and the matrix test catches drift.
		return support.LocalLightweightMax
	case ProfileLocalFullStack:
		return support.LocalFullStackMax
	case ProfileProduction:
		return support.ProductionMax
	default:
		return support.ProductionMax
	}
}

func requiredProfile(capability string) QueryProfile {
	support, ok := capabilityMatrix[capability]
	if !ok || support.RequiredProfile == "" {
		return ProfileLocalFullStack
	}
	return support.RequiredProfile
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
	rank := map[TruthLevel]int{
		TruthLevelExact:    3,
		TruthLevelDerived:  2,
		TruthLevelFallback: 1,
	}
	if rank[a] <= rank[b] {
		return a
	}
	return b
}

func BuildTruthEnvelope(profile QueryProfile, capability string, basis TruthBasis, reason string) *TruthEnvelope {
	if _, ok := capabilityMatrix[capability]; !ok {
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

func normalizeTruthBasis(profile QueryProfile, basis TruthBasis) TruthBasis {
	if profile == ProfileLocalLightweight && basis == TruthBasisAuthoritativeGraph {
		return TruthBasisHybrid
	}
	return basis
}
