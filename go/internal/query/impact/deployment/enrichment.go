// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file hosts the deployment-trace enrichment entry points behind the
// service-story workload context (Issue #6060, lane B B4). They moved here
// from the query root (deployment_trace_support_helpers.go and
// deployment_trace_candidate_enrichment.go) because the service family in
// internal/query/service consumes them and a handler-family subpackage
// cannot import the query root without an import cycle. The staying root
// wrappers (loadProvisioningSourceChainsWithLimit,
// loadConsumerRepositoryEnrichmentWithLimit) and the staying
// deployment-trace tests keep calling the package-local names through thin
// forwarders in deployment_trace_support_helpers.go. Bodies are unchanged
// modulo package qualifiers and the export renames below.

// QueryProvisioningRepositoryCandidates is the sole production feeder for
// the workload-context dependents, consumer_repositories, and
// provisioning_source_chains fields (service/query_enrichment.go).
// Pinned by the staying deployment_trace_truncation_disclosure_test.go via
// the root forwarder, and by the service enrichment production path. #5720
// round-2 P1-1: the returned truncated bool is required, not cosmetic --
// every one of those three fields reports its length as a "dependent_count"
// / "consumer_repository_count" / "provisioning_source_count" without ever
// disclosing that the read hit its bound, because the default indirect-
// evidence search limit (DefaultIndirectEvidenceSearchLimit, 25) sits below
// the disclosure threshold callers compare against
// (ServiceStoryItemLimit, 50) -- a row count of 25 never looks truncated by
// that comparison alone, even when a service genuinely has 40+ dependent
// repositories. Probing limit+1 rows and reporting whether the backend
// actually returned more than limit makes truncation observable
// independent of any downstream count-vs-limit comparison.
func QueryProvisioningRepositoryCandidates(
	ctx context.Context,
	graph querycontract.GraphQuery,
	serviceRepoID string,
	limit int,
) ([]ProvisioningRepositoryCandidate, bool, error) {
	if graph == nil || strings.TrimSpace(serviceRepoID) == "" {
		return nil, false, nil
	}
	// Self-clamp so the LIMIT clause below is always applied, mirroring the
	// LoadUncorrelatedCloudResourceCandidatesBounded precedent
	// (cloud_resource_candidates.go): a caller-supplied limit of 0 or less
	// (or above the package cap) no longer disables the bound. This makes
	// the row count -- and therefore the deduplicated candidate count -- a
	// real, structurally guaranteed maximum of MaxIndirectEvidenceSearchLimit
	// regardless of caller behavior, which the query-source-coverage
	// keyed_support disposition below cites as max_results.
	if limit <= 0 || limit > querycontract.MaxIndirectEvidenceSearchLimit {
		limit = querycontract.MaxIndirectEvidenceSearchLimit
	}
	// #5720 round-2 P1-1: probe one row past the caller's limit, mirroring
	// the LoadUncorrelatedCloudResourceCandidatesBounded precedent
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
	// closes the repo-level tie (mirrors the
	// LoadUncorrelatedCloudResourceCandidatesBounded precedent this
	// function's self-clamp above already cites:
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

	grouped := make(map[string]*ProvisioningRepositoryCandidate, len(rows))
	for _, row := range rows {
		repoID := strings.TrimSpace(querycontract.StringVal(row, "repo_id"))
		repoName := strings.TrimSpace(querycontract.StringVal(row, "repo_name"))
		if repoID == "" || repoName == "" {
			continue
		}
		candidate, ok := grouped[repoID]
		if !ok {
			candidate = &ProvisioningRepositoryCandidate{
				RepoID:              repoID,
				RepoName:            repoName,
				RelationshipTypes:   []string{},
				RelationshipReasons: []string{},
			}
			grouped[repoID] = candidate
		}
		querycontract.AppendUniqueString(&candidate.RelationshipTypes, querycontract.StringVal(row, "relationship_type"))
		querycontract.AppendUniqueString(&candidate.RelationshipReasons, querycontract.StringVal(row, "relationship_reason"))
	}

	candidates := make([]ProvisioningRepositoryCandidate, 0, len(grouped))
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

// LoadProvisioningSourceChainsFromCandidates loads the provisioning source
// chains for the pre-read candidate slice. Pinned by the service enrichment
// production path (service/query_enrichment.go) and the staying
// loadProvisioningSourceChainsWithLimit wrapper.
func LoadProvisioningSourceChainsFromCandidates(
	ctx context.Context,
	content querycontract.ContentStore,
	candidates []ProvisioningRepositoryCandidate,
) ([]map[string]any, error) {
	if len(candidates) == 0 || content == nil {
		return nil, nil
	}

	chains := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		entities, err := content.ListRepoEntities(ctx, candidate.RepoID, querycontract.RepositorySemanticEntityLimit)
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
		if left, right := querycontract.StringVal(chains[i], "repository"), querycontract.StringVal(chains[j], "repository"); left != right {
			return left < right
		}
		return querycontract.StringVal(chains[i], "repo_id") < querycontract.StringVal(chains[j], "repo_id")
	})
	return chains, nil
}
