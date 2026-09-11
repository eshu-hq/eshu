// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/metrics"
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
//     production would call the forwarder itself: language/handler.go, the
//     staying caller this export exists for, keeps calling the function
//     directly. TestWriteGraphReadErrorCapabilitiesExistInMatrix resolves
//     that capability argument by tracing every caller of the enclosing
//     function, so a caller whose own parameter has no callers makes the
//     whole chain unresolvable and fails the sweep. Renaming in place leaves
//     the sweep the same two resolvable callers it had before this PR. The
//     guard is right and stays untouched; the fix belongs on our side of it.
//
// deadCodeIncomingEdge needs no entry here: it already aliases
// querycontract.DeadCodeIncomingEdge (codequery/aliases.go), so a staying
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
type CodeReachabilityCoverage = deadcode.CodeReachabilityCoverage

// CodeTopicContentInvestigator is the exported seam for
// codeTopicContentInvestigator, which content_reader.go and
// family_impact_change_surface_code.go read from outside the code move set.
// See #6060.
type CodeTopicContentInvestigator = codequery.CodeTopicContentInvestigator

// CodeTopicEvidenceRow is the exported seam for codeTopicEvidenceRow, which
// content_reader_code_topic.go and family_impact_change_surface_code.go read
// from outside the code move set. See #6060.
type CodeTopicEvidenceRow = codequery.CodeTopicEvidenceRow

// CodeTopicInvestigationRequest is the exported seam for
// codeTopicInvestigationRequest, which answer_packet_routes.go,
// content_reader_code_topic.go, and family_impact_change_surface_code.go read
// from outside the code move set. See #6060.
type CodeTopicInvestigationRequest = codequery.CodeTopicInvestigationRequest

// CrossRepoDeadCodeEvidence is the exported seam for crossRepoDeadCodeEvidence,
// which content_reader_dead_code_cross_repo.go reads from outside the code
// move set. See #6060.
type CrossRepoDeadCodeEvidence = deadcode.CrossRepoDeadCodeEvidence

// HardcodedSecretFindingRow is the exported seam for
// hardcodedSecretFindingRow, which content_reader_security_secrets.go reads
// from outside the code move set. See #6060.
type HardcodedSecretFindingRow = codequery.HardcodedSecretFindingRow

// HardcodedSecretInvestigationRequest is the exported seam for
// hardcodedSecretInvestigationRequest, which
// content_reader_security_secrets.go reads from outside the code move set.
// See #6060.
type HardcodedSecretInvestigationRequest = codequery.HardcodedSecretInvestigationRequest

// HardcodedSecretInvestigator is the exported seam for
// hardcodedSecretInvestigator, which content_reader.go reads from outside the
// code move set. See #6060.
type HardcodedSecretInvestigator = codequery.HardcodedSecretInvestigator

// LanguageQueryGrant is the exported seam for languageQueryGrant, which
// language/handler.go and language/metadata.go read from outside the
// code move set. See #6060.
type LanguageQueryGrant = codequery.LanguageQueryGrant

// StructuralInventoryRequest is the exported seam for
// structuralInventoryRequest, which content_reader_structural_inventory.go
// reads from outside the code move set. See #6060.
type StructuralInventoryRequest = codequery.StructuralInventoryRequest

// SymbolContentSearcher is the exported seam for symbolContentSearcher, which
// content_reader.go reads from outside the code move set. See #6060.
type SymbolContentSearcher = codequery.SymbolContentSearcher

// SymbolSearchRequest is the exported seam for symbolSearchRequest, which
// content_reader_symbol_search.go reads from outside the code move set. See
// #6060.
type SymbolSearchRequest = codequery.SymbolSearchRequest

const (
	// CallGraphMetricsEdgeScanLimit is the exported seam for
	// callGraphMetricsEdgeScanLimit, which infra_graph_summary_packet.go reads
	// from outside the code move set. See #6060.
	CallGraphMetricsEdgeScanLimit = codequery.CallGraphMetricsEdgeScanLimit

	// CodeFlowCFGSummaryCapability is the exported seam for
	// codeFlowCFGSummaryCapability, which contract_code_flow.go reads from
	// outside the code move set. See #6060.
	CodeFlowCFGSummaryCapability = codequery.CodeFlowCFGSummaryCapability

	// CodeFlowPDGSummaryCapability is the exported seam for
	// codeFlowPDGSummaryCapability, which contract_code_flow.go reads from
	// outside the code move set. See #6060.
	CodeFlowPDGSummaryCapability = codequery.CodeFlowPDGSummaryCapability

	// CodeFlowReachingDefCapability is the exported seam for
	// codeFlowReachingDefCapability, which contract_code_flow.go reads from
	// outside the code move set. See #6060.
	CodeFlowReachingDefCapability = codequery.CodeFlowReachingDefCapability

	// CodeFlowTaintPathCapability is the exported seam for
	// codeFlowTaintPathCapability, which contract_code_flow.go reads from
	// outside the code move set. See #6060.
	CodeFlowTaintPathCapability = codequery.CodeFlowTaintPathCapability

	// RouteToCallerCapability is the exported seam for
	// routeToCallerCapability, which contract_capability_matrix.go reads from
	// outside the code move set. See #6060.
	RouteToCallerCapability = codequery.RouteToCallerCapability

	// StructuralInventoryDefaultLimit is the exported seam for
	// structuralInventoryDefaultLimit, which
	// content_reader_structural_inventory.go reads from outside the code move
	// set. See #6060.
	StructuralInventoryDefaultLimit = codequery.StructuralInventoryDefaultLimit

	// CrossRepoDeadCodeUngrantedConsumerProbeQuery is the exported seam for
	// crossRepoDeadCodeUngrantedConsumerProbeQuery. The statement is declared
	// in code_dead_code_cross_repo_filter.go, which moves, while its only
	// production reader is ContentReader.crossRepoDeadCodeUngrantedConsumers,
	// which this PR relocated onto its staying receiver in
	// content_reader_dead_code_cross_repo.go. That relocation is what created
	// this cross-boundary read: it does not exist on main. Aliasing rather
	// than moving the statement keeps the query text in exactly one place, so
	// the tree-wide literal comparison stays byte-identical. See #6060.
	CrossRepoDeadCodeUngrantedConsumerProbeQuery = deadcode.CrossRepoDeadCodeUngrantedConsumerProbeQuery

	// CodeTopicCapability is the exported seam for codeTopicCapability, which
	// answer_metadata_test.go reads from outside the code move set. See
	// #6060.
	CodeTopicCapability = codequery.CodeTopicCapability
)

// ErrCodeTopicBackendUnavailable is the exported seam for
// errCodeTopicBackendUnavailable, which family_impact_change_surface_code.go
// reads from outside the code move set. See #6060.
var ErrCodeTopicBackendUnavailable = codequery.ErrCodeTopicBackendUnavailable

// AppendMatchedFile is the exported seam for codequery.AppendMatchedFile, which
// family_impact_change_surface_code.go calls from outside the code move set.
// It forwards so the code family can move without touching callers. See
// #6060.
func AppendMatchedFile(files []map[string]any, row CodeTopicEvidenceRow) []map[string]any {
	return codequery.AppendMatchedFile(files, row)
}

// CallGraphMetricsEdgesCypher is the exported seam for
// metrics.CallGraphMetricsEdgesCypher, which infra_graph_summary_packet.go calls from
// outside the code move set. It forwards so the code family can nest without
// touching callers. See #6060.
func CallGraphMetricsEdgesCypher(repoID string) (string, map[string]any) {
	return metrics.CallGraphMetricsEdgesCypher(repoID)
}

// CodeTopicEvidenceGroup is the exported seam for codequery.CodeTopicEvidenceGroup,
// which family_impact_change_surface_code.go calls from outside the code
// move set. It forwards so the code family can move without touching
// callers. See #6060.
func CodeTopicEvidenceGroup(row CodeTopicEvidenceRow, rank int) map[string]any {
	return codequery.CodeTopicEvidenceGroup(row, rank)
}

// CodeTopicSearchTerms is the exported seam for codequery.CodeTopicSearchTerms, which
// family_impact_change_surface_code.go calls from outside the code move set.
// It forwards so the code family can move without touching callers. See
// #6060.
func CodeTopicSearchTerms(topic, intent string, explicit []string) []string {
	return codequery.CodeTopicSearchTerms(topic, intent, explicit)
}

// CodeTopicSymbol is the exported seam for codequery.CodeTopicSymbol, which
// family_impact_change_surface_code.go calls from outside the code move set.
// It forwards so the code family can move without touching callers. See
// #6060.
func CodeTopicSymbol(row CodeTopicEvidenceRow, rank int) map[string]any {
	return codequery.CodeTopicSymbol(row, rank)
}

// CrossRepoDeadCodeConfidenceLabel is the exported seam for
// deadcode.CrossRepoDeadCodeConfidenceLabel, which
// content_reader_dead_code_cross_repo.go calls from outside the code move
// set. It forwards so the code family can move without touching callers. See
// #6060.
func CrossRepoDeadCodeConfidenceLabel(confidence float64) string {
	return deadcode.CrossRepoDeadCodeConfidenceLabel(confidence)
}

// LanguageQueryGrantFor is the exported seam for codequery.LanguageQueryGrantFor, which
// language/handler.go calls from outside the code move set. It forwards so
// the code family can move without touching callers. See #6060.
func LanguageQueryGrantFor(ctx context.Context, repoID string) (LanguageQueryGrant, bool) {
	return codequery.LanguageQueryGrantFor(ctx, repoID)
}

// MergeStrongestDeadCodeIncomingEdge is the exported seam for
// deadcode.MergeStrongestDeadCodeIncomingEdge, which content_reader_dead_code.go calls
// from outside the code move set. It forwards so the code family can move
// without touching callers. deadCodeIncomingEdge already aliases
// querycontract.DeadCodeIncomingEdge (codequery/aliases.go), so the
// signature names that exported type directly rather than the local
// unexported alias. See #6060.
func MergeStrongestDeadCodeIncomingEdge(
	incoming map[string]querycontract.DeadCodeIncomingEdge,
	entityID string,
	edge querycontract.DeadCodeIncomingEdge,
) {
	deadcode.MergeStrongestDeadCodeIncomingEdge(incoming, entityID, edge)
}

// ResultContentEntityType is the exported seam for codequery.ResultContentEntityType,
// which entity_metadata.go calls from outside the code move set. It forwards
// so the code family can move without touching callers. See #6060.
func ResultContentEntityType(result map[string]any) string {
	return codequery.ResultContentEntityType(result)
}

// DeadCodeIncomingEdgeIsWeak is the exported seam for deadcode.DeadCodeIncomingEdgeIsWeak,
// which content_reader_dead_code_provenance_test.go calls from outside the
// code move set. It forwards so the code family can move without touching
// callers. See #6060.
func DeadCodeIncomingEdgeIsWeak(confidence float64) bool {
	return deadcode.DeadCodeIncomingEdgeIsWeak(confidence)
}

// CodeTopicResponse is the exported seam for codequery.CodeTopicResponse, which
// answer_metadata_test.go calls from outside the code move set to prove
// answer metadata is attached consistently across the service-story,
// repository-story, and code-topic response builders in the same assertion.
// It forwards so the code family can move without splitting that
// cross-family test. See #6060.
func CodeTopicResponse(req CodeTopicInvestigationRequest, rows []CodeTopicEvidenceRow, truncated bool) map[string]any {
	return codequery.CodeTopicResponse(req, rows, truncated)
}
