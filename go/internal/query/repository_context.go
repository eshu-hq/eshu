// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"log/slog"
	"net/http"
)

func (h *RepositoryHandler) getRepositoryContext(w http.ResponseWriter, r *http.Request) {
	if !requireContextOverview(w, r, h.profile(), "repository context requires authoritative platform context truth") {
		return
	}

	ctx := r.Context()
	repoID, ok := h.resolveRepositoryPathSelector(w, r, "platform_impact.context_overview")
	if !ok {
		return
	}
	params := map[string]any{"repo_id": repoID}

	timer := startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "repository_lookup")
	baseRow, err := h.Neo4j.RunSingle(ctx, repositoryBaseCypher, params)
	timer.Done(ctx, slog.Bool("found", baseRow != nil), slog.Bool("error", err != nil))
	if err != nil {
		if WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
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
		// #5167 W3 P0 (fourth vector): bind the merged read-model relationship
		// rows and consumers to the caller's grant before result["relationships"],
		// result["relationship_overview"], and result["consumers"] derive from
		// them. This is the production-primary path and it (plus the unfiltered
		// deployable-unit graph supplement merged just above) otherwise bypasses
		// the grant filter the graph helpers apply.
		relationshipReadModel = filterRepositoryRelationshipReadModelForAccess(
			relationshipReadModel,
			repoID,
			repositoryAccessFilterFromContext(ctx),
		)
	}

	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "summary_counts")
	counts := queryRepositoryContextCounts(ctx, h.Neo4j, params, baseRow, contentCoverage, readModelSummary)
	timer.Done(
		ctx,
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

	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "tech_fingerprint")
	languageBreakdown := buildLanguageBreakdownFromRows(result["languages"].([]map[string]any))
	if len(languageBreakdown) > 0 {
		result["language_breakdown"] = languageBreakdown
	}
	sourceToolBreakdown := buildSourceToolBreakdownFromRows(queryRepoSourceToolBreakdown(ctx, h.Neo4j, params))
	if len(sourceToolBreakdown) > 0 {
		result["source_tool_breakdown"] = sourceToolBreakdown
	}
	timer.Done(
		ctx,
		slog.Int("language_count", len(languageBreakdown)),
		slog.Int("source_tool_count", len(sourceToolBreakdown)),
	)

	WriteSuccess(w, r, http.StatusOK, result, BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisHybrid, "resolved from repository context and platform evidence"))
}
