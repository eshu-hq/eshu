// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships/story"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file keeps the pre-move spellings the handlers, the pinned
// readers, and the story tests resolve, forwarding to the
// relationships/story leaf. The pinned readers in story_reads.go and
// story_nornicdb.go stay byte-identical under their queryplan
// source_sha256 pins by resolving through these spellings.

// relationshipStoryData assembles the story payload. Tests name this
// spelling.
func relationshipStoryData(
	req codemodel.RelationshipStoryRequest,
	resolution codemodel.RelationshipStoryResolution,
	rows []map[string]any,
) map[string]any {
	return story.Data(req, resolution, rows)
}

// relationshipStoryDepthSummary reports the deepest inheritance hop in
// each direction and whether the caller's page was full. Tests name
// this spelling.
func relationshipStoryDepthSummary(
	ancestors []map[string]any,
	descendants []map[string]any,
	ancestorsRaw int,
	descendantsRaw int,
	limit int,
) map[string]any {
	return story.DepthSummary(ancestors, descendants, ancestorsRaw, descendantsRaw, limit)
}

// relationshipStoryGraphCypher builds the direct one-direction
// relationship read. The pinned graph reader and tests name this
// spelling.
func relationshipStoryGraphCypher(
	req codemodel.RelationshipStoryRequest,
	entity *EntityContent,
	direction string,
	predicate func(string, string) string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	return story.GraphCypher(req, entity, direction, predicate, access)
}

// relationshipStoryEntityID resolves the entity id a story read
// anchors on. The pinned readers name this spelling.
func relationshipStoryEntityID(req codemodel.RelationshipStoryRequest, entity *EntityContent) string {
	return story.EntityID(req, entity)
}

// relationshipStoryClassMethodsCypher builds the methods-of-a-class
// read. The pinned class reader and tests name this spelling.
func relationshipStoryClassMethodsCypher(
	req codemodel.RelationshipStoryRequest,
	entityID string,
	predicate func(string, string) string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	return story.ClassMethodsCypher(req, entityID, predicate, access)
}

// relationshipStoryInheritanceDepthCypher builds one direction of the
// bounded INHERITS walk. The pinned depth reader and tests name this
// spelling.
func relationshipStoryInheritanceDepthCypher(
	req codemodel.RelationshipStoryRequest,
	entityID string,
	direction string,
	predicate func(string, string) string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	return story.InheritanceDepthCypher(req, entityID, direction, predicate, access)
}

// relationshipStoryOverrideRowsCypher builds the repo-anchored
// OVERRIDES read. The pinned override reader and tests name this
// spelling.
func relationshipStoryOverrideRowsCypher(
	req codemodel.RelationshipStoryRequest,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	return story.OverrideRowsCypher(req, access)
}

// relationshipStoryAccessParams binds the parameters the story reads'
// repository predicates reference. The hot cypher builders name this
// spelling.
func relationshipStoryAccessParams(
	req codemodel.RelationshipStoryRequest,
	access querycontract.RepositoryAccessFilter,
	params map[string]any,
) map[string]any {
	return story.AccessParams(req, access, params)
}

// relationshipStoryRepoPredicates returns the predicates that decide
// which relationship rows the caller may see. The hot cypher builders
// and tests name this spelling.
func relationshipStoryRepoPredicates(
	req codemodel.RelationshipStoryRequest,
	access querycontract.RepositoryAccessFilter,
	sourceAlias string,
	targetAlias string,
	anchorAlias string,
) []string {
	return story.RepoPredicates(req, access, sourceAlias, targetAlias, anchorAlias)
}

// relationshipStoryGrantPredicates returns the caller's grant condition
// on each alias's repo_id. The class builders and the hot cypher
// builders name this spelling.
func relationshipStoryGrantPredicates(access querycontract.RepositoryAccessFilter, aliases ...string) []string {
	return story.GrantPredicates(access, aliases...)
}

// nornicDBRelationshipStoryWhere joins predicates into a WHERE clause.
// The hot cypher builders name this spelling.
func nornicDBRelationshipStoryWhere(predicates []string) string {
	return story.Where(predicates)
}

// normalizedRelationshipStoryMaxDepth bounds a caller-supplied
// traversal depth. The hot cypher builder and the transitive walk name
// this spelling.
func normalizedRelationshipStoryMaxDepth(maxDepth int) int {
	return story.NormalizeMaxDepth(maxDepth)
}

// nornicDBRelationshipStoryAnchorPreflightSupported reports whether the
// one-time uid-first preflight has a bounded multi-type lookup. The
// pinned anchor resolver names this spelling.
func nornicDBRelationshipStoryAnchorPreflightSupported(
	req relationshipStoryRequest,
	entity *EntityContent,
) bool {
	return story.AnchorPreflightSupported(req, entity)
}

// nornicDBRelationshipStoryAnchorLookupCypher builds the one-row
// identity-preflight read. The pinned anchor resolver names this
// spelling.
func nornicDBRelationshipStoryAnchorLookupCypher(
	entityLabel string,
	property string,
	repoScoped bool,
) string {
	return story.AnchorLookupCypher(entityLabel, property, repoScoped)
}

// normalizeNornicDBRelationshipStoryRows collapses the NornicDB story
// projection's aliased columns onto the canonical story keys. The
// pinned readers name this spelling.
func normalizeNornicDBRelationshipStoryRows(rows []map[string]any) []map[string]any {
	return story.NormalizeRows(rows)
}

// nornicDBRelationshipStoryClassMethodsCypher builds the NornicDB
// methods-of-a-class read. The pinned class reader names this
// spelling.
func nornicDBRelationshipStoryClassMethodsCypher(
	req relationshipStoryRequest,
	entityID string,
	property string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	return story.NornicDBClassMethodsCypher(req, entityID, property, access)
}

// relationshipStoryGrantedCandidates resolves the target-name lookup
// with the caller's repository grant bound at the read. The staying
// resolver names this spelling.
func relationshipStoryGrantedCandidates(
	ctx context.Context,
	content ContentStore,
	req codemodel.RelationshipStoryRequest,
	allowed []string,
) ([]EntityContent, error) {
	return story.GrantedCandidates(ctx, content, req, allowed)
}

// relationshipStoryClassHierarchyCandidates keeps only the candidates
// whose entity type can anchor a class-hierarchy story. The staying
// resolver names this spelling.
func relationshipStoryClassHierarchyCandidates(candidates []EntityContent) []EntityContent {
	return story.ClassHierarchyCandidates(candidates)
}

// relationshipStoryClassHierarchyEntityType reports whether an entity
// type can anchor a class-hierarchy story. The staying resolver names
// this spelling.
func relationshipStoryClassHierarchyEntityType(entityType string) bool {
	return story.ClassHierarchyEntityType(entityType)
}
