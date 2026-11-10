// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// LoadConsumerRepositoryEnrichmentFromCandidates merges graph-derived
// provisioning candidates with content-evidence consumer matches into the
// consumer_repositories field. Pinned by the service enrichment production
// path (service/query_enrichment.go), the staying
// deployment_trace_overflow_regression_test.go and
// deployment_trace_truncation_disclosure_test.go via the root forwarder,
// and the moving service evidence-bound and determinism tests.
//
// # What belongs on the enumeration below
//
// A step is a member when it is a cardinality bound -- a numeric cap on how
// many items survive, applied either as a backend LIMIT or as a slice trim
// against a constant or the caller's limit -- sitting on a read that feeds this
// set, such that a consumer repository past the cap never enters the set. "How
// many", not "which ones", and "this set", not some other one.
//
// That sentence is the actual fix for the defect this comment kept having.
// Rounds 2, 7, 8 and 9 each declared the enumeration closed and each was wrong,
// and the cause was never a bound that hid especially well: the list never said
// what made something a member, so every round redrew the line and found one
// more thing on the far side of it. Round 8 admitted the hostname affinity
// narrowing as "2b" even though it is a relevance predicate rather than a cap,
// which quietly widened the criterion to "any narrowing that loses a reachable
// consumer" -- and under that reading at least three more steps qualified and
// were absent. They are named below the numbered list rather than folded in,
// because the criterion above puts them outside it.
//
// The returned truncated bool is the OR of the six numbered bounds plus the one
// non-bound narrowing named under "Not a bound, folded in anyway" --
// ConsumerEnrichmentTruncation.Any on the WithSources variant below, which is
// the same OR with the cause attached.
//
// Source 0 is the shape to look for when extending the numbered list: it bounds
// *files*, and the hostnames are derived from the files, so reading only the
// calls that return repositories misses it. Walk every read that feeds the
// three arrays, including reads whose output is transformed on the way in.
//
//  0. evidenceFilesTruncated -- loadServiceQueryEvidence read the service
//     repository's file list at serviceEvidenceFileLimit (5000, a real SQL
//     LIMIT in ContentReader.ListRepoFiles) and the page came back full. Every
//     hostname in `hostnames` is extracted from those files, so a hostname
//     that lives only in a file past the cut is never searched for and a
//     consumer repository reachable only through it never enters the merged
//     set. The furthest upstream of the six; numbered 0 rather than
//     renumbering 1-5, which are referenced by number elsewhere in the
//     package. Undisclosed until #5720 round 9, where it was carried as "a
//     generic shared upstream evidence bound rather than a consumer-set
//     bound" -- true of what it bounds, false of what it drops.
//  1. candidatesTruncated -- the caller's upstream
//     QueryProvisioningRepositoryCandidates read already dropped rows at its
//     own LIMIT. Rows, not repositories: that read returns one row per
//     (repo_id, relationship_type, relationship_reason) tuple and groups them
//     by repo_id only after trimming, so this source can be true while every
//     repository the graph held is still present and only one entry's
//     relationship metadata was clipped. #5720 round 10 P1-1; see
//     TestQueryProvisioningRepositoryCandidatesTruncatesRowsNotRepositories.
//  2. hostnamesTruncated -- IndirectEvidenceHostnameLimit (4) dropped
//     hostnames before any search ran, so consumers reachable only through a
//     dropped hostname are never searched for. Upstream of 4 and 5.
//  3. the trimmedHostnames[:limit] cut below, for the case where the surviving
//     hostname set still exceeds the caller's limit. Upstream of 4 and 5.
//     Unreachable from production as the code stands: source 2 has already
//     capped the list at IndirectEvidenceHostnameLimit (4), and every
//     production limit comes from querycontract.BoundedTraceEnrichmentLimit,
//     whose smallest result is 10. It fires only for limit in {1,2,3}, which
//     today only the test-only loadConsumerRepositoryEnrichment* wrappers can
//     pass. Kept and tested rather than deleted, because it is the bound that
//     would start firing if either constant moved.
//  4. searchTruncated -- at least one per-search content read in
//     searchConsumerEvidenceAnyRepo came back full at its own row cap.
//  5. this function's own final consumers[:limit] cap, which can trim the
//     merged list even when every source above is false: content-evidence
//     search can add consumer repositories the graph candidates never named,
//     so the merged set can exceed limit purely from that side.
//
// # Not a bound, folded in anyway
//
// The hostname affinity narrowing in BoundedIndirectEvidenceHostnamesForService
// discards every hostname carrying no distinctive token from the service's own
// name, so a service answering on a legacy or vanity domain loses that domain
// and every consumer reachable only through it. That is the same consequence as
// source 2 from the same function, which is why round 8 numbered it "2b" -- but
// it decides which hostnames are relevant rather than capping how many survive,
// so it fails the criterion above and round 10 took the number back. It still
// sets hostnamesTruncated and therefore this bool: it really does drop
// reachable consumers, and a false claim of completeness is the worse failure
// for an evidence-backed answer. Keeping it named but unnumbered is what lets
// the numbered list close.
//
// # Narrowings that are outside, and why
//
// Each of these narrows something on the way to this answer and each is outside
// the enumeration because of the criterion above, not because nobody noticed
// it. Naming them is the point. The affinity narrowing is exactly one of these
// that round 8 numbered instead of naming, and that is the step that let the
// criterion drift; the first three below are the ones a reader who inherits the
// drifted reading finds next.
//
//   - isServiceEvidenceCandidate (service/query_evidence.go) -- a 10-extension
//     whitelist plus a 12-keyword path filter, applied to every listed file
//     before a single hostname is extracted. A hostname living only in
//     terraform/main.tf, Dockerfile, nginx/nginx.conf or .env.production is
//     never extracted, so a consumer reachable only through it never enters
//     this set and the flag stays false. Same shape as the affinity narrowing,
//     one layer earlier: a relevance predicate, not a cap.
//   - exactObservedHostnameCandidates (service/hostname_evidence.go)
//     -- keeps only Classification == "exact_hostname". Ambiguous candidates
//     reach the caller as entrypoint_candidates but are never searched for.
//     Also a relevance predicate.
//   - lineLikelyContainsHostname and the falsePositiveTLDs /
//     falsePositiveSegments / falsePositiveConfigKeyTerminals tables in
//     internal/contentrefs/hostnames.go -- the extractor's own precision
//     filters, the furthest upstream relevance predicates of the three.
//   - RepositorySemanticEntityLimit (5000) -- a real cardinality bound, on a
//     different set. It clips evidence_kinds / sample_paths / modules /
//     config_paths INSIDE a chain entry that
//     LoadProvisioningSourceChainsFromCandidates appends unconditionally, so no
//     repository is ever dropped from any of the three arrays. Pre-existing and
//     undisclosed; the clipped payload has no channel of its own, which is a
//     gap in what a caller can tell about an entry rather than about the list.
//   - ContentReader.ListFrameworkRoutes -- frameworkRouteEvidenceLimit (50), a
//     SQL LIMIT with no truncation channel, also on a different set. Its rows
//     land only on service.QueryEvidence.FrameworkRoutes, which
//     service/query_enrichment_rows.go reads to build api_surface
//     endpoints; no hostname, candidate, or consumer search touches it.
//     Recorded so the next round does not re-derive that it cannot reach
//     these arrays.
//   - the repoID == "" || repoName == "" skip in
//     QueryProvisioningRepositoryCandidates -- not a narrowing of reachable
//     consumers at all. Such a row carries nothing a caller could render or
//     address, so dropping it loses nothing.
//
// ConsumerEnrichmentTruncation names which cardinality bound fired while
// merging consumer repositories, so callers can attribute a truncated
// consumer set to the bound the caller can act on (#10477). The fields mirror
// the numbered sources on LoadConsumerRepositoryEnrichmentFromCandidates:
// Candidates is source 1 (the upstream candidate read at the downstream
// bound), EvidenceFiles is source 0 (the service file list at
// serviceEvidenceFileLimit), Hostnames covers sources 2 and 3 plus the
// affinity narrowing (hostname inputs narrowed before any search ran),
// Searches is source 4 (a per-search content read at its row cap), and
// MergeCap is source 5 (the final merged-list trim).
type ConsumerEnrichmentTruncation struct {
	Candidates    bool
	EvidenceFiles bool
	Hostnames     bool
	Searches      bool
	MergeCap      bool
}

// Any reports whether any source fired.
func (s ConsumerEnrichmentTruncation) Any() bool {
	return s.Candidates || s.EvidenceFiles || s.Hostnames || s.Searches || s.MergeCap
}

// Downstream reports whether any source governed by the caller's
// downstream_read_limit fired -- every source except the evidence-file bound,
// which answers to serviceEvidenceFileLimit instead.
func (s ConsumerEnrichmentTruncation) Downstream() bool {
	return s.Candidates || s.Hostnames || s.Searches || s.MergeCap
}

func LoadConsumerRepositoryEnrichmentFromCandidates(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	serviceRepoID string,
	serviceName string,
	hostnames []string,
	limit int,
	candidates []ProvisioningRepositoryCandidate,
	candidatesTruncated bool,
	evidenceFilesTruncated bool,
) (consumers []map[string]any, truncated bool, err error) {
	consumers, sources, err := LoadConsumerRepositoryEnrichmentWithSources(ctx, graph, content, serviceRepoID, serviceName, hostnames, limit, candidates, candidatesTruncated, evidenceFilesTruncated)
	if err != nil {
		return nil, false, err
	}
	return consumers, sources.Any(), nil
}

// LoadConsumerRepositoryEnrichmentWithSources is LoadConsumerRepositoryEnrichmentFromCandidates
// with the truncation cause attached: same rows, same bound, plus the
// per-bound sources (#10477). Prefer it when the caller must name which bound
// fired; the bool wrapper above stays for callers that only need the signal.
func LoadConsumerRepositoryEnrichmentWithSources(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	serviceRepoID string,
	serviceName string,
	hostnames []string,
	limit int,
	candidates []ProvisioningRepositoryCandidate,
	candidatesTruncated bool,
	evidenceFilesTruncated bool,
) (consumers []map[string]any, sources ConsumerEnrichmentTruncation, err error) {
	sources.Candidates = candidatesTruncated
	sources.EvidenceFiles = evidenceFilesTruncated
	trimmedHostnames := normalizedIndirectEvidenceHostnames(hostnames)
	if limit > 0 {
		var hostnamesTruncated bool
		trimmedHostnames, hostnamesTruncated = BoundedIndirectEvidenceHostnamesForService(trimmedHostnames, serviceName)
		if hostnamesTruncated {
			sources.Hostnames = true
		}
		// Source 3. BoundedIndirectEvidenceHostnamesForService above has
		// already capped trimmedHostnames at IndirectEvidenceHostnameLimit (4)
		// and querycontract.BoundedTraceEnrichmentLimit never returns below
		// 10, so no production caller can reach this branch -- only limit in
		// {1,2,3} does, which today means the test-only wrappers. The #5720
		// round-9 P2-1 mutant that deleted this `truncated = true` survived
		// the whole suite for exactly that reason; it is exercised now rather
		// than removed, because it is what would fire first if either
		// constant moved.
		if len(trimmedHostnames) > limit {
			trimmedHostnames = trimmedHostnames[:limit]
			sources.Hostnames = true
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
			return nil, ConsumerEnrichmentTruncation{}, err
		}
		if searchTruncated {
			sources.Searches = true
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
		return nil, ConsumerEnrichmentTruncation{}, err
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
		if leftRepository, rightRepository := querycontract.StringVal(consumers[i], "repository"), querycontract.StringVal(consumers[j], "repository"); leftRepository != rightRepository {
			return leftRepository < rightRepository
		}
		return querycontract.StringVal(consumers[i], "repo_id") < querycontract.StringVal(consumers[j], "repo_id")
	})
	if limit > 0 && len(consumers) > limit {
		consumers = consumers[:limit]
		// Source 5 in the enumeration on this function's doc comment: this cap
		// can trim the merged list even when every upstream source is false,
		// because content-evidence search can add consumer repositories the
		// graph candidates never named. Set rather than overwritten so a
		// signal already recorded above is never lost.
		sources.MergeCap = true
	}
	return consumers, sources, nil
}
