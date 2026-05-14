package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const (
	defaultIndirectEvidenceSearchLimit = 25
	maxIndirectEvidenceSearchLimit     = 100
)

type provisioningRepositoryCandidate struct {
	RepoID              string
	RepoName            string
	RelationshipTypes   []string
	RelationshipReasons []string
}

type traceEvidenceAccumulator struct {
	samplePaths   map[string]struct{}
	evidenceKinds map[string]struct{}
	modules       map[string]struct{}
	configPaths   map[string]struct{}
	matchedValues map[string]struct{}
}

func loadProvisioningSourceChains(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	serviceRepoID string,
) ([]map[string]any, error) {
	return loadProvisioningSourceChainsWithLimit(ctx, graph, content, serviceRepoID, 0)
}

func loadProvisioningSourceChainsWithLimit(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	serviceRepoID string,
	limit int,
) ([]map[string]any, error) {
	candidates, err := queryProvisioningRepositoryCandidates(ctx, graph, serviceRepoID, limit)
	if err != nil {
		return nil, err
	}
	return loadProvisioningSourceChainsFromCandidates(ctx, content, candidates)
}

func loadConsumerRepositoryEnrichment(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	serviceRepoID string,
	serviceName string,
	hostnames []string,
) ([]map[string]any, error) {
	return loadConsumerRepositoryEnrichmentWithLimit(
		ctx,
		graph,
		content,
		serviceRepoID,
		serviceName,
		hostnames,
		defaultIndirectEvidenceSearchLimit,
	)
}

func loadConsumerRepositoryEnrichmentWithLimit(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	serviceRepoID string,
	serviceName string,
	hostnames []string,
	limit int,
) ([]map[string]any, error) {
	candidates, err := queryProvisioningRepositoryCandidates(ctx, graph, serviceRepoID, limit)
	if err != nil {
		return nil, err
	}
	return loadConsumerRepositoryEnrichmentFromCandidates(ctx, graph, content, serviceRepoID, serviceName, hostnames, limit, candidates)
}

func backfillConsumerRepositoryDisplayNames(
	ctx context.Context,
	graph GraphQuery,
	consumersByRepo map[string]map[string]any,
) error {
	if graph == nil || len(consumersByRepo) == 0 {
		return nil
	}

	repoIDs := make([]string, 0, len(consumersByRepo))
	for repoID, entry := range consumersByRepo {
		if repoID == "" {
			continue
		}
		repository := StringVal(entry, "repository")
		repoName := StringVal(entry, "repo_name")
		if repoName == "" || repository == "" || repository == repoID {
			repoIDs = append(repoIDs, repoID)
		}
	}

	namesByID, err := queryRepositoryNamesByID(ctx, graph, repoIDs)
	if err != nil {
		return err
	}
	for repoID, repoName := range namesByID {
		entry := consumersByRepo[repoID]
		if entry == nil || repoName == "" {
			continue
		}
		entry["repo_name"] = repoName
		if repository := StringVal(entry, "repository"); repository == "" || repository == repoID {
			entry["repository"] = repoName
		}
	}
	return nil
}

func queryProvisioningRepositoryCandidates(
	ctx context.Context,
	graph GraphQuery,
	serviceRepoID string,
	limit int,
) ([]provisioningRepositoryCandidate, error) {
	if graph == nil || strings.TrimSpace(serviceRepoID) == "" {
		return nil, nil
	}

	query := `
		MATCH (target:Repository {id: $repo_id})<-[rel:PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN|READS_CONFIG_FROM]-(repo:Repository)
		RETURN repo.id AS repo_id,
		       repo.name AS repo_name,
		       type(rel) AS relationship_type,
		       coalesce(rel.reason, rel.evidence_type, '') AS relationship_reason
		ORDER BY repo.name, relationship_type
	`
	params := map[string]any{"repo_id": serviceRepoID}
	if limit > 0 {
		query += " LIMIT $limit"
		params["limit"] = limit
	}
	rows, err := graph.Run(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("query provisioning repository candidates: %w", err)
	}

	grouped := make(map[string]*provisioningRepositoryCandidate, len(rows))
	for _, row := range rows {
		repoID := strings.TrimSpace(StringVal(row, "repo_id"))
		repoName := strings.TrimSpace(StringVal(row, "repo_name"))
		if repoID == "" || repoName == "" {
			continue
		}
		candidate, ok := grouped[repoID]
		if !ok {
			candidate = &provisioningRepositoryCandidate{
				RepoID:              repoID,
				RepoName:            repoName,
				RelationshipTypes:   []string{},
				RelationshipReasons: []string{},
			}
			grouped[repoID] = candidate
		}
		appendUniqueString(&candidate.RelationshipTypes, StringVal(row, "relationship_type"))
		appendUniqueString(&candidate.RelationshipReasons, StringVal(row, "relationship_reason"))
	}

	candidates := make([]provisioningRepositoryCandidate, 0, len(grouped))
	for _, candidate := range grouped {
		sort.Strings(candidate.RelationshipTypes)
		sort.Strings(candidate.RelationshipReasons)
		candidates = append(candidates, *candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].RepoName < candidates[j].RepoName
	})
	return candidates, nil
}

func collectProvisioningChainEvidence(entities []EntityContent) traceEvidenceAccumulator {
	evidence := newTraceEvidenceAccumulator()
	for _, entity := range entities {
		switch entity.EntityType {
		case "TerraformModule", "TerragruntConfig", "TerragruntDependency":
		default:
			continue
		}
		evidence.samplePaths[entity.RelativePath] = struct{}{}
		relationships, handled, err := buildOutgoingTerraformRelationships(entity)
		if err != nil || !handled {
			continue
		}
		for _, relationship := range relationships {
			if reason := strings.TrimSpace(StringVal(relationship, "reason")); reason != "" {
				evidence.evidenceKinds[reason] = struct{}{}
			}
			targetName := strings.TrimSpace(StringVal(relationship, "target_name"))
			switch StringVal(relationship, "type") {
			case "USES_MODULE":
				if targetName != "" {
					evidence.modules[targetName] = struct{}{}
				}
			case "DISCOVERS_CONFIG_IN":
				if targetName != "" {
					evidence.configPaths[targetName] = struct{}{}
				}
			case "READS_CONFIG_FROM":
				if targetName != "" {
					evidence.configPaths[targetName] = struct{}{}
				}
			}
		}
	}
	return evidence
}

func boundedTraceEnrichmentLimit(maxDepth int) int {
	if maxDepth <= 0 {
		return defaultIndirectEvidenceSearchLimit
	}
	limit := maxDepth * 10
	if limit > maxIndirectEvidenceSearchLimit {
		return maxIndirectEvidenceSearchLimit
	}
	return limit
}

func normalizedIndirectEvidenceHostnames(hostnames []string) []string {
	if len(hostnames) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(hostnames))
	for _, hostname := range hostnames {
		hostname = strings.TrimSpace(hostname)
		if hostname == "" {
			continue
		}
		if _, ok := seen[hostname]; ok {
			continue
		}
		seen[hostname] = struct{}{}
		normalized = append(normalized, hostname)
	}
	sort.Strings(normalized)
	return normalized
}

func buildStorySections(platforms, platformKinds, environments []string) []map[string]any {
	sections := []map[string]any{
		{
			"title":   "deployment",
			"summary": fmt.Sprintf("%d platform target(s) across %d environment(s)", len(platforms), len(environments)),
		},
	}
	if len(platformKinds) > 0 {
		sections = append(sections, map[string]any{
			"title":   "controllers",
			"summary": fmt.Sprintf("Observed controller families: %s", joinOrNone(platformKinds)),
		})
	}
	return sections
}

func buildGitOpsOverview(
	platforms []string,
	platformKinds []string,
	deploymentSources []map[string]any,
	deploymentEvidence map[string]any,
	controllerEntities []map[string]any,
) map[string]any {
	toolFamilies := deploymentTraceGitOpsToolFamilies(platformKinds, deploymentSources, deploymentEvidence, controllerEntities)
	enabled := len(toolFamilies) > 0
	if len(toolFamilies) == 0 {
		toolFamilies = platformKinds
	}
	return map[string]any{
		"enabled":          enabled,
		"tool_families":    toolFamilies,
		"observed_targets": platforms,
	}
}

func buildRuntimeOverview(environments []string) map[string]any {
	return map[string]any{
		"environment_count": len(environments),
		"environments":      environments,
	}
}

func buildDeploymentFacts(
	instances []map[string]any,
	deploymentSources []map[string]any,
) []map[string]any {
	facts := make([]map[string]any, 0, len(instances)*2+len(deploymentSources))
	for _, instance := range instances {
		for _, platform := range platformTargets(instance) {
			platformName := StringVal(platform, "platform_name")
			if platformName == "" {
				continue
			}
			fact := map[string]any{
				"type":   "RUNS_ON_PLATFORM",
				"target": platformName,
				"confidence": firstPositiveFloat(
					floatVal(platform, "platform_confidence"),
					floatVal(instance, "materialization_confidence"),
				),
			}
			if kind := StringVal(platform, "platform_kind"); kind != "" {
				fact["kind"] = kind
			}
			if reason := StringVal(platform, "platform_reason"); reason != "" {
				fact["reason"] = reason
			}
			facts = append(facts, fact)
		}
	}
	for _, environment := range distinctSortedInstanceField(instances, "environment") {
		facts = append(facts, map[string]any{
			"type":       "MATERIALIZED_IN_ENVIRONMENT",
			"target":     environment,
			"confidence": averageInstanceConfidenceForEnvironment(instances, environment),
		})
	}
	for _, source := range deploymentSources {
		facts = append(facts, map[string]any{
			"type":       "DEPLOYS_FROM",
			"target":     safeStr(source, "repo_name"),
			"target_id":  safeStr(source, "repo_id"),
			"confidence": floatVal(source, "confidence"),
			"reason":     safeStr(source, "reason"),
		})
	}
	return facts
}

func averageInstanceConfidenceForEnvironment(instances []map[string]any, environment string) float64 {
	total := 0.0
	count := 0
	for _, instance := range instances {
		if StringVal(instance, "environment") != environment {
			continue
		}
		if confidence := firstPositiveFloat(
			floatVal(instance, "materialization_confidence"),
			floatVal(instance, "platform_confidence"),
		); confidence > 0 {
			total += confidence
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func firstPositiveFloat(candidates ...float64) float64 {
	for _, candidate := range candidates {
		if candidate > 0 {
			return candidate
		}
	}
	return 0
}

func buildControllerDrivenPaths(instances []map[string]any) []map[string]any {
	seen := make(map[string]struct{}, len(instances))
	paths := make([]map[string]any, 0, len(instances))
	for _, instance := range instances {
		for _, platform := range platformTargets(instance) {
			platformName := StringVal(platform, "platform_name")
			platformKind := StringVal(platform, "platform_kind")
			if platformName == "" && platformKind == "" {
				continue
			}
			key := platformName + "|" + platformKind
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			path := map[string]any{}
			if platformKind != "" {
				path["controller_kind"] = platformKind
			}
			if platformName != "" {
				path["observed_target"] = platformName
			}
			paths = append(paths, path)
		}
	}
	sortDeploymentTraceMaps(paths)
	return paths
}

// deploymentTraceGitOpsToolFamilies returns GitOps tool families backed by
// controller entities, platform kinds, or relationship evidence.
func deploymentTraceGitOpsToolFamilies(
	platformKinds []string,
	deploymentSources []map[string]any,
	deploymentEvidence map[string]any,
	controllerEntities []map[string]any,
) []string {
	families := deploymentTraceEvidenceControllerFamilies(deploymentSources, deploymentEvidence, controllerEntities)
	for _, kind := range platformKinds {
		switch strings.TrimSpace(strings.ToLower(kind)) {
		case "argocd", "argocd_application", "argocd_applicationset":
			families = append(families, "argocd")
		case "flux", "flux_kustomization", "flux_helmrelease":
			families = append(families, "flux")
		}
	}
	return uniqueSortedStrings(families)
}

// deploymentTraceEvidenceControllerFamilies lifts controller families out of
// provenance evidence so read surfaces do not lose GitOps truth when runtime
// platform kinds are generic values like kubernetes or ecs.
func deploymentTraceEvidenceControllerFamilies(
	deploymentSources []map[string]any,
	deploymentEvidence map[string]any,
	controllerEntities []map[string]any,
) []string {
	families := make([]string, 0, len(controllerEntities)+len(deploymentSources))
	for _, entity := range controllerEntities {
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(entity, "controller_kind")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(entity, "entity_type")))
	}
	for _, source := range deploymentSources {
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(source, "reason")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(source, "evidence_type")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(source, "evidence_kind")))
	}
	for _, family := range stringSliceMapValue(deploymentEvidence, "tool_families") {
		families = append(families, deploymentTraceControllerFamilyFromText(family))
	}
	for _, artifact := range mapSliceValue(deploymentEvidence, "artifacts") {
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(artifact, "family")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(artifact, "tool_family")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(artifact, "evidence_type")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(artifact, "evidence_kind")))
		for _, kind := range StringSliceVal(artifact, "evidence_kinds") {
			families = append(families, deploymentTraceControllerFamilyFromText(kind))
		}
	}
	for _, path := range mapSliceValue(deploymentEvidence, "delivery_paths") {
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(path, "family")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(path, "tool_family")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(path, "kind")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(path, "evidence_type")))
	}
	return uniqueSortedStrings(families)
}

func deploymentTraceControllerFamilyFromText(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch {
	case normalized == "":
		return ""
	case strings.Contains(normalized, "argocd"):
		return "argocd"
	case strings.Contains(normalized, "flux"):
		return "flux"
	default:
		return ""
	}
}
