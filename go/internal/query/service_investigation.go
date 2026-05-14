package query

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type serviceInvestigationOptions struct {
	Environment string
	Intent      string
	Question    string
}

// investigateService returns the coverage and next-call plan promised by the
// service onboarding prompts.
func (h *EntityHandler) investigateService(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "platform_impact.context_overview") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"service investigation requires authoritative platform context truth",
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

	ctx, err := h.fetchServiceWorkloadContext(r.Context(), serviceName, "service_investigation")
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
		Operation:                 "service_investigation",
	}); err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("enrich service investigation: %v", err))
		return
	}

	WriteSuccess(
		w,
		r,
		http.StatusOK,
		buildServiceInvestigationPacket(serviceName, ctx, serviceInvestigationOptions{
			Environment: QueryParam(r, "environment"),
			Intent:      QueryParam(r, "intent"),
			Question:    QueryParam(r, "question"),
		}),
		BuildTruthEnvelope(
			h.profile(),
			"platform_impact.context_overview",
			TruthBasisHybrid,
			"resolved from service investigation coverage and platform evidence",
		),
	)
}

func buildServiceInvestigationPacket(
	serviceName string,
	workloadContext map[string]any,
	opts serviceInvestigationOptions,
) map[string]any {
	serviceName = canonicalServiceName(serviceName, workloadContext)
	evidenceFamilies := serviceInvestigationEvidenceFamilies(workloadContext)
	repositories := serviceInvestigationRepositories(workloadContext, evidenceFamilies)
	withEvidence := repositoriesWithEvidence(repositories)
	findings := serviceInvestigationFindings(workloadContext, evidenceFamilies)
	nextCalls := serviceInvestigationNextCalls(serviceName, workloadContext)
	coverage := serviceInvestigationCoverage(workloadContext, repositories, withEvidence, evidenceFamilies)

	packet := map[string]any{
		"service_name":               serviceName,
		"repositories_considered":    repositories,
		"repositories_with_evidence": withEvidence,
		"evidence_families_found":    evidenceFamilies,
		"coverage_summary":           coverage,
		"investigation_findings":     findings,
		"recommended_next_calls":     nextCalls,
		"service_story_path":         "/api/v0/services/" + serviceName + "/story",
		"service_context_path":       "/api/v0/services/" + serviceName + "/context",
	}
	if opts.Environment != "" {
		packet["environment"] = opts.Environment
	}
	if opts.Intent != "" {
		packet["intent"] = opts.Intent
	}
	if opts.Question != "" {
		packet["question"] = opts.Question
	}
	return packet
}

func serviceInvestigationEvidenceFamilies(workloadContext map[string]any) []string {
	families := make([]string, 0, 6)
	if apiSurface := mapValue(workloadContext, "api_surface"); len(apiSurface) > 0 &&
		(IntVal(apiSurface, "endpoint_count") > 0 || IntVal(apiSurface, "spec_count") > 0 ||
			len(mapSliceValue(apiSurface, "endpoints")) > 0) {
		families = append(families, "api_surface")
	}
	if len(mapSliceValue(workloadContext, "instances")) > 0 || len(serviceDeploymentArtifacts(workloadContext)) > 0 {
		families = append(families, "deployment_lanes")
	}
	if len(mapValue(workloadContext, "documentation_overview")) > 0 {
		families = append(families, "documentation")
	}
	if len(mapSliceValue(workloadContext, "dependencies")) > 0 ||
		len(mapSliceValue(workloadContext, "provisioning_source_chains")) > 0 ||
		len(serviceDeploymentArtifacts(workloadContext)) > 0 {
		families = append(families, "upstream_dependencies")
	}
	if len(mapSliceValue(workloadContext, "dependents")) > 0 ||
		len(mapSliceValue(workloadContext, "consumer_repositories")) > 0 {
		families = append(families, "downstream_consumers")
	}
	if len(mapValue(workloadContext, "support_overview")) > 0 {
		families = append(families, "support")
	}
	sort.Strings(families)
	return families
}

func serviceInvestigationRepositories(workloadContext map[string]any, evidenceFamilies []string) []map[string]any {
	repos := map[string]map[string]any{}
	serviceRepoID := safeStr(workloadContext, "repo_id")
	addInvestigationRepo(repos, serviceRepoID, safeStr(workloadContext, "repo_name"), "service_owner", evidenceFamilies...)

	for _, artifact := range serviceDeploymentArtifacts(workloadContext) {
		addInvestigationRepo(
			repos,
			StringVal(artifact, "source_repo_id"),
			StringVal(artifact, "source_repo_name"),
			"deployment_source",
			firstNonEmptyString(StringVal(artifact, "artifact_family"), StringVal(artifact, "relationship_type")),
		)
		addInvestigationRepo(
			repos,
			StringVal(artifact, "target_repo_id"),
			StringVal(artifact, "target_repo_name"),
			"deployment_target",
			firstNonEmptyString(StringVal(artifact, "artifact_family"), StringVal(artifact, "relationship_type")),
		)
	}
	for _, dependent := range mapSliceValue(workloadContext, "dependents") {
		addInvestigationRepo(repos, StringVal(dependent, "repo_id"), StringVal(dependent, "repository"), "graph_dependent", "downstream_consumers")
	}
	for _, consumer := range mapSliceValue(workloadContext, "consumer_repositories") {
		addInvestigationRepo(repos, StringVal(consumer, "repo_id"), StringVal(consumer, "repository"), "content_consumer", "downstream_consumers")
	}
	for _, chain := range mapSliceValue(workloadContext, "provisioning_source_chains") {
		addInvestigationRepo(repos, StringVal(chain, "repo_id"), StringVal(chain, "repository"), "provisioning_source", "upstream_dependencies")
	}

	rows := make([]map[string]any, 0, len(repos))
	for _, repo := range repos {
		sortStringFields(repo, "roles", "evidence_families")
		rows = append(rows, repo)
	}
	sort.Slice(rows, func(i, j int) bool {
		return StringVal(rows[i], "repo_id") < StringVal(rows[j], "repo_id")
	})
	capped, truncated := capMapRows(rows, serviceStoryItemLimit)
	if truncated {
		capped = append(capped, map[string]any{
			"repo_id": serviceInvestigationTruncationMarkerID,
			"note":    fmt.Sprintf("repository list truncated at %d rows", serviceStoryItemLimit),
		})
	}
	return capped
}

const serviceInvestigationTruncationMarkerID = "__truncated__"

func addInvestigationRepo(repos map[string]map[string]any, repoID string, repoName string, role string, families ...string) {
	repoID = strings.TrimSpace(repoID)
	repoName = strings.TrimSpace(repoName)
	if repoID == "" && repoName == "" {
		return
	}
	key := repoID
	if key == "" {
		key = repoName
	}
	row := repos[key]
	if row == nil {
		row = map[string]any{
			"repo_id":           repoID,
			"repo_name":         repoName,
			"roles":             []string{},
			"evidence_families": []string{},
		}
		repos[key] = row
	}
	addUniqueStringField(row, "roles", role)
	for _, family := range families {
		addUniqueStringField(row, "evidence_families", family)
	}
	row["evidence_family_count"] = len(StringSliceVal(row, "evidence_families"))
}

func repositoriesWithEvidence(repositories []map[string]any) []map[string]any {
	withEvidence := make([]map[string]any, 0, len(repositories))
	for _, repo := range repositories {
		if StringVal(repo, "repo_id") == serviceInvestigationTruncationMarkerID {
			continue
		}
		if len(StringSliceVal(repo, "evidence_families")) == 0 {
			continue
		}
		withEvidence = append(withEvidence, repo)
	}
	return withEvidence
}

func serviceInvestigationFindings(workloadContext map[string]any, evidenceFamilies []string) []map[string]any {
	findings := make([]map[string]any, 0, len(evidenceFamilies))
	for _, family := range evidenceFamilies {
		findings = append(findings, map[string]any{
			"family":        family,
			"summary":       serviceInvestigationFamilySummary(workloadContext, family),
			"evidence_path": serviceInvestigationEvidencePath(family),
		})
	}
	return findings
}

func serviceInvestigationFamilySummary(workloadContext map[string]any, family string) string {
	switch family {
	case "api_surface":
		apiSurface := mapValue(workloadContext, "api_surface")
		return fmt.Sprintf("%d endpoint(s) across %d spec file(s)", IntVal(apiSurface, "endpoint_count"), IntVal(apiSurface, "spec_count"))
	case "deployment_lanes":
		return fmt.Sprintf("%d runtime instance(s), %d deployment artifact(s)", len(mapSliceValue(workloadContext, "instances")), len(serviceDeploymentArtifacts(workloadContext)))
	case "documentation":
		return "indexed documentation metadata is available for the service repository"
	case "downstream_consumers":
		return fmt.Sprintf("%d graph dependent(s), %d content consumer repo(s)", len(mapSliceValue(workloadContext, "dependents")), len(mapSliceValue(workloadContext, "consumer_repositories")))
	case "upstream_dependencies":
		return fmt.Sprintf("%d dependency row(s), %d provisioning chain(s)", len(mapSliceValue(workloadContext, "dependencies")), len(mapSliceValue(workloadContext, "provisioning_source_chains")))
	case "support":
		return "support metadata is available for the service"
	default:
		return "evidence family is present"
	}
}

func serviceInvestigationEvidencePath(family string) string {
	return map[string]string{
		"api_surface":           "api_surface",
		"deployment_lanes":      "deployment_evidence.artifacts",
		"documentation":         "documentation_overview",
		"downstream_consumers":  "dependents, consumer_repositories",
		"upstream_dependencies": "dependencies, provisioning_source_chains, deployment_evidence.artifacts",
		"support":               "support_overview",
	}[family]
}

func serviceInvestigationNextCalls(serviceName string, workloadContext map[string]any) []map[string]any {
	nextCalls := []map[string]any{
		{
			"tool":   "get_service_story",
			"reason": "retrieve the full one-call dossier for answer generation",
			"arguments": map[string]any{
				"workload_id": serviceName,
			},
		},
		{
			"tool":   "get_service_context",
			"reason": "drill into raw service context when the dossier is not enough",
			"arguments": map[string]any{
				"workload_id": serviceName,
			},
		},
		{
			"tool":   "trace_deployment_chain",
			"reason": "walk the deployment graph only when chain details are needed",
			"arguments": map[string]any{
				"service_name":                 serviceName,
				"include_related_module_usage": true,
			},
		},
	}
	for _, artifact := range serviceDeploymentArtifacts(workloadContext) {
		resolvedID := StringVal(artifact, "resolved_id")
		if resolvedID == "" {
			continue
		}
		nextCalls = append(nextCalls, map[string]any{
			"tool":   "get_relationship_evidence",
			"reason": "dereference the durable source evidence for a relationship",
			"arguments": map[string]any{
				"resolved_id": resolvedID,
			},
		})
		if len(nextCalls) >= 8 {
			break
		}
	}
	return nextCalls
}

func serviceInvestigationCoverage(
	workloadContext map[string]any,
	repositories []map[string]any,
	withEvidence []map[string]any,
	evidenceFamilies []string,
) map[string]any {
	state := "unknown"
	reason := "no cross-repository evidence families were found"
	if len(evidenceFamilies) > 0 {
		state = "partial"
		reason = "evidence was found, but the index cannot prove exhaustive coverage across every related repository"
	}
	if len(StringSliceVal(workloadContext, "limitations")) > 0 ||
		StringVal(workloadContext, "materialization_status") == "identity_only" {
		state = "partial"
		reason = "service materialization reports limitations"
	}
	return map[string]any{
		"state":                            state,
		"reason":                           reason,
		"repository_count":                 len(repositories),
		"repositories_with_evidence_count": len(withEvidence),
		"evidence_family_count":            len(evidenceFamilies),
		"result_limit":                     serviceStoryItemLimit,
		"truncated":                        serviceInvestigationRepositoriesTruncated(repositories),
	}
}

func serviceInvestigationRepositoriesTruncated(repositories []map[string]any) bool {
	for _, repo := range repositories {
		if StringVal(repo, "repo_id") == serviceInvestigationTruncationMarkerID {
			return true
		}
	}
	return false
}
