// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/contentread"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// The codemodel.RubyRailsControllerActionRootKind constant split to
// codemodel/code_dead_code_ruby_roots.go and the codemodel.DeadCodeDowngradedRoots
// type with its isDowngraded predicate split to
// codemodel/code_dead_code_analysis.go (#6060 lane A L1); both run in the
// leaf now. Root's family_code_shim.go aliases them back so the staying
// readers and tests keep their names; the verdict loader itself moved to
// deadcode (LoadDeadCodeDowngradedRoots) with the analysis.

// This file is the codequery-local mirror of root package query's
// contract.go, ports.go, handler.go, and neo4j.go compatibility shims
// (#6060, lane A CodeHandler move). Every declaration below is a plain type
// alias or thin forwarder onto querycontract (or another dependency-neutral
// leaf package), nothing here implements new behavior. It exists so the
// moved CodeHandler-family files keep every unqualified call site they had
// in package query, unchanged: several of those call sites sit inside
// functions registered in internal/queryplan/grandfathered_non_hot.go with a
// source digest frozen from the `func` keyword through the closing brace,
// and qualifying an in-body call (or renaming the symbol it resolves to)
// would edit that text and break the pin. New code in this package should
// still prefer naming querycontract directly; this file is a compatibility
// floor for the files that moved with call sites already written against
// the unqualified names, not a template for new code.

// QueryProfile names one supported query runtime profile.
type QueryProfile = querycontract.QueryProfile

// GraphBackend names one supported graph adapter.
type GraphBackend = querycontract.GraphBackend

// GraphQuery is the concurrent-safe read-only graph traversal surface.
type GraphQuery = querycontract.GraphQuery

// ContentStore is the relational content-query surface used by read handlers.
type ContentStore = querycontract.ContentStore

// EntityContent is one content-store entity row.
type EntityContent = querycontract.EntityContent

// FileContent is one content-store file row.
type FileContent = contentread.FileContent

// AuthContext carries request-scoped authorization bounds for query handlers.
type AuthContext = queryauth.AuthContext

// repositoryAccessFilter carries a caller's resolved repository authorization
// bounds for a graph or content read.
type repositoryAccessFilter = querycontract.RepositoryAccessFilter

// crossRepoDeadCodeConsumerReads names the consumer repositories a cross-repo
// dead-code evidence page is bound to.
type crossRepoDeadCodeConsumerReads = querycontract.CrossRepoDeadCodeConsumerReads

// crossRepoDeadCodeHiddenConsumers reports producer entities with a consumer
// outside the caller's grant.
type crossRepoDeadCodeHiddenConsumers = querycontract.CrossRepoDeadCodeHiddenConsumers

// ProfileLocalAuthoritative is a supported query runtime profile.
const ProfileLocalAuthoritative = querycontract.ProfileLocalAuthoritative

// AuthModeScoped identifies a scoped-token AuthContext.
const AuthModeScoped = queryauth.AuthModeScoped

// TruthBasis names the evidence source used to produce an answer.
type TruthBasis = querycontract.TruthBasis

// TruthEnvelope carries query capability, evidence, and freshness metadata.
type TruthEnvelope = querycontract.TruthEnvelope

// ErrorCode is a stable machine-readable query error code.
type ErrorCode = querycontract.ErrorCode

// ErrorEnvelope carries a stable query error and optional profile detail.
type ErrorEnvelope = querycontract.ErrorEnvelope

// ResponseEnvelope is the negotiated query response wire contract.
type ResponseEnvelope = querycontract.ResponseEnvelope

// RepositoryContentCoverage summarizes content-store coverage for one repository.
type RepositoryContentCoverage = querycontract.RepositoryContentCoverage

const (
	ProfileProduction = querycontract.ProfileProduction

	GraphBackendNeo4j    = querycontract.GraphBackendNeo4j
	GraphBackendNornicDB = querycontract.GraphBackendNornicDB

	TruthBasisAuthoritativeGraph = querycontract.TruthBasisAuthoritativeGraph
	TruthBasisContentIndex       = querycontract.TruthBasisContentIndex
	TruthBasisHybrid             = querycontract.TruthBasisHybrid
	TruthBasisNoBackendRead      = querycontract.TruthBasisNoBackendRead

	FreshnessFresh    = querycontract.FreshnessFresh
	FreshnessStale    = querycontract.FreshnessStale
	FreshnessBuilding = querycontract.FreshnessBuilding

	ErrorCodeUnsupportedCapability = querycontract.ErrorCodeUnsupportedCapability
	ErrorCodeAmbiguous             = querycontract.ErrorCodeAmbiguous
	ErrorCodeInvalidArgument       = querycontract.ErrorCodeInvalidArgument
	ErrorCodeNotFound              = querycontract.ErrorCodeNotFound
	ErrorCodeBackendUnavailable    = querycontract.ErrorCodeBackendUnavailable
	ErrorCodeBackendTimeout        = querycontract.ErrorCodeBackendTimeout
	ErrorCodeInternalError         = querycontract.ErrorCodeInternalError

	// EntityNameMatchExact requires a case-sensitive complete name match.
	EntityNameMatchExact = querycontract.EntityNameMatchExact
	// EntityNameMatchSubstring requires a case-sensitive substring match.
	EntityNameMatchSubstring = querycontract.EntityNameMatchSubstring
	// EntityNameScopeAll searches every repository visible to an all-scopes caller.
	EntityNameScopeAll = querycontract.EntityNameScopeAll
	// EntityNameScopeRepositories searches one explicit authorized repository set.
	EntityNameScopeRepositories = querycontract.EntityNameScopeRepositories
)

// EntityNameSearch is the bounded, authorization-aware content name-search contract.
type EntityNameSearch = querycontract.EntityNameSearch

// EntityNameSearcher is the narrow extension used by global entity-name routes.
type EntityNameSearcher = querycontract.EntityNameSearcher

// ErrContentSubstringIndexesNotReady aliases querycontract's sentinel so
// callers here and in root compare the same instance with errors.Is.
var ErrContentSubstringIndexesNotReady = querycontract.ErrContentSubstringIndexesNotReady

// ParseGraphBackend validates raw against the supported graph adapters.
func ParseGraphBackend(raw string) (GraphBackend, error) { return querycontract.ParseGraphBackend(raw) }

// NormalizeQueryProfile returns a supported profile or the empty profile.
func NormalizeQueryProfile(raw string) QueryProfile { return querycontract.NormalizeQueryProfile(raw) }

// BuildTruthEnvelope builds truth metadata from the capability ceiling.
func BuildTruthEnvelope(profile QueryProfile, capability string, basis TruthBasis, reason string) *TruthEnvelope {
	return querycontract.BuildTruthEnvelope(profile, capability, basis, reason)
}

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	querycontract.WriteJSON(w, status, v)
}

// WriteError writes a JSON error response.
func WriteError(w http.ResponseWriter, status int, message string) {
	querycontract.WriteError(w, status, message)
}

// WriteSuccess writes the negotiated {data, truth} success envelope.
func WriteSuccess(w http.ResponseWriter, r *http.Request, status int, data any, truth *TruthEnvelope) {
	querycontract.WriteSuccess(w, r, status, data, truth)
}

// WriteErrorEnvelope writes a stable query error using the same envelope/plain
// split as WriteSuccess.
func WriteErrorEnvelope(w http.ResponseWriter, r *http.Request, status int, errEnv *ErrorEnvelope) {
	querycontract.WriteErrorEnvelope(w, r, status, errEnv)
}

func WriteContractError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	message string,
	errCode ErrorCode,
	capability string,
	currentProfile QueryProfile,
	requiredProfile QueryProfile,
) {
	querycontract.WriteContractError(w, r, status, message, errCode, capability, currentProfile, requiredProfile)
}

// WriteGraphReadError writes the stable HTTP contract for a bounded
// graph-read availability error. It returns false without touching the
// response when err is not one of the shared graph-read errors.
func WriteGraphReadError(w http.ResponseWriter, r *http.Request, err error, capability string) bool {
	return querycontract.WriteGraphReadError(w, r, err, capability)
}

// ReadJSON decodes a JSON request body into v.
func ReadJSON(r *http.Request, v any) error {
	return querycontract.ReadJSON(r, v)
}

// StringVal safely extracts a string from a map value.
func StringVal(row map[string]any, key string) string {
	return querycontract.StringVal(row, key)
}

// BoolVal safely extracts a bool from a map value.
func BoolVal(row map[string]any, key string) bool {
	return querycontract.BoolVal(row, key)
}

// IntVal safely extracts an int from a map value.
func IntVal(row map[string]any, key string) int {
	return querycontract.IntVal(row, key)
}

// StringSliceVal safely extracts a []string from a map value.
func StringSliceVal(row map[string]any, key string) []string {
	return querycontract.StringSliceVal(row, key)
}

// ContextWithAuthContext attaches auth to ctx for downstream AuthContext reads.
func ContextWithAuthContext(ctx context.Context, auth AuthContext) context.Context {
	return queryauth.ContextWithAuthContext(ctx, auth)
}

// resolveExactGraphEntityCandidates lists exact-name entity candidates.
func resolveExactGraphEntityCandidates(
	ctx context.Context,
	reader ContentStore,
	repoID string,
	name string,
) ([]EntityContent, error) {
	return querycontract.ResolveExactGraphEntityCandidates(ctx, reader, repoID, name)
}

// VisualizationView names the derived-view family a packet was built from.
type VisualizationView = querycontract.VisualizationView

// VisualizationViewGraphQuery is the executed-Cypher-result subgraph.
const VisualizationViewGraphQuery = querycontract.VisualizationViewGraphQuery

// VisualizationNode is one bounded node in a visualization packet.
type VisualizationNode = querycontract.VisualizationNode

// VisualizationEdge is one bounded edge in a visualization packet.
type VisualizationEdge = querycontract.VisualizationEdge

// VisualizationPacket is a compact, bounded, derived view of an executed
// read-only Cypher query.
type VisualizationPacket = querycontract.VisualizationPacket

// deadCodeIncomingEdge is the strongest incoming reachability edge observed for
// a dead-code candidate.
//
// It is an alias onto querycontract rather than a declaration: the type appears
// in a ContentStore read's signature, and a shared double promoted to
// querytestutil for #6060 cannot name an unexported root type. An alias
// preserves type identity, so every existing caller and every composite literal
// is unchanged.
type deadCodeIncomingEdge = querycontract.DeadCodeIncomingEdge

// deadCodeCandidateQuery is the pre-move spelling of
// codeshaping.DeadCodeCandidateQuery, kept for the digest-pinned
// deadCodeCandidateRows body in analyzer.go.
type deadCodeCandidateQuery = codeshaping.DeadCodeCandidateQuery

// deadCodeCandidateContentStore is the pre-move spelling of
// codeshaping.DeadCodeCandidateContentStore, kept for the same
// digest-pinned body.
type deadCodeCandidateContentStore = codeshaping.DeadCodeCandidateContentStore
