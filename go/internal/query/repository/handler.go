// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	"github.com/eshu-hq/eshu/go/internal/query/repository/readmodel"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var repositoryBaseCypher = fmt.Sprintf(`
	MATCH (r:Repository {id: $repo_id})
	RETURN %s
`, repository.RepoProjection("r"))

// Handler exposes HTTP routes for repository queries.
type Handler struct {
	Neo4j               querycontract.GraphQuery
	Content             querycontract.ContentStore
	CICDRunCorrelations querycontract.CICDRunCorrelationStore
	// ServiceCatalogCorrelations is the optional Postgres-backed store used to
	// enrich catalog workload rows with tier, category, domain, and language
	// declared in service-catalog manifests (Backstage, Cortex, OpsLevel).
	// When nil the catalog endpoint still works but those fields are omitted.
	ServiceCatalogCorrelations querycontract.ServiceCatalogCorrelationStore
	// Freshness reads the per-repository commit-receipt and
	// build-completeness evidence for GET
	// /api/v0/repositories/{id}/freshness (#5143). Nil is treated as
	// not-configured (503), matching the sibling nil-reader checks on the
	// status routes.
	Freshness FreshnessReader
	// Selections annotates GET /api/v0/repositories entries with the
	// collector selection aggregate (#8454). Nil renders every entry
	// unknown, matching the freshness block for a scope with no live
	// selector.
	Selections PageSelectionReader
	// Retirements annotates GET /api/v0/repositories entries with the
	// bounded retirement label (#7766): retiring and retired rows stay in
	// the list, labelled, never dropped. Nil derives index_state from the
	// row's evidence source alone.
	Retirements PageRetirementReader
	Profile     querycontract.QueryProfile
	Logger      *slog.Logger
	// Instruments records eshu_dp_config_artifact_candidates_total for the
	// story and context deployment-artifact loads (#7951). Nil records none.
	Instruments *telemetry.Instruments
}

// Mount registers all repository routes on the given mux.
func (h *Handler) Mount(mux *http.ServeMux) {
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

func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil {
		return querycontract.ProfileProduction
	}
	return querycontract.NormalizeQueryProfile(string(h.Profile))
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
func queryRepositoryTotal(ctx context.Context, graph querycontract.GraphQuery, access querycontract.RepositoryAccessFilter) (int, error) {
	var cypher string
	var params map[string]any
	if access.Scoped() {
		cypher = fmt.Sprintf(
			`MATCH (r:Repository) %s RETURN count(r) AS total`,
			access.GraphWhereClause("r"),
		)
		params = access.GraphParams(nil)
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
func (h *Handler) listRepositories(w http.ResponseWriter, r *http.Request) {
	page := readmodel.ListPageFromRequest(r)
	access := querycontract.RepositoryAccessFilterFromContext(r.Context())
	if h == nil {
		querycontract.WriteSuccess(w, r, http.StatusOK, repositoryInventoryResponse([]map[string]any{}, page, false, 0), nil)
		return
	}
	if h.Neo4j == nil {
		repos, err := h.listRepositoriesFromContent(r.Context())
		if err != nil {
			writeReadFailure(w, r, err, "platform_impact.context_overview", repositoryListContentFailedMessage)
			return
		}
		repos = access.FilterRepositoryMaps(repos)
		// Capture total before paging so it reflects the full filtered set.
		total := len(repos)
		repos, truncated := readmodel.PageRepositoryMaps(repos, page)
		var stage listAnnotationStage
		repos, stage, err = annotateListPageWithSelectionAndRetirement(r.Context(), h.Selections, h.Retirements, repos)
		if err != nil {
			writeReadFailure(w, r, err, "platform_impact.context_overview", stage.failureMessage())
			return
		}
		querycontract.WriteSuccess(w, r, http.StatusOK, repositoryInventoryResponse(repos, page, truncated, total), querycontract.BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", querycontract.TruthBasisContentIndex, "resolved from bounded repository content catalog"))
		return
	}
	if access.Empty() {
		querycontract.WriteSuccess(w, r, http.StatusOK, repositoryInventoryResponse([]map[string]any{}, page, false, 0), querycontract.BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", querycontract.TruthBasisAuthoritativeGraph, "resolved from bounded repository graph catalog"))
		return
	}

	// Run the total COUNT and the page query. The COUNT is a cheap label scan
	// that resolves in a few milliseconds; it does not need to be parallelised
	// with the page query on any graph backend at production scale.
	total, err := queryRepositoryTotal(r.Context(), h.Neo4j, access)
	if err != nil {
		writeReadFailure(w, r, err, "platform_impact.context_overview", repositoryListCountFailedMessage)
		return
	}

	// The page carries evidence_source alongside the shared projection so
	// the retirement annotation can derive indexed versus stub without a
	// second graph round trip (#7766). The shared RepoProjection is
	// untouched: only this list page needs the anchor.
	cypher := fmt.Sprintf(`
		MATCH (r:Repository)
		%s
		RETURN %s, coalesce(r.evidence_source, '') as evidence_source
		ORDER BY r.name, r.id
		SKIP $offset
		LIMIT $limit
	`, access.GraphWhereClause("r"), repository.RepoProjection("r"))

	rows, err := h.Neo4j.Run(r.Context(), cypher, access.GraphParams(map[string]any{"offset": page.Offset, "limit": page.Limit + 1}))
	if err != nil {
		writeReadFailure(w, r, err, "platform_impact.context_overview", repositoryListPageFailedMessage)
		return
	}
	truncated := len(rows) > page.Limit
	if truncated {
		rows = rows[:page.Limit]
	}

	// Dependency-edge pre-pass, clustering, and the is_dependency marker;
	// see resolveRepositoryDependencyEvidence's doc comment (issue #3504,
	// #6786 defect 1, #6786 review F1).
	evidence := resolveRepositoryDependencyEvidence(r.Context(), h.Logger, h.Neo4j, access)

	repos := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		id := querycontract.StringVal(row, "id")
		_, isDependency := evidence.Targets[id]
		repo := map[string]any{
			"id":              id,
			"name":            querycontract.StringVal(row, "name"),
			"path":            querycontract.StringVal(row, "path"),
			"local_path":      querycontract.StringVal(row, "local_path"),
			"remote_url":      querycontract.StringVal(row, "remote_url"),
			"repo_slug":       querycontract.StringVal(row, "repo_slug"),
			"has_remote":      querycontract.BoolVal(row, "has_remote"),
			"is_dependency":   isDependency,
			"evidence_source": querycontract.StringVal(row, "evidence_source"),
		}
		repos = append(repos, decorateRepositoryGroupEvidenceWithClusters(repo, evidence.Clusters))
	}

	var stage listAnnotationStage
	repos, stage, err = annotateListPageWithSelectionAndRetirement(r.Context(), h.Selections, h.Retirements, repos)
	if err != nil {
		writeReadFailure(w, r, err, "platform_impact.context_overview", stage.failureMessage())
		return
	}

	querycontract.WriteSuccess(w, r, http.StatusOK, repositoryInventoryResponse(repos, page, truncated, total, evidence.ExtraPartialReasons...), querycontract.BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", querycontract.TruthBasisAuthoritativeGraph, "resolved from bounded repository graph catalog"))
}

func (h *Handler) GetRepositoryStory(w http.ResponseWriter, r *http.Request) {
	h.getRepositoryStory(w, r)
}

func (h *Handler) getRepositoryStory(w http.ResponseWriter, r *http.Request) {
	if !querycontract.RequireContextOverview(w, r, h.profile(), "repository story requires authoritative platform context truth") {
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
		writeReadFailure(w, r, err, "platform_impact.context_overview", repositoryStoryLookupFailedMessage)
		return
	}
	if row == nil {
		querycontract.WriteError(w, http.StatusNotFound, "repository not found")
		return
	}
	repoID = querycontract.StringVal(row, "id")

	repo := repository.RepoRefFromRow(row)
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "content_coverage")
	contentCoverage, coverageSummary, coverageErr := h.repositoryStoryContentCoverage(r.Context(), repoID)
	timer.Done(r.Context(), repositoryStatsCoverageLogAttrs(coverageSummary, coverageErr)...)
	if writeCoverageVerdict(w, r, coverageErr, repositoryStoryCoverageFailedMessage) {
		return
	}
	readModelSummary := repository.LoadRepositoryReadModelSummary(r.Context(), h.Content, repoID)
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "graph_summary")
	storySummary, err := queryRepositoryStoryGraphSummary(r.Context(), h.Neo4j, map[string]any{"repo_id": repoID}, row, contentCoverage, readModelSummary)
	if err != nil {
		timer.Done(r.Context(), slog.Bool("error", true))
		writeReadFailure(w, r, err, "platform_impact.context_overview", repositoryStoryGraphSummaryFailedMessage)
		return
	}
	timer.Done(
		r.Context(),
		slog.Int("file_count", storySummary.fileCount),
		slog.Int("workload_count", len(storySummary.workloadNames)),
		slog.Int("platform_count", len(storySummary.platformTypes)),
		slog.Int("dependency_count", storySummary.dependencyCount),
		slog.Bool("truncated", storySummary.truncated),
	)
	fileCount := storySummary.fileCount
	languages := storySummary.languages
	workloadNames := storySummary.workloadNames
	platformTypes := storySummary.platformTypes
	dependencyCount := storySummary.dependencyCount
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "semantic_overview")
	semanticOverview, files, semanticTruncated, filesTruncated, err := loadRepositorySemanticOverview(r.Context(), h.Content, repoID)
	timer.Done(r.Context(), slog.Bool("error", err != nil), slog.Int("file_count", len(files)), slog.Bool("truncated", semanticTruncated), slog.Bool("files_truncated", filesTruncated))
	if err != nil {
		writeReadFailure(w, r, err, "platform_impact.context_overview", repositoryStorySemanticOverviewFailedMessage)
		return
	}
	var infrastructureOverview map[string]any
	narrativeFiles := []querycontract.FileContent(nil)
	storyLimitations := []string{}
	storyTruncated := storySummary.truncated
	if storySummary.truncated {
		storyLimitations = append(storyLimitations, storyRowsTruncatedReason)
	}
	if semanticTruncated {
		// The semantic entity/file reads found a row past their cap (#7126); the
		// overview and every stage sharing the file list are lower bounds.
		storyLimitations = append(storyLimitations, SemanticReadTruncatedReason)
		storyTruncated = true
	}
	if h.Content != nil {
		// files is the list the semantic overview stage already read; it is
		// shared by the stages below and not listed a second time (#7126).
		if files == nil {
			files = []querycontract.FileContent{}
		}
		timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "infrastructure")
		infrastructure, infrastructureDegraded, infrastructureTruncated := queryRepoInfrastructure(r.Context(), h.Neo4j, h.Content, map[string]any{"repo_id": repoID})
		timer.Done(r.Context(), infrastructureDegradeLogAttrs(len(infrastructure), infrastructureDegraded, infrastructureTruncated)...)
		if infrastructureDegraded {
			storyLimitations = append(storyLimitations, infrastructureReadDegradedReason)
		}
		if infrastructureTruncated {
			storyLimitations = append(storyLimitations, infrastructureTruncatedReason)
			// P3 review follow-up to #5764: fold the infrastructure panel's own
			// truncation into the top-level "truncated" field below, not only
			// storyLimitations/answer_metadata.partial_reasons. Before this fix
			// answer_metadata.truncated stayed false whenever only the
			// infrastructure read (not the headline workload/platform/language
			// rows) landed past its bound -- the same affirmative-false-claim
			// shape #5764 exists to remove from the story rows themselves.
			storyTruncated = true
		}
		infrastructureOverview = buildRepositoryInfrastructureOverview(infrastructure, files)
		timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "deployment_artifacts")
		deploymentOverview, deploymentStats, deploymentErr := artifacts.LoadDeploymentArtifactOverviewWithStats(
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
		if deploymentErr != nil {
			// The stage answers with the artifacts from the loads that
			// succeeded and lacks the failed load's artifacts; name the gap
			// instead of presenting the partial overview as complete (#7126).
			storyLimitations = append(storyLimitations, deploymentArtifactsReadDegradedReason)
			reportDeploymentArtifactsFailure(r.Context(), h.Logger, "repository_story", repoID, deploymentErr, deploymentStats)
		}
		if bound := deploymentStats.BoundReasons(); len(bound) > 0 {
			// A candidate bound bit (#7951): the config rows are a lower bound.
			storyLimitations, storyTruncated = append(storyLimitations, bound...), true
		}
		RecordDeploymentArtifactCandidates(r.Context(), h.Instruments, deploymentStats)
		RecordDeploymentArtifactReads(r.Context(), h.Instruments, deploymentStats)
		timer.Done(r.Context(), deploymentArtifactsStageAttrs(deploymentOverview != nil, deploymentErr, deploymentStats)...)
		timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "narrative_files")
		narrativeFiles, err = hydrateRepositoryNarrativeFiles(r.Context(), h.Content, repoID, files)
		timer.Done(r.Context(), slog.Bool("error", err != nil), slog.Int("file_count", len(narrativeFiles)))
		if err != nil {
			writeReadFailure(w, r, err, "platform_impact.context_overview", repositoryStoryNarrativeFilesFailedMessage)
			return
		}
		timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "relationships")
		relationships, relationshipsDegraded := queryRepoDependencies(r.Context(), h.Neo4j, map[string]any{"repo_id": repoID})
		timer.Done(r.Context(), degradedReadLogAttrs(len(relationships), relationshipsDegraded, relationshipsReadDegradedReason)...)
		if relationshipsDegraded {
			storyLimitations = append(storyLimitations, relationshipsReadDegradedReason)
		}
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
		writeReadFailure(w, r, err, "platform_impact.context_overview", repositoryStoryDeploymentEvidenceFailedMessage)
		return
	}
	infrastructureOverview = attachRepositoryDeploymentEvidence(infrastructureOverview, deploymentEvidence)
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "ci_cd_evidence")
	ciCDEvidence, err := artifacts.LoadRepositoryScopedCICDEvidenceFromFiles(r.Context(), h.Content, h.CICDRunCorrelations, repoID, files, filesTruncated)
	timer.Done(r.Context(), slog.Bool("has_result", len(ciCDEvidence) > 0), slog.Bool("error", err != nil))
	if err != nil {
		writeReadFailure(w, r, err, "platform_impact.context_overview", repositoryStoryCICDEvidenceFailedMessage)
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
		storyLimitations,
		storyTruncated,
	)
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "target_documentation")
	targetDocumentation, err := querycontract.LoadRepositoryStoryTargetDocumentation(r.Context(), h.Content, repoID)
	timer.Done(
		r.Context(),
		slog.Bool("has_result", len(targetDocumentation) > 0),
		slog.Int("finding_count", querycontract.IntVal(targetDocumentation, "finding_count")),
		slog.Bool("error", err != nil),
	)
	if err != nil {
		writeReadFailure(w, r, err, "platform_impact.context_overview", repositoryStoryTargetDocumentationFailedMessage)
		return
	}
	if documentationOverview := querycontract.AttachStoryTargetDocumentation(
		querycontract.MapValue(response, "documentation_overview"),
		targetDocumentation,
	); len(documentationOverview) > 0 {
		response["documentation_overview"] = documentationOverview
	}
	timer = startRepositoryQueryStage(r.Context(), h.Logger, "repository_story", repoID, "target_support")
	targetSupport, err := repository.LoadRepositoryStoryTargetSupport(r.Context(), h.Content, repoID)
	timer.Done(
		r.Context(),
		slog.Bool("has_result", len(targetSupport) > 0),
		slog.Int("evidence_count", querycontract.IntVal(targetSupport, "evidence_count")),
		slog.Int("target_support_incident_routing_count", querycontract.IntVal(targetSupport, "incident_routing_count")),
		slog.Int("target_support_ambiguous_count", querycontract.IntVal(targetSupport, "ambiguous_count")),
		slog.String("target_support_missing_reason", repository.FirstMissingEvidenceReason(targetSupport)),
		slog.Bool("error", err != nil),
	)
	if err != nil {
		writeReadFailure(w, r, err, "platform_impact.context_overview", repositoryStoryTargetSupportFailedMessage)
		return
	}
	if len(targetSupport) > 0 {
		supportOverview := querycontract.MapValue(response, "support_overview")
		if supportOverview == nil {
			supportOverview = map[string]any{}
		}
		supportOverview["target_support"] = targetSupport
		response["support_overview"] = supportOverview
	}
	enrichRepositoryStoryResponseWithEvidence(response, semanticOverview, narrativeFiles)
	evidence.AttachEvidenceBoundaries(response, "get_repo_story")

	querycontract.WriteSuccess(
		w,
		r,
		http.StatusOK,
		response,
		querycontract.BuildTruthEnvelope(
			h.profile(),
			"platform_impact.context_overview",
			querycontract.TruthBasisHybrid,
			"resolved from bounded repository story, content coverage, and platform evidence",
		),
	)
}
