package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
				"w.name = $service_name": {
					"id":      "workload:service-edge-api",
					"name":    "service-edge-api",
					"kind":    "service",
					"repo_id": "repo-service-edge-api",
				},
			},
			runByMatch: map[string][]map[string]any{
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
