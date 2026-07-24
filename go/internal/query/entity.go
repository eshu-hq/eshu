// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// EntityHandler exposes HTTP routes for entity queries.
type EntityHandler struct {
	Neo4j                    GraphQuery
	Content                  ContentStore
	CICDRunCorrelations      CICDRunCorrelationStore
	ContainerImageIdentities ContainerImageIdentityStore
	SBOMAttachments          SBOMAttestationAttachmentStore
	Profile                  QueryProfile
	Logger                   *slog.Logger
	// Instruments backs operator-facing metrics for degraded-but-successful
	// entity-context reads, e.g. QueryK8sSelectCandidateScanTruncated. Nil is
	// tolerated (metric emission is skipped) so tests can construct
	// EntityHandler without wiring the full telemetry stack.
	Instruments *telemetry.Instruments
}

// Mount registers all entity routes on the given mux.
func (h *EntityHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/entities/resolve", h.resolveEntity)
	mux.HandleFunc("GET /api/v0/entities/{entity_id}/context", h.getEntityContext)
	mux.HandleFunc("GET /api/v0/workloads/{workload_id}/context", h.getWorkloadContext)
	mux.HandleFunc("GET /api/v0/workloads/{workload_id}/story", h.getWorkloadStory)
	mux.HandleFunc("GET /api/v0/services/{service_name}/context", h.getServiceContext)
	mux.HandleFunc("GET /api/v0/services/{service_name}/story", h.getServiceStory)
	mux.HandleFunc("GET /api/v0/investigations/services/{service_name}", h.investigateService)
}

func (h *EntityHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// resolveEntityRequest is the request body for entity resolution.
type resolveEntityRequest struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	RepoID string `json:"repo_id"`
	Limit  int    `json:"limit"`
}

const serviceLookupWhereClause = "w.name = $service_name OR w.id = $service_name" // #nosec G101 -- Cypher parameterised query template, not a hardcoded credential

func buildResolveEntityGraphQuery(
	req resolveEntityRequest,
	limit int,
	access repositoryAccessFilter,
) (string, map[string]any) {
	repositoryAnchored := req.RepoID != ""
	if !repositoryAnchored {
		return "", nil
	}
	cypher := `MATCH (e) WHERE e.name = $name`
	params := map[string]any{"name": req.Name}
	if repositoryAnchored {
		cypher = `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e) WHERE e.name = $name`
		params["repo_id"] = req.RepoID
	}

	if req.Type != "" {
		graphLabel, semanticKey, semanticValue, ok := resolveGraphEntityType(req.Type)
		if ok {
			cypher += " AND $type IN labels(e)"
			params["type"] = graphLabel
			if semanticKey != "" {
				cypher += fmt.Sprintf(" AND coalesce(e.%s, '') = $semantic_filter", semanticKey)
				params["semantic_filter"] = semanticValue
			}
		}
	}

	if !repositoryAnchored && access.scoped() {
		cypher += `
			AND EXISTS {
				MATCH (e)<-[:CONTAINS]-(scopeFile:File)<-[:REPO_CONTAINS]-(scopeRepo:Repository)
				WHERE ` + access.graphCondition("scopeRepo") + `
			}
		`
		params = access.graphParams(params)
	}

	if !repositoryAnchored {
		cypher += `
			OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
		`
		if access.scoped() {
			cypher += `
			WHERE ` + access.graphCondition("r") + `
		`
		}
	}
	cypher += `
		RETURN e.id as id, labels(e) as labels, e.name as name,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		ORDER BY e.name
		LIMIT $limit
	`
	params["limit"] = limit + 1
	return cypher, params
}

// resolveEntity resolves an entity by name and optional type/repo filters.
func (h *EntityHandler) resolveEntity(w http.ResponseWriter, r *http.Request) {
	var req resolveEntityRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	canonicalContentHandle := strings.HasPrefix(strings.TrimSpace(req.Name), contentEntityIDPrefix)
	if req.Type != "" && req.Type != "workload" && !knownResolveEntityType(req.Type) {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("unknown entity type %q", req.Type))
		return
	}
	if req.RepoID == "" {
		if req.Type == "" && !canonicalContentHandle {
			WriteError(w, http.StatusBadRequest, "global entity resolution requires type or repo_id")
			return
		}
		if _, graphOnly := globalGraphOnlyEntityTypes[req.Type]; graphOnly {
			WriteError(w, http.StatusBadRequest, fmt.Sprintf("global entity type %q requires repo_id", req.Type))
			return
		}
	}
	limit := normalizeResolveEntityLimit(req.Limit)
	access := repositoryAccessFilterFromContext(r.Context())
	if req.RepoID != "" {
		resolvedRepoID, err := resolveRepositorySelectorExactForAccess(r.Context(), h.Neo4j, h.Content, req.RepoID, access)
		if err != nil {
			if WriteGraphReadError(w, r, err, "code_search.fuzzy_symbol") {
				return
			}
			status := http.StatusBadRequest
			if isRepositorySelectorNotFound(err) {
				status = http.StatusNotFound
			}
			WriteError(w, status, err.Error())
			return
		}
		req.RepoID = resolvedRepoID
	}
	if access.empty() {
		truth := entityResolveTruthEnvelope(h.profile())
		if req.RepoID == "" {
			truth = globalContentEntityResolveTruthEnvelope(h.profile())
		}
		if strings.EqualFold(strings.TrimSpace(req.Type), "workload") {
			truth = workloadEntityResolveTruthEnvelope(h.profile())
		}
		WriteSuccess(w, r, http.StatusOK, resolvedEntityResponse([]map[string]any{}, limit, false), truth)
		return
	}
	if h.writeCanonicalContentEntityResolution(w, r, req, limit) {
		return
	}
	if h.writeWorkloadEntityResolution(w, r, req, limit) {
		return
	}
	if req.RepoID == "" {
		entities, err := h.resolveGlobalContentEntities(r.Context(), req.Name, req.Type, limit+1)
		if err != nil {
			if errors.Is(err, errEntityNameSearchUnavailable) {
				WriteError(w, http.StatusServiceUnavailable, err.Error())
				return
			}
			if writeContentSubstringIndexUnavailable(w, err) {
				return
			}
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve content entities: %v", err))
			return
		}
		entities, truncated := trimResolvedEntityPage(normalizeResolvedEntities(entities, limit+1), limit)
		WriteSuccess(w, r, http.StatusOK, resolvedEntityResponse(entities, limit, truncated), globalContentEntityResolveTruthEnvelope(h.profile()))
		return
	}

	cypher, params := buildResolveEntityGraphQuery(req, limit, access)

	var (
		rows []map[string]any
		err  error
	)
	if h.Neo4j != nil {
		rows, err = h.Neo4j.Run(r.Context(), cypher, params)
		if err != nil {
			if WriteGraphReadError(w, r, err, "code_search.fuzzy_symbol") {
				return
			}
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
			return
		}
	}

	entities := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entity := map[string]any{
			"id":         StringVal(row, "id"),
			"labels":     StringSliceVal(row, "labels"),
			"name":       StringVal(row, "name"),
			"file_path":  StringVal(row, "file_path"),
			"repo_id":    StringVal(row, "repo_id"),
			"repo_name":  StringVal(row, "repo_name"),
			"language":   StringVal(row, "language"),
			"start_line": IntVal(row, "start_line"),
			"end_line":   IntVal(row, "end_line"),
		}
		if metadata := graphResultMetadata(row); len(metadata) > 0 {
			entity["metadata"] = metadata
		}
		entities = append(entities, entity)
	}
	entities, err = h.enrichEntityResultsWithContentMetadata(r.Context(), entities, req.RepoID, req.Name, limit+1)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich entities: %v", err))
		return
	}
	for i := range entities {
		attachSemanticSummary(entities[i])
	}
	if _, err := hydrateResolvedEntityRepoIdentity(r.Context(), h.Neo4j, h.Content, entities); err != nil {
		if WriteGraphReadError(w, r, err, "code_search.fuzzy_symbol") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("hydrate entity repo identity: %v", err))
		return
	}
	entities = normalizeResolvedEntities(entities, limit+1)
	entities, truncated := trimResolvedEntityPage(entities, limit)
	if len(entities) == 0 {
		entities, err = h.resolveEntityFromContent(r.Context(), req.Name, req.Type, req.RepoID, limit+1)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve content entities: %v", err))
			return
		}
		entities, truncated = trimResolvedEntityPage(entities, limit)
	}

	WriteSuccess(w, r, http.StatusOK, resolvedEntityResponse(entities, limit, truncated), entityResolveTruthEnvelope(h.profile()))
}

// getEntityContext retrieves the context for a specific entity.
func (h *EntityHandler) getEntityContext(w http.ResponseWriter, r *http.Request) {
	entityID := PathParam(r, "entity_id")
	if entityID == "" {
		WriteError(w, http.StatusBadRequest, "entity_id is required")
		return
	}

	access := repositoryAccessFilterFromContext(r.Context())
	if access.empty() {
		WriteError(w, http.StatusNotFound, "entity not found")
		return
	}

	cypher := `
		MATCH (e) WHERE e.id = $entity_id
	`
	if access.scoped() {
		cypher += `
		AND EXISTS {
			MATCH (e)<-[:CONTAINS]-(scopeFile:File)<-[:REPO_CONTAINS]-(scopeRepo:Repository)
			WHERE ` + access.graphCondition("scopeRepo") + `
		}
	`
	}
	cypher += `
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
	`
	if access.scoped() {
		cypher += `
		WHERE ` + access.graphCondition("r") + `
	`
	}
	cypher += `
		OPTIONAL MATCH (e)-[rel]->(target)
		RETURN e.id as id, labels(e) as labels, e.name as name,
		       f.relative_path as file_path,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		       ,r.id as repo_id, r.name as repo_name,
		       collect(DISTINCT {type: type(rel), target_name: target.name, target_id: target.id}) as relationships
	`

	params := access.graphParams(map[string]any{"entity_id": entityID})
	var row map[string]any
	var err error
	if h.Neo4j != nil {
		row, err = h.Neo4j.RunSingle(r.Context(), cypher, params)
		if err != nil {
			if WriteGraphReadError(w, r, err, "code_search.fuzzy_symbol") {
				return
			}
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
			return
		}
	}

	if row == nil {
		response, fallbackErr := h.getEntityContextFromContent(r.Context(), entityID)
		if fallbackErr != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", fallbackErr))
			return
		}
		if response == nil {
			WriteError(w, http.StatusNotFound, "entity not found")
			return
		}
		response["result_limits"] = entityContextResultLimits(response, entityID)
		response["partial_reasons"] = contextPartialReasons(response)
		WriteSuccess(w, r, http.StatusOK, response, entityContextTruthEnvelope(h.profile()))
		return
	}

	response := map[string]any{
		"id":            StringVal(row, "id"),
		"labels":        StringSliceVal(row, "labels"),
		"name":          StringVal(row, "name"),
		"file_path":     StringVal(row, "file_path"),
		"repo_id":       StringVal(row, "repo_id"),
		"repo_name":     StringVal(row, "repo_name"),
		"language":      StringVal(row, "language"),
		"start_line":    IntVal(row, "start_line"),
		"end_line":      IntVal(row, "end_line"),
		"relationships": extractRelationships(row),
	}
	if metadata := graphResultMetadata(row); len(metadata) > 0 {
		response["metadata"] = metadata
	}
	if _, err := hydrateResolvedEntityRepoIdentity(r.Context(), h.Neo4j, h.Content, []map[string]any{response}); err != nil {
		if WriteGraphReadError(w, r, err, "code_search.fuzzy_symbol") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("hydrate entity repo identity: %v", err))
		return
	}
	if access.scoped() && !access.allowsRepositoryID(StringVal(response, "repo_id")) {
		WriteError(w, http.StatusNotFound, "entity not found")
		return
	}
	enriched, err := h.enrichEntityResultsWithContentMetadata(r.Context(), []map[string]any{response}, StringVal(response, "repo_id"), StringVal(row, "name"), 1)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich entity context: %v", err))
		return
	}
	response = enriched[0]
	attachSemanticSummary(response)

	response["result_limits"] = entityContextResultLimits(response, entityID)
	response["partial_reasons"] = contextPartialReasons(response)
	WriteSuccess(w, r, http.StatusOK, response, entityContextTruthEnvelope(h.profile()))
}

// getServiceContext retrieves the context for a service by name.
func (h *EntityHandler) getServiceContext(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service context requires authoritative platform context truth",
			"unsupported_capability",
			"platform_impact.context_overview",
			h.profile(),
			requiredProfile("platform_impact.context_overview"),
		)
		return
	}

	serviceName := PathParam(r, "service_name")
	if serviceName == "" {
		WriteError(w, http.StatusBadRequest, "service_name is required")
		return
	}
	if repositoryAccessFilterFromContext(r.Context()).empty() {
		WriteError(w, http.StatusNotFound, "service not found")
		return
	}

	ctx, err := h.fetchServiceWorkloadContext(r.Context(), serviceName, "service_context")
	if err != nil {
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	if ctx == nil {
		WriteError(w, http.StatusNotFound, "service not found")
		return
	}
	if err := enrichServiceQueryContextWithOptions(r.Context(), h.Neo4j, h.Content, ctx, serviceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Operation:                 "service_context",
	}); err != nil {
		if writeContentSubstringIndexUnavailable(w, err) {
			return
		}
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich service context: %v", err))
		return
	}

	if langBreakdown, toolBreakdown := queryServiceTechFingerprint(r.Context(), h.Neo4j, ctx); len(langBreakdown) > 0 || len(toolBreakdown) > 0 {
		if len(langBreakdown) > 0 {
			ctx["language_breakdown"] = langBreakdown
		}
		if len(toolBreakdown) > 0 {
			ctx["source_tool_breakdown"] = toolBreakdown
		}
	}

	WriteSuccess(w, r, http.StatusOK, ctx, BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisHybrid, "resolved from service context and platform evidence"))
}
