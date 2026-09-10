// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships/story"
)

// The class-hierarchy, graph, and override readers live here. The pinned
// ones stay byte-identical under their queryplan source_sha256 pins;
// the pure builders they resolve moved to the relationships/story leaf
// and are reached through same-named forwarders in story_forwarders.go.

func (h *CodeHandler) relationshipStoryClassHierarchy(
	ctx context.Context,
	req codemodel.RelationshipStoryRequest,
	entity *EntityContent,
	relationships []map[string]any,
) (map[string]any, error) {
	var methods []map[string]any
	var ancestorDepthRows []map[string]any
	var descendantDepthRows []map[string]any
	var ancestorRawCount, descendantRawCount int
	errs := make(chan error, 3)
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		rows, err := h.relationshipStoryClassMethods(ctx, req, entity)
		if err != nil {
			errs <- err
			return
		}
		methods = rows
	}()
	go func() {
		defer wg.Done()
		rows, rawCount, err := h.relationshipStoryInheritanceDepthRows(ctx, req, entity, "outgoing")
		if err != nil {
			errs <- err
			return
		}
		ancestorDepthRows, ancestorRawCount = rows, rawCount
	}()
	go func() {
		defer wg.Done()
		rows, rawCount, err := h.relationshipStoryInheritanceDepthRows(ctx, req, entity, "incoming")
		if err != nil {
			errs <- err
			return
		}
		descendantDepthRows, descendantRawCount = rows, rawCount
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return nil, err
		}
	}

	parents, children := story.SplitClassHierarchyRelationships(relationships)
	return map[string]any{
		"methods":           story.MethodRowsWithHandles(methods, req.NormalizedLimit()),
		"methods_truncated": len(methods) > req.NormalizedLimit(),
		"parents":           story.RowsWithHandles(story.LimitRows(parents, req.NormalizedLimit())),
		"children":          story.RowsWithHandles(story.LimitRows(children, req.NormalizedLimit())),
		"depth_summary": story.DepthSummary(
			ancestorDepthRows, descendantDepthRows,
			ancestorRawCount, descendantRawCount, req.NormalizedLimit()),
	}, nil
}

func (h *CodeHandler) relationshipStoryClassMethods(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return []map[string]any{}, nil
	}
	entityID := relationshipStoryEntityID(req, entity)
	if entityID == "" {
		return []map[string]any{}, nil
	}
	if h.graphBackend() == GraphBackendNornicDB {
		return h.nornicDBRelationshipStoryClassMethods(ctx, req, entityID)
	}
	cypher, params := relationshipStoryClassMethodsCypher(req, entityID, graphEntityIDPredicate, codeGrantAccessFilter(ctx))
	return h.Neo4j.Run(ctx, cypher, params)
}

func (h *CodeHandler) relationshipStoryInheritanceDepthRows(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
	direction string,
) ([]map[string]any, int, error) {
	if h == nil || h.Neo4j == nil {
		return []map[string]any{}, 0, nil
	}
	entityID := relationshipStoryEntityID(req, entity)
	if entityID == "" {
		return []map[string]any{}, 0, nil
	}
	if h.graphBackend() == GraphBackendNornicDB {
		return h.nornicDBRelationshipStoryInheritanceDepthRows(ctx, req, entityID, direction)
	}
	cypher, params := relationshipStoryInheritanceDepthCypher(req, entityID, direction, graphEntityIDPredicate, codeGrantAccessFilter(ctx))
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	// The compat lane bounds its interior in the statement, so no row is
	// dropped after the read and the raw count is the returned count.
	return rows, len(rows), err
}

func (h *CodeHandler) relationshipStoryOverrideRows(
	ctx context.Context,
	req relationshipStoryRequest,
) ([]map[string]any, string, TruthBasis, error) {
	if h == nil || h.Neo4j == nil {
		return nil, "", "", errSymbolBackendUnavailable
	}
	cypher, params := relationshipStoryOverrideRowsCypher(req, codeGrantAccessFilter(ctx))
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, "", "", err
	}
	return rows, "graph", TruthBasisAuthoritativeGraph, nil
}

func (h *CodeHandler) relationshipStoryRelationships(
	ctx context.Context,
	req codemodel.RelationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, string, TruthBasis, error) {
	types, err := req.NormalizedRelationshipTypes()
	if err != nil {
		return nil, "", "", err
	}
	if len(types) > 1 && !req.GraphAnchorPropertyResolved && h != nil && h.Neo4j != nil && h.graphBackend() == GraphBackendNornicDB &&
		!req.IncludeTransitive && nornicDBRelationshipStoryAnchorPreflightSupported(req, entity) {
		resolvedReq, err := h.resolveNornicDBRelationshipStoryAnchorProperty(ctx, req, entity)
		if err != nil {
			return nil, "", "", err
		}
		req = resolvedReq
	}
	if len(types) <= 1 {
		return h.relationshipStoryRelationshipsForType(ctx, req, entity)
	}
	var (
		backend string
		basis   TruthBasis
		merged  []map[string]any
	)
	for _, relationshipType := range types {
		sub := req
		sub.RelationshipType = relationshipType
		sub.RelationshipTypes = nil
		rows, rowsBackend, rowsBasis, rowsErr := h.relationshipStoryRelationshipsForType(ctx, sub, entity)
		if rowsErr != nil {
			return nil, "", "", rowsErr
		}
		backend = rowsBackend
		basis = rowsBasis
		merged = append(merged, rows...)
	}
	return merged, backend, basis, nil
}

func (h *CodeHandler) relationshipStoryRelationshipsForType(
	ctx context.Context,
	req codemodel.RelationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, string, TruthBasis, error) {
	if h != nil && h.Neo4j != nil {
		rows, err := h.relationshipStoryGraphRows(ctx, req, entity)
		if err != nil {
			return nil, "", "", err
		}
		return rows, "graph", TruthBasisAuthoritativeGraph, nil
	}
	if h != nil && h.Content != nil && entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		row, err := h.relationshipsFromEntity(ctx, *entity)
		if err != nil {
			return nil, "", "", err
		}
		return story.ContentRows(row, req), "postgres_content_store", TruthBasisContentIndex, nil
	}
	return nil, "", "", errSymbolBackendUnavailable
}

func (h *CodeHandler) relationshipStoryGraphRows(
	ctx context.Context,
	req codemodel.RelationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, error) {
	if req.IncludeTransitive {
		return h.relationshipStoryTransitiveGraphRows(ctx, req, entity)
	}
	direction, _ := req.NormalizedDirection()
	if direction != "both" {
		return h.relationshipStoryGraphRowsForDirection(ctx, req, entity, direction)
	}

	type directionResult struct {
		direction string
		rows      []map[string]any
		err       error
	}
	results := make(chan directionResult, 2)
	for _, current := range []string{"incoming", "outgoing"} {
		go func(direction string) {
			rows, err := h.relationshipStoryGraphRowsForDirection(ctx, req, entity, direction)
			results <- directionResult{direction: direction, rows: rows, err: err}
		}(current)
	}
	byDirection := map[string][]map[string]any{}
	for range 2 {
		result := <-results
		if result.err != nil {
			return nil, result.err
		}
		byDirection[result.direction] = result.rows
	}
	return story.InterleaveDirections(byDirection["incoming"], byDirection["outgoing"]), nil
}

func (h *CodeHandler) relationshipStoryTransitiveGraphRows(
	ctx context.Context,
	req codemodel.RelationshipStoryRequest,
	entity *EntityContent,
) ([]map[string]any, error) {
	direction, _ := req.NormalizedDirection()
	limit := req.NormalizedLimit() + 1
	rootID := strings.TrimSpace(req.EntityID)
	if entity != nil && strings.TrimSpace(entity.EntityID) != "" {
		rootID = strings.TrimSpace(entity.EntityID)
	}
	if rootID == "" {
		return []map[string]any{}, nil
	}

	frontier := []string{rootID}
	seen := map[string]struct{}{rootID: {}}
	rows := make([]map[string]any, 0, limit)
	for depth := 1; depth <= story.NormalizeMaxDepth(req.MaxDepth) && len(frontier) > 0 && len(rows) < limit; depth++ {
		next := make([]string, 0)
		for _, currentID := range frontier {
			hopReq := req
			hopReq.EntityID = currentID
			hopReq.Offset = 0
			hopReq.Limit = limit - len(rows)
			hopReq.IncludeTransitive = false
			hopRows, err := h.relationshipStoryGraphRowsForDirection(
				ctx,
				hopReq,
				&EntityContent{EntityID: currentID},
				direction,
			)
			if err != nil {
				return nil, err
			}
			for _, hop := range hopRows {
				nextID := story.NextID(hop, direction)
				if nextID == "" {
					continue
				}
				item := cloneQueryAnyMap(hop)
				item["depth"] = depth
				rows = append(rows, item)
				if _, ok := seen[nextID]; !ok {
					seen[nextID] = struct{}{}
					next = append(next, nextID)
				}
				if len(rows) >= limit {
					break
				}
			}
			if len(rows) >= limit {
				break
			}
		}
		frontier = next
	}
	return rows, nil
}

func (h *CodeHandler) relationshipStoryGraphRowsForDirection(
	ctx context.Context,
	req relationshipStoryRequest,
	entity *EntityContent,
	direction string,
) ([]map[string]any, error) {
	if h.graphBackend() == GraphBackendNornicDB {
		return h.nornicDBRelationshipStoryGraphRows(ctx, req, entity, direction)
	}
	cypher, params := relationshipStoryGraphCypher(
		req,
		entity,
		direction,
		graphEntityIDPredicate,
		codeGrantAccessFilter(ctx),
	)
	return h.Neo4j.Run(ctx, cypher, params)
}

// relationshipStoryRequest is the pre-move spelling of
// codemodel.RelationshipStoryRequest. The queryplan-pinned relationship-story
// builders name the bare type, and their source_sha256 digests cover the
// declaration text, so the alias keeps the moved bodies byte-identical
// instead of re-freezing digests around a qualification.
type relationshipStoryRequest = codemodel.RelationshipStoryRequest
