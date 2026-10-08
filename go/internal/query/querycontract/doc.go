// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package querycontract defines the dependency-neutral contracts shared by query families.
//
// It owns response envelopes, profile and capability gates, freshness metadata,
// read ports, the content models those ports exchange, the collector-list
// readiness contract that lets a caller tell an empty page from a disabled
// collector, and the scoped-token repository-access authorization seam
// (RepositoryAccessFilter and the inline-map grant predicate primitives),
// plus the Go-side Workload grant decision (WorkloadGrantAdmitted) and the
// shared name-selector bound (WorkloadSelectorCandidateBound) whose overflow
// error, ErrWorkloadSelectorCandidatesExceedBound, handlers write as a
// count-free 409 through WriteWorkloadSelectorOverflow.
// Family packages can depend on these contracts without importing the root
// query router. Capability registration preserves the established profile
// ceilings, ordered catalog, and unknown-capability panic.
//
// It no longer owns the graph row-value decoders every read path uses to pull
// typed columns out of a driver's map[string]any. Those live in the
// querycontract/rowvalue leaf as of #6597; StringVal, BoolVal, IntVal,
// StringSliceVal and FloatVal survive here only as forwarders, so existing
// callers compile unchanged while a handler-family subpackage can decode rows
// by importing rowvalue directly.
//
// It owns the bounded graph-read error contract: the ErrGraphReadDeadline and
// ErrGraphUnavailable sentinels, WriteGraphReadError's stable 504/503 mapping,
// and ClassifyBoundedGraphReadError, which turns any read error into the
// deadline sentinel once the bounded read context has expired (#7353).
//
// The readiness types and their two Build functions live here; deciding when to
// run the probe and attaching the result to a response body stays in package
// query, because that is request-time orchestration rather than contract.
//
// It also owns the handler seams promoted out of root for #6060 that have not
// yet moved to their own leaf, so a family package can reach them without an
// import cycle; the README promoted-seam table says where each one lives now.
// The #6597 split has moved these to subpackages: the k8s SELECTS
// matcher (kubernetes), row-value decoding (rowvalue), the dead-code
// contract types (code), entity-name search, exact graph entity resolution and the #6408
// projection-placeholder scrubber (entity), the evidence-citation handles,
// citation packet and evidence boundaries (evidence), the
// visualization-packet contract and its builder (visualization), the
// answer packet and answer_metadata companion (answer), the language and
// entity-type vocabulary (taxonomy), and the repository read models, summary
// loaders, and row projection (repository).
// Content-index readiness covers ErrContentSubstringIndexesNotReady and
// WriteContentSubstringIndexUnavailable. Language normalization
// (CanonicalLanguage, NormalizedLanguageVariants, CoverageLanguageMaps) lives
// in the taxonomy leaf; the accepted-language set lives in the language
// handler package (go/internal/query/language, registry.go).
//
// The sentinel errors are the reason several of these moved rather than being
// copied: root compares them with errors.Is, so both sides have to resolve to
// the same value. A re-declared error would compile and compare false.
//
// It also owns the ContentStore read models and their filters. ContentStore
// itself already lived here, but the narrow optional ports package query
// type-asserts a store against exchanged types declared in package query, and
// those types blocked the shared content-read double from leaving root: a
// _test.go symbol is not importable across a package boundary, and neither is
// an unexported one. Documentation covers DocumentationFindingFilter,
// DocumentationFindingListReadModel, DocumentationFactFilter,
// DocumentationFactListReadModel, the packet and freshness read models and
// their authorization filters, and the DocumentationTargetScope,
// DocumentationTargetCoverage and DocumentationMissingEvidence readback types.
// Repository stories (in the repository/ leaf) cover
// RepositoryEntryPointReadModel, RepositoryDeploymentEvidenceReadModel,
// RelationshipEvidenceReadModel,
// RepositoryReadModelSummary, RepositoryRelationshipReadModel, RepositoryRef,
// CatalogWorkloadIdentityEntry, ServiceStoryTargetSupportFilter and
// ServiceStoryTargetSupportReadModel. The filter carries the graph gate a
// service target needs (RepositoryWorkloadCount and RepositoryDefinesTarget,
// #7138), and FirstMissingEvidenceReason reads a support block's first missing
// reason for the story stage events. The candidate projection the double needs
// moved on to the kubernetes/ subpackage with the rest of the SELECTS matcher;
// K8sSelectCandidate itself stays here, because it is a ContentStore read model
// that the projection returns rather than part of the matching decision. Root
// aliases every type, so its call sites are unchanged.
//
// The Available field several of these carry is a fallback signal, not an
// emptiness one. A caller that reads a zero-value read model as "nothing
// found" reports a repository with real data as having none.
//
// #6642 added the auth-adjacent 401/403 response writers: WriteUnauthorized
// (moved from root's unauthorizedResponse), WritePermissionDenied (moved from
// root's writePermissionDeniedEnvelope), and RequirePermissionFeature (moved
// from root's requirePermissionFeature). They live here rather than in
// auth -- the more auth-shaped leaf -- because they need this package's
// own WriteJSON/ResponseEnvelope/ErrorEnvelope/ErrorCode primitives, and
// auth cannot import querycontract: querycontract already imports
// auth (RepositoryAccessFilterFromContext reads AuthContext), so the
// reverse edge would cycle. WriteUnauthorized's WWW-Authenticate header
// needed a types-only hoist alongside it -- OAuthChallengePolicy (the
// interface only; root's PostureOAuthChallengePolicy, DeriveAuthPosture, and
// OAuthProtectedResourceHandler all stay in root untouched), its context-key
// pair (RequestWithOAuthChallenge / the unexported reader), and
// OAuthWWWAuthenticateChallengeForRequest -- plus the stdlib-only
// DocumentationCorrelationID it also calls. Root keeps a type alias and thin
// function forwarders at every original declaration site that still has a
// root caller (the WWW-Authenticate lookup's only caller moved with it, so
// it keeps none), and cmd/mcp-server, auth_constructors.go, and every
// other existing caller compile unchanged.
package querycontract // Shared-seam home for #6060 family moves: root, impact/ and the family packages must share these seams without an import cycle. The #6597 split extracted nine leaves into subpackages (kubernetes/, rowvalue/, code/, entity/, evidence/, visualization/, answer/, taxonomy/ and repository/); what stays fits the 40-file cap, so no file-count dirgate marker remains.
