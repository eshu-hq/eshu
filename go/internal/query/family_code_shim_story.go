// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// This file is part of the #6060 lane-A P0 shim split out of
// family_code_shim.go to keep every file under the repo's 500-line
// cap. The deletion protocol in family_code_shim.go's header applies
// to every entry here: delete each entry with its named handler move.

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// relationshipsRequest aliases the leaf-owned lookup request so the
// staying relationships handler keeps its literal unchanged. Delete with
// code_relationships.go's handler move.
type relationshipsRequest = codemodel.RelationshipsRequest

// resolveRelationshipsNameTarget forwards to the leaf-owned name resolver
// so the staying relationships handler keeps its call site unchanged.
// Delete with code_relationships.go's handler move.
func resolveRelationshipsNameTarget(
	ctx context.Context,
	reader ContentStore,
	req relationshipsRequest,
) (*EntityContent, *relationshipStoryResolution, error) {
	return codemodel.ResolveRelationshipsNameTarget(ctx, reader, req)
}

// ambiguousRelationshipsResponse forwards to the leaf-owned ambiguity
// shaper so the staying relationships handler keeps its call site
// unchanged. Delete with code_relationships.go's handler move.
func ambiguousRelationshipsResponse(req relationshipsRequest, resolution relationshipStoryResolution) map[string]any {
	return codemodel.AmbiguousRelationshipsResponse(req, resolution)
}

// sortRelationshipStoryCandidates forwards to the leaf-owned candidate
// sorter so the staying story resolver keeps its call site unchanged.
// Delete with code_relationship_story.go's handler move.
func sortRelationshipStoryCandidates(candidates []EntityContent) {
	codemodel.SortRelationshipStoryCandidates(candidates)
}

// relationshipStoryCandidateMaps forwards to the leaf-owned candidate
// shaper so the staying story resolver keeps its call site unchanged.
// Delete with code_relationship_story.go's handler move.
func relationshipStoryCandidateMaps(candidates []EntityContent, limit int) []map[string]any {
	return codemodel.RelationshipStoryCandidateMaps(candidates, limit)
}

// relationshipGraphRowCypher forwards to the leaf-owned row fragment so
// the staying relationships handler and tests keep their call sites
// unchanged. Delete with code_relationships.go's handler move.
func relationshipGraphRowCypher(predicate string) string {
	return codemodel.RelationshipGraphRowCypher(predicate)
}

// buildTransitiveRelationshipRowsCypher forwards to the leaf-owned
// traversal builder so the staying relationships handler keeps its call
// site unchanged. Delete with code_relationships.go's handler move.
func buildTransitiveRelationshipRowsCypher(
	entityID string,
	direction string,
	maxDepth int,
	backend GraphBackend,
) (string, map[string]any) {
	return codemodel.BuildTransitiveRelationshipRowsCypher(entityID, direction, maxDepth, backend)
}

// buildTransitiveRelationshipGraphResponse forwards to the leaf-owned
// traversal shaper so the staying relationships handler keeps its call
// site unchanged. Delete with code_relationships.go's handler move.
func buildTransitiveRelationshipGraphResponse(
	metadataRow map[string]any,
	rows []map[string]any,
	direction string,
) map[string]any {
	return codemodel.BuildTransitiveRelationshipGraphResponse(metadataRow, rows, direction)
}

// normalizeGraphRelationships forwards to the leaf-owned response
// normalizer so the staying relationships handler keeps its call site
// unchanged. Delete with code_relationships.go's handler move.
func normalizeGraphRelationships(response map[string]any) {
	codemodel.NormalizeGraphRelationships(response)
}

// graphEntityIDPredicate forwards to the leaf-owned identity predicate so
// the staying call-chain, relationships, and story readers keep their call
// sites unchanged. Delete with code_relationships.go's handler move.
func graphEntityIDPredicate(alias string, param string) string {
	return codemodel.GraphEntityIDPredicate(alias, param)
}

// relationshipStoryRequest aliases the leaf-owned story request so the
// staying handlers, resolvers, and tests keep their signatures and
// literals unchanged. The request split to codemodel with its methods in
// L1; the exported accessors are what staying callers use, and
// GraphAnchorProperty carries the NornicDB anchor. Delete with
// code_relationship_story.go's handler move.
type relationshipStoryRequest = codemodel.RelationshipStoryRequest

// relationshipStoryResolution aliases the leaf-owned resolution outcome so
// the staying handlers and resolvers keep their signatures unchanged.
// Delete with code_relationship_story.go's handler move.
type relationshipStoryResolution = codemodel.RelationshipStoryResolution

// relationshipStoryEvidenceInputs aliases the leaf-owned classifier inputs
// so the staying story shaper and evidence tests keep constructing them
// unchanged. Delete with code_relationship_story.go's handler move.
type relationshipStoryEvidenceInputs = codemodel.RelationshipStoryEvidenceInputs

// relationshipStoryEvidenceState aliases the leaf-owned classifier outcome
// so the staying story shaper and evidence tests keep reading it
// unchanged. Delete with code_relationship_story.go's handler move.
type relationshipStoryEvidenceState = codemodel.RelationshipStoryEvidenceState

// relationshipStoryProvenance forwards to the leaf-owned provenance
// deriver so the staying story shaper keeps its call site unchanged.
// Delete with code_relationship_story.go's handler move.
func relationshipStoryProvenance(row map[string]any) map[string]any {
	return codemodel.RelationshipStoryProvenance(row)
}

// relationshipStoryRowsAboveConfidenceFloor forwards to the leaf-owned
// confidence filter so the staying story shaper keeps its call site
// unchanged. Delete with code_relationship_story.go's handler move.
func relationshipStoryRowsAboveConfidenceFloor(
	rows []map[string]any,
	req relationshipStoryRequest,
) []map[string]any {
	return codemodel.RelationshipStoryRowsAboveConfidenceFloor(rows, req)
}

// classifyRelationshipStoryEvidence forwards to the leaf-owned evidence
// classifier so the staying story shaper and evidence tests keep their
// call sites unchanged. Delete with code_relationship_story.go's handler
// move.
func classifyRelationshipStoryEvidence(
	in relationshipStoryEvidenceInputs,
) relationshipStoryEvidenceState {
	return codemodel.ClassifyRelationshipStoryEvidence(in)
}

// callGraphMetricIdentity forwards to the leaf-owned identity resolver so
// the staying metrics tests keep their call sites unchanged. Delete with
// code_call_graph_metrics.go's handler move.
func callGraphMetricIdentity(row map[string]any, canonicalKey string, legacyKey string) string {
	return codemodel.CallGraphMetricIdentity(row, canonicalKey, legacyKey)
}

// relationshipStoryReasonComplete aliases the leaf-owned evidence reason
// so the staying evidence tests keep naming it. Delete with
// code_relationship_story.go's handler move.
const relationshipStoryReasonComplete = codemodel.RelationshipStoryReasonComplete

// relationshipStoryReasonTargetUnresolved aliases the leaf-owned evidence
// reason so the staying evidence tests keep naming it. Delete with
// code_relationship_story.go's handler move.
const relationshipStoryReasonTargetUnresolved = codemodel.RelationshipStoryReasonTargetUnresolved

// relationshipStoryReasonNoEdges aliases the leaf-owned evidence reason so
// the staying evidence tests keep naming it. Delete with
// code_relationship_story.go's handler move.
const relationshipStoryReasonNoEdges = codemodel.RelationshipStoryReasonNoEdges

// relationshipStoryReasonFloorFiltered aliases the leaf-owned evidence
// reason so the staying evidence tests keep naming it. Delete with
// code_relationship_story.go's handler move.
const relationshipStoryReasonFloorFiltered = codemodel.RelationshipStoryReasonFloorFiltered

// relationshipStoryReasonTruncatedLimit aliases the leaf-owned evidence
// reason so the staying evidence tests keep naming it. Delete with
// code_relationship_story.go's handler move.
const relationshipStoryReasonTruncatedLimit = codemodel.RelationshipStoryReasonTruncatedLimit

// relationshipStoryReasonTruncatedBudget aliases the leaf-owned evidence
// reason so the staying evidence tests keep naming it. Delete with
// code_relationship_story.go's handler move.
const relationshipStoryReasonTruncatedBudget = codemodel.RelationshipStoryReasonTruncatedBudget

// relationshipStoryTruncationNone aliases the leaf-owned truncation state
// so the staying evidence tests keep naming it. Delete with
// code_relationship_story.go's handler move.
const relationshipStoryTruncationNone = codemodel.RelationshipStoryTruncationNone

// relationshipStoryTruncationCount aliases the leaf-owned truncation state
// so the staying evidence tests keep naming it. Delete with
// code_relationship_story.go's handler move.
const relationshipStoryTruncationCount = codemodel.RelationshipStoryTruncationCount

// relationshipStoryTruncationBudget aliases the leaf-owned truncation
// state so the staying evidence tests keep naming it. Delete with
// code_relationship_story.go's handler move.
const relationshipStoryTruncationBudget = codemodel.RelationshipStoryTruncationBudget
