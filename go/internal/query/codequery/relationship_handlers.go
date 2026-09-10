// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/queryselector"
)

// The relationshipsRequest type split to
// codemodel/code_relationships_resolution.go (#6060 lane A L1) with the
// name-target resolver that reads it. Root's family_code_shim.go aliases
// it back so the staying relationships handler keeps its literal.

// errContentRelationshipBuilderNotConfigured is relationshipsFromEntity's
// sentinel for a nil CodeHandler.ContentRelationships. It is returned only
// after the entity is already resolved (relationshipsFromContent calls
// resolveRelationshipEntity first), so an unknown entity still gets its
// existing 404 instead of this 503 (#6060).
var errContentRelationshipBuilderNotConfigured = errors.New("content relationship builder not configured")

// handleRelationships returns incoming and outgoing relationships for an entity.
func (h *CodeHandler) handleRelationships(w http.ResponseWriter, r *http.Request) {
	var req relationshipsRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.EntityID == "" && strings.TrimSpace(req.Name) == "" {
		WriteError(w, http.StatusBadRequest, "entity_id or name is required")
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, relationshipCapability(req.Direction, req.RelationshipType)) {
		return
	}
	ctx := r.Context()
	if strings.TrimSpace(req.EntityID) == "" && strings.TrimSpace(req.Name) != "" {
		resolved, resolution, err := codemodel.ResolveRelationshipsNameTarget(ctx, h.Content, req)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if resolution != nil {
			WriteSuccess(
				w,
				r,
				http.StatusOK,
				codemodel.AmbiguousRelationshipsResponse(req, *resolution),
				BuildTruthEnvelope(h.profile(), relationshipCapability(req.Direction, req.RelationshipType), TruthBasisContentIndex, "resolved from content-backed relationship target candidates"),
			)
			return
		}
		if resolved != nil {
			req.EntityID = resolved.EntityID
		}
	}

	direction, err := normalizeRelationshipDirection(req.Direction)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	relationshipType := strings.ToUpper(strings.TrimSpace(req.RelationshipType))
	if req.Transitive {
		h.serveTransitiveRelationships(w, r, ctx, req, direction, relationshipType)
		return
	}
	capability := relationshipCapability(direction, relationshipType)

	row, err := h.relationshipsGraphRow(ctx, req.EntityID, req.Name, req.RepoID, direction, relationshipType)
	if err != nil {
		if WriteGraphReadError(w, r, err, capability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		response, fallbackErr := h.relationshipsFromContent(ctx, req.EntityID, req.Name, req.RepoID)
		if fallbackErr != nil {
			if errors.Is(fallbackErr, errContentRelationshipBuilderNotConfigured) {
				WriteError(w, http.StatusServiceUnavailable, fallbackErr.Error())
				return
			}
			WriteError(w, http.StatusInternalServerError, fallbackErr.Error())
			return
		}
		if response == nil {
			WriteError(w, http.StatusNotFound, "entity not found")
			return
		}
		WriteSuccess(w, r, http.StatusOK, filterRelationshipResponse(response, direction, relationshipType), BuildTruthEnvelope(h.profile(), capability, TruthBasisContentIndex, "resolved from content-backed relationship fallback"))
		return
	}

	response := map[string]any{
		"entity_id":  StringVal(row, "id"),
		"name":       StringVal(row, "name"),
		"labels":     StringSliceVal(row, "labels"),
		"file_path":  StringVal(row, "file_path"),
		"repo_id":    StringVal(row, "repo_id"),
		"repo_name":  StringVal(row, "repo_name"),
		"language":   StringVal(row, "language"),
		"start_line": IntVal(row, "start_line"),
		"end_line":   IntVal(row, "end_line"),
		"outgoing":   querycontract.FilterNullRelationships(row["outgoing"]),
		"incoming":   querycontract.FilterNullRelationships(row["incoming"]),
	}
	if metadata := querycontract.GraphResultMetadata(row); len(metadata) > 0 {
		response["metadata"] = metadata
	}
	if err := h.hydrateRelationshipResponseRepoIdentity(ctx, response); err != nil {
		if WriteGraphReadError(w, r, err, capability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	codemodel.NormalizeGraphRelationships(response)
	response = filterRelationshipResponse(response, direction, relationshipType)
	enriched, err := h.enrichGraphSearchResultsWithContentMetadata(
		ctx,
		[]map[string]any{response},
		StringVal(row, "repo_id"),
		StringVal(row, "name"),
		1,
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteSuccess(w, r, http.StatusOK, enriched[0], BuildTruthEnvelope(h.profile(), capability, TruthBasisAuthoritativeGraph, "resolved from graph relationships"))
}

// serveTransitiveRelationships answers a transitive CALLS request. Every
// path writes exactly one response. Extracted from handleRelationships to
// keep the handler under the repo's 150-line function cap; the logic is
// unchanged.
func (h *CodeHandler) serveTransitiveRelationships(
	w http.ResponseWriter,
	r *http.Request,
	ctx context.Context,
	req relationshipsRequest,
	direction string,
	relationshipType string,
) {
	if relationshipType != "CALLS" {
		WriteError(w, http.StatusBadRequest, "transitive relationships are only supported for CALLS")
		return
	}
	if direction == "" {
		WriteError(w, http.StatusBadRequest, "direction is required for transitive CALLS relationships")
		return
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = 5
	}
	if req.MaxDepth > 10 {
		req.MaxDepth = 10
	}
	capability := transitiveRelationshipCapability(direction)
	if querycontract.CapabilityUnsupported(h.profile(), capability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			transitiveRelationshipUnsupportedMessage(direction),
			ErrorCodeUnsupportedCapability,
			capability,
			h.profile(),
			querycontract.RequiredProfile(capability),
		)
		return
	}

	row, err := h.transitiveRelationshipsGraphRow(ctx, req)
	if err != nil {
		if WriteGraphReadError(w, r, err, capability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		WriteError(w, http.StatusNotFound, "entity not found")
		return
	}

	response := map[string]any{
		"entity_id":  StringVal(row, "id"),
		"name":       StringVal(row, "name"),
		"labels":     StringSliceVal(row, "labels"),
		"file_path":  StringVal(row, "file_path"),
		"repo_id":    StringVal(row, "repo_id"),
		"repo_name":  StringVal(row, "repo_name"),
		"language":   StringVal(row, "language"),
		"start_line": IntVal(row, "start_line"),
		"end_line":   IntVal(row, "end_line"),
		"outgoing":   mapRelationships(row["outgoing"]),
		"incoming":   mapRelationships(row["incoming"]),
	}
	if metadata := querycontract.GraphResultMetadata(row); len(metadata) > 0 {
		response["metadata"] = metadata
	}
	if err := h.hydrateRelationshipResponseRepoIdentity(ctx, response); err != nil {
		if WriteGraphReadError(w, r, err, capability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	codemodel.NormalizeGraphRelationships(response)
	response = filterRelationshipResponse(response, direction, relationshipType)
	enriched, err := h.enrichGraphSearchResultsWithContentMetadata(
		ctx,
		[]map[string]any{response},
		StringVal(row, "repo_id"),
		StringVal(row, "name"),
		1,
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteSuccess(w, r, http.StatusOK, enriched[0], BuildTruthEnvelope(h.profile(), capability, TruthBasisAuthoritativeGraph, "resolved from transitive graph relationships"))
}

func (h *CodeHandler) hydrateRelationshipResponseRepoIdentity(ctx context.Context, response map[string]any) error {
	if len(response) == 0 {
		return nil
	}
	entityID := StringVal(response, "entity_id")
	if entityID == "" {
		entityID = StringVal(response, "id")
	}
	entity := map[string]any{
		"id":        entityID,
		"repo_id":   StringVal(response, "repo_id"),
		"repo_name": StringVal(response, "repo_name"),
		"labels":    response["labels"],
	}
	querycontract.ClearResolvedEntityRepoProjectionPlaceholders(entity)
	if h == nil {
		return nil
	}
	if _, err := queryselector.HydrateResolvedEntityRepoIdentity(ctx, h.Neo4j, h.Content, []map[string]any{entity}); err != nil {
		return fmt.Errorf("hydrate relationship repo identity: %w", err)
	}
	response["repo_id"] = StringVal(entity, "repo_id")
	response["repo_name"] = StringVal(entity, "repo_name")
	return nil
}

// The pre-move spellings the handler and the pinned readers below
// resolve live in relationship_forwarders.go, forwarding to the
// relationships leaf. The pinned readers stay here under their
// queryplan source_sha256 pins.

func (h *CodeHandler) relationshipsGraphRow(
	ctx context.Context,
	entityID string,
	name string,
	repoID string,
	direction string,
	relationshipType string,
) (map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil
	}
	if h.graphBackend() == GraphBackendNornicDB {
		return h.nornicDBRelationshipsGraphRow(ctx, entityID, name, repoID, direction, relationshipType)
	}

	if strings.TrimSpace(entityID) != "" {
		return h.Neo4j.RunSingle(ctx, relationshipGraphRowCypher(graphEntityIDPredicate("e", "$entity_id")), map[string]any{
			"entity_id": entityID,
		})
	}
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}
	if strings.TrimSpace(repoID) != "" {
		return h.Neo4j.RunSingle(ctx, relationshipGraphRowCypher(
			"e.name = $name AND EXISTS { MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository) WHERE repo.id = $repo_id }",
		), map[string]any{
			"name":    name,
			"repo_id": repoID,
		})
	}

	rows, err := h.Neo4j.Run(ctx, relationshipGraphRowCypher("e.name = $name"), map[string]any{
		"name": name,
	})
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, nil
	}
	return rows[0], nil
}

func (h *CodeHandler) transitiveRelationshipsGraphRow(
	ctx context.Context,
	req relationshipsRequest,
) (map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil
	}
	if h.graphBackend() == GraphBackendNornicDB {
		metadataRow, err := h.nornicDBRelationshipMetadataRow(ctx, req.EntityID, req.Name, req.RepoID)
		if err != nil || metadataRow == nil {
			return metadataRow, err
		}
		rows, err := h.nornicDBTransitiveRelationshipRows(
			ctx,
			StringVal(metadataRow, "id"),
			req.Direction,
			req.MaxDepth,
		)
		if err != nil {
			return nil, err
		}
		return buildTransitiveRelationshipGraphResponse(metadataRow, rows, req.Direction), nil
	}

	metadataRow, err := h.relationshipsGraphRow(ctx, req.EntityID, req.Name, req.RepoID, "", "")
	if err != nil || metadataRow == nil {
		return metadataRow, err
	}

	cypher, params := buildTransitiveRelationshipRowsCypher(
		StringVal(metadataRow, "id"),
		req.Direction,
		req.MaxDepth,
		h.graphBackend(),
	)
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return buildTransitiveRelationshipGraphResponse(metadataRow, rows, req.Direction), nil
}

func (h *CodeHandler) relationshipsFromContent(
	ctx context.Context,
	entityID string,
	name string,
	repoID string,
) (map[string]any, error) {
	if h == nil || h.Content == nil {
		return nil, nil
	}

	entity, err := h.resolveRelationshipEntity(ctx, entityID, name, repoID)
	if err != nil || entity == nil {
		return nil, err
	}

	return h.relationshipsFromEntity(ctx, *entity)
}

func (h *CodeHandler) resolveRelationshipEntity(
	ctx context.Context,
	entityID string,
	name string,
	repoID string,
) (*EntityContent, error) {
	if strings.TrimSpace(entityID) != "" {
		return h.Content.GetEntityContent(ctx, entityID)
	}
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}

	var (
		matches []EntityContent
		err     error
	)
	if strings.TrimSpace(repoID) != "" {
		matches, err = h.Content.SearchEntitiesByName(ctx, repoID, "", name, 2)
	} else {
		matches, err = h.Content.SearchEntitiesByNameAnyRepo(ctx, "", name, 2)
	}
	if err != nil {
		return nil, err
	}
	if len(matches) != 1 {
		return nil, nil
	}
	return &matches[0], nil
}

func (h *CodeHandler) relationshipsFromEntity(
	ctx context.Context,
	entity EntityContent,
) (map[string]any, error) {
	if h.ContentRelationships == nil {
		return nil, errContentRelationshipBuilderNotConfigured
	}
	// CodeHandler.Logger (code.go) is not passed here: no file in the code
	// family calls the logger directly, and the sole sink -- the
	// mixed-vintage k8s SELECTS Debug diagnostic
	// (logK8sSelectMixedVintageDrop, content_relationships.go) -- nil-checks
	// it, so passing nil is a no-op rather than a behavior change (#6060).
	relationshipSet, err := h.ContentRelationships.BuildContentRelationships(ctx, h.Content, entity, nil)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"entity_id":  entity.EntityID,
		"name":       entity.EntityName,
		"labels":     []string{entity.EntityType},
		"file_path":  entity.RelativePath,
		"repo_id":    entity.RepoID,
		"language":   entity.Language,
		"start_line": entity.StartLine,
		"end_line":   entity.EndLine,
		"metadata":   entity.Metadata,
		"outgoing":   relationshipSet.Outgoing,
		"incoming":   relationshipSet.Incoming,
	}, nil
}
