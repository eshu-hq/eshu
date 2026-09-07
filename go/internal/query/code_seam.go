// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// code_seam.go is the exported seam of the code handler family (#6060 lane A
// PR1). A go/types pass over internal/query, loaded with Tests: false and
// reporting zero package errors, found every unexported symbol the 39-file
// code move set declares that a staying PRODUCTION file reads. Each item
// below aliases or forwards to its unexported original with identical
// behavior, so the later move of the family into its own subpackage touches
// no caller: the aliases become cross-package references and the forwarders
// delegate to them.
//
// Test-side consumers are deliberately out of scope here. A tests-inclusive
// pass finds a much larger set, because staying test files read symbols the
// moving test files declare. PR 2 dissolves most of that by rewriting those
// call sites onto the registered route (Mount plus a mux) instead of reaching
// into the handler, and the remainder has to be re-derived after this PR
// lands rather than predicted from here.
//
// Three kinds of symbol the same pass found are handled at their
// declarations rather than here -- the first two because Go cannot alias
// them, the third because a forwarder would break a guard:
//
//   - Methods (structuralInventoryRequest.EntityType/Kind/NormalizedLimit/
//     QueryLimit, symbolSearchRequest.MustMatchMode/NormalizedEntityTypes/
//     ResolvedSymbol) are renamed at their declarations in
//     code_structural_inventory.go and code_symbol.go, with every caller in
//     the package updated. Go has no method aliases. QueryLimit is on this
//     list because this PR relocated it onto its receiver's own file; on main
//     it lived beside its staying callers and crossed no boundary.
//   - Struct fields (languageQueryGrant.Access/AllowedRepositoryIDs) are
//     renamed at the struct declaration in code_repository_selector.go, with
//     every reader and literal in the package updated. Go has no field
//     aliases.
//   - ApplyRepositorySelectorForAccess is renamed at its declaration in
//     code_repository_selector.go, with both callers updated, rather than
//     forwarded from here. A forwarder would be a third call site passing a
//     capability parameter into WriteGraphReadError, and nothing in
//     production would call the forwarder itself: language_queries.go, the
//     staying caller this export exists for, keeps calling the function
//     directly. TestWriteGraphReadErrorCapabilitiesExistInMatrix resolves
//     that capability argument by tracing every caller of the enclosing
//     function, so a caller whose own parameter has no callers makes the
//     whole chain unresolvable and fails the sweep. Renaming in place leaves
//     the sweep the same two resolvable callers it had before this PR. The
//     guard is right and stays untouched; the fix belongs on our side of it.
//
// deadCodeIncomingEdge needs no entry here: it already aliases
// querycontract.DeadCodeIncomingEdge (code_dead_code_scan.go), so a staying
// caller across the future move names querycontract directly.
//
// ContentReader.crossRepoDeadCodeUngrantedConsumers needs no entry here
// either: its only outside-the-move-set reader,
// content_reader_dead_code_cross_repo.go, is where the method itself moved
// to (its receiver, ContentReader, is declared in content_reader.go, which
// stays in root), so after the move it is a same-package call again.
//
// Nothing here implements behavior. code_seam_export_test.go trips any
// divergence or deletion.

// CodeReachabilityCoverage is the exported seam for codeReachabilityCoverage,
// which content_reader_dead_code.go reads from outside the code move set.
// See #6060.
type CodeReachabilityCoverage = codeReachabilityCoverage

// CodeTopicContentInvestigator is the exported seam for
// codeTopicContentInvestigator, which content_reader.go and
// family_impact_change_surface_code.go read from outside the code move set.
// See #6060.
type CodeTopicContentInvestigator = codeTopicContentInvestigator

// CodeTopicEvidenceRow is the exported seam for codeTopicEvidenceRow, which
// content_reader_code_topic.go and family_impact_change_surface_code.go read
// from outside the code move set. See #6060.
type CodeTopicEvidenceRow = codeTopicEvidenceRow

// CodeTopicInvestigationRequest is the exported seam for
// codeTopicInvestigationRequest, which answer_packet_routes.go,
// content_reader_code_topic.go, and family_impact_change_surface_code.go read
// from outside the code move set. See #6060.
type CodeTopicInvestigationRequest = codeTopicInvestigationRequest

// CrossRepoDeadCodeEvidence is the exported seam for crossRepoDeadCodeEvidence,
// which content_reader_dead_code_cross_repo.go reads from outside the code
// move set. See #6060.
type CrossRepoDeadCodeEvidence = crossRepoDeadCodeEvidence

// HardcodedSecretFindingRow is the exported seam for
// hardcodedSecretFindingRow, which content_reader_security_secrets.go reads
// from outside the code move set. See #6060.
type HardcodedSecretFindingRow = hardcodedSecretFindingRow

// HardcodedSecretInvestigationRequest is the exported seam for
// hardcodedSecretInvestigationRequest, which
// content_reader_security_secrets.go reads from outside the code move set.
// See #6060.
type HardcodedSecretInvestigationRequest = hardcodedSecretInvestigationRequest

// HardcodedSecretInvestigator is the exported seam for
// hardcodedSecretInvestigator, which content_reader.go reads from outside the
// code move set. See #6060.
type HardcodedSecretInvestigator = hardcodedSecretInvestigator

// LanguageQueryGrant is the exported seam for languageQueryGrant, which
// language_queries.go and language_query_metadata.go read from outside the
// code move set. See #6060.
type LanguageQueryGrant = languageQueryGrant

// StructuralInventoryRequest is the exported seam for
// structuralInventoryRequest, which content_reader_structural_inventory.go
// reads from outside the code move set. See #6060.
type StructuralInventoryRequest = structuralInventoryRequest

// SymbolContentSearcher is the exported seam for symbolContentSearcher, which
// content_reader.go reads from outside the code move set. See #6060.
type SymbolContentSearcher = symbolContentSearcher

// SymbolSearchRequest is the exported seam for symbolSearchRequest, which
// content_reader_symbol_search.go reads from outside the code move set. See
// #6060.
type SymbolSearchRequest = symbolSearchRequest

const (
	// CallGraphMetricsEdgeScanLimit is the exported seam for
	// callGraphMetricsEdgeScanLimit, which infra_graph_summary_packet.go reads
	// from outside the code move set. See #6060.
	CallGraphMetricsEdgeScanLimit = callGraphMetricsEdgeScanLimit

	// CodeFlowCFGSummaryCapability is the exported seam for
	// codeFlowCFGSummaryCapability, which contract_code_flow.go reads from
	// outside the code move set. See #6060.
	CodeFlowCFGSummaryCapability = codeFlowCFGSummaryCapability

	// CodeFlowPDGSummaryCapability is the exported seam for
	// codeFlowPDGSummaryCapability, which contract_code_flow.go reads from
	// outside the code move set. See #6060.
	CodeFlowPDGSummaryCapability = codeFlowPDGSummaryCapability

	// CodeFlowReachingDefCapability is the exported seam for
	// codeFlowReachingDefCapability, which contract_code_flow.go reads from
	// outside the code move set. See #6060.
	CodeFlowReachingDefCapability = codeFlowReachingDefCapability

	// CodeFlowTaintPathCapability is the exported seam for
	// codeFlowTaintPathCapability, which contract_code_flow.go reads from
	// outside the code move set. See #6060.
	CodeFlowTaintPathCapability = codeFlowTaintPathCapability

	// RouteToCallerCapability is the exported seam for
	// routeToCallerCapability, which contract_capability_matrix.go reads from
	// outside the code move set. See #6060.
	RouteToCallerCapability = routeToCallerCapability

	// StructuralInventoryDefaultLimit is the exported seam for
	// structuralInventoryDefaultLimit, which
	// content_reader_structural_inventory.go reads from outside the code move
	// set. See #6060.
	StructuralInventoryDefaultLimit = structuralInventoryDefaultLimit

	// CrossRepoDeadCodeUngrantedConsumerProbeQuery is the exported seam for
	// crossRepoDeadCodeUngrantedConsumerProbeQuery. The statement is declared
	// in code_dead_code_cross_repo_filter.go, which moves, while its only
	// production reader is ContentReader.crossRepoDeadCodeUngrantedConsumers,
	// which this PR relocated onto its staying receiver in
	// content_reader_dead_code_cross_repo.go. That relocation is what created
	// this cross-boundary read: it does not exist on main. Aliasing rather
	// than moving the statement keeps the query text in exactly one place, so
	// the tree-wide literal comparison stays byte-identical. See #6060.
	CrossRepoDeadCodeUngrantedConsumerProbeQuery = crossRepoDeadCodeUngrantedConsumerProbeQuery
)

// ErrCodeTopicBackendUnavailable is the exported seam for
// errCodeTopicBackendUnavailable, which family_impact_change_surface_code.go
// reads from outside the code move set. See #6060.
var ErrCodeTopicBackendUnavailable = errCodeTopicBackendUnavailable

// AppendMatchedFile is the exported seam for appendMatchedFile, which
// family_impact_change_surface_code.go calls from outside the code move set.
// It forwards so the code family can move without touching callers. See
// #6060.
func AppendMatchedFile(files []map[string]any, row CodeTopicEvidenceRow) []map[string]any {
	return appendMatchedFile(files, row)
}

// CallGraphMetricsEdgesCypher is the exported seam for
// callGraphMetricsEdgesCypher, which infra_graph_summary_packet.go calls from
// outside the code move set. It forwards so the code family can move without
// touching callers. See #6060.
func CallGraphMetricsEdgesCypher(repoID string) (string, map[string]any) {
	return callGraphMetricsEdgesCypher(repoID)
}

// CodeTopicEvidenceGroup is the exported seam for codeTopicEvidenceGroup,
// which family_impact_change_surface_code.go calls from outside the code
// move set. It forwards so the code family can move without touching
// callers. See #6060.
func CodeTopicEvidenceGroup(row CodeTopicEvidenceRow, rank int) map[string]any {
	return codeTopicEvidenceGroup(row, rank)
}

// CodeTopicSearchTerms is the exported seam for codeTopicSearchTerms, which
// family_impact_change_surface_code.go calls from outside the code move set.
// It forwards so the code family can move without touching callers. See
// #6060.
func CodeTopicSearchTerms(topic, intent string, explicit []string) []string {
	return codeTopicSearchTerms(topic, intent, explicit)
}

// CodeTopicSymbol is the exported seam for codeTopicSymbol, which
// family_impact_change_surface_code.go calls from outside the code move set.
// It forwards so the code family can move without touching callers. See
// #6060.
func CodeTopicSymbol(row CodeTopicEvidenceRow, rank int) map[string]any {
	return codeTopicSymbol(row, rank)
}

// CrossRepoDeadCodeConfidenceLabel is the exported seam for
// crossRepoDeadCodeConfidenceLabel, which
// content_reader_dead_code_cross_repo.go calls from outside the code move
// set. It forwards so the code family can move without touching callers. See
// #6060.
func CrossRepoDeadCodeConfidenceLabel(confidence float64) string {
	return crossRepoDeadCodeConfidenceLabel(confidence)
}

// LanguageQueryGrantFor is the exported seam for languageQueryGrantFor, which
// language_queries.go calls from outside the code move set. It forwards so
// the code family can move without touching callers. See #6060.
func LanguageQueryGrantFor(ctx context.Context, repoID string) (LanguageQueryGrant, bool) {
	return languageQueryGrantFor(ctx, repoID)
}

// MergeStrongestDeadCodeIncomingEdge is the exported seam for
// mergeStrongestDeadCodeIncomingEdge, which content_reader_dead_code.go calls
// from outside the code move set. It forwards so the code family can move
// without touching callers. deadCodeIncomingEdge already aliases
// querycontract.DeadCodeIncomingEdge (code_dead_code_scan.go), so the
// signature names that exported type directly rather than the local
// unexported alias. See #6060.
func MergeStrongestDeadCodeIncomingEdge(
	incoming map[string]querycontract.DeadCodeIncomingEdge,
	entityID string,
	edge querycontract.DeadCodeIncomingEdge,
) {
	mergeStrongestDeadCodeIncomingEdge(incoming, entityID, edge)
}

// ResultContentEntityType is the exported seam for resultContentEntityType,
// which entity_metadata.go calls from outside the code move set. It forwards
// so the code family can move without touching callers. See #6060.
func ResultContentEntityType(result map[string]any) string {
	return resultContentEntityType(result)
}
