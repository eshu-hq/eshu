// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// provisioningCandidateCypherFragment identifies the one graph read whose
// LIMIT feeds dependents, consumer_repositories, and
// provisioning_source_chains (queryProvisioningRepositoryCandidates in
// deployment_trace_support_helpers.go).
const provisioningCandidateCypherFragment = "MATCH (target:Repository {id: $repo_id})<-[rel:PROVISIONS_DEPENDENCY_FOR"

// provisioningCandidateRows builds `count` distinct candidate rows for the
// seeded graph reader. Distinct repo ids mean one candidate per row, so the
// row count and the candidate count line up.
func provisioningCandidateRows(count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for index := range count {
		rows = append(rows, map[string]any{
			"repo_id":             fmt.Sprintf("repository:consumer-%02d", index),
			"repo_name":           fmt.Sprintf("consumer-%02d", index),
			"relationship_type":   "USES_MODULE",
			"relationship_reason": "terraform_module_source_path",
		})
	}
	return rows
}

// provisioningCandidateGraphReader answers the provisioning-candidate read
// with `rows` and every other read with nothing, so a test controls exactly
// one bound.
func provisioningCandidateGraphReader(workload map[string]any, rows []map[string]any) fakeWorkloadGraphReader {
	return fakeWorkloadGraphReader{
		runSingleByMatch: map[string]map[string]any{
			"w.name = $service_name": workload,
			"w.id = $workload_id":    workload,
		},
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, provisioningCandidateCypherFragment):
				return rows, nil
			case strings.Contains(cypher, "<-[:DEFINES]-(r:Repository)"):
				// fetchWorkloadRepositoryForAccess resolves the owning
				// repository through this read, and enrichment returns early
				// when repo_id comes back empty -- without it the
				// provisioning-candidate read never runs at all.
				return []map[string]any{{
					"repo_id":   StringVal(workload, "repo_id"),
					"repo_name": StringVal(workload, "repo_name"),
				}}, nil
			default:
				return nil, nil
			}
		},
	}
}

// fullPageConsumerSearchContentStore answers the service-name consumer search
// with exactly `limit` rows, all in one repository, so
// searchConsumerEvidenceAnyRepo's `len(rows) >= limit` probe reports a
// truncated per-search read (source 4 of the enumeration on
// loadConsumerRepositoryEnrichmentFromCandidates) while the merged consumer
// set grows by a single entry and stays far under the final cap.
type fullPageConsumerSearchContentStore struct {
	fakePortContentStore
	pattern string
	repoID  string
}

func (s fullPageConsumerSearchContentStore) SearchFileContentAnyRepoExactCase(
	_ context.Context,
	pattern string,
	limit int,
) ([]FileContent, error) {
	if pattern != s.pattern || limit <= 0 {
		return nil, nil
	}
	rows := make([]FileContent, 0, limit)
	for index := range limit {
		rows = append(rows, FileContent{
			RepoID:       s.repoID,
			RelativePath: fmt.Sprintf("deploy/values-%02d.yaml", index),
		})
	}
	return rows, nil
}

func provisioningTruncationWorkload() map[string]any {
	return map[string]any{
		"id":        "workload:orders-api",
		"instances": []any{},
		"kind":      "service",
		"name":      "orders-api",
		"repo_id":   "repository:orders",
		"repo_name": "orders-api",
	}
}

// runProvisioningTruncationTrace drives the real POST
// /api/v0/impact/trace-deployment-chain handler with a graph whose
// provisioning-candidate read returns candidateRowCount rows, and returns the
// decoded response body.
func runProvisioningTruncationTrace(t *testing.T, candidateRowCount int) map[string]any {
	t.Helper()

	handler := &ImpactHandler{
		Neo4j:   provisioningCandidateGraphReader(provisioningTruncationWorkload(), provisioningCandidateRows(candidateRowCount)),
		Content: fakePortContentStore{},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-deployment-chain",
		strings.NewReader(`{"service_name":"orders-api","include_related_module_usage":true}`),
	)
	w := httptest.NewRecorder()

	handler.traceDeploymentChain(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("traceDeploymentChain status = %d, body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trace response: %v", err)
	}
	return body
}

// provisioningTruncationResponseFields are the three disclosure fields the
// #5720 fix declares on /impact/trace-deployment-chain in
// openapi_paths_impact.go.
var provisioningTruncationResponseFields = []string{
	"dependents_truncated",
	"consumer_repositories_truncated",
	"provisioning_source_chains_truncated",
}

// TestTraceDeploymentChainDisclosesProvisioningReadTruncation is the #5720
// round-7 P1-2 regression: the production wiring of the whole disclosure fix
// had no test at all.
//
// Every step between the bounded graph read and the wire was individually
// unproven end to end -- enrichServiceQueryContextWithOptions writing the
// three *_truncated keys onto the workload context, buildDeploymentTraceFields
// reading them back, and attachOptionalFields putting them on the response.
// Deleting any one of those (or hardcoding all three reads to false) left the
// full ./internal/query suite green, so the entire declared disclosure surface
// of this route could be removed with CI passing. verify-openapi.sh checks
// route parity only and cannot see response fields.
//
// This drives the real handler over a seeded graph and asserts the round trip
// in both directions: present-and-true when the underlying read truncates,
// and absent when it does not. It mirrors
// impact_trace_deployment_config_bounds_test.go, which already does exactly
// this for the sibling uncorrelated_cloud_resources_truncated field.
func TestTraceDeploymentChainDisclosesProvisioningReadTruncation(t *testing.T) {
	t.Parallel()

	t.Run("truncated read discloses all three fields", func(t *testing.T) {
		t.Parallel()

		// The route resolves max_depth 0 to defaultIndirectEvidenceSearchLimit,
		// so one row past that bound is what the over-fetch probe detects.
		body := runProvisioningTruncationTrace(t, defaultIndirectEvidenceSearchLimit+1)
		for _, field := range provisioningTruncationResponseFields {
			if !BoolVal(body, field) {
				t.Fatalf(
					"%s = %#v, want true (the provisioning-candidate read returned more than defaultIndirectEvidenceSearchLimit = %d rows)",
					field, body[field], defaultIndirectEvidenceSearchLimit,
				)
			}
		}
		// The disclosed lists must still be bounded at the limit, so a caller
		// cannot infer completeness from the count alone.
		if got, want := len(mapSliceValue(body, "dependents")), defaultIndirectEvidenceSearchLimit; got != want {
			t.Fatalf("len(dependents) = %d, want %d", got, want)
		}
	})

	t.Run("untruncated read omits all three fields", func(t *testing.T) {
		t.Parallel()

		body := runProvisioningTruncationTrace(t, defaultIndirectEvidenceSearchLimit)
		for _, field := range provisioningTruncationResponseFields {
			if value, present := body[field]; present {
				t.Fatalf(
					"%s = %#v, want the key absent (the read returned exactly defaultIndirectEvidenceSearchLimit = %d rows, nothing was dropped)",
					field, value, defaultIndirectEvidenceSearchLimit,
				)
			}
		}
		// Guards the assertion above against passing for the wrong reason: the
		// lists themselves must be populated, so an absent flag really means
		// "nothing was dropped" rather than "the read produced nothing".
		if got := len(mapSliceValue(body, "dependents")); got == 0 {
			t.Fatal("len(dependents) = 0, want the untruncated read to still return rows")
		}
	})
}

// TestTraceDeploymentChainDistinguishesConsumerTruncationFromCandidateTruncation
// is the #5720 round-8 P1-1 regression.
//
// Round 7 wired consumer_repositories_truncated to the bool
// loadConsumerRepositoryEnrichmentFromCandidates returns, which ORed five
// sources at the time (seven as of round 9). Nothing in the suite held that
// wiring in place. Replacing the
// returned bool at the production call site with the upstream
// candidatesTruncated -- one line, discarding sources 2, 3, 4 and 5 plus the
// hostname affinity narrowing, which is exactly the set rounds 7 and 8 found
// missing -- left every test green:
//
//   - deployment_trace_truncation_disclosure_test.go asserts those sources only
//     against the helper, and never calls the enrichment or the handler.
//   - the sibling cases in this file seed a workload with no hostnames and a
//     content store that matches nothing, so consumersTruncated and
//     candidatesTruncated are equal in every scenario they exercise. They
//     cannot tell the two flags apart.
//
// This case drives the real handler with the two flags deliberately opposed: an
// untruncated provisioning-candidate read (so dependents_truncated and
// provisioning_source_chains_truncated must be absent) alongside a per-search
// content read that comes back full (so consumer_repositories_truncated must be
// true). Substituting either flag for the other reds it.
func TestTraceDeploymentChainDistinguishesConsumerTruncationFromCandidateTruncation(t *testing.T) {
	t.Parallel()

	const candidateRowCount = 2
	workload := provisioningTruncationWorkload()
	handler := &ImpactHandler{
		Neo4j: provisioningCandidateGraphReader(workload, provisioningCandidateRows(candidateRowCount)),
		Content: fullPageConsumerSearchContentStore{
			pattern: "orders-api",
			repoID:  "repository:content-consumer",
		},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-deployment-chain",
		strings.NewReader(`{"service_name":"orders-api","include_related_module_usage":true}`),
	)
	w := httptest.NewRecorder()

	handler.traceDeploymentChain(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("traceDeploymentChain status = %d, body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trace response: %v", err)
	}

	if !BoolVal(body, "consumer_repositories_truncated") {
		t.Fatalf(
			"consumer_repositories_truncated = %#v, want true (the service-name consumer search returned a full page of %d rows at its own cap)",
			body["consumer_repositories_truncated"], defaultIndirectEvidenceSearchLimit,
		)
	}
	for _, field := range []string{"dependents_truncated", "provisioning_source_chains_truncated"} {
		if value, present := body[field]; present {
			t.Fatalf(
				"%s = %#v, want the key absent (the provisioning-candidate read returned %d rows, well under its bound of %d, so it dropped nothing)",
				field, value, candidateRowCount, defaultIndirectEvidenceSearchLimit,
			)
		}
	}
	// Guards the assertions above against passing for the wrong reason. The
	// consumer list must carry the graph candidates plus the one repository the
	// content search named, so the flag really reports a bounded read rather
	// than an empty one, and the merged set must stay under the final cap so
	// source 5 is not what set it.
	if got, want := len(mapSliceValue(body, "consumer_repositories")), candidateRowCount+1; got != want {
		t.Fatalf("len(consumer_repositories) = %d, want %d", got, want)
	}
	if got, want := len(mapSliceValue(body, "dependents")), candidateRowCount; got != want {
		t.Fatalf("len(dependents) = %d, want %d", got, want)
	}
}

// enrichWithProvisioningCandidateRows runs the real
// enrichServiceQueryContextWithOptions over a graph seeded with
// candidateRowCount provisioning-candidate rows and returns the mutated
// workload context.
func enrichWithProvisioningCandidateRows(
	t *testing.T,
	ctx context.Context,
	candidateRowCount int,
) map[string]any {
	t.Helper()

	workloadContext := provisioningTruncationWorkload()
	graph := provisioningCandidateGraphReader(workloadContext, provisioningCandidateRows(candidateRowCount))
	if err := enrichServiceQueryContextWithOptions(
		ctx,
		graph,
		fakePortContentStore{},
		workloadContext,
		serviceQueryEnrichmentOptions{
			IncludeRelatedModuleUsage: true,
			Operation:                 "service_context",
		},
	); err != nil {
		t.Fatalf("enrichServiceQueryContextWithOptions() error = %v, want nil", err)
	}
	return workloadContext
}

// TestEnrichServiceQueryContextSetsProvisioningTruncationKeys proves the
// enrichment half of the #5720 round-7 P1-2 wiring directly: the three
// *_truncated keys every downstream consumer reads (the trace response, the
// story dossier's downstream_consumers and result_limits, and the service
// investigation coverage summary) are actually written onto the workload
// context by enrichServiceQueryContextWithOptions, and only when the read was
// truncated.
func TestEnrichServiceQueryContextSetsProvisioningTruncationKeys(t *testing.T) {
	t.Parallel()

	t.Run("truncated read sets all three keys", func(t *testing.T) {
		t.Parallel()

		workloadContext := enrichWithProvisioningCandidateRows(t, context.Background(), defaultIndirectEvidenceSearchLimit+1)
		for _, key := range provisioningTruncationResponseFields {
			if !BoolVal(workloadContext, key) {
				t.Fatalf("workloadContext[%s] = %#v, want true", key, workloadContext[key])
			}
		}
	})

	t.Run("untruncated read sets none of them", func(t *testing.T) {
		t.Parallel()

		workloadContext := enrichWithProvisioningCandidateRows(t, context.Background(), defaultIndirectEvidenceSearchLimit)
		for _, key := range provisioningTruncationResponseFields {
			if value, present := workloadContext[key]; present {
				t.Fatalf("workloadContext[%s] = %#v, want the key absent", key, value)
			}
		}
		if got := len(mapSliceValue(workloadContext, "dependents")); got == 0 {
			t.Fatal("len(dependents) = 0, want the untruncated read to still populate dependents")
		}
	})
}

// TestEnrichServiceQueryContextDisclosesTruncationToScopedCallers is the #5720
// round-7 P1-4 regression.
//
// candidatesTruncated is computed from the raw graph read, which the backend
// bounds with LIMIT BEFORE
// filterProvisioningRepositoryCandidatesForAccess runs. A scoped caller whose
// granted repositories all sort after the `ORDER BY repo.name, repo.id` cut
// therefore receives rows that the filter removes entirely. While the three
// disclosure writes sat inside `if len(...) > 0` guards, that caller got
// neither dependents nor a truncation signal: an empty answer that reads as
// complete, while their own dependents sat past the cut. The rows past the cut
// were never read, so they cannot be shown to fall outside the grant --
// disclosing the bound is the only honest option.
func TestEnrichServiceQueryContextDisclosesTruncationToScopedCallers(t *testing.T) {
	t.Parallel()

	scopedContext := func(allowedRepoIDs ...string) context.Context {
		return ContextWithAuthContext(context.Background(), AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant_a",
			WorkspaceID:          "workspace_a",
			AllowedRepositoryIDs: allowedRepoIDs,
		})
	}

	t.Run("grant emptied by the access filter still discloses", func(t *testing.T) {
		t.Parallel()

		// The grant names a repository the truncated read never returned, so
		// every admitted row is filtered away.
		workloadContext := enrichWithProvisioningCandidateRows(
			t,
			scopedContext("repository:orders", "repository:granted-but-past-the-cut"),
			defaultIndirectEvidenceSearchLimit+1,
		)
		if got := len(mapSliceValue(workloadContext, "dependents")); got != 0 {
			t.Fatalf("len(dependents) = %d, want 0 (the access filter removes every candidate)", got)
		}
		for _, key := range provisioningTruncationResponseFields {
			if !BoolVal(workloadContext, key) {
				t.Fatalf(
					"workloadContext[%s] = %#v, want true (an emptied-by-filter result over a truncated read must not read as complete)",
					key, workloadContext[key],
				)
			}
		}
	})

	t.Run("surviving grant still discloses", func(t *testing.T) {
		t.Parallel()

		workloadContext := enrichWithProvisioningCandidateRows(
			t,
			scopedContext("repository:orders", "repository:consumer-00"),
			defaultIndirectEvidenceSearchLimit+1,
		)
		if got, want := len(mapSliceValue(workloadContext, "dependents")), 1; got != want {
			t.Fatalf("len(dependents) = %d, want %d (only the granted candidate survives the filter)", got, want)
		}
		for _, key := range provisioningTruncationResponseFields {
			if !BoolVal(workloadContext, key) {
				t.Fatalf("workloadContext[%s] = %#v, want true", key, workloadContext[key])
			}
		}
	})
}
