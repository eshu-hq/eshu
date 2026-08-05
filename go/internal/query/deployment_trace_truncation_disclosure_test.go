// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"
)

// TestQueryProvisioningRepositoryCandidatesDisclosesTruncation is the #5720
// round-2 P1-1 regression. Before this fix, queryProvisioningRepositoryCandidates
// requested exactly `limit` rows, so "the backend had exactly limit matching
// rows" and "the backend had more than limit and the rest were silently
// dropped" produced an identical return value -- every caller (dependents,
// consumer_repositories, provisioning_source_chains) reported truncated:
// false in both cases. This drives a seeded fake graph reader through the
// real function and proves it now tells the two apart: it probes limit+1
// rows, trims back to limit before grouping, and returns a truncated bool
// that agrees with the returned (bounded) candidate count.
func TestQueryProvisioningRepositoryCandidatesDisclosesTruncation(t *testing.T) {
	t.Parallel()

	const limit = 3

	t.Run("more rows than limit sets truncated and caps the count", func(t *testing.T) {
		t.Parallel()
		rows := make([]map[string]any, 0, limit+1)
		for i := 0; i < limit+1; i++ {
			rows = append(rows, map[string]any{
				"repo_id":             fmt.Sprintf("repository:candidate-%02d", i),
				"repo_name":           fmt.Sprintf("candidate-%02d", i),
				"relationship_type":   "USES_MODULE",
				"relationship_reason": "",
			})
		}
		reader := fakeGraphReader{run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
			if got, want := params["limit"], limit+1; got != want {
				t.Fatalf("params[limit] = %#v, want %#v (must probe one row past the caller limit to detect truncation)", got, want)
			}
			return rows, nil
		}}

		candidates, truncated, err := queryProvisioningRepositoryCandidates(context.Background(), reader, "repository:orders", limit)
		if err != nil {
			t.Fatalf("queryProvisioningRepositoryCandidates() error = %v, want nil", err)
		}
		if !truncated {
			t.Fatalf("truncated = false, want true (backend returned limit+1 = %d rows for a limit of %d)", len(rows), limit)
		}
		if got, want := len(candidates), limit; got != want {
			t.Fatalf("len(candidates) = %d, want %d (the disclosed truncated flag and the returned length must agree)", got, want)
		}
	})

	t.Run("exactly limit rows leaves truncated false", func(t *testing.T) {
		t.Parallel()
		rows := make([]map[string]any, 0, limit)
		for i := 0; i < limit; i++ {
			rows = append(rows, map[string]any{
				"repo_id":             fmt.Sprintf("repository:candidate-%02d", i),
				"repo_name":           fmt.Sprintf("candidate-%02d", i),
				"relationship_type":   "USES_MODULE",
				"relationship_reason": "",
			})
		}
		reader := fakeGraphReader{run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
			if got, want := params["limit"], limit+1; got != want {
				t.Fatalf("params[limit] = %#v, want %#v", got, want)
			}
			return rows, nil
		}}

		candidates, truncated, err := queryProvisioningRepositoryCandidates(context.Background(), reader, "repository:orders", limit)
		if err != nil {
			t.Fatalf("queryProvisioningRepositoryCandidates() error = %v, want nil", err)
		}
		if truncated {
			t.Fatalf("truncated = true, want false (backend returned exactly limit = %d rows, nothing was dropped)", limit)
		}
		if got, want := len(candidates), limit; got != want {
			t.Fatalf("len(candidates) = %d, want %d", got, want)
		}
	})
}

// TestBuildServiceDownstreamConsumersDisclosesUpstreamTruncation proves
// buildServiceDownstreamConsumers (service_story_dossier.go) reports
// truncated: true when the workload context carries a truncated signal from
// the provisioning-candidate read, even though neither returned list is long
// enough to exceed serviceStoryItemLimit (50) on its own -- the #5720
// round-2 P1-1 gap: the default indirect-evidence search limit (25) sits
// below serviceStoryItemLimit, so a query-level truncation could never
// surface through the count-vs-limit comparison alone.
func TestBuildServiceDownstreamConsumersDisclosesUpstreamTruncation(t *testing.T) {
	t.Parallel()

	baseContext := func() map[string]any {
		return map[string]any{
			"dependents":            []map[string]any{{"repo_id": "repository:dependent-1"}},
			"consumer_repositories": []map[string]any{{"repo_id": "repository:consumer-1"}},
		}
	}

	t.Run("no truncation signal reports untruncated", func(t *testing.T) {
		t.Parallel()
		got := buildServiceDownstreamConsumers(baseContext())
		if got["truncated"] != false {
			t.Fatalf("truncated = %#v, want false", got["truncated"])
		}
	})

	t.Run("dependents_truncated propagates even under the row-count cap", func(t *testing.T) {
		t.Parallel()
		workloadContext := baseContext()
		workloadContext["dependents_truncated"] = true
		got := buildServiceDownstreamConsumers(workloadContext)
		if got["truncated"] != true {
			t.Fatalf("truncated = %#v, want true (dependents_truncated must propagate)", got["truncated"])
		}
	})

	t.Run("consumer_repositories_truncated propagates even under the row-count cap", func(t *testing.T) {
		t.Parallel()
		workloadContext := baseContext()
		workloadContext["consumer_repositories_truncated"] = true
		got := buildServiceDownstreamConsumers(workloadContext)
		if got["truncated"] != true {
			t.Fatalf("truncated = %#v, want true (consumer_repositories_truncated must propagate)", got["truncated"])
		}
	})
}

// TestBuildServiceResultLimitsWithContextDisclosesUpstreamTruncation is the
// same #5720 round-2 P1-1 gap proven against result_limits.truncated: a
// workload context whose dependents/consumer_repositories/
// provisioning_source_chains counts all sit under serviceStoryItemLimit must
// still report truncated: true once any of the three upstream truncation
// signals is set.
func TestBuildServiceResultLimitsWithContextDisclosesUpstreamTruncation(t *testing.T) {
	t.Parallel()

	newBuildCtx := func(extra map[string]any) serviceStoryBuildContext {
		workloadContext := map[string]any{
			"name": "orders-api",
		}
		for key, value := range extra {
			workloadContext[key] = value
		}
		return newServiceStoryBuildContext(workloadContext)
	}

	t.Run("no truncation signal reports untruncated", func(t *testing.T) {
		t.Parallel()
		got := buildServiceResultLimitsWithContext(newBuildCtx(nil))
		if got["truncated"] != false {
			t.Fatalf("truncated = %#v, want false", got["truncated"])
		}
	})

	for _, key := range []string{"dependents_truncated", "consumer_repositories_truncated", "provisioning_source_chains_truncated"} {
		key := key
		t.Run(key+" propagates", func(t *testing.T) {
			t.Parallel()
			got := buildServiceResultLimitsWithContext(newBuildCtx(map[string]any{key: true}))
			if got["truncated"] != true {
				t.Fatalf("truncated = %#v, want true (%s must propagate)", got["truncated"], key)
			}
		})
	}
}

// TestLoadConsumerRepositoryEnrichmentFromCandidatesDisclosesTruncation
// proves loadConsumerRepositoryEnrichmentFromCandidates
// (deployment_trace_candidate_enrichment.go) reports truncation from both of
// its two independent sources: the upstream candidatesTruncated flag passed
// in from queryProvisioningRepositoryCandidates, and its own final
// consumers[:limit] cap over the merged graph-candidate/content-evidence set
// (line ~146), which can trim rows even when the graph candidates themselves
// were not truncated.
func TestLoadConsumerRepositoryEnrichmentFromCandidatesDisclosesTruncation(t *testing.T) {
	t.Parallel()

	t.Run("propagates an upstream candidatesTruncated flag", func(t *testing.T) {
		t.Parallel()
		candidates := []provisioningRepositoryCandidate{
			{RepoID: "repository:consumer-1", RepoName: "consumer-1", RelationshipTypes: []string{"USES_MODULE"}},
		}
		_, truncated, err := loadConsumerRepositoryEnrichmentFromCandidates(
			context.Background(), nil, nil, "repository:orders", "orders-api", nil, 5, candidates, true,
		)
		if err != nil {
			t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
		}
		if !truncated {
			t.Fatalf("truncated = false, want true (must propagate the upstream candidatesTruncated flag)")
		}
	})

	t.Run("no truncation when candidates fit and the flag is false", func(t *testing.T) {
		t.Parallel()
		candidates := []provisioningRepositoryCandidate{
			{RepoID: "repository:consumer-1", RepoName: "consumer-1", RelationshipTypes: []string{"USES_MODULE"}},
		}
		_, truncated, err := loadConsumerRepositoryEnrichmentFromCandidates(
			context.Background(), nil, nil, "repository:orders", "orders-api", nil, 5, candidates, false,
		)
		if err != nil {
			t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
		}
		if truncated {
			t.Fatalf("truncated = true, want false")
		}
	})

	t.Run("detects its own final-cap truncation independent of the upstream flag", func(t *testing.T) {
		t.Parallel()
		candidates := []provisioningRepositoryCandidate{
			{RepoID: "repository:consumer-1", RepoName: "consumer-1", RelationshipTypes: []string{"USES_MODULE"}},
			{RepoID: "repository:consumer-2", RepoName: "consumer-2", RelationshipTypes: []string{"USES_MODULE"}},
			{RepoID: "repository:consumer-3", RepoName: "consumer-3", RelationshipTypes: []string{"USES_MODULE"}},
		}
		consumers, truncated, err := loadConsumerRepositoryEnrichmentFromCandidates(
			context.Background(), nil, nil, "repository:orders", "orders-api", nil, 2, candidates, false,
		)
		if err != nil {
			t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
		}
		if !truncated {
			t.Fatalf("truncated = false, want true (3 candidates capped to a limit of 2 by the function's own final cap)")
		}
		if got, want := len(consumers), 2; got != want {
			t.Fatalf("len(consumers) = %d, want %d", got, want)
		}
	})
}

// TestServiceInvestigationCoverageDisclosesProvisioningReadTruncation is the
// #5720 round-7 P1-1 regression.
//
// GET /investigations/services/{name} runs the same enrichment as every other
// service surface, but its packet carried no result_limits and read none of
// the three *_truncated keys. Its only truncation field was
// coverage_summary.truncated, driven by the 50-row repository marker -- a
// threshold the 25-row indirect-evidence read can never reach. A 40-dependent
// service therefore reported "25 graph dependent(s), 12 content consumer
// repo(s)" alongside coverage_summary.truncated: false, exactly the pre-fix
// behavior the rest of #5720 removed everywhere else.
func TestServiceInvestigationCoverageDisclosesProvisioningReadTruncation(t *testing.T) {
	t.Parallel()

	investigationContext := func(extra map[string]any) map[string]any {
		workloadContext := map[string]any{
			"name":      "orders-api",
			"repo_id":   "repository:orders",
			"repo_name": "orders-api",
			"dependents": []map[string]any{
				{"repo_id": "repository:dependent-1", "repository": "dependent-1"},
			},
			"consumer_repositories": []map[string]any{
				{"repo_id": "repository:consumer-1", "repository": "consumer-1"},
			},
		}
		for key, value := range extra {
			workloadContext[key] = value
		}
		return workloadContext
	}
	coverageFor := func(t *testing.T, extra map[string]any) map[string]any {
		t.Helper()
		packet := buildServiceInvestigationPacket("orders-api", investigationContext(extra), serviceInvestigationOptions{})
		return mapValue(packet, "coverage_summary")
	}

	t.Run("no truncation signal reports untruncated", func(t *testing.T) {
		t.Parallel()
		coverage := coverageFor(t, nil)
		if BoolVal(coverage, "truncated") {
			t.Fatalf("coverage_summary[truncated] = %#v, want false", coverage["truncated"])
		}
	})

	// The packet must also name the bound that fires on the downstream lists.
	// result_limit alone reports the 50-row repository cap, which is not the
	// bound the truncated flag above reports on.
	t.Run("names the downstream read bound", func(t *testing.T) {
		t.Parallel()
		coverage := coverageFor(t, nil)
		if got, want := IntVal(coverage, "downstream_read_limit"), defaultIndirectEvidenceSearchLimit; got != want {
			t.Fatalf("coverage_summary[downstream_read_limit] = %d, want %d", got, want)
		}
		if got, want := IntVal(coverage, "result_limit"), serviceStoryItemLimit; got != want {
			t.Fatalf("coverage_summary[result_limit] = %d, want %d", got, want)
		}
	})

	for _, key := range []string{
		"dependents_truncated",
		"consumer_repositories_truncated",
		"provisioning_source_chains_truncated",
	} {
		t.Run(key+" propagates to coverage_summary", func(t *testing.T) {
			t.Parallel()
			coverage := coverageFor(t, map[string]any{key: true})
			if !BoolVal(coverage, "truncated") {
				t.Fatalf("coverage_summary[truncated] = %#v, want true (%s must propagate)", coverage["truncated"], key)
			}
		})
	}
}

// TestBuildServiceResultLimitsReportsTheDownstreamReadBound is the #5720
// round-7 P2-5 regression: result_limits reported {limit: 50,
// downstream_count: 25, truncated: true}, which tells a caller more than 50
// downstream rows existed. The truth is more than 25 -- the bound that fired
// is the indirect-evidence search limit under the downstream lists, not the
// 50-row rendering cap. downstream_read_limit names it.
func TestBuildServiceResultLimitsReportsTheDownstreamReadBound(t *testing.T) {
	t.Parallel()

	limits := buildServiceResultLimitsWithContext(newServiceStoryBuildContext(map[string]any{"name": "orders-api"}))
	if got, want := IntVal(limits, "downstream_read_limit"), defaultIndirectEvidenceSearchLimit; got != want {
		t.Fatalf("result_limits[downstream_read_limit] = %d, want %d", got, want)
	}
	if got, want := IntVal(limits, "limit"), serviceStoryItemLimit; got != want {
		t.Fatalf("result_limits[limit] = %d, want %d", got, want)
	}
	// The two must stay distinct: a caller that cannot tell them apart is back
	// to reading the 50 as the bound that fired.
	if IntVal(limits, "downstream_read_limit") >= IntVal(limits, "limit") {
		t.Fatalf(
			"result_limits downstream_read_limit = %d is not below limit = %d; the whole point of the field is that the downstream bound is the tighter one",
			IntVal(limits, "downstream_read_limit"), IntVal(limits, "limit"),
		)
	}
}

// TestLoadConsumerRepositoryEnrichmentDisclosesUpstreamHostnameAndSearchBounds
// is the #5720 round-7 P1-3 regression.
//
// Round 2 fixed two truncation sources and left a comment asserting there were
// only two. Two more sit UPSTREAM of both, inside this same call:
// indirectEvidenceHostnameLimit (a hard cap of 4) drops hostnames before any
// consumer search runs, and each per-search content read has its own row cap.
// A repository either bound drops never enters consumersByRepo at all, so the
// merged set fits comfortably under limit, this function's own final cap never
// fires, and candidatesTruncated stays false -- the answer reported a
// complete-looking consumer_repository_count while consumers were missing.
// Both subtests keep the merged set well under limit so nothing but the
// upstream bound can be setting the flag.
func TestLoadConsumerRepositoryEnrichmentDisclosesUpstreamHostnameAndSearchBounds(t *testing.T) {
	t.Parallel()

	oneCandidate := []provisioningRepositoryCandidate{
		{RepoID: "repository:consumer-1", RepoName: "consumer-1", RelationshipTypes: []string{"USES_MODULE"}},
	}

	t.Run("hostname cap above indirectEvidenceHostnameLimit discloses", func(t *testing.T) {
		t.Parallel()

		// Nine hostnames, none carrying the service's own distinctive token,
		// so the affinity filter falls through to the first-N fallback and
		// indirectEvidenceHostnameLimit drops five of them.
		hostnames := make([]string, 0, indirectEvidenceHostnameLimit*2+1)
		for index := range indirectEvidenceHostnameLimit*2 + 1 {
			hostnames = append(hostnames, fmt.Sprintf("vanity-%02d.example.test", index))
		}
		consumers, truncated, err := loadConsumerRepositoryEnrichmentFromCandidates(
			context.Background(), nil, fakePortContentStore{}, "repository:orders", "orders-api",
			hostnames, defaultIndirectEvidenceSearchLimit, oneCandidate, false,
		)
		if err != nil {
			t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
		}
		if got, want := len(consumers), defaultIndirectEvidenceSearchLimit; got >= want {
			t.Fatalf("len(consumers) = %d, want well under the limit of %d so only the hostname bound can set the flag", got, want)
		}
		if !truncated {
			t.Fatalf(
				"truncated = false, want true (%d hostnames were bounded to indirectEvidenceHostnameLimit = %d, so consumers reachable only through the dropped ones were never searched for)",
				len(hostnames), indirectEvidenceHostnameLimit,
			)
		}
	})

	t.Run("a per-search read that comes back full discloses", func(t *testing.T) {
		t.Parallel()

		const limit = 3
		// Every matched row lands in one repository, so the merged consumer
		// set is two entries -- far under limit -- and only the search's own
		// row cap can be reporting truncation.
		rows := make([]FileContent, 0, limit)
		for index := range limit {
			rows = append(rows, FileContent{
				RepoID:       "repository:search-consumer",
				RelativePath: fmt.Sprintf("deploy/values-%02d.yaml", index),
			})
		}
		content := patternConsumerSearchContentStore{
			exactRows: map[string][]FileContent{"orders-api": rows},
		}
		consumers, truncated, err := loadConsumerRepositoryEnrichmentFromCandidates(
			context.Background(), nil, content, "repository:orders", "orders-api",
			nil, limit, oneCandidate, false,
		)
		if err != nil {
			t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
		}
		if got, want := len(consumers), limit; got >= want {
			t.Fatalf("len(consumers) = %d, want under the limit of %d so only the per-search cap can set the flag", got, want)
		}
		if !truncated {
			t.Fatalf("truncated = false, want true (the service-name search returned %d rows at its own cap of %d)", len(rows), limit)
		}
	})

	t.Run("a per-search read below its cap does not disclose", func(t *testing.T) {
		t.Parallel()

		const limit = 3
		content := patternConsumerSearchContentStore{
			exactRows: map[string][]FileContent{
				"orders-api": {{RepoID: "repository:search-consumer", RelativePath: "deploy/values.yaml"}},
			},
		}
		_, truncated, err := loadConsumerRepositoryEnrichmentFromCandidates(
			context.Background(), nil, content, "repository:orders", "orders-api",
			nil, limit, oneCandidate, false,
		)
		if err != nil {
			t.Fatalf("loadConsumerRepositoryEnrichmentFromCandidates() error = %v, want nil", err)
		}
		if truncated {
			t.Fatal("truncated = true, want false (one row against a cap of 3 dropped nothing)")
		}
	})
}
