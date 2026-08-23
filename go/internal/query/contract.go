// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	EnvelopeMIMEType         = querycontract.EnvelopeMIMEType
	CapabilityQueryPlaybooks = "query.playbooks"
	semanticSearchCapability = "semantic_search.curated_retrieval"
)

type (
	QueryProfile      = querycontract.QueryProfile
	GraphBackend      = querycontract.GraphBackend
	TruthLevel        = querycontract.TruthLevel
	TruthBasis        = querycontract.TruthBasis
	FreshnessState    = querycontract.FreshnessState
	TruthFreshness    = querycontract.TruthFreshness
	TruthEnvelope     = querycontract.TruthEnvelope
	ErrorProfiles     = querycontract.ErrorProfiles
	ErrorCode         = querycontract.ErrorCode
	ErrorEnvelope     = querycontract.ErrorEnvelope
	ResponseEnvelope  = querycontract.ResponseEnvelope
	capabilitySupport = querycontract.CapabilitySupport
)

const (
	ProfileLocalLightweight   = querycontract.ProfileLocalLightweight
	ProfileLocalAuthoritative = querycontract.ProfileLocalAuthoritative
	ProfileLocalFullStack     = querycontract.ProfileLocalFullStack
	ProfileProduction         = querycontract.ProfileProduction

	GraphBackendNeo4j    = querycontract.GraphBackendNeo4j
	GraphBackendNornicDB = querycontract.GraphBackendNornicDB

	TruthLevelExact    = querycontract.TruthLevelExact
	TruthLevelDerived  = querycontract.TruthLevelDerived
	TruthLevelFallback = querycontract.TruthLevelFallback

	TruthBasisAuthoritativeGraph = querycontract.TruthBasisAuthoritativeGraph
	TruthBasisSemanticFacts      = querycontract.TruthBasisSemanticFacts
	TruthBasisContentIndex       = querycontract.TruthBasisContentIndex
	TruthBasisHybrid             = querycontract.TruthBasisHybrid
	TruthBasisRuntimeState       = querycontract.TruthBasisRuntimeState

	FreshnessFresh       = querycontract.FreshnessFresh
	FreshnessStale       = querycontract.FreshnessStale
	FreshnessBuilding    = querycontract.FreshnessBuilding
	FreshnessUnavailable = querycontract.FreshnessUnavailable

	ErrorCodeUnsupportedCapability        = querycontract.ErrorCodeUnsupportedCapability
	ErrorCodeAmbiguous                    = querycontract.ErrorCodeAmbiguous
	ErrorCodeUnauthenticated              = querycontract.ErrorCodeUnauthenticated
	ErrorCodeInvalidArgument              = querycontract.ErrorCodeInvalidArgument
	ErrorCodeNotFound                     = querycontract.ErrorCodeNotFound
	ErrorCodePermissionDenied             = querycontract.ErrorCodePermissionDenied
	ErrorCodeBackendUnavailable           = querycontract.ErrorCodeBackendUnavailable
	ErrorCodeBackendTimeout               = querycontract.ErrorCodeBackendTimeout
	ErrorCodeIndexBuilding                = querycontract.ErrorCodeIndexBuilding
	ErrorCodeScopeNotFound                = querycontract.ErrorCodeScopeNotFound
	ErrorCodeServiceNotFound              = querycontract.ErrorCodeServiceNotFound
	ErrorCodeCapabilityDegraded           = querycontract.ErrorCodeCapabilityDegraded
	ErrorCodeOverloaded                   = querycontract.ErrorCodeOverloaded
	ErrorCodeInternalError                = querycontract.ErrorCodeInternalError
	ErrorCodeReadModelUnavailable         = querycontract.ErrorCodeReadModelUnavailable
	ErrorCodeComponentRegistryUnavailable = querycontract.ErrorCodeComponentRegistryUnavailable
)

func ParseGraphBackend(raw string) (GraphBackend, error) { return querycontract.ParseGraphBackend(raw) }

func NormalizeQueryProfile(raw string) QueryProfile { return querycontract.NormalizeQueryProfile(raw) }

func ParseQueryProfile(raw string) (QueryProfile, error) { return querycontract.ParseQueryProfile(raw) }

func acceptsEnvelope(r *http.Request) bool { return querycontract.AcceptsEnvelope(r) }

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
	return querycontract.RequiredProfile(capability)
}

func minTruthLevel(a, b TruthLevel) TruthLevel {
	rank := map[TruthLevel]int{TruthLevelExact: 3, TruthLevelDerived: 2, TruthLevelFallback: 1}
	if rank[a] <= rank[b] {
		return a
	}
	return b
}

func BuildTruthEnvelope(profile QueryProfile, capability string, basis TruthBasis, reason string) *TruthEnvelope {
	return querycontract.BuildTruthEnvelope(profile, capability, basis, reason)
}
