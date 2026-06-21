package query

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
)

var repositoryBaseCypher = fmt.Sprintf(`
	MATCH (r:Repository {id: $repo_id})
	RETURN %s
`, RepoProjection("r"))

// RepositoryHandler exposes HTTP routes for repository queries.
type RepositoryHandler struct {
	Neo4j                    GraphQuery
	Content                  ContentStore
	CICDRunCorrelations      CICDRunCorrelationStore
	// ServiceCatalogCorrelations is the optional Postgres-backed store used to
	// enrich catalog workload rows with tier, category, domain, and language
	// declared in service-catalog manifests (Backstage, Cortex, OpsLevel).
	// When nil the catalog endpoint still works but those fields are omitted.
	ServiceCatalogCorrelations ServiceCatalogCorrelationStore
	Profile                  QueryProfile
	Logger                   *slog.Logger
}

// Mount registers all repository routes on the given mux.
func (h *RepositoryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/catalog", h.listCatalog)
	mux.HandleFunc("GET /api/v0/repositories", h.listRepositories)
	mux.HandleFunc("GET /api/v0/repositories/by-language", h.listRepositoriesByLanguage)
	mux.HandleFunc("GET /api/v0/repositories/language-inventory", h.getRepositoryLanguageInventory)
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/context", h.getRepositoryContext)
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/story", h.getRepositoryStory)
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/stats", h.getRepositoryStats)
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/coverage", h.getRepositoryCoverage)
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/tree", h.getRepositoryTree)
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/content", h.getRepositoryContent)
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/branches", h.getRepositoryBranches)
}

func (h *RepositoryHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// listRepositories returns a bounded page of indexed repositories. It also
// serves the inventory (empty-selector) form of get_repository_stats, so the
// response carries an additive result_limits drilldown block and an explicit
// partial_reasons slot, preserving the existing truncated paging field.
func (h *RepositoryHandler) listRepositories(w http.ResponseWriter, r *http.Request) {
	page := repositoryListPageFromRequest(r)
	access := repositoryAccessFilterFromContext(r.Context())
	if h == nil {
		WriteSuccess(w, r, http.StatusOK, repositoryInventoryResponse([]map[string]any{}, page, false), nil)
		return
	}
	if h.Neo4j == nil {
		repos, err := h.listRepositoriesFromContent(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
			return
		}
		repos = access.filterRepositoryMaps(repos)
		repos, truncated := pageRepositoryMaps(repos, page)
		WriteSuccess(w, r, http.StatusOK, repositoryInventoryResponse(repos, page, truncated), BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisContentIndex, "resolved from bounded repository content catalog"))
		return
	}
	if access.empty() {
		WriteSuccess(w, r, http.StatusOK, repositoryInventoryResponse([]map[string]any{}, page, false), BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisAuthoritativeGraph, "resolved from bounded repository graph catalog"))
		return
	}

	cypher := fmt.Sprintf(`
		MATCH (r:Repository)
		%s
		RETURN %s, coalesce(r.is_dependency, false) as is_dependency
		ORDER BY r.name, r.id
		SKIP $offset
		LIMIT $limit
	`, access.graphWhereClause("r"), RepoProjection("r"))

	rows, err := h.Neo4j.Run(r.Context(), cypher, access.graphParams(map[string]any{"offset": page.Offset, "limit": page.Limit + 1}))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	truncated := len(rows) > page.Limit
	if truncated {
		rows = rows[:page.Limit]
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
		repos = append(repos, decorateRepositoryGroupEvidence(repo))
	}

	WriteSuccess(w, r, http.StatusOK, repositoryInventoryResponse(repos, page, truncated), BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisAuthoritativeGraph, "resolved from bounded repository graph catalog"))
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
	if !requireContextOverview(w, r, h.profile(), "repository context requires authoritative platform context truth") {
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
	if relationshipReadModel != nil {
		relationshipReadModel = mergeRepositoryDeployableUnitRelationships(
			relationshipReadModel,
			queryRepoDeployableUnitRelationshipOverview(ctx, h.Neo4j, params),
		)
	}

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

func (h *RepositoryHandler) getRepositoryStory(w http.ResponseWriter, r *http.Request) {
	if !requireContextOverview(w, r, h.profile(), "repository story requires authoritative platform context truth") {
		return
	}

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
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "content_coverage")
	contentCoverage, coverageSummary, coverageErr := h.repositoryStoryContentCoverage(r.Context(), repoID)
	timer.Done(r.Context(), repositoryStatsCoverageLogAttrs(coverageSummary, coverageErr)...)
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
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "deployment_evidence")
	deploymentEvidence, err := loadRepositoryDeploymentEvidenceForOverview(r.Context(), h.Neo4j, h.Content, repoID)
	timer.Done(r.Context(), slog.Bool("has_result", len(deploymentEvidence) > 0), slog.Bool("error", err != nil))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load deployment evidence: %v", err))
		return
	}
	infrastructureOverview = attachRepositoryDeploymentEvidence(infrastructureOverview, deploymentEvidence)
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "ci_cd_evidence")
	ciCDEvidence, err := loadRepositoryScopedCICDEvidence(r.Context(), h.Content, h.CICDRunCorrelations, repoID)
	timer.Done(r.Context(), slog.Bool("has_result", len(ciCDEvidence) > 0), slog.Bool("error", err != nil))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load ci/cd evidence: %v", err))
		return
	}
	if len(ciCDEvidence) > 0 {
		if infrastructureOverview == nil {
			infrastructureOverview = map[string]any{}
		}
		infrastructureOverview["ci_cd_evidence"] = ciCDEvidence
	}

	response := buildRepositoryStoryResponseWithCoverage(
		repo,
		fileCount,
		languages,
		workloadNames,
		platformTypes,
		dependencyCount,
		infrastructureOverview,
		semanticOverview,
		coverageSummary,
	)
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "target_documentation")
	targetDocumentation, err := loadRepositoryStoryTargetDocumentation(r.Context(), h.Content, repoID)
	timer.Done(
		r.Context(),
		slog.Bool("has_result", len(targetDocumentation) > 0),
		slog.Int("finding_count", IntVal(targetDocumentation, "finding_count")),
		slog.Bool("error", err != nil),
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load target documentation: %v", err))
		return
	}
	if documentationOverview := attachStoryTargetDocumentation(
		mapValue(response, "documentation_overview"),
		targetDocumentation,
	); len(documentationOverview) > 0 {
		response["documentation_overview"] = documentationOverview
	}
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "target_support")
	targetSupport, err := loadRepositoryStoryTargetSupport(r.Context(), h.Content, repoID)
	timer.Done(
		r.Context(),
		slog.Bool("has_result", len(targetSupport) > 0),
		slog.Int("evidence_count", IntVal(targetSupport, "evidence_count")),
		slog.Bool("error", err != nil),
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load target support: %v", err))
		return
	}
	if len(targetSupport) > 0 {
		supportOverview := mapValue(response, "support_overview")
		if supportOverview == nil {
			supportOverview = map[string]any{}
		}
		supportOverview["target_support"] = targetSupport
		response["support_overview"] = supportOverview
	}
	enrichRepositoryStoryResponseWithEvidence(response, semanticOverview, narrativeFiles)

	WriteSuccess(
		w,
		r,
		http.StatusOK,
		response,
		BuildTruthEnvelope(
			h.profile(),
			"platform_impact.context_overview",
			TruthBasisHybrid,
			"resolved from bounded repository story, content coverage, and platform evidence",
		),
	)
}
