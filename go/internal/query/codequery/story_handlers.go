// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/relationships/story"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	relationshipStoryCapability = "call_graph.relationship_story"
)

// The codemodel.RelationshipStoryRequest/codemodel.RelationshipStoryResolution types, the page
// limit bounds, and the request's methods split to
// codemodel/code_relationship_story_evidence_state.go (#6060 lane A L1);
// the evidence-state classifier takes the request there. Root's
// family_code_shim.go aliases the types back so the staying handlers,
// resolvers, and tests keep their names.

func (h *CodeHandler) handleRelationshipStory(w http.ResponseWriter, r *http.Request) {
	var req codemodel.RelationshipStoryRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if querycontract.CapabilityUnsupported(h.profile(), relationshipStoryCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"code relationship story requires a supported query profile",
			ErrorCodeUnsupportedCapability,
			relationshipStoryCapability,
			h.profile(),
			querycontract.RequiredProfile(relationshipStoryCapability),
		)
		return
	}
	if err := req.Validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, relationshipStoryCapability) {
		return
	}

	if req.IsRepoScopedOverrideStory() {
		h.handleRepoScopedOverrideStory(w, r, req)
		return
	}

	resolution, entity, err := h.resolveRelationshipStoryTarget(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if resolution.Status != "resolved" {
		h.writeRelationshipStory(w, r, req, resolution, nil, TruthBasisContentIndex)
		return
	}
	if req.NormalizedQueryType() == "class_hierarchy" && entity != nil &&
		strings.TrimSpace(entity.EntityType) != "" &&
		!relationshipStoryClassHierarchyEntityType(entity.EntityType) {
		WriteError(w, http.StatusBadRequest, "class_hierarchy target must resolve to a class or inheritable entity")
		return
	}

	relationships, sourceBackend, basis, err := h.relationshipStoryRelationships(r.Context(), req, entity)
	if err != nil {
		if errors.Is(err, errSymbolBackendUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if WriteGraphReadError(w, r, err, relationshipStoryCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := story.Data(req, resolution, relationships)
	data["source_backend"] = sourceBackend
	if req.NormalizedQueryType() == "class_hierarchy" {
		hierarchy, err := h.relationshipStoryClassHierarchy(r.Context(), req, entity, relationships)
		if err != nil {
			if WriteGraphReadError(w, r, err, relationshipStoryCapability) {
				return
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data["class_hierarchy"] = hierarchy
		story.MarkClassHierarchyCoverage(data, req)
	}
	if req.NormalizedQueryType() == "overrides" {
		data["override_story"] = story.OverrideData(req, relationships)
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), relationshipStoryCapability, basis, "resolved from bounded relationship story lookup"),
	)
}

func (h *CodeHandler) handleRepoScopedOverrideStory(
	w http.ResponseWriter,
	r *http.Request,
	req codemodel.RelationshipStoryRequest,
) {
	if strings.TrimSpace(req.RepoID) == "" {
		WriteError(w, http.StatusBadRequest, "repo_id is required for repo-scoped overrides")
		return
	}
	if relationshipStoryGrantBlocked(r.Context(), req) {
		h.writeRelationshipStory(w, r, req, codemodel.RelationshipStoryResolution{
			Status: "not_found",
			RepoID: strings.TrimSpace(req.RepoID),
		}, nil, TruthBasisContentIndex)
		return
	}
	rows, sourceBackend, basis, err := h.relationshipStoryOverrideRows(r.Context(), req)
	if err != nil {
		if err == errSymbolBackendUnavailable {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if WriteGraphReadError(w, r, err, relationshipStoryCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resolution := codemodel.RelationshipStoryResolution{
		Status:   "repo_scoped",
		RepoID:   strings.TrimSpace(req.RepoID),
		Language: strings.TrimSpace(req.Language),
	}
	data := story.Data(req, resolution, rows)
	data["source_backend"] = sourceBackend
	data["override_story"] = story.OverrideData(req, rows)
	story.MarkRepoOverrideCoverage(data)
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), relationshipStoryCapability, basis, "resolved from bounded override story lookup"),
	)
}

func (h *CodeHandler) writeRelationshipStory(
	w http.ResponseWriter,
	r *http.Request,
	req codemodel.RelationshipStoryRequest,
	resolution codemodel.RelationshipStoryResolution,
	relationships []map[string]any,
	basis TruthBasis,
) {
	data := story.Data(req, resolution, relationships)
	if basis == TruthBasisContentIndex {
		data["source_backend"] = "postgres_content_store"
		if h == nil || h.Content == nil {
			data["source_backend"] = "unavailable"
		}
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), relationshipStoryCapability, basis, "resolved from bounded relationship story lookup"),
	)
}

func (h *CodeHandler) resolveRelationshipStoryTarget(
	ctx context.Context,
	req codemodel.RelationshipStoryRequest,
) (codemodel.RelationshipStoryResolution, *EntityContent, error) {
	target := req.EffectiveTarget()
	if relationshipStoryGrantBlocked(ctx, req) {
		return codemodel.RelationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}
	if entityID := strings.TrimSpace(req.EntityID); entityID != "" {
		resolution := codemodel.RelationshipStoryResolution{
			Status:   "resolved",
			Target:   target,
			EntityID: entityID,
			RepoID:   strings.TrimSpace(req.RepoID),
			Language: strings.TrimSpace(req.Language),
		}
		if h != nil && h.Content != nil {
			entity, err := h.Content.GetEntityContent(ctx, entityID)
			if err != nil {
				return resolution, nil, err
			}
			if entity != nil {
				access := codeGrantAccessFilter(ctx)
				if strings.TrimSpace(req.RepoID) != "" && strings.TrimSpace(entity.RepoID) != strings.TrimSpace(req.RepoID) {
					return codemodel.RelationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
				}
				if !access.AllowsRepositoryID(strings.TrimSpace(entity.RepoID)) {
					return codemodel.RelationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
				}
				resolution.Name = entity.EntityName
				resolution.RepoID = entity.RepoID
				resolution.Language = entity.Language
				return resolution, entity, nil
			}
		}
		return resolution, &EntityContent{EntityID: entityID, EntityName: target, RepoID: req.RepoID}, nil
	}
	if h == nil || h.Content == nil {
		return codemodel.RelationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}

	candidates, err := h.relationshipStoryCandidates(ctx, req)
	if err != nil {
		return codemodel.RelationshipStoryResolution{}, nil, err
	}
	if len(candidates) == 0 {
		return codemodel.RelationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}
	candidates = querycontract.ExactEntityNameMatches(candidates, target)
	if req.NormalizedQueryType() == "class_hierarchy" {
		candidates = relationshipStoryClassHierarchyCandidates(candidates)
	}
	if len(candidates) == 0 {
		return codemodel.RelationshipStoryResolution{Status: "not_found", Target: target}, nil, nil
	}
	codemodel.SortRelationshipStoryCandidates(candidates)
	limit := req.NormalizedLimit()
	truncated := len(candidates) > limit
	if len(candidates) != 1 {
		return codemodel.RelationshipStoryResolution{
			Status:     "ambiguous",
			Target:     target,
			RepoID:     strings.TrimSpace(req.RepoID),
			Language:   strings.TrimSpace(req.Language),
			Candidates: codemodel.RelationshipStoryCandidateMaps(candidates, limit),
			Truncated:  truncated,
		}, nil, nil
	}
	entity := candidates[0]
	return codemodel.RelationshipStoryResolution{
		Status:   "resolved",
		Target:   target,
		EntityID: entity.EntityID,
		Name:     entity.EntityName,
		RepoID:   entity.RepoID,
		Language: entity.Language,
	}, &entity, nil
}

func (h *CodeHandler) relationshipStoryCandidates(
	ctx context.Context,
	req codemodel.RelationshipStoryRequest,
) ([]EntityContent, error) {
	allowed, blocked := codeContentGrantScope(ctx, req.RepoID)
	if blocked {
		return nil, nil
	}
	return relationshipStoryGrantedCandidates(ctx, h.Content, req, allowed)
}

// relationshipStoryGrantBlocked reports whether the caller's grant admits
// nothing, so the route must answer its own not-found story without reading a
// backend.
//
// not_found rather than an error or an empty-but-distinguishable shape: it is
// the same answer a target that does not exist produces, so a grantless caller
// cannot use this route to probe which symbols the index holds.
func relationshipStoryGrantBlocked(ctx context.Context, req codemodel.RelationshipStoryRequest) bool {
	_, blocked := codeContentGrantScope(ctx, req.RepoID)
	return blocked
}
