package query

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
)

// EntityHandler exposes HTTP routes for entity queries.
type EntityHandler struct {
	Neo4j   GraphQuery
	Content ContentStore
	Profile QueryProfile
	Logger  *slog.Logger
}

// Mount registers all entity routes on the given mux.
func (h *EntityHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/entities/resolve", h.resolveEntity)
	mux.HandleFunc("GET /api/v0/entities/{entity_id}/context", h.getEntityContext)
	mux.HandleFunc("GET /api/v0/workloads/{workload_id}/context", h.getWorkloadContext)
	mux.HandleFunc("GET /api/v0/workloads/{workload_id}/story", h.getWorkloadStory)
	mux.HandleFunc("GET /api/v0/services/{service_name}/context", h.getServiceContext)
	mux.HandleFunc("GET /api/v0/services/{service_name}/story", h.getServiceStory)
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
}

const serviceLookupWhereClause = "w.name = $service_name OR w.id = $service_name"

// resolveEntity resolves an entity by name and optional type/repo filters.
func (h *EntityHandler) resolveEntity(w http.ResponseWriter, r *http.Request) {
	var req resolveEntityRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Name == "" {
		WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.RepoID != "" {
		resolvedRepoID, err := resolveRepositorySelectorExact(r.Context(), h.Neo4j, h.Content, req.RepoID)
		if err != nil {
			WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		req.RepoID = resolvedRepoID
	}

	cypher := `MATCH (e) WHERE e.name = $name`
	params := map[string]any{"name": req.Name}

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

	if req.RepoID != "" {
		cypher += `
			AND EXISTS {
				MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
				WHERE r.id = $repo_id
			}
		`
		params["repo_id"] = req.RepoID
	}

	cypher += `
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
		RETURN e.id as id, labels(e) as labels, e.name as name,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		ORDER BY e.name
		LIMIT 20
	`

	var rows []map[string]any
	var err error
	if h.Neo4j != nil {
		rows, err = h.Neo4j.Run(r.Context(), cypher, params)
		if err != nil {
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
	entities, err = h.enrichEntityResultsWithContentMetadata(r.Context(), entities, req.RepoID, req.Name, 20)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich entities: %v", err))
		return
	}
	for i := range entities {
		attachSemanticSummary(entities[i])
	}
	if err := hydrateResolvedEntityRepoIdentity(r.Context(), h.Neo4j, h.Content, entities); err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("hydrate entity repo identity: %v", err))
		return
	}
	entities = normalizeResolvedEntities(entities, 20)
	if len(entities) == 0 {
		entities, err = h.resolveEntityFromContent(r.Context(), req.Name, req.Type, req.RepoID, 20)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("resolve content entities: %v", err))
			return
		}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"entities": entities,
		"matches":  entities,
		"count":    len(entities),
	})
}

// getEntityContext retrieves the context for a specific entity.
func (h *EntityHandler) getEntityContext(w http.ResponseWriter, r *http.Request) {
	entityID := PathParam(r, "entity_id")
	if entityID == "" {
		WriteError(w, http.StatusBadRequest, "entity_id is required")
		return
	}

	cypher := `
		MATCH (e) WHERE e.id = $entity_id
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
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

	params := map[string]any{"entity_id": entityID}
	var row map[string]any
	var err error
	if h.Neo4j != nil {
		row, err = h.Neo4j.RunSingle(r.Context(), cypher, params)
		if err != nil {
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
		WriteJSON(w, http.StatusOK, response)
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
	if err := hydrateResolvedEntityRepoIdentity(r.Context(), h.Neo4j, h.Content, []map[string]any{response}); err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("hydrate entity repo identity: %v", err))
		return
	}
	enriched, err := h.enrichEntityResultsWithContentMetadata(r.Context(), []map[string]any{response}, StringVal(response, "repo_id"), StringVal(row, "name"), 1)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich entity context: %v", err))
		return
	}
	response = enriched[0]
	attachSemanticSummary(response)

	WriteJSON(w, http.StatusOK, response)
}

func (h *EntityHandler) resolveEntityFromContent(
	ctx context.Context,
	name string,
	typeName string,
	repoID string,
	limit int,
) ([]map[string]any, error) {
	if h == nil || h.Content == nil || repoID == "" || name == "" {
		if h == nil || h.Content == nil || name == "" {
			return nil, nil
		}
	}

	entityType := contentEntityTypeForResolve(typeName)
	var (
		rows []EntityContent
		err  error
	)
	if repoID != "" {
		rows, err = h.Content.SearchEntitiesByName(ctx, repoID, entityType, name, limit)
		if err != nil {
			return nil, err
		}
	} else {
		rows, err = h.Content.SearchEntitiesByNameAnyRepo(ctx, entityType, name, limit)
		if err != nil {
			return nil, err
		}
	}

	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		results = append(results, contentEntityToMap(row))
	}
	return results, nil
}

func (h *EntityHandler) getEntityContextFromContent(ctx context.Context, entityID string) (map[string]any, error) {
	if h == nil || h.Content == nil || entityID == "" {
		return nil, nil
	}

	entity, err := h.Content.GetEntityContent(ctx, entityID)
	if err != nil || entity == nil {
		return nil, err
	}

	response := contentEntityToMap(*entity)
	relationshipSet, err := buildContentRelationshipSet(ctx, h.Content, *entity)
	if err != nil {
		return nil, err
	}
	relationships := append([]map[string]any{}, relationshipSet.incoming...)
	relationships = append(relationships, relationshipSet.outgoing...)
	response["relationships"] = relationships
	attachSemanticSummary(response)
	return response, nil
}

func contentEntityTypeForResolve(typeName string) string {
	if typeName == "" {
		return ""
	}
	if entityType, ok := resolveContentBackedEntityTypes[typeName]; ok {
		return entityType
	}
	if entityType, ok := contentBackedEntityTypes[typeName]; ok {
		return entityType
	}
	if entityType, ok := graphBackedEntityTypes[typeName]; ok {
		return entityType
	}
	return typeName
}

func resolveGraphEntityType(typeName string) (string, string, string, bool) {
	if graphLabel, semanticKey, semanticValue, ok := elixirGraphSemanticEntityType(typeName); ok {
		return graphLabel, semanticKey, semanticValue, true
	}
	if graphLabel, ok := graphBackedEntityTypes[typeName]; ok {
		return graphLabel, "", "", true
	}
	if graphLabel, ok := graphFirstContentBackedEntityTypes[typeName]; ok {
		return graphLabel, "", "", true
	}
	return "", "", "", false
}

var resolveContentBackedEntityTypes = map[string]string{
	"analytics_model":          "AnalyticsModel",
	"annotation":               "Annotation",
	"argocd_application":       "ArgoCDApplication",
	"argocd_applicationset":    "ArgoCDApplicationSet",
	"component":                "Component",
	"cloudformation_condition": "CloudFormationCondition",
	"cloudformation_export":    "CloudFormationExport",
	"cloudformation_import":    "CloudFormationImport",
	"cloudformation_output":    "CloudFormationOutput",
	"cloudformation_parameter": "CloudFormationParameter",
	"cloudformation_resource":  "CloudFormationResource",
	"data_asset":               "DataAsset",
	"impl_block":               "ImplBlock",
	"k8s_resource":             "K8sResource",
	"kustomize_overlay":        "KustomizeOverlay",
	"protocol":                 "Protocol",
	"terraform_block":          "TerraformBlock",
	"terragrunt_dependency":    "TerragruntDependency",
	"terragrunt_input":         "TerragruntInput",
	"terragrunt_local":         "TerragruntLocal",
	"type_alias":               "TypeAlias",
	"type_annotation":          "TypeAnnotation",
	"typedef":                  "Typedef",
	"guard":                    "guard",
	"protocol_implementation":  "ProtocolImplementation",
	"module_attribute":         "module_attribute",
}

func contentEntityToMap(entity EntityContent) map[string]any {
	result := map[string]any{
		"id":         entity.EntityID,
		"entity_id":  entity.EntityID,
		"name":       entity.EntityName,
		"labels":     []string{entity.EntityType},
		"file_path":  entity.RelativePath,
		"repo_id":    entity.RepoID,
		"language":   entity.Language,
		"start_line": entity.StartLine,
		"end_line":   entity.EndLine,
		"metadata":   entity.Metadata,
	}
	attachSemanticSummary(result)
	return result
}

// getWorkloadContext retrieves the context for a specific workload.
func (h *EntityHandler) getWorkloadContext(w http.ResponseWriter, r *http.Request) {
	workloadID := PathParam(r, "workload_id")
	if workloadID == "" {
		WriteError(w, http.StatusBadRequest, "workload_id is required")
		return
	}

	ctx, err := h.fetchWorkloadContext(r.Context(), "w.id = $workload_id", map[string]any{"workload_id": workloadID})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	if ctx == nil {
		WriteError(w, http.StatusNotFound, "workload not found")
		return
	}
	if err := enrichServiceQueryContextWithOptions(r.Context(), h.Neo4j, h.Content, ctx, serviceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Operation:                 "workload_context",
	}); err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich workload context: %v", err))
		return
	}

	WriteJSON(w, http.StatusOK, ctx)
}

// getWorkloadStory retrieves a narrative summary for a workload.
func (h *EntityHandler) getWorkloadStory(w http.ResponseWriter, r *http.Request) {
	workloadID := PathParam(r, "workload_id")
	if workloadID == "" {
		WriteError(w, http.StatusBadRequest, "workload_id is required")
		return
	}

	ctx, err := h.fetchWorkloadContext(r.Context(), "w.id = $workload_id", map[string]any{"workload_id": workloadID})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	if ctx == nil {
		WriteError(w, http.StatusNotFound, "workload not found")
		return
	}
	if err := enrichServiceQueryContextWithOptions(r.Context(), h.Neo4j, h.Content, ctx, serviceQueryEnrichmentOptions{
		IncludeRelatedModuleUsage: true,
		Logger:                    h.Logger,
		Operation:                 "workload_story",
	}); err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich workload story: %v", err))
		return
	}

	story := buildWorkloadStory(ctx)
	WriteJSON(w, http.StatusOK, map[string]any{
		"workload_id": workloadID,
		"name":        ctx["name"],
		"story":       story,
	})
}

// getServiceContext retrieves the context for a service by name.
func (h *EntityHandler) getServiceContext(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service context requires full platform context truth",
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

	ctx, err := h.fetchServiceWorkloadContext(r.Context(), serviceName, "service_context")
	if err != nil {
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
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich service context: %v", err))
		return
	}

	WriteSuccess(w, r, http.StatusOK, ctx, BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisHybrid, "resolved from service context and platform evidence"))
}

// getServiceStory retrieves a narrative summary for a service.
func (h *EntityHandler) getServiceStory(w http.ResponseWriter, r *http.Request) {
	serviceName := PathParam(r, "service_name")
	if serviceName == "" {
		WriteError(w, http.StatusBadRequest, "service_name is required")
		return
	}

	ctx, err := h.fetchServiceWorkloadContext(r.Context(), serviceName, "service_story")
	if err != nil {
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
		Operation:                 "service_story",
	}); err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich service story: %v", err))
		return
	}

	WriteJSON(w, http.StatusOK, buildServiceStoryResponse(serviceName, ctx))
}
