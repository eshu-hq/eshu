// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
)

var repositoryBaseCypher = fmt.Sprintf(`
	MATCH (r:Repository {id: $repo_id})
	RETURN %s
`, RepoProjection("r"))

// RepositoryHandler exposes HTTP routes for repository queries.
type RepositoryHandler struct {
	Neo4j               GraphQuery
	Content             ContentStore
	CICDRunCorrelations CICDRunCorrelationStore
	// ServiceCatalogCorrelations is the optional Postgres-backed store used to
	// enrich catalog workload rows with tier, category, domain, and language
	// declared in service-catalog manifests (Backstage, Cortex, OpsLevel).
	// When nil the catalog endpoint still works but those fields are omitted.
	ServiceCatalogCorrelations ServiceCatalogCorrelationStore
	// Freshness reads the per-repository commit-receipt and
	// build-completeness evidence for GET
	// /api/v0/repositories/{id}/freshness (#5143). Nil is treated as
	// not-configured (503), matching the sibling nil-reader checks on the
	// status routes.
	Freshness RepositoryFreshnessReader
	Profile   QueryProfile
	Logger    *slog.Logger
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
	mux.HandleFunc("GET /api/v0/repositories/{repo_id}/freshness", h.getRepositoryFreshness)
}

func (h *RepositoryHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

// repositoryCountCypher is the bounded COUNT query used to populate the total
// field on the repository list response independently of the page size. It runs
// in a few milliseconds on production-scale graphs because it uses the same
// Repository node scan that the backing store optimises for label counts.
const repositoryCountCypher = `MATCH (r:Repository) RETURN count(r) AS total`

// isRepositoryCountCypher reports whether a Cypher string is the bounded count
// query emitted by queryRepositoryTotal. Test fakes use this to dispatch the
// count response separately from the page response.
func isRepositoryCountCypher(cypher string) bool {
	return cypher == repositoryCountCypher
}

// queryRepositoryTotal runs a bounded COUNT query and returns the true number
// of Repository nodes visible to the caller. A failed or malformed count is an
// error rather than an exact zero because zero is a valid, materially different
// inventory result.
func queryRepositoryTotal(ctx context.Context, graph GraphQuery, access repositoryAccessFilter) (int, error) {
	var cypher string
	var params map[string]any
	if access.scoped() {
		cypher = fmt.Sprintf(
			`MATCH (r:Repository) %s RETURN count(r) AS total`,
			access.graphWhereClause("r"),
		)
		params = access.graphParams(nil)
	} else {
		cypher = repositoryCountCypher
	}
	row, err := graph.RunSingle(ctx, cypher, params)
	if err != nil {
		return 0, fmt.Errorf("repository total: %w", err)
	}
	if row == nil {
		return 0, fmt.Errorf("repository total: count query returned no row")
	}
	raw, ok := row["total"]
	if !ok || raw == nil {
		return 0, fmt.Errorf("repository total: count query omitted total")
	}
	var total int
	switch value := raw.(type) {
	case int:
		total = value
	case int64:
		if value > int64(math.MaxInt) {
			return 0, fmt.Errorf("repository total: count query total exceeds platform integer range")
		}
		total = int(value)
	case float64:
		if value != math.Trunc(value) || value > float64(math.MaxInt) {
			return 0, fmt.Errorf("repository total: count query returned non-integer total")
		}
		total = int(value)
	default:
		return 0, fmt.Errorf("repository total: count query returned unsupported total type %T", raw)
	}
	if total < 0 {
		return 0, fmt.Errorf("repository total: count query returned negative total")
	}
	return total, nil
}

// listRepositories returns a bounded page of indexed repositories. It also
// serves the inventory (empty-selector) form of get_repository_stats, so the
// response carries an additive result_limits drilldown block, an explicit
// partial_reasons slot, and a total field that reflects the true repository
// count independent of page size.
func (h *RepositoryHandler) listRepositories(w http.ResponseWriter, r *http.Request) {
	page := repositoryListPageFromRequest(r)
	access := repositoryAccessFilterFromContext(r.Context())
	if h == nil {
		WriteSuccess(w, r, http.StatusOK, repositoryInventoryResponse([]map[string]any{}, page, false, 0), nil)
		return
	}
	if h.Neo4j == nil {
		repos, err := h.listRepositoriesFromContent(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
			return
		}
		repos = access.filterRepositoryMaps(repos)
		// Capture total before paging so it reflects the full filtered set.
		total := len(repos)
		repos, truncated := pageRepositoryMaps(repos, page)
		WriteSuccess(w, r, http.StatusOK, repositoryInventoryResponse(repos, page, truncated, total), BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisContentIndex, "resolved from bounded repository content catalog"))
		return
	}
	if access.empty() {
		WriteSuccess(w, r, http.StatusOK, repositoryInventoryResponse([]map[string]any{}, page, false, 0), BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisAuthoritativeGraph, "resolved from bounded repository graph catalog"))
		return
	}

	// Run the total COUNT and the page query. The COUNT is a cheap label scan
	// that resolves in a few milliseconds; it does not need to be parallelised
	// with the page query on any graph backend at production scale.
	total, err := queryRepositoryTotal(r.Context(), h.Neo4j, access)
	if err != nil {
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}

	cypher := fmt.Sprintf(`
		MATCH (r:Repository)
		%s
		RETURN %s, %s
		ORDER BY r.name, r.id
		SKIP $offset
		LIMIT $limit
	`, access.graphWhereClause("r"), RepoProjection("r"), repositoryDependencyMarkerProjection("r", access))

	rows, err := h.Neo4j.Run(r.Context(), cypher, access.graphParams(map[string]any{"offset": page.Offset, "limit": page.Limit + 1}))
	if err != nil {
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	truncated := len(rows) > page.Limit
	if truncated {
		rows = rows[:page.Limit]
	}

	// Dependency-cluster pre-pass: one bounded edge query over
	// (:Repository)-[:DEPENDS_ON]->(:Repository), then connected-component
	// grouping in Go. This is the primary grouping signal (issue #3504):
	// repositories that depend on each other share a cluster key, and the
	// per-row decoration below gives that cluster precedence over the
	// source-backed slug/owner/flag derivation. Repositories in no dependency
	// edge fall through to honest missing_evidence rather than a name heuristic.
	//
	// The pre-pass is instrumented with the existing stage timer so operators
	// can diagnose its duration and edge count from the
	// repository_query.stage_started / repository_query.stage_completed log
	// events (operation=repository_list, stage=dependency_cluster_edges).
	clusterTimer := startRepositoryQueryStage(r.Context(), h.Logger, "repository_list", "", "dependency_cluster_edges")
	clusters := loadRepositoryDependencyClusters(r.Context(), h.Neo4j, access)
	clusterTimer.Done(r.Context(), slog.Int("cluster_count", len(clusters)))

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
		repos = append(repos, decorateRepositoryGroupEvidenceWithClusters(repo, clusters))
	}

	WriteSuccess(w, r, http.StatusOK, repositoryInventoryResponse(repos, page, truncated, total), BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisAuthoritativeGraph, "resolved from bounded repository graph catalog"))
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
func (h *RepositoryHandler) getRepositoryStory(w http.ResponseWriter, r *http.Request) {
	if !requireContextOverview(w, r, h.profile(), "repository story requires authoritative platform context truth") {
		return
	}

	repoID, ok := h.resolveRepositoryPathSelector(w, r, "platform_impact.context_overview")
	if !ok {
		return
	}

	timer := startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "repository_lookup")
	row, err := h.Neo4j.RunSingle(r.Context(), repositoryBaseCypher, map[string]any{"repo_id": repoID})
	timer.Done(r.Context(), slog.Bool("found", row != nil), slog.Bool("error", err != nil))
	if err != nil {
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
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
	timer.Done(
		r.Context(),
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
	attachEvidenceBoundaries(response, "get_repo_story")

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
