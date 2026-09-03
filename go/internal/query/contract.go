// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/semanticsearch"
)

// Root contract identifiers preserve the stable envelope and playbook names.
const (
	// EnvelopeMIMEType selects the stable Eshu response envelope.
	EnvelopeMIMEType = querycontract.EnvelopeMIMEType
	// CapabilityQueryPlaybooks identifies deterministic playbook catalog reads.
	CapabilityQueryPlaybooks = "query.playbooks"
	// semanticSearchCapability is read from the family that implements the
	// route (#6060) so this package's capability matrix and that handler's
	// profile gate and envelopes cannot name two different capabilities.
	semanticSearchCapability = semanticsearch.Capability
)

// QueryProfile names one supported query runtime profile.
type QueryProfile = querycontract.QueryProfile

// GraphBackend names one supported graph adapter.
type GraphBackend = querycontract.GraphBackend

// TruthLevel describes how directly evidence supports an answer.
type TruthLevel = querycontract.TruthLevel

// TruthBasis names the evidence source used to produce an answer.
type TruthBasis = querycontract.TruthBasis

// FreshnessState reports whether evidence is current and available.
type FreshnessState = querycontract.FreshnessState

// TruthFreshness carries freshness state and a proven cause when known.
type TruthFreshness = querycontract.TruthFreshness

// TruthEnvelope carries query capability, evidence, and freshness metadata.
type TruthEnvelope = querycontract.TruthEnvelope

// ErrorProfiles names the active and minimum required profiles.
type ErrorProfiles = querycontract.ErrorProfiles

// ErrorCode is a stable machine-readable query error code.
type ErrorCode = querycontract.ErrorCode

// ErrorEnvelope carries a stable query error and optional profile detail.
type ErrorEnvelope = querycontract.ErrorEnvelope

// ResponseEnvelope is the negotiated query response wire contract.
type ResponseEnvelope = querycontract.ResponseEnvelope

type capabilitySupport = querycontract.CapabilitySupport

// Compatibility constants preserve the root query package's public contract.
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

// ParseGraphBackend validates raw against the supported graph adapters.
func ParseGraphBackend(raw string) (GraphBackend, error) { return querycontract.ParseGraphBackend(raw) }

// NormalizeQueryProfile returns a supported profile or the empty profile.
func NormalizeQueryProfile(raw string) QueryProfile { return querycontract.NormalizeQueryProfile(raw) }

// ParseQueryProfile validates raw against the supported query profiles.
func ParseQueryProfile(raw string) (QueryProfile, error) { return querycontract.ParseQueryProfile(raw) }

func acceptsEnvelope(r *http.Request) bool { return querycontract.AcceptsEnvelope(r) }

func requiredProfile(capability string) QueryProfile {
	return querycontract.RequiredProfile(capability)
}

func minTruthLevel(a, b TruthLevel) TruthLevel {
	return querycontract.MinTruthLevel(a, b)
}

// BuildTruthEnvelope builds truth metadata from the capability ceiling.
func BuildTruthEnvelope(profile QueryProfile, capability string, basis TruthBasis, reason string) *TruthEnvelope {
	return querycontract.BuildTruthEnvelope(profile, capability, basis, reason)
}
