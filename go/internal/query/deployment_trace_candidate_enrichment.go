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
// consumer_repositories field.
//
// The single returned truncated bool is the OR of every bound that can drop a
// consumer repository from the merged set. Every round of review so far has
// found the preceding enumeration incomplete: round 2 disclosed two sources and
// asserted there were only two, round 7 found three more (two of them UPSTREAM
// of both disclosed ones -- a repository they drop never reaches the merged
// set, so neither the final cap nor candidatesTruncated can observe it), round
// 8 found 2b, a second drop path inside the same function as source 2, and
// round 9 found source 0. Treat this list as the current state of an
// enumeration that has been wrong four times, not as a closed one.
//
// Source 0 is the shape to look for when extending this: it bounds *files*, and
// the hostnames are derived from the files, so reading only the calls that
// return repositories misses it. Walk every read that feeds the three arrays,
// including reads whose output is transformed on the way in.
//
//  0. evidenceFilesTruncated -- loadServiceQueryEvidence read the service
//     repository's file list at serviceEvidenceFileLimit (5000, a real SQL
//     LIMIT in ContentReader.ListRepoFiles) and the page came back full. Every
//     hostname in `hostnames` is extracted from those files, so a hostname
//     that lives only in a file past the cut is never searched for and a
//     consumer repository reachable only through it never enters the merged
//     set. The furthest upstream of the seven; numbered 0 rather than
//     renumbering 1-5, which are referenced by number elsewhere in the
//     package. Undisclosed until #5720 round 9, where it was carried as "a
//     generic shared upstream evidence bound rather than a consumer-set
//     bound" -- true of what it bounds, false of what it drops.
//  1. candidatesTruncated -- the caller's upstream
//     queryProvisioningRepositoryCandidates read already dropped rows at its
//     own LIMIT.
//  2. hostnamesTruncated -- indirectEvidenceHostnameLimit (4) dropped
//     hostnames before any search ran, so consumers reachable only through a
//     dropped hostname are never searched for. Upstream of 4 and 5.
//     2b. the same bool also now covers the hostname affinity narrowing in
//     boundedIndirectEvidenceHostnamesForService, which discards every hostname
//     carrying no distinctive token from the service's own name. Same upstream
//     position and same consequence as 2: a service answering on a legacy or
//     vanity domain loses that domain, and every consumer reachable only
//     through it. Undisclosed until #5720 round 8.
//  3. the trimmedHostnames[:limit] cut below, for the case where the surviving
//     hostname set still exceeds the caller's limit. Upstream of 4 and 5.
//     Unreachable from production as the code stands: source 2 has already
//     capped the list at indirectEvidenceHostnameLimit (4), and every
//     production limit comes from boundedTraceEnrichmentLimit, whose smallest
//     result is 10. It fires only for limit in {1,2,3}, which today only the
//     test-only loadConsumerRepositoryEnrichment* wrappers can pass. Kept and
//     tested rather than deleted, because it is the bound that would start
//     firing if either constant moved.
//  4. searchTruncated -- at least one per-search content read in
//     searchConsumerEvidenceAnyRepo came back full at its own row cap.
//  5. this function's own final consumers[:limit] cap, which can trim the
//     merged list even when every source above is false: content-evidence
//     search can add consumer repositories the graph candidates never named,
//     so the merged set can exceed limit purely from that side.
//
// Two narrowings are deliberately NOT folded in here:
//
//   - repositorySemanticEntityLimit (5000) clips the per-chain nested entity
//     evidence loadProvisioningSourceChainsFromCandidates reads. It bounds the
//     evidence attached to a chain, not which repositories appear, so it cannot
//     drop a consumer from this set; it is pre-existing and remains undisclosed.
//   - the repoID == "" || repoName == "" skip in
//     queryProvisioningRepositoryCandidates, which drops only rows the graph
//     returned without an identity. Naming it here so the next enumeration does
//     not have to rediscover that it was considered: such a row carries nothing
//     a caller could render or address, so dropping it loses no reachable
//     consumer.
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
	evidenceFilesTruncated bool,
) (consumers []map[string]any, truncated bool, err error) {
	truncated = candidatesTruncated || evidenceFilesTruncated
	trimmedHostnames := normalizedIndirectEvidenceHostnames(hostnames)
	if limit > 0 {
		var hostnamesTruncated bool
		trimmedHostnames, hostnamesTruncated = boundedIndirectEvidenceHostnamesForService(trimmedHostnames, serviceName)
		if hostnamesTruncated {
			truncated = true
		}
		// Source 3. boundedIndirectEvidenceHostnamesForService above has
		// already capped trimmedHostnames at indirectEvidenceHostnameLimit (4)
		// and boundedTraceEnrichmentLimit never returns below 10, so no
		// production caller can reach this branch -- only limit in {1,2,3}
		// does, which today means the test-only wrappers. The #5720 round-9
		// P2-1 mutant that deleted this `truncated = true` survived the whole
		// suite for exactly that reason; it is exercised now rather than
		// removed, because it is what would fire first if either constant
		// moved.
		if len(trimmedHostnames) > limit {
			trimmedHostnames = trimmedHostnames[:limit]
			truncated = true
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
		var (
			contentEvidence map[string]traceEvidenceAccumulator
			searchTruncated bool
		)
		contentEvidence, searchTruncated, err = searchConsumerEvidenceAnyRepo(ctx, content, serviceRepoID, serviceName, trimmedHostnames, limit)
		if err != nil {
			return nil, false, err
		}
		if searchTruncated {
			truncated = true
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
		// Source 5 in the enumeration on this function's doc comment: this cap
		// can trim the merged list even when every upstream source is false,
		// because content-evidence search can add consumer repositories the
		// graph candidates never named. Assigned true rather than overwritten
		// with a fresh value so a signal already set above is never lost.
		truncated = true
	}
	return consumers, truncated, nil
}
