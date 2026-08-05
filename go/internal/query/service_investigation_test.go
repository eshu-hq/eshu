// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildServiceStoryResponseIncludesInvestigationPacket(t *testing.T) {
	t.Parallel()

	got := buildServiceStoryResponse("sample-service-api", sampleServiceDossierContext())

	investigation := mapValue(got, "investigation")
	if len(investigation) == 0 {
		t.Fatalf("investigation = %#v, want cross-repo investigation packet", got["investigation"])
	}
	if got, want := StringVal(investigation, "service_name"), "sample-service-api"; got != want {
		t.Fatalf("investigation.service_name = %q, want %q", got, want)
	}
	repositories := mapSliceValue(investigation, "repositories_considered")
	if len(repositories) < 4 {
		t.Fatalf("len(repositories_considered) = %d, want service plus related repos: %#v", len(repositories), repositories)
	}
	families := StringSliceVal(investigation, "evidence_families_found")
	for _, want := range []string{"api_surface", "deployment_lanes", "documentation", "downstream_consumers", "upstream_dependencies"} {
		if !stringSliceContains(families, want) {
			t.Fatalf("evidence_families_found = %#v, missing %q", families, want)
		}
	}
	coverage := mapValue(investigation, "coverage_summary")
	if got := StringVal(coverage, "state"); got == "" || got == "complete" {
		t.Fatalf("coverage_summary.state = %q, want truthful non-complete coverage", got)
	}
	if nextCalls := mapSliceValue(investigation, "recommended_next_calls"); len(nextCalls) == 0 {
		t.Fatalf("recommended_next_calls missing, want drilldown handles")
	}
}

func TestInvestigateServiceRouteReturnsCoverageAndRecommendations(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {
					"id":      "workload:service-edge-api",
					"name":    "service-edge-api",
					"kind":    "service",
					"repo_id": "repo-service-edge-api",
				},
				"MATCH (r:Repository {id: $repo_id})": {
					"repo_name": "service-edge-api",
				},
			},
			runByMatch: map[string][]map[string]any{
				"w.name = $service_name": {
					{
						"id":      "workload:service-edge-api",
						"name":    "service-edge-api",
						"kind":    "service",
						"repo_id": "repo-service-edge-api",
					},
				},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
		Profile: ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/investigations/services/service-edge-api?intent=runbook&question=explain", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "service-edge-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	if _, ok := data["coverage_summary"]; !ok {
		t.Fatalf("coverage_summary missing from investigation response: %#v", data)
	}
	if _, ok := data["recommended_next_calls"]; !ok {
		t.Fatalf("recommended_next_calls missing from investigation response: %#v", data)
	}
}

// TestServiceInvestigationFamilySummaryMarksBoundedReads is the #5720 round-8
// P3-2 regression.
//
// Round 7 gave coverage_summary.truncated the three upstream disclosure
// signals, but stopped there. findings[].summary -- the human-readable half of
// the same response, and the half an operator actually reads -- still rendered
// a 40-dependent service as a bare "25 graph dependent(s), 0 content consumer
// repo(s)". The number read as the whole population with nothing marking it as
// a ceiling.
//
// The marker is per family on purpose. consumer_repositories_truncated can fire
// from bounds that never touch provisioning_source_chains, so OR-ing all three
// signals into both summaries would mark an upstream list that was never
// bounded.
func TestServiceInvestigationFamilySummaryMarksBoundedReads(t *testing.T) {
	t.Parallel()

	summaryFor := func(family string, flags ...string) string {
		workloadContext := map[string]any{
			"name":                       "orders-api",
			"dependents":                 []map[string]any{{"repository": "consumer-a"}},
			"consumer_repositories":      []map[string]any{{"repository": "consumer-b"}},
			"dependencies":               []map[string]any{{"name": "shared-lib"}},
			"provisioning_source_chains": []map[string]any{{"repository": "infra"}},
		}
		for _, flag := range flags {
			workloadContext[flag] = true
		}
		return serviceInvestigationFamilySummaryWithContext(newServiceStoryBuildContext(workloadContext), family)
	}

	t.Run("unbounded reads carry no marker", func(t *testing.T) {
		t.Parallel()
		for _, family := range []string{"downstream_consumers", "upstream_dependencies"} {
			if got := summaryFor(family); strings.Contains(got, "(bounded)") {
				t.Fatalf("summary(%s) = %q, want no marker when nothing was truncated", family, got)
			}
		}
	})

	for _, testCase := range []struct {
		family string
		flag   string
	}{
		{family: "downstream_consumers", flag: "dependents_truncated"},
		{family: "downstream_consumers", flag: "consumer_repositories_truncated"},
		{family: "upstream_dependencies", flag: "provisioning_source_chains_truncated"},
	} {
		testCase := testCase
		t.Run(testCase.family+" marked by "+testCase.flag, func(t *testing.T) {
			t.Parallel()
			got := summaryFor(testCase.family, testCase.flag)
			if !strings.HasSuffix(got, " (bounded)") {
				t.Fatalf("summary(%s) with %s = %q, want a trailing (bounded) marker", testCase.family, testCase.flag, got)
			}
		})
	}

	// The counts stay in the string. The marker qualifies them, it does not
	// replace them.
	t.Run("marker keeps the counts", func(t *testing.T) {
		t.Parallel()
		got := summaryFor("downstream_consumers", "consumer_repositories_truncated")
		if want := "1 graph dependent(s), 1 content consumer repo(s) (bounded)"; got != want {
			t.Fatalf("summary = %q, want %q", got, want)
		}
	})

	// A signal that belongs to the other family must not leak across. This is
	// the assertion that fails if the per-family flags are collapsed into one
	// OR of all three.
	t.Run("a downstream signal does not mark the upstream family", func(t *testing.T) {
		t.Parallel()
		got := summaryFor("upstream_dependencies", "consumer_repositories_truncated")
		if strings.Contains(got, "(bounded)") {
			t.Fatalf(
				"summary(upstream_dependencies) with only consumer_repositories_truncated = %q, want no marker (the provisioning chain read was not bounded)",
				got,
			)
		}
	})
}
