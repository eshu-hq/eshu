package query

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const repositorySelectorWhereClause = "r.id = $repo_selector OR r.name = $repo_selector"

const repositoryGraphCoverageStatsTimeout = 2 * time.Second

var repositoryBaseCypher = fmt.Sprintf(`
	MATCH (r:Repository {id: $repo_id})
	RETURN %s
`, RepoProjection("r"))

// RepositoryHandler exposes HTTP routes for repository queries.
type RepositoryHandler struct {
	Neo4j   GraphQuery
	Content ContentStore
	Profile QueryProfile
	Logger  *slog.Logger
}

// Mount registers all repository routes on the given mux.
func (h *RepositoryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/repositories", h.listRepositories)
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/context", h.getRepositoryContext)
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/story", h.getRepositoryStory)
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/stats", h.getRepositoryStats)
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/coverage", h.getRepositoryCoverage)
}

func (h *RepositoryHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// listRepositories returns all indexed repositories.
func (h *RepositoryHandler) listRepositories(w http.ResponseWriter, r *http.Request) {
	if h == nil {
		WriteJSON(w, http.StatusOK, map[string]any{
			"repositories": []map[string]any{},
			"count":        0,
		})
		return
	}
	if h.Neo4j == nil {
		repos, err := h.listRepositoriesFromContent(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"repositories": repos,
			"count":        len(repos),
		})
		return
	}

	cypher := fmt.Sprintf(`
		MATCH (r:Repository)
		RETURN %s, coalesce(r.is_dependency, false) as is_dependency
		ORDER BY r.name
	`, RepoProjection("r"))

	rows, err := h.Neo4j.Run(r.Context(), cypher, nil)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	repos := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		repo := map[string]any{
			"id":            StringVal(row, "id"),
			"name":          StringVal(row, "name"),
			"path":          StringVal(row, "path"),
			"local_path":    StringVal(row, "local_path"),
			"remote_url":    StringVal(row, "remote_url"),
			"repo_slug":     StringVal(row, "repo_slug"),
			"has_remote":    BoolVal(row, "has_remote"),
			"is_dependency": BoolVal(row, "is_dependency"),
		}
		repos = append(repos, repo)
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"repositories": repos,
		"count":        len(repos),
	})
}

func (h *RepositoryHandler) listRepositoriesFromContent(ctx context.Context) ([]map[string]any, error) {
	if h == nil || h.Content == nil {
		return []map[string]any{}, nil
	}

	entries, err := h.Content.ListRepositories(ctx)
	if err != nil {
		return nil, err
	}
	repos := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		repos = append(repos, repositoryCatalogMap(entry))
	}
	return repos, nil
}

// getRepositoryContext returns repository metadata with graph statistics and
// enriched context including entry points, infrastructure entities, language
// distribution, cross-repo relationships, and consumer repositories.
func (h *RepositoryHandler) getRepositoryContext(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"repository context requires authoritative platform context truth",
			"unsupported_capability",
			"platform_impact.context_overview",
			h.profile(),
			requiredProfile("platform_impact.context_overview"),
		)
		return
	}

	ctx := r.Context()
	repoID, ok := h.resolveRepositoryPathSelector(w, r)
	if !ok {
		return
	}
	params := map[string]any{"repo_id": repoID}

	timer := startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "repository_lookup")
	baseRow, err := h.Neo4j.RunSingle(ctx, repositoryBaseCypher, params)
	timer.Done(ctx, slog.Bool("found", baseRow != nil), slog.Bool("error", err != nil))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	if baseRow == nil {
		WriteError(w, http.StatusNotFound, "repository not found")
		return
	}
	contentCoverage := loadRepositoryContentCoverage(ctx, h.Content, repoID)
	readModelSummary := loadRepositoryReadModelSummary(ctx, h.Content, repoID)
	relationshipReadModel := loadRepositoryRelationshipReadModel(ctx, h.Content, repoID)

	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "summary_counts")
	counts := queryRepositoryContextCounts(ctx, h.Neo4j, params, baseRow, contentCoverage, readModelSummary)
	timer.Done(ctx,
		slog.Int("file_count", counts.fileCount),
		slog.Int("workload_count", counts.workloadCount),
		slog.Int("platform_count", counts.platformCount),
		slog.Int("dependency_count", counts.dependencyCount),
	)
	result := map[string]any{
		"repository":       RepoRefFromRow(baseRow),
		"file_count":       counts.fileCount,
		"workload_count":   counts.workloadCount,
		"platform_count":   counts.platformCount,
		"dependency_count": counts.dependencyCount,
	}

	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "entry_points")
	result["entry_points"] = queryRepoEntryPoints(ctx, h.Neo4j, h.Content, params)
	timer.Done(ctx, slog.Int("row_count", len(result["entry_points"].([]map[string]any))))
	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "infrastructure")
	result["infrastructure"] = queryRepoInfrastructure(ctx, h.Neo4j, h.Content, params)
	timer.Done(ctx, slog.Int("row_count", len(result["infrastructure"].([]map[string]any))))
	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "relationships")
	if dependencies := repositoryReadModelDependencies(relationshipReadModel); dependencies != nil {
		result["relationships"] = dependencies
	} else {
		result["relationships"] = queryRepoDependencies(ctx, h.Neo4j, params)
	}
	timer.Done(ctx, slog.Int("row_count", len(result["relationships"].([]map[string]any))))
	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "relationship_overview")
	var relationshipRows []map[string]any
	if relationshipReadModel != nil {
		relationshipRows = relationshipReadModel.Relationships
	} else {
		relationshipRows = queryRepoRelationshipOverview(ctx, h.Neo4j, params)
	}
	timer.Done(ctx, slog.Int("row_count", len(relationshipRows)))
	if len(relationshipRows) == 0 {
		relationshipRows = result["relationships"].([]map[string]any)
	}
	if relationshipOverview := buildRepositoryRelationshipOverview(relationshipRows); relationshipOverview != nil {
		result["relationship_overview"] = relationshipOverview
	}
	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "consumers")
	if relationshipReadModel != nil {
		result["consumers"] = relationshipReadModel.Consumers
	} else {
		result["consumers"] = queryRepoConsumers(ctx, h.Neo4j, params)
	}
	timer.Done(ctx, slog.Int("row_count", len(result["consumers"].([]map[string]any))))
	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "api_surface")
	if apiSurface := queryRepoAPISurface(ctx, h.Neo4j, params); len(apiSurface) > 0 {
		result["api_surface"] = apiSurface
		timer.Done(ctx, slog.Int("row_count", len(apiSurface)))
	} else {
		timer.Done(ctx, slog.Int("row_count", 0))
	}
	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "deployment_evidence")
	deploymentEvidence, err := queryRepoDeploymentEvidence(ctx, h.Neo4j, h.Content, params)
	if err != nil {
		timer.Done(ctx, slog.Bool("error", true))
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load deployment evidence: %v", err))
		return
	}
	if len(deploymentEvidence) > 0 {
		result["deployment_evidence"] = deploymentEvidence
		timer.Done(ctx, slog.Int("row_count", len(deploymentEvidence)))
	} else {
		timer.Done(ctx, slog.Int("row_count", 0))
	}
	if h.Content != nil {
		timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "content_infrastructure_overview")
		files, err := h.Content.ListRepoFiles(ctx, repoID, repositorySemanticEntityLimit)
		if err == nil {
			if files == nil {
				files = []FileContent{}
			}
			overview := buildRepositoryInfrastructureOverview(result["infrastructure"].([]map[string]any), files)
			deploymentOverview, _ := loadDeploymentArtifactOverview(
				ctx,
				h.Neo4j,
				h.Content,
				repoID,
				StringVal(baseRow, "name"),
				files,
				result["infrastructure"].([]map[string]any),
				overview,
			)
			if deploymentOverview != nil {
				overview = deploymentOverview
			}
			if overview != nil {
				if deploymentArtifacts := mapValue(overview, "deployment_artifacts"); len(deploymentArtifacts) > 0 {
					result["deployment_artifacts"] = deploymentArtifacts
				}
				result["infrastructure_overview"] = overview
			}
		}
		timer.Done(ctx, slog.Bool("error", err != nil))
	}
	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "languages")
	if languages, ok := repositoryLanguageDistributionFromCoverage(contentCoverage); ok {
		result["languages"] = languages
	} else {
		result["languages"] = queryRepoLanguageDistribution(ctx, h.Neo4j, params)
	}
	timer.Done(ctx, slog.Int("row_count", len(result["languages"].([]map[string]any))))

	WriteSuccess(w, r, http.StatusOK, result, BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisHybrid, "resolved from repository context and platform evidence"))
}

func queryRepoEntryPoints(ctx context.Context, reader GraphQuery, content ContentStore, params map[string]any) []map[string]any {
	repoID := StringVal(params, "repo_id")
	if entryPoints := loadRepositoryEntryPoints(ctx, content, repoID); entryPoints != nil {
		return entryPoints
	}

	rows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(fn:Function)
		WHERE fn.name IN ['main', 'handler', 'app', 'create_app', 'lambda_handler',
		                   'Main', 'Handler', 'App', 'CreateApp', 'LambdaHandler']
		RETURN fn.name AS name, f.relative_path AS relative_path, fn.language AS language
		ORDER BY fn.name
	`, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if !isRepositoryEntryPointName(StringVal(row, "name")) {
			continue
		}
		result = append(result, map[string]any{
			"name":          StringVal(row, "name"),
			"relative_path": StringVal(row, "relative_path"),
			"language":      StringVal(row, "language"),
		})
	}
	return result
}

func isRepositoryEntryPointName(name string) bool {
	switch name {
	case "main", "handler", "app", "create_app", "lambda_handler",
		"Main", "Handler", "App", "CreateApp", "LambdaHandler":
		return true
	default:
		return false
	}
}

func queryRepoInfrastructure(ctx context.Context, reader GraphQuery, content ContentStore, params map[string]any) []map[string]any {
	return queryRepoInfrastructureRows(ctx, reader, content, params)
}

func queryRepoLanguageDistribution(ctx context.Context, reader GraphQuery, params map[string]any) []map[string]any {
	rows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
		WHERE f.language IS NOT NULL
		RETURN f.language AS language, count(f) AS file_count
		ORDER BY file_count DESC
	`, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"language":   StringVal(row, "language"),
			"file_count": IntVal(row, "file_count"),
		})
	}
	return result
}

func queryRepoDependencies(ctx context.Context, reader GraphQuery, params map[string]any) []map[string]any {
	rows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON]->(target:Repository)
		RETURN type(rel) AS type, target.name AS target_name,
		       target.id AS target_id, rel.evidence_type AS evidence_type,
		       rel.resolved_id AS resolved_id,
		       rel.generation_id AS generation_id,
		       rel.confidence AS confidence,
		       rel.evidence_count AS evidence_count,
		       rel.evidence_kinds AS evidence_kinds,
		       rel.resolution_source AS resolution_source,
		       rel.rationale AS rationale
		ORDER BY type, target_name
	`, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"type":        StringVal(row, "type"),
			"target_name": StringVal(row, "target_name"),
			"target_id":   StringVal(row, "target_id"),
		}
		if evidenceType := StringVal(row, "evidence_type"); evidenceType != "" {
			entry["evidence_type"] = evidenceType
		}
		copyRelationshipEvidenceMetadata(entry, row)
		result = append(result, entry)
	}
	return result
}

func queryRepoRelationshipOverview(ctx context.Context, reader GraphQuery, params map[string]any) []map[string]any {
	outgoing := queryRepoRelationshipOverviewDirection(ctx, reader, params, `
		MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON]->(target:Repository)
		RETURN 'outgoing' AS direction,
		       type(rel) AS type,
		       r.name AS source_name,
		       r.id AS source_id,
		       target.name AS target_name,
		       target.id AS target_id,
		       rel.evidence_type AS evidence_type,
		       rel.resolved_id AS resolved_id,
		       rel.generation_id AS generation_id,
		       rel.confidence AS confidence,
		       rel.evidence_count AS evidence_count,
		       rel.evidence_kinds AS evidence_kinds,
		       rel.resolution_source AS resolution_source,
		       rel.rationale AS rationale
		ORDER BY type, target_name
	`)
	incoming := queryRepoRelationshipOverviewDirection(ctx, reader, params, `
		MATCH (source:Repository)-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON]->(r:Repository {id: $repo_id})
		RETURN 'incoming' AS direction,
		       type(rel) AS type,
		       source.name AS source_name,
		       source.id AS source_id,
		       r.name AS target_name,
		       r.id AS target_id,
		       rel.evidence_type AS evidence_type,
		       rel.resolved_id AS resolved_id,
		       rel.generation_id AS generation_id,
		       rel.confidence AS confidence,
		       rel.evidence_count AS evidence_count,
		       rel.evidence_kinds AS evidence_kinds,
		       rel.resolution_source AS resolution_source,
		       rel.rationale AS rationale
		ORDER BY type, source_name
	`)
	if len(outgoing) == 0 {
		return incoming
	}
	return append(outgoing, incoming...)
}

func queryRepoRelationshipOverviewDirection(ctx context.Context, reader GraphQuery, params map[string]any, cypher string) []map[string]any {
	rows, err := reader.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"direction":   StringVal(row, "direction"),
			"type":        StringVal(row, "type"),
			"source_name": StringVal(row, "source_name"),
			"source_id":   StringVal(row, "source_id"),
			"target_name": StringVal(row, "target_name"),
			"target_id":   StringVal(row, "target_id"),
		}
		if evidenceType := StringVal(row, "evidence_type"); evidenceType != "" {
			entry["evidence_type"] = evidenceType
		}
		copyRelationshipEvidenceMetadata(entry, row)
		result = append(result, entry)
	}
	return result
}

func queryRepoConsumers(ctx context.Context, reader GraphQuery, params map[string]any) []map[string]any {
	rows, err := reader.Run(ctx, `
		MATCH (consumer:Repository)-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON]->(r:Repository {id: $repo_id})
		RETURN consumer.name AS consumer_name, consumer.id AS consumer_id
		ORDER BY consumer_name
	`, params)
	if err != nil || len(rows) == 0 {
		return make([]map[string]any, 0)
	}

	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"name": StringVal(row, "consumer_name"),
			"id":   StringVal(row, "consumer_id"),
		})
	}
	return result
}

func (h *RepositoryHandler) getRepositoryStory(w http.ResponseWriter, r *http.Request) {
	repoID, ok := h.resolveRepositoryPathSelector(w, r)
	if !ok {
		return
	}

	timer := startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "repository_lookup")
	row, err := h.Neo4j.RunSingle(r.Context(), repositoryBaseCypher, map[string]any{"repo_id": repoID})
	timer.Done(r.Context(), slog.Bool("found", row != nil), slog.Bool("error", err != nil))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	if row == nil {
		WriteError(w, http.StatusNotFound, "repository not found")
		return
	}
	repoID = StringVal(row, "id")

	repo := RepoRefFromRow(row)
	contentCoverage := loadRepositoryContentCoverage(r.Context(), h.Content, repoID)
	readModelSummary := loadRepositoryReadModelSummary(r.Context(), h.Content, repoID)
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "graph_summary")
	storySummary := queryRepositoryStoryGraphSummary(r.Context(), h.Neo4j, map[string]any{"repo_id": repoID}, row, contentCoverage, readModelSummary)
	timer.Done(r.Context(),
		slog.Int("file_count", storySummary.fileCount),
		slog.Int("workload_count", len(storySummary.workloadNames)),
		slog.Int("platform_count", len(storySummary.platformTypes)),
		slog.Int("dependency_count", storySummary.dependencyCount),
	)
	fileCount := storySummary.fileCount
	languages := storySummary.languages
	workloadNames := storySummary.workloadNames
	platformTypes := storySummary.platformTypes
	dependencyCount := storySummary.dependencyCount
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "semantic_overview")
	semanticOverview, err := loadRepositorySemanticOverview(r.Context(), h.Content, repoID)
	timer.Done(r.Context(), slog.Bool("error", err != nil))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("semantic overview failed: %v", err))
		return
	}
	var infrastructureOverview map[string]any
	narrativeFiles := []FileContent(nil)
	if h.Content != nil {
		timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "content_files")
		files, err := h.Content.ListRepoFiles(r.Context(), repoID, repositorySemanticEntityLimit)
		timer.Done(r.Context(), slog.Bool("error", err != nil), slog.Int("file_count", len(files)))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("list repository files failed: %v", err))
			return
		}
		if files == nil {
			files = []FileContent{}
		}
		timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "infrastructure")
		infrastructure := queryRepoInfrastructure(r.Context(), h.Neo4j, h.Content, map[string]any{"repo_id": repoID})
		timer.Done(r.Context(), slog.Int("row_count", len(infrastructure)))
		infrastructureOverview = buildRepositoryInfrastructureOverview(infrastructure, files)
		timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "deployment_artifacts")
		deploymentOverview, _ := loadDeploymentArtifactOverview(
			r.Context(),
			h.Neo4j,
			h.Content,
			repoID,
			repo.Name,
			files,
			infrastructure,
			infrastructureOverview,
		)
		if deploymentOverview != nil {
			infrastructureOverview = deploymentOverview
		}
		timer.Done(r.Context(), slog.Bool("found", deploymentOverview != nil))
		timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "narrative_files")
		narrativeFiles, err = hydrateRepositoryNarrativeFiles(r.Context(), h.Content, repoID, files)
		timer.Done(r.Context(), slog.Bool("error", err != nil), slog.Int("file_count", len(narrativeFiles)))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("hydrate repository narrative files failed: %v", err))
			return
		}
		timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "relationships")
		relationships := queryRepoDependencies(r.Context(), h.Neo4j, map[string]any{"repo_id": repoID})
		timer.Done(r.Context(), slog.Int("row_count", len(relationships)))
		if relationshipOverview := buildRepositoryRelationshipOverview(relationships); relationshipOverview != nil {
			if infrastructureOverview == nil {
				infrastructureOverview = map[string]any{}
			}
			infrastructureOverview["relationship_overview"] = relationshipOverview
		}
	}

	response := buildRepositoryStoryResponse(
		repo,
		fileCount,
		languages,
		workloadNames,
		platformTypes,
		dependencyCount,
		infrastructureOverview,
		semanticOverview,
	)
	enrichRepositoryStoryResponseWithEvidence(response, semanticOverview, narrativeFiles)

	WriteJSON(w, http.StatusOK, response)
}

// getRepositoryStats returns repository statistics including entity counts.
func (h *RepositoryHandler) getRepositoryStats(w http.ResponseWriter, r *http.Request) {
	repoID, ok := h.resolveRepositoryPathSelector(w, r)
	if !ok {
		return
	}

	cypher := fmt.Sprintf(`
		MATCH (r:Repository) WHERE %s
		OPTIONAL MATCH (r)-[:REPO_CONTAINS]->(f:File)
		WITH r, count(f) as file_count, collect(DISTINCT f.language) as languages
		OPTIONAL MATCH (r)-[:REPO_CONTAINS]->(f2:File)-[:CONTAINS]->(e)
		RETURN %s,
		       file_count,
		       languages,
		       count(DISTINCT e) as entity_count,
		       collect(DISTINCT labels(e)[0]) as entity_types
	`, repositorySelectorWhereClause, RepoProjection("r"))

	row, err := h.Neo4j.RunSingle(r.Context(), cypher, map[string]any{"repo_selector": repoID})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	if row == nil {
		WriteError(w, http.StatusNotFound, "repository not found")
		return
	}

	stats := map[string]any{
		"repository":   RepoRefFromRow(row),
		"file_count":   IntVal(row, "file_count"),
		"languages":    StringSliceVal(row, "languages"),
		"entity_count": IntVal(row, "entity_count"),
		"entity_types": StringSliceVal(row, "entity_types"),
	}

	WriteJSON(w, http.StatusOK, stats)
}

// getRepositoryCoverage returns content store coverage for the repository.
func (h *RepositoryHandler) getRepositoryCoverage(w http.ResponseWriter, r *http.Request) {
	repoID, ok := h.resolveRepositoryPathSelector(w, r)
	if !ok {
		return
	}

	resolvedRepoID, err := h.resolveCoverageRepositoryID(r.Context(), repoID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	if resolvedRepoID == "" {
		WriteError(w, http.StatusNotFound, "repository not found")
		return
	}
	repoID = resolvedRepoID

	// Get content store coverage
	coverage, err := h.queryContentStoreCoverage(r.Context(), repoID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("coverage query failed: %v", err))
		return
	}

	WriteJSON(w, http.StatusOK, coverage)
}

func (h *RepositoryHandler) resolveCoverageRepositoryID(ctx context.Context, selector string) (string, error) {
	return h.resolveRepositorySelector(ctx, selector)
}

func (h *RepositoryHandler) resolveRepositorySelector(ctx context.Context, selector string) (string, error) {
	return resolveRepositorySelectorExact(ctx, h.Neo4j, h.Content, selector)
}

func (h *RepositoryHandler) resolveRepositoryPathSelector(w http.ResponseWriter, r *http.Request) (string, bool) {
	repoSelector := PathParam(r, "repo_id")
	if repoSelector == "" {
		WriteError(w, http.StatusBadRequest, "repo_id is required")
		return "", false
	}
	repoID, err := h.resolveRepositorySelector(r.Context(), repoSelector)
	if err != nil {
		status := http.StatusBadRequest
		if isRepositorySelectorNotFound(err) {
			status = http.StatusNotFound
		}
		WriteError(w, status, err.Error())
		return "", false
	}
	return repoID, true
}

func repositoryCatalogMap(entry RepositoryCatalogEntry) map[string]any {
	return map[string]any{
		"id":            entry.ID,
		"name":          entry.Name,
		"path":          entry.Path,
		"local_path":    entry.LocalPath,
		"remote_url":    entry.RemoteURL,
		"repo_slug":     entry.RepoSlug,
		"has_remote":    entry.HasRemote,
		"is_dependency": false,
	}
}

// queryContentStoreCoverage queries the Postgres content store for repository coverage.
func (h *RepositoryHandler) queryContentStoreCoverage(ctx context.Context, repoID string) (map[string]any, error) {
	var contentCoverage RepositoryContentCoverage
	if h.Content != nil {
		var err error
		contentCoverage, err = h.Content.RepositoryCoverage(ctx, repoID)
		if err != nil {
			return nil, fmt.Errorf("query content coverage: %w", err)
		}
		if contentCoverage.Available {
			return repositoryCoverageResponse(
				repoID,
				repositoryGraphCoverageStats{},
				contentCoverage,
				"graph coverage stats skipped because content store coverage is available",
			), nil
		}
	}

	graphStats, graphErr := h.queryRepositoryGraphCoverageStatsWithTimeout(ctx, repoID)
	if graphErr != nil {
		return nil, fmt.Errorf("query graph coverage stats: %w", graphErr)
	}

	return repositoryCoverageResponse(repoID, graphStats, contentCoverage, ""), nil
}

func repositoryCoverageResponse(
	repoID string,
	graphStats repositoryGraphCoverageStats,
	contentCoverage RepositoryContentCoverage,
	lastError string,
) map[string]any {
	graphGapCount, contentGapCount := 0, 0
	if graphStats.Available && contentCoverage.Available {
		graphGapCount, contentGapCount = computeCoverageGapCounts(
			graphStats.FileCount,
			graphStats.EntityCount,
			contentCoverage.FileCount,
			contentCoverage.EntityCount,
		)
	}
	coverage := map[string]any{
		"repo_id":                  repoID,
		"file_count":               0,
		"entity_count":             0,
		"languages":                []map[string]any{},
		"graph_available":          graphStats.Available,
		"server_content_available": contentCoverage.Available,
		"graph_gap_count":          graphGapCount,
		"content_gap_count":        contentGapCount,
		"completeness_state": completenessStateForCoverage(
			graphStats.Available,
			contentCoverage.Available,
			graphGapCount,
			contentGapCount,
		),
		"content_last_indexed_at": "",
		"last_error":              lastError,
		"summary": map[string]any{
			"graph_file_count":     graphStats.FileCount,
			"graph_entity_count":   graphStats.EntityCount,
			"content_file_count":   0,
			"content_entity_count": 0,
		},
	}
	if !contentCoverage.Available {
		coverage["last_error"] = "content store not available"
		return coverage
	}
	coverage["server_content_available"] = contentCoverage.Available
	coverage["file_count"] = contentCoverage.FileCount
	coverage["entity_count"] = contentCoverage.EntityCount
	coverage["languages"] = coverageLanguageMaps(contentCoverage.Languages)
	if latest := latestCoverageTimestamp(contentCoverage.FileIndexedAt, contentCoverage.EntityIndexedAt); !latest.IsZero() {
		coverage["content_last_indexed_at"] = latest.Format(time.RFC3339Nano)
	}
	summary := mapValue(coverage, "summary")
	summary["content_file_count"] = contentCoverage.FileCount
	summary["content_entity_count"] = contentCoverage.EntityCount
	summary["content_files_last_indexed_at"] = formatCoverageTimestamp(contentCoverage.FileIndexedAt)
	summary["content_entities_last_indexed_at"] = formatCoverageTimestamp(contentCoverage.EntityIndexedAt)
	summary["graph_gap_count"] = graphGapCount
	summary["content_gap_count"] = contentGapCount
	summary["completeness_state"] = coverage["completeness_state"]
	coverage["summary"] = summary
	return coverage
}

func (h *RepositoryHandler) queryRepositoryGraphCoverageStatsWithTimeout(
	ctx context.Context,
	repoID string,
) (repositoryGraphCoverageStats, error) {
	graphCtx, cancel := context.WithTimeout(ctx, repositoryGraphCoverageStatsTimeout)
	defer cancel()
	return h.queryRepositoryGraphCoverageStats(graphCtx, repoID)
}

func coverageLanguageMaps(languages []RepositoryLanguageCount) []map[string]any {
	if len(languages) == 0 {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(languages))
	for _, language := range languages {
		result = append(result, map[string]any{
			"language":   language.Language,
			"file_count": language.FileCount,
		})
	}
	return result
}

type repositoryGraphCoverageStats struct {
	FileCount   int
	EntityCount int
	Available   bool
}

func (h *RepositoryHandler) queryRepositoryGraphCoverageStats(
	ctx context.Context,
	repoID string,
) (repositoryGraphCoverageStats, error) {
	if h.Neo4j == nil || repoID == "" {
		return repositoryGraphCoverageStats{}, nil
	}

	row, err := h.Neo4j.RunSingle(ctx, `
		MATCH (r:Repository {id: $repo_id})
		OPTIONAL MATCH (r)-[:REPO_CONTAINS]->(f:File)
		WITH r, count(DISTINCT f) as file_count
		OPTIONAL MATCH (r)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(e)
		RETURN file_count, count(DISTINCT e) as entity_count
	`, map[string]any{"repo_id": repoID})
	if err != nil {
		return repositoryGraphCoverageStats{}, err
	}
	if row == nil {
		return repositoryGraphCoverageStats{}, nil
	}
	return repositoryGraphCoverageStats{
		FileCount:   IntVal(row, "file_count"),
		EntityCount: IntVal(row, "entity_count"),
		Available:   true,
	}, nil
}

func queryMaxIndexedAt(ctx context.Context, db *sql.DB, table string, repoID string) (time.Time, error) {
	var indexedAt sql.NullTime
	err := db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT max(indexed_at) as indexed_at
		FROM %s
		WHERE repo_id = $1
	`, table), repoID).Scan(&indexedAt)
	if err != nil {
		return time.Time{}, err
	}
	if !indexedAt.Valid {
		return time.Time{}, nil
	}
	return indexedAt.Time.UTC(), nil
}

func computeCoverageGapCounts(
	graphFileCount int,
	graphEntityCount int,
	contentFileCount int,
	contentEntityCount int,
) (int, int) {
	graphGapCount := maxInt(contentFileCount-graphFileCount, 0) + maxInt(contentEntityCount-graphEntityCount, 0)
	contentGapCount := maxInt(graphFileCount-contentFileCount, 0) + maxInt(graphEntityCount-contentEntityCount, 0)
	return graphGapCount, contentGapCount
}

func completenessStateForCoverage(
	graphAvailable bool,
	contentAvailable bool,
	graphGapCount int,
	contentGapCount int,
) string {
	switch {
	case !graphAvailable && !contentAvailable:
		return "unknown"
	case !graphAvailable:
		return "graph_unavailable"
	case !contentAvailable:
		return "content_unavailable"
	case graphGapCount == 0 && contentGapCount == 0:
		return "complete"
	case graphGapCount > 0 && contentGapCount > 0:
		return "graph_and_content_partial"
	case graphGapCount > 0:
		return "graph_partial"
	default:
		return "content_partial"
	}
}

func latestCoverageTimestamp(timestamps ...time.Time) time.Time {
	var latest time.Time
	for _, ts := range timestamps {
		if ts.IsZero() {
			continue
		}
		if latest.IsZero() || ts.After(latest) {
			latest = ts
		}
	}
	return latest
}

func formatCoverageTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
