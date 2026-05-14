package query

import (
	"context"
	"fmt"
	"sort"
)

func loadProvisioningSourceChainsFromCandidates(
	ctx context.Context,
	content ContentStore,
	candidates []provisioningRepositoryCandidate,
) ([]map[string]any, error) {
	if len(candidates) == 0 || content == nil {
		return nil, nil
	}

	chains := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		entities, err := content.ListRepoEntities(ctx, candidate.RepoID, repositorySemanticEntityLimit)
		if err != nil {
			return nil, fmt.Errorf("list provisioning entities for %q: %w", candidate.RepoID, err)
		}
		evidence := collectProvisioningChainEvidence(entities)
		entry := map[string]any{
			"repository":         candidate.RepoName,
			"repo_id":            candidate.RepoID,
			"relationship_types": candidate.RelationshipTypes,
		}
		if len(candidate.RelationshipReasons) > 0 {
			entry["relationship_reasons"] = candidate.RelationshipReasons
			for _, reason := range candidate.RelationshipReasons {
				evidence.evidenceKinds[reason] = struct{}{}
			}
		}
		if values := sortedAccumulatorValues(evidence.evidenceKinds); len(values) > 0 {
			entry["evidence_kinds"] = values
		}
		if values := sortedAccumulatorValues(evidence.samplePaths); len(values) > 0 {
			entry["sample_paths"] = values
		}
		if values := sortedAccumulatorValues(evidence.modules); len(values) > 0 {
			entry["modules"] = values
		}
		if values := sortedAccumulatorValues(evidence.configPaths); len(values) > 0 {
			entry["config_paths"] = values
		}
		chains = append(chains, entry)
	}

	sort.Slice(chains, func(i, j int) bool {
		return StringVal(chains[i], "repository") < StringVal(chains[j], "repository")
	})
	return chains, nil
}

func loadConsumerRepositoryEnrichmentFromCandidates(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	serviceRepoID string,
	serviceName string,
	hostnames []string,
	limit int,
	candidates []provisioningRepositoryCandidate,
) ([]map[string]any, error) {
	trimmedHostnames := normalizedIndirectEvidenceHostnames(hostnames)
	if limit > 0 {
		trimmedHostnames = boundedIndirectEvidenceHostnamesForService(trimmedHostnames, serviceName)
		if len(trimmedHostnames) > limit {
			trimmedHostnames = trimmedHostnames[:limit]
		}
	}

	consumersByRepo := make(map[string]map[string]any, len(candidates))
	for _, candidate := range candidates {
		entry := map[string]any{
			"repository":               candidate.RepoName,
			"repo_id":                  candidate.RepoID,
			"consumer_kinds":           []string{"graph_provisioning_consumer"},
			"graph_relationship_types": candidate.RelationshipTypes,
		}
		if len(candidate.RelationshipReasons) > 0 {
			entry["graph_relationship_reasons"] = candidate.RelationshipReasons
		}
		consumersByRepo[candidate.RepoID] = entry
	}

	if content != nil {
		contentEvidence, err := searchConsumerEvidenceAnyRepo(ctx, content, serviceRepoID, serviceName, trimmedHostnames, limit)
		if err != nil {
			return nil, err
		}
		for repoID, evidence := range contentEvidence {
			entry, ok := consumersByRepo[repoID]
			if !ok {
				entry = map[string]any{
					"repo_id":        repoID,
					"repository":     repoID,
					"consumer_kinds": []string{},
				}
				consumersByRepo[repoID] = entry
			}
			appendConsumerEvidence(entry, evidence)
		}
	}
	if err := backfillConsumerRepositoryDisplayNames(ctx, graph, consumersByRepo); err != nil {
		return nil, err
	}

	consumers := make([]map[string]any, 0, len(consumersByRepo))
	for _, entry := range consumersByRepo {
		consumers = append(consumers, entry)
	}

	sort.Slice(consumers, func(i, j int) bool {
		leftScore := consumerRepositorySortScore(consumers[i])
		rightScore := consumerRepositorySortScore(consumers[j])
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		return StringVal(consumers[i], "repository") < StringVal(consumers[j], "repository")
	})
	if limit > 0 && len(consumers) > limit {
		consumers = consumers[:limit]
	}
	return consumers, nil
}
