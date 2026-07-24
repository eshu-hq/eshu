// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var errWorkloadGraphUnavailable = errors.New("authoritative graph workload resolution is unavailable")

func (h *EntityHandler) writeWorkloadEntityResolution(
	w http.ResponseWriter,
	r *http.Request,
	req resolveEntityRequest,
	limit int,
) bool {
	if !strings.EqualFold(strings.TrimSpace(req.Type), "workload") {
		return false
	}

	entities, err := h.resolveWorkloadEntities(r.Context(), req.Name, req.RepoID, limit+1)
	if err != nil {
		if errors.Is(err, errWorkloadGraphUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return true
		}
		if WriteGraphReadError(w, r, err, "code_search.exact_symbol") {
			return true
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve workload: %v", err))
		return true
	}
	for i := range entities {
		attachSemanticSummary(entities[i])
	}
	entities, truncated := trimResolvedEntityPage(entities, limit)
	if entities == nil {
		entities = []map[string]any{}
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		resolvedEntityResponse(entities, limit, truncated),
		workloadEntityResolveTruthEnvelope(h.profile()),
	)
	return true
}

func workloadEntityResolveTruthEnvelope(profile QueryProfile) *TruthEnvelope {
	return BuildTruthEnvelope(
		profile,
		"code_search.exact_symbol",
		TruthBasisAuthoritativeGraph,
		"resolved by exact workload name from the authoritative graph",
	)
}

func (h *EntityHandler) resolveWorkloadEntities(
	ctx context.Context,
	name string,
	repoID string,
	limit int,
) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, errWorkloadGraphUnavailable
	}

	access := repositoryAccessFilterFromContext(ctx)
	propertyCypher, relationshipCypher, params := buildResolveWorkloadQueries(name, repoID, limit, access)
	propertyRows, err := h.Neo4j.Run(ctx, propertyCypher, params)
	if err != nil {
		return nil, fmt.Errorf("query workloads by repository property: %w", err)
	}

	relationshipRows, err := h.Neo4j.Run(ctx, relationshipCypher, params)
	if err != nil {
		return nil, fmt.Errorf("query workloads by defining repository: %w", err)
	}

	rows := append(propertyRows, relationshipRows...)
	entities := make([]map[string]any, 0, len(rows))
	entitiesByID := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		entity := map[string]any{
			"id":        StringVal(row, "id"),
			"labels":    StringSliceVal(row, "labels"),
			"name":      StringVal(row, "name"),
			"repo_id":   StringVal(row, "repo_id"),
			"repo_name": StringVal(row, "repo_name"),
		}
		id := entityString(entity, "id")
		if id == "" {
			continue
		}
		if existing, ok := entitiesByID[id]; ok {
			if entityString(existing, "repo_id") == "" && entityString(entity, "repo_id") != "" {
				existing["repo_id"] = entity["repo_id"]
			}
			continue
		}
		entities = append(entities, entity)
		entitiesByID[id] = entity
	}
	entities = normalizeResolvedEntities(entities, limit)
	if err := h.hydrateResolvedWorkloadRepoNames(ctx, entities); err != nil {
		return nil, err
	}
	return entities, nil
}

func (h *EntityHandler) hydrateResolvedWorkloadRepoNames(
	ctx context.Context,
	entities []map[string]any,
) error {
	repoIDs := make([]string, 0, len(entities))
	for _, entity := range entities {
		if entityString(entity, "repo_name") == "" {
			repoIDs = append(repoIDs, entityString(entity, "repo_id"))
		}
	}
	repoIDs = sortedUniqueStrings(repoIDs)
	if len(repoIDs) == 0 {
		return nil
	}

	access := repositoryAccessFilterFromContext(ctx)
	cypher, params := buildHydrateResolvedWorkloadRepoNamesQuery(repoIDs, access)
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return fmt.Errorf("hydrate workload repository names: %w", err)
	}
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		names[StringVal(row, "repo_id")] = StringVal(row, "repo_name")
	}

	for _, entity := range entities {
		if entityString(entity, "repo_name") == "" {
			entity["repo_name"] = names[entityString(entity, "repo_id")]
		}
	}
	return nil
}
