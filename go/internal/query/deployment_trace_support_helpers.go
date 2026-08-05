// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	// This wrapper's own return shape carries no truncation signal today and
	// has no production caller (only tests reach it directly); the truncated
	// bool is discarded here rather than plumbed through, since
	// loadProvisioningSourceChainsFromCandidates is 1:1 over the candidate
	// slice with no further capping of its own.
	candidates, _, err := queryProvisioningRepositoryCandidates(ctx, graph, serviceRepoID, limit)
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
	// This wrapper's own return shape carries no truncation signal today and
	// has no production caller (only tests reach it directly); the
	// truncated bool from both stages is discarded here rather than
	// plumbed through.
	candidates, candidatesTruncated, err := queryProvisioningRepositoryCandidates(ctx, graph, serviceRepoID, limit)
	if err != nil {
		return nil, err
	}
	consumers, _, err := loadConsumerRepositoryEnrichmentFromCandidates(ctx, graph, content, serviceRepoID, serviceName, hostnames, limit, candidates, candidatesTruncated)
	return consumers, err
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

// queryProvisioningRepositoryCandidates is the sole production feeder for
// the workload-context dependents, consumer_repositories, and
// provisioning_source_chains fields (service_query_enrichment.go). #5720
// round-2 P1-1: the returned truncated bool is required, not cosmetic --
// every one of those three fields reports its length as a "dependent_count"
// / "consumer_repository_count" / "provisioning_source_count" without ever
// disclosing that the read hit its bound, because the default indirect-
// evidence search limit (defaultIndirectEvidenceSearchLimit, 25) sits below
// the disclosure threshold callers compare against
// (serviceStoryItemLimit, 50) -- a row count of 25 never looks truncated by
// that comparison alone, even when a service genuinely has 40+ dependent
// repositories. Probing limit+1 rows and reporting whether the backend
// actually returned more than limit makes truncation observable
// independent of any downstream count-vs-limit comparison.
func queryProvisioningRepositoryCandidates(
	ctx context.Context,
	graph GraphQuery,
	serviceRepoID string,
	limit int,
) ([]provisioningRepositoryCandidate, bool, error) {
	if graph == nil || strings.TrimSpace(serviceRepoID) == "" {
		return nil, false, nil
	}
	// Self-clamp so the LIMIT clause below is always applied, mirroring the
	// loadUncorrelatedCloudResourceCandidatesBounded precedent
	// (cloud_resource_candidates.go): a caller-supplied limit of 0 or less
	// (or above the package cap) no longer disables the bound. This makes
	// the row count -- and therefore the deduplicated candidate count -- a
	// real, structurally guaranteed maximum of maxIndirectEvidenceSearchLimit
	// regardless of caller behavior, which the query-source-coverage
	// keyed_support disposition below cites as max_results.
	if limit <= 0 || limit > maxIndirectEvidenceSearchLimit {
		limit = maxIndirectEvidenceSearchLimit
	}
	// #5720 round-2 P1-1: probe one row past the caller's limit, mirroring
	// the loadUncorrelatedCloudResourceCandidatesBounded precedent
	// (cloud_resource_candidates.go: "Over-fetch by one row to detect
	// truncation without a second count query"). fetchLimit only changes
	// how many rows the backend returns for this one read; the disclosed
	// limit and the final candidate count are still bounded at `limit`.
	fetchLimit := limit + 1

	// #5720 round-2 P1-1: repo.id is absent from ORDER BY below, so two
	// distinct repositories sharing both repo.name and type(rel) tie at the
	// backend -- which rows survive LIMIT is then backend choice, not a
	// function of the data. The Go sort.Slice below (repo.name then
	// candidate.RepoID) makes the RETURNED slice a total order but cannot
	// recover a candidate the backend already dropped at LIMIT. repo.id
	// closes the repo-level tie (mirrors the loadUncorrelatedCloudResourceCandidatesBounded
	// precedent this function's self-clamp above already cites:
	// cloud_resource_candidates.go's `ORDER BY n.name, n.id`, and the
	// package-wide convention in catalog.go, code_complexity_queries.go,
	// code_quality.go, code_relationship_story_class.go, and
	// code_relationship_story_graph.go). relationship_reason additionally
	// closes the same-repo/same-type/different-reason row tie -- without it,
	// which of two differently-reasoned rows for the same (repo, type) pair
	// survives LIMIT is also backend choice, and the dropped row's reason is
	// silently missing from RelationshipReasons.
	query := `
		MATCH (target:Repository {id: $repo_id})<-[rel:PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN|READS_CONFIG_FROM]-(repo:Repository)
		RETURN repo.id AS repo_id,
		       repo.name AS repo_name,
		       type(rel) AS relationship_type,
		       coalesce(rel.reason, rel.evidence_type, '') AS relationship_reason
		ORDER BY repo.name, repo.id, relationship_type, relationship_reason
		LIMIT $limit
	`
	params := map[string]any{"repo_id": serviceRepoID, "limit": fetchLimit}
	rows, err := graph.Run(ctx, query, params)
	if err != nil {
		return nil, false, fmt.Errorf("query provisioning repository candidates: %w", err)
	}
	// #5720 round-2 P1-1: the backend returned more than `limit` rows, so
	// trim back to the disclosed bound before grouping -- the returned
	// candidate count must never exceed `limit`, matching every caller's
	// existing behavior -- and report truncated so callers can distinguish
	// "there were exactly limit rows" from "there were more and the rest
	// were dropped."
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
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
	// candidates is built by ranging the grouped Go map above, so its
	// pre-sort order is randomized per call. A comparator that leaves a
	// display-name tie unresolved (two distinct repositories can share a
	// RepoName) lets repeated calls over unchanged data return those tied
	// entries in a different relative order (#5720, same class as #5644).
	// RepoID is unique per map key, so adding it as the final tiebreaker
	// makes this a total order regardless of map iteration order.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].RepoName != candidates[j].RepoName {
			return candidates[i].RepoName < candidates[j].RepoName
		}
		return candidates[i].RepoID < candidates[j].RepoID
	})
	return candidates, truncated, nil
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

// boundedTraceEnrichmentLimit converts a caller-supplied max_depth into a
// bounded indirect-evidence search limit. #5720 P2-3: an unvalidated
// max_depth large enough that maxDepth*10 overflows int64 (e.g.
// 922337203685477581) used to return a negative value here, which every
// downstream caller of the resulting limit treats identically to "no bound"
// (limit > 0 gates). Clamping both directions here -- not only the
// already-present upper clamp -- makes the return value a real, structurally
// guaranteed member of (0, maxIndirectEvidenceSearchLimit] regardless of
// caller input, so no downstream caller can observe an unbounded limit.
func boundedTraceEnrichmentLimit(maxDepth int) int {
	if maxDepth <= 0 {
		return defaultIndirectEvidenceSearchLimit
	}
	limit := maxDepth * 10
	if limit <= 0 || limit > maxIndirectEvidenceSearchLimit {
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
