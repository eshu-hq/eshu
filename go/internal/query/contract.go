package query

import (
	"fmt"
	"net/http"
	"strings"
)

const EnvelopeMIMEType = "application/eshu.envelope+json"

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
	ErrorCodeUnsupportedCapability ErrorCode = "unsupported_capability"
	ErrorCodeUnauthenticated       ErrorCode = "unauthenticated"
	ErrorCodeInvalidArgument       ErrorCode = "invalid_argument"
	ErrorCodeNotFound              ErrorCode = "not_found"
	ErrorCodePermissionDenied      ErrorCode = "permission_denied"
	ErrorCodeBackendUnavailable    ErrorCode = "backend_unavailable"
	ErrorCodeIndexBuilding         ErrorCode = "index_building"
	ErrorCodeScopeNotFound         ErrorCode = "scope_not_found"
	ErrorCodeCapabilityDegraded    ErrorCode = "capability_degraded"
	ErrorCodeOverloaded            ErrorCode = "overloaded"
	ErrorCodeInternalError         ErrorCode = "internal_error"
	ErrorCodeReadModelUnavailable  ErrorCode = "documentation_read_model_unavailable"
)

type ErrorEnvelope struct {
	Code          ErrorCode      `json:"code"`
	Message       string         `json:"message"`
	Capability    string         `json:"capability,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Profiles      *ErrorProfiles `json:"profiles,omitempty"`
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

var (
	truthExact   = TruthLevelExact
	truthDerived = TruthLevelDerived
)

var capabilityMatrix = map[string]capabilitySupport{
	"code_search.exact_symbol": {
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	},
	"code_search.fuzzy_symbol": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
	},
	"code_search.symbol_lookup": {
		LocalLightweightMax:   &truthExact,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	},
	"code_inventory.structural": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
	},
	"code_search.variable_lookup": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	},
	"code_search.content_search": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
	},
	"code_search.topic_investigation": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
	},
	"graph_query.read_only_cypher": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"symbol_graph.decorators": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	},
	"symbol_graph.argument_names": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	},
	"symbol_graph.class_methods": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	},
	"call_graph.direct_callers": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	},
	"call_graph.direct_callees": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	},
	"call_graph.relationship_story": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	},
	"call_graph.transitive_callers": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"call_graph.transitive_callees": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"symbol_graph.imports": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	},
	"symbol_graph.inheritance": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	},
	"code_quality.complexity": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
	},
	"code_quality.refactoring": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"call_graph.call_chain_path": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"code_quality.dead_code": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"iac_quality.dead_iac": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"iac_management.find_unmanaged_resources": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"iac_management.get_status": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"iac_management.explain_status": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"iac_management.propose_terraform_import_plan": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"aws_runtime_drift.findings.list": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"platform_impact.deployment_chain": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"platform_impact.deployment_config_influence": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"platform_impact.context_overview": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"platform_impact.catalog": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
	},
	"platform_impact.blast_radius": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"platform_impact.change_surface": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"platform_impact.resource_to_code": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"platform_impact.resource_investigation": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"platform_impact.dependency_path": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"platform_impact.environment_compare": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"relationship_evidence.drilldown": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"evidence_citation.packet": {
		LocalLightweightMax:   &truthDerived,
		LocalAuthoritativeMax: &truthDerived,
		LocalFullStackMax:     &truthDerived,
		ProductionMax:         &truthDerived,
	},
	"documentation_findings.list": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"documentation_evidence_packet.read": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"documentation_evidence_packet.freshness": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"package_registry.packages.list": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"package_registry.versions.list": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
	"package_registry.dependencies.list": {
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	},
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
	case TruthBasisAuthoritativeGraph, TruthBasisSemanticFacts:
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
