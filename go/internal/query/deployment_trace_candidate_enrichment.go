// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

	// Two distinct repositories can share a display name (#5720, same class
	// as #5644), so a comparator that leaves that tie unresolved lets
	// repeated calls over unchanged data return those tied entries in a
	// different relative order. repo_id is unique per candidate, so it is
	// the final tiebreaker that makes this a total order.
	sort.Slice(chains, func(i, j int) bool {
		if left, right := StringVal(chains[i], "repository"), StringVal(chains[j], "repository"); left != right {
			return left < right
		}
		return StringVal(chains[i], "repo_id") < StringVal(chains[j], "repo_id")
	})
	return chains, nil
}

// loadConsumerRepositoryEnrichmentFromCandidates merges graph-derived
// provisioning candidates with content-evidence consumer matches into the
// consumer_repositories field. #5720 round-2 P1-1: truncation can come from
// either of two independent, unrelated sources, so both must be disclosed
// through the single returned bool -- candidatesTruncated (the caller's
// upstream queryProvisioningRepositoryCandidates read already dropped rows)
// and this function's own final consumers[:limit] cap below, which can trim
// the list even when candidatesTruncated is false: content-evidence search
// can add consumer repositories the graph candidates never named, so the
// merged set can exceed limit purely from that side.
func loadConsumerRepositoryEnrichmentFromCandidates(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	serviceRepoID string,
	serviceName string,
	hostnames []string,
	limit int,
	candidates []provisioningRepositoryCandidate,
	candidatesTruncated bool,
) (consumers []map[string]any, truncated bool, err error) {
	truncated = candidatesTruncated
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
		var contentEvidence map[string]traceEvidenceAccumulator
		contentEvidence, err = searchConsumerEvidenceAnyRepo(ctx, content, serviceRepoID, serviceName, trimmedHostnames, limit)
		if err != nil {
			return nil, false, err
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
	if err = backfillConsumerRepositoryDisplayNames(ctx, graph, consumersByRepo); err != nil {
		return nil, false, err
	}

	consumers = make([]map[string]any, 0, len(consumersByRepo))
	for _, entry := range consumersByRepo {
		consumers = append(consumers, entry)
	}

	// consumersByRepo is a Go map, so the pre-sort order of consumers is
	// randomized per process. sort.Slice is not stable, so a comparator that
	// leaves ties unresolved (equal score and equal display name, e.g. two
	// repositories sharing a display name) let repeated service-story calls
	// over unchanged retained data return those tied entries in a different
	// relative order, which also shifted which entries survived truncation
	// (#5644). repo_id is unique per map key, so adding it as the final
	// tiebreaker makes this a total order regardless of map iteration order.
	sort.Slice(consumers, func(i, j int) bool {
		leftScore := consumerRepositorySortScore(consumers[i])
		rightScore := consumerRepositorySortScore(consumers[j])
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if leftRepository, rightRepository := StringVal(consumers[i], "repository"), StringVal(consumers[j], "repository"); leftRepository != rightRepository {
			return leftRepository < rightRepository
		}
		return StringVal(consumers[i], "repo_id") < StringVal(consumers[j], "repo_id")
	})
	if limit > 0 && len(consumers) > limit {
		consumers = consumers[:limit]
		// #5720 round-2 P1-1: this cap is a second, independent truncation
		// source from candidatesTruncated above -- content-evidence search
		// can add consumer repositories the graph candidates never named, so
		// the merged set can exceed limit even when the graph read itself
		// was not truncated. OR rather than overwrite so a candidatesTruncated
		// signal already set to true is never lost.
		truncated = true
	}
	return consumers, truncated, nil
}
