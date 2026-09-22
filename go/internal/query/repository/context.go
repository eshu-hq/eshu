// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
)

// GetRepositoryContext serves repository context. It forwards to getRepositoryContext; exported for
// #6060 so the cross-family graph-read sweep tests in package query can name
// it from outside this package.
func (h *Handler) GetRepositoryContext(w http.ResponseWriter, r *http.Request) {
	h.getRepositoryContext(w, r)
}

func (h *Handler) getRepositoryContext(w http.ResponseWriter, r *http.Request) {
	if !querycontract.RequireContextOverview(w, r, h.profile(), "repository context requires authoritative platform context truth") {
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
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	if baseRow == nil {
		querycontract.WriteError(w, http.StatusNotFound, "repository not found")
		return
	}
	contentCoverage := loadRepositoryContentCoverage(ctx, h.Content, repoID)
	readModelSummary := querycontract.LoadRepositoryReadModelSummary(ctx, h.Content, repoID)
	relationshipReadModel := querycontract.LoadRepositoryRelationshipReadModel(ctx, h.Content, repoID)
	partialReasons := make([]string, 0)
	if relationshipReadModel != nil {
		deployableUnitRows, deployableUnitDegraded := queryRepoDeployableUnitRelationshipOverview(ctx, h.Neo4j, params)
		if deployableUnitDegraded {
			partialReasons = append(partialReasons, deployableUnitRelationshipsReadDegradedReason)
		}
		relationshipReadModel = mergeRepositoryDeployableUnitRelationships(relationshipReadModel, deployableUnitRows)
		// #5167 W3 P0 (fourth vector): bind the merged read-model relationship
		// rows and consumers to the caller's grant before result["relationships"],
		// result["relationship_overview"], and result["consumers"] derive from
		// them. This is the production-primary path and it (plus the unfiltered
		// deployable-unit graph supplement merged just above) otherwise bypasses
		// the grant filter the graph helpers apply.
		relationshipReadModel = filterRepositoryRelationshipReadModelForAccess(
			relationshipReadModel,
			repoID,
			querycontract.RepositoryAccessFilterFromContext(ctx),
		)
	}

	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "summary_counts")
	counts, err := queryRepositoryContextCounts(ctx, h.Neo4j, params, baseRow, contentCoverage, readModelSummary)
	if err != nil {
		timer.Done(ctx, slog.Bool("error", true))
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))
		return
	}
	timer.Done(
		ctx,
		slog.Int("file_count", counts.fileCount),
		slog.Int("workload_count", counts.workloadCount),
		slog.Int("platform_count", counts.platformCount),
		slog.Int("dependency_count", counts.dependencyCount),
	)
	result := map[string]any{
		"repository":       querycontract.RepoRefFromRow(baseRow),
		"file_count":       counts.fileCount,
		"workload_count":   counts.workloadCount,
		"platform_count":   counts.platformCount,
		"dependency_count": counts.dependencyCount,
	}

	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "entry_points")
	entryPoints, entryPointsDegraded := queryRepoEntryPoints(ctx, h.Neo4j, h.Content, params)
	result["entry_points"] = entryPoints
	timer.Done(ctx, degradedReadLogAttrs(len(entryPoints), entryPointsDegraded, entryPointsReadDegradedReason)...)
	if entryPointsDegraded {
		partialReasons = append(partialReasons, entryPointsReadDegradedReason)
	}

	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "infrastructure")
	infrastructure, infrastructureDegraded, infrastructureTruncated := queryRepoInfrastructure(ctx, h.Neo4j, h.Content, params)
	result["infrastructure"] = infrastructure
	timer.Done(ctx, infrastructureDegradeLogAttrs(len(infrastructure), infrastructureDegraded, infrastructureTruncated)...)
	if infrastructureDegraded {
		partialReasons = append(partialReasons, infrastructureReadDegradedReason)
	}
	if infrastructureTruncated {
		partialReasons = append(partialReasons, infrastructureTruncatedReason)
	}

	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "relationships")
	relationshipsDegraded := false
	if dependencies := repositoryReadModelDependencies(relationshipReadModel); dependencies != nil {
		result["relationships"] = dependencies
	} else {
		result["relationships"], relationshipsDegraded = queryRepoDependencies(ctx, h.Neo4j, params)
	}
	timer.Done(ctx, degradedReadLogAttrs(len(result["relationships"].([]map[string]any)), relationshipsDegraded, relationshipsReadDegradedReason)...)
	if relationshipsDegraded {
		partialReasons = append(partialReasons, relationshipsReadDegradedReason)
	}

	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "relationship_overview")
	var relationshipRows []map[string]any
	relationshipOverviewDegraded := false
	if relationshipReadModel != nil {
		relationshipRows = relationshipReadModel.Relationships
	} else {
		relationshipRows, relationshipOverviewDegraded = queryRepoRelationshipOverview(ctx, h.Neo4j, params)
	}
	timer.Done(ctx, degradedReadLogAttrs(len(relationshipRows), relationshipOverviewDegraded, relationshipOverviewReadDegradedReason)...)
	if relationshipOverviewDegraded {
		partialReasons = append(partialReasons, relationshipOverviewReadDegradedReason)
	}
	if len(relationshipRows) == 0 {
		relationshipRows = result["relationships"].([]map[string]any)
	}
	if relationshipOverview := buildRepositoryRelationshipOverview(relationshipRows); relationshipOverview != nil {
		result["relationship_overview"] = relationshipOverview
	}

	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "consumers")
	consumersDegraded := false
	if relationshipReadModel != nil {
		result["consumers"] = relationshipReadModel.Consumers
	} else {
		result["consumers"], consumersDegraded = queryRepoConsumers(ctx, h.Neo4j, params)
	}
	timer.Done(ctx, degradedReadLogAttrs(len(result["consumers"].([]map[string]any)), consumersDegraded, consumersReadDegradedReason)...)
	if consumersDegraded {
		partialReasons = append(partialReasons, consumersReadDegradedReason)
	}
	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "api_surface")
	apiSurface, apiSurfaceDegraded := queryRepoAPISurface(ctx, h.Neo4j, params)
	if apiSurfaceDegraded {
		partialReasons = append(partialReasons, apiSurfaceReadDegradedReason)
	}
	if len(apiSurface) > 0 {
		result["api_surface"] = apiSurface
		timer.Done(ctx, degradedReadLogAttrs(len(apiSurface), apiSurfaceDegraded, apiSurfaceReadDegradedReason)...)
	} else {
		timer.Done(ctx, degradedReadLogAttrs(0, apiSurfaceDegraded, apiSurfaceReadDegradedReason)...)
	}
	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "deployment_evidence")
	deploymentEvidence, err := queryRepoDeploymentEvidence(ctx, h.Neo4j, h.Content, params)
	if err != nil {
		timer.Done(ctx, slog.Bool("error", true))
		if querycontract.WriteGraphReadError(w, r, err, "platform_impact.context_overview") {
			return
		}
		querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load deployment evidence: %v", err))
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
		files, err := h.Content.ListRepoFiles(ctx, repoID, querycontract.RepositorySemanticEntityLimit)
		if err == nil {
			if files == nil {
				files = []querycontract.FileContent{}
			}
			overview := buildRepositoryInfrastructureOverview(result["infrastructure"].([]map[string]any), files)
			deploymentOverview, _ := artifacts.LoadDeploymentArtifactOverview(
				ctx,
				h.Neo4j,
				h.Content,
				repoID,
				querycontract.StringVal(baseRow, "name"),
				files,
				result["infrastructure"].([]map[string]any),
				overview,
			)
			if deploymentOverview != nil {
				overview = deploymentOverview
			}
			if overview != nil {
				if deploymentArtifacts := querycontract.MapValue(overview, "deployment_artifacts"); len(deploymentArtifacts) > 0 {
					result["deployment_artifacts"] = deploymentArtifacts
				}
				result["infrastructure_overview"] = overview
			}
		}
		timer.Done(ctx, slog.Bool("error", err != nil))
	}
	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "languages")
	languagesDegraded := false
	if languages, ok := repositoryLanguageDistributionFromCoverage(contentCoverage); ok {
		result["languages"] = languages
	} else {
		result["languages"], languagesDegraded = queryRepoLanguageDistribution(ctx, h.Neo4j, params)
	}
	timer.Done(ctx, degradedReadLogAttrs(len(result["languages"].([]map[string]any)), languagesDegraded, languagesReadDegradedReason)...)
	if languagesDegraded {
		partialReasons = append(partialReasons, languagesReadDegradedReason)
	}

	timer = startRepositoryQueryStage(ctx, h.Logger, "repository_context", repoID, "tech_fingerprint")
	languageBreakdown := buildLanguageBreakdownFromRows(result["languages"].([]map[string]any))
	if len(languageBreakdown) > 0 {
		result["language_breakdown"] = languageBreakdown
	}
	sourceToolRows, sourceToolDegraded := queryRepoSourceToolBreakdown(ctx, h.Neo4j, params)
	sourceToolBreakdown := buildSourceToolBreakdownFromRows(sourceToolRows)
	if len(sourceToolBreakdown) > 0 {
		result["source_tool_breakdown"] = sourceToolBreakdown
	}
	if sourceToolDegraded {
		partialReasons = append(partialReasons, sourceToolBreakdownReadDegradedReason)
	}
	fingerprintAttrs := []slog.Attr{
		slog.Int("language_count", len(languageBreakdown)),
		slog.Int("source_tool_count", len(sourceToolBreakdown)),
	}
	if sourceToolDegraded {
		fingerprintAttrs = append(fingerprintAttrs, slog.String("failure_class", sourceToolBreakdownReadDegradedReason))
	}
	timer.Done(ctx, fingerprintAttrs...)

	result["partial_reasons"] = partialReasons

	querycontract.WriteSuccess(w, r, http.StatusOK, result, querycontract.BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", querycontract.TruthBasisHybrid, "resolved from repository context and platform evidence"))
}
