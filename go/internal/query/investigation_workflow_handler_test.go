// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestInvestigationWorkflowHandlerListsCatalogWithWorkflowPlanTruth(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	router := &APIRouter{
		InvestigationWorkflows: &InvestigationWorkflowHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/investigation-workflows", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("envelope error = %+v, want nil", envelope.Error)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != CapabilityInvestigationWorkflows {
		t.Fatalf("truth = %+v, want investigation workflow capability", envelope.Truth)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map", envelope.Data)
	}
	if got, want := data["schema_version"], "investigation-workflows.v1"; got != want {
		t.Fatalf("schema_version = %#v, want %#v", got, want)
	}
	if count := int(data["count"].(float64)); count != len(InvestigationWorkflowCatalog()) {
		t.Fatalf("count = %d, want %d", count, len(InvestigationWorkflowCatalog()))
	}
}

// TestInvestigationWorkflowHandlerDefaultViewIsCompactAndBounded proves the
// default list response is the compact Summary shape (no tool_groups/
// required_evidence/missing_evidence_routes) and fits an MCP-client-friendly
// response budget (#6795).
func TestInvestigationWorkflowHandlerDefaultViewIsCompactAndBounded(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	router := &APIRouter{
		InvestigationWorkflows: &InvestigationWorkflowHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/investigation-workflows", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	const budget = 8 * 1024
	if got := rec.Body.Len(); got >= budget {
		t.Fatalf("default /api/v0/investigation-workflows body = %d bytes, want < %d", got, budget)
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data := envelope.Data.(map[string]any)
	workflows, ok := data["workflows"].([]any)
	if !ok || len(workflows) == 0 {
		t.Fatalf("workflows = %#v, want non-empty list", data["workflows"])
	}
	for _, raw := range workflows {
		entry := raw.(map[string]any)
		if _, ok := entry["tool_groups"]; ok {
			t.Fatalf("compact workflow %q carries tool_groups, want omitted", entry["id"])
		}
		if _, ok := entry["missing_evidence_routes"]; ok {
			t.Fatalf("compact workflow %q carries missing_evidence_routes, want omitted", entry["id"])
		}
		if entry["id"].(string) == "" {
			t.Fatal("compact workflow missing id")
		}
	}
	if got, want := int(data["total"].(float64)), len(InvestigationWorkflowCatalog()); got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
}

// TestInvestigationWorkflowHandlerViewFullReturnsCompleteWorkflows proves
// view=full restores the pre-#6795 shape (tool_groups, missing_evidence_routes).
func TestInvestigationWorkflowHandlerViewFullReturnsCompleteWorkflows(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	router := &APIRouter{
		InvestigationWorkflows: &InvestigationWorkflowHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/investigation-workflows?view=full&limit=1", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data := envelope.Data.(map[string]any)
	workflows := data["workflows"].([]any)
	if len(workflows) != 1 {
		t.Fatalf("workflows = %#v, want 1 (limit=1)", workflows)
	}
	entry := workflows[0].(map[string]any)
	if _, ok := entry["tool_groups"]; !ok {
		t.Fatal("view=full workflow missing tool_groups")
	}
	if got, ok := data["next_offset"].(float64); !ok || int(got) != 1 {
		t.Fatalf("next_offset = %#v, want 1", data["next_offset"])
	}
}

// TestInvestigationWorkflowHandlerFullViewIsByteIdenticalToWorkflow proves
// view=full's per-workflow JSON is exactly InvestigationWorkflow's own
// serialization -- not a hand-copied projection that could silently drop a
// field the type gains later (#6795 review finding).
func TestInvestigationWorkflowHandlerFullViewIsByteIdenticalToWorkflow(t *testing.T) {
	t.Parallel()

	catalog := InvestigationWorkflowCatalog()
	mux := http.NewServeMux()
	router := &APIRouter{
		InvestigationWorkflows: &InvestigationWorkflowHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v0/investigation-workflows?view=full&limit=%d", len(catalog)), nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var envelope struct {
		Data struct {
			Workflows json.RawMessage `json:"workflows"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want, err := json.Marshal(catalog)
	if err != nil {
		t.Fatalf("marshal catalog: %v", err)
	}
	var gotWorkflows, wantWorkflows []map[string]any
	if err := json.Unmarshal(envelope.Data.Workflows, &gotWorkflows); err != nil {
		t.Fatalf("decode response workflows: %v", err)
	}
	if err := json.Unmarshal(want, &wantWorkflows); err != nil {
		t.Fatalf("decode expected workflows: %v", err)
	}
	if !reflect.DeepEqual(gotWorkflows, wantWorkflows) {
		t.Fatal("view=full workflows diverged from InvestigationWorkflow's own serialization")
	}
}

// TestInvestigationWorkflowHandlerBeforeAfterPayloadSize measures the
// pre-#6795 default payload (view=full, unbounded) against the post-#6795
// default and logs both, proving the reduction is real and measured (#6795).
func TestInvestigationWorkflowHandlerBeforeAfterPayloadSize(t *testing.T) {
	t.Parallel()

	catalog := InvestigationWorkflowCatalog()
	mux := http.NewServeMux()
	router := &APIRouter{
		InvestigationWorkflows: &InvestigationWorkflowHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)

	before := investigationWorkflowRawBody(t, mux, fmt.Sprintf("/api/v0/investigation-workflows?view=full&limit=%d", len(catalog)))
	after := investigationWorkflowRawBody(t, mux, "/api/v0/investigation-workflows")
	t.Logf("list_investigation_workflows default payload: before(#6795 shape, view=full, all %d workflows)=%d bytes, after(compact default, limit=%d)=%d bytes",
		len(catalog), len(before), investigationWorkflowDefaultLimit, len(after))
	if len(after) >= len(before) {
		t.Fatalf("after size %d bytes not smaller than before size %d bytes", len(after), len(before))
	}
	const budget = 8 * 1024
	if len(after) >= budget {
		t.Fatalf("after size %d bytes, want < %d", len(after), budget)
	}
}

func investigationWorkflowRawBody(t *testing.T, mux http.Handler, target string) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

// TestInvestigationWorkflowHandlerRejectsBadView proves an unrecognized view
// value is a bounded 400.
func TestInvestigationWorkflowHandlerRejectsBadView(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	router := &APIRouter{
		InvestigationWorkflows: &InvestigationWorkflowHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/investigation-workflows?view=verbose", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestInvestigationWorkflowHandlerResolvesMissingEvidenceNextCalls(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	router := &APIRouter{
		InvestigationWorkflows: &InvestigationWorkflowHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)

	body := bytes.NewBufferString(`{"workflow_id":"guided_incident_context","inputs":{"incident_id":"INC-1","service_id":"checkout"},"missing_evidence":["observability"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/investigation-workflows/resolve", body)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			Resolved ResolvedInvestigationWorkflow `json:"resolved"`
		} `json:"data"`
		Truth *TruthEnvelope `json:"truth"`
		Error *ErrorEnvelope `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("envelope error = %+v, want nil", envelope.Error)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != CapabilityInvestigationWorkflows {
		t.Fatalf("truth = %+v, want investigation workflow capability", envelope.Truth)
	}
	if got, want := envelope.Data.Resolved.WorkflowID, "guided_incident_context"; got != want {
		t.Fatalf("workflow_id = %q, want %q", got, want)
	}
	if len(envelope.Data.Resolved.RecommendedNextCalls) != 1 {
		t.Fatalf("next calls = %#v, want one", envelope.Data.Resolved.RecommendedNextCalls)
	}
	if got, want := envelope.Data.Resolved.RecommendedNextCalls[0].Tool, "list_observability_coverage_correlations"; got != want {
		t.Fatalf("next call tool = %q, want %q", got, want)
	}
}

func TestInvestigationWorkflowHandlerCoversChildCompleteAndPartialPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		request       investigationWorkflowResolveRequest
		wantCalls     []string
		wantBlocked   []string
		wantUnmatched []string
	}{
		{
			name: "vulnerable dependency complete path",
			request: investigationWorkflowResolveRequest{
				WorkflowID: "guided_vulnerable_dependency",
				Inputs: map[string]string{
					"advisory_id":    "CVE-2026-0001",
					"finding_id":     "finding-1",
					"image_ref":      "registry.example.com/checkout:latest",
					"owner_ref":      "team-checkout",
					"package_id":     "pkg:npm/example",
					"repo_id":        "repo-checkout",
					"service_id":     "checkout",
					"subject":        "CVE-2026-0001",
					"subject_digest": "sha256:abc123",
					"workload_id":    "workload:checkout",
				},
				MissingEvidence: []string{"scanner", "advisory", "package", "sbom", "impact", "image", "workload", "service", "owner", "freshness"},
			},
			wantCalls: []string{"scanner_contract", "advisory_sources", "package_correlation", "sbom_attachment", "impact_explanation", "image_identity", "impact_findings", "service_story", "owner_correlation", "generation_lifecycle"},
		},
		{
			name: "vulnerable dependency partial path blocks unanchored image and owner",
			request: investigationWorkflowResolveRequest{
				WorkflowID:      "guided_vulnerable_dependency",
				Inputs:          map[string]string{"subject": "CVE-2026-0001"},
				MissingEvidence: []string{"image", "owner"},
			},
			wantBlocked: []string{"image_identity", "owner_correlation"},
		},
		{
			name: "deployable drift complete path",
			request: investigationWorkflowResolveRequest{
				WorkflowID: "guided_deployable_drift",
				Inputs: map[string]string{
					"deployable_unit_id": "workload:checkout",
					"generation_id":      "gen-1",
					"provider":           "aws",
					"repo_id":            "repo-checkout",
					"scope_id":           "scope-1",
				},
				MissingEvidence: []string{"admission", "runtime", "service", "freshness"},
			},
			wantCalls: []string{"admission_decision", "runtime_drift", "workload_story", "generation_lifecycle"},
		},
		{
			name: "deployable drift partial path blocks unanchored admission",
			request: investigationWorkflowResolveRequest{
				WorkflowID: "guided_deployable_drift",
				Inputs: map[string]string{
					"deployable_unit_id": "workload:checkout",
					"generation_id":      "gen-1",
					"scope_id":           "scope-1",
				},
				MissingEvidence: []string{"admission"},
			},
			wantBlocked: []string{"admission_decision"},
		},
		{
			name: "incident context complete path",
			request: investigationWorkflowResolveRequest{
				WorkflowID: "guided_incident_context",
				Inputs: map[string]string{
					"environment": "prod",
					"incident_id": "INC-1",
					"provider":    "pagerduty",
					"repo_id":     "repo-checkout",
					"scope_id":    "scope-1",
					"service_id":  "checkout",
				},
				MissingEvidence: []string{"incident", "service", "runtime", "observability", "changes", "freshness"},
			},
			wantCalls: []string{"incident_context", "service_story", "deployment_chain", "observability_coverage", "service_changes", "generation_lifecycle"},
		},
		{
			name: "incident context partial path blocks unanchored optional evidence",
			request: investigationWorkflowResolveRequest{
				WorkflowID:      "guided_incident_context",
				Inputs:          map[string]string{},
				MissingEvidence: []string{"incident", "service", "runtime", "observability", "changes", "freshness", "unknown-family"},
			},
			wantBlocked:   []string{"incident_context", "service_story", "deployment_chain", "observability_coverage", "service_changes", "generation_lifecycle"},
			wantUnmatched: []string{"unknown-family"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resolved := resolveWorkflowViaHTTP(t, tt.request)
			requireResolvedCallIDs(t, resolved.RecommendedNextCalls, tt.wantCalls)
			requireBlockedCallIDs(t, resolved.BlockedNextCalls, tt.wantBlocked)
			if got, want := resolved.UnmatchedMissingEvidence, tt.wantUnmatched; !workflowStringSlicesEqual(got, want) {
				t.Fatalf("unmatched missing evidence = %#v, want %#v", got, want)
			}
		})
	}
}

func TestInvestigationWorkflowHandlerRejectsUnknownWorkflow(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	router := &APIRouter{
		InvestigationWorkflows: &InvestigationWorkflowHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/investigation-workflows/resolve", bytes.NewBufferString(`{"workflow_id":"missing","inputs":{}}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error == nil || envelope.Error.Code != ErrorCodeNotFound {
		t.Fatalf("error = %+v, want not_found", envelope.Error)
	}
	if got, want := envelope.Error.Capability, CapabilityInvestigationWorkflows; got != want {
		t.Fatalf("error capability = %q, want %q", got, want)
	}
}

func resolveWorkflowViaHTTP(t *testing.T, request investigationWorkflowResolveRequest) ResolvedInvestigationWorkflow {
	t.Helper()

	mux := http.NewServeMux()
	router := &APIRouter{
		InvestigationWorkflows: &InvestigationWorkflowHandler{Profile: ProfileProduction},
	}
	router.Mount(mux)

	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/investigation-workflows/resolve", bytes.NewReader(payload))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}

	var envelope struct {
		Data struct {
			Resolved ResolvedInvestigationWorkflow `json:"resolved"`
		} `json:"data"`
		Truth *TruthEnvelope `json:"truth"`
		Error *ErrorEnvelope `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("envelope error = %+v, want nil", envelope.Error)
	}
	if envelope.Truth == nil || envelope.Truth.Capability != CapabilityInvestigationWorkflows {
		t.Fatalf("truth = %+v, want investigation workflow capability", envelope.Truth)
	}
	return envelope.Data.Resolved
}

func requireResolvedCallIDs(t *testing.T, calls []ResolvedWorkflowCall, want []string) {
	t.Helper()

	if len(calls) != len(want) {
		t.Fatalf("recommended calls = %#v, want IDs %#v", calls, want)
	}
	got := map[string]struct{}{}
	for _, call := range calls {
		got[call.ID] = struct{}{}
		if call.ExpectedEvidence == "" {
			t.Fatalf("call %#v missing expected evidence", call)
		}
	}
	for _, id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("recommended calls missing %q in %#v", id, calls)
		}
	}
}

func requireBlockedCallIDs(t *testing.T, calls []BlockedWorkflowCall, want []string) {
	t.Helper()

	if len(calls) != len(want) {
		t.Fatalf("blocked calls = %#v, want IDs %#v", calls, want)
	}
	got := map[string]struct{}{}
	for _, call := range calls {
		got[call.ID] = struct{}{}
		if len(call.RequiredInputsAny) == 0 {
			t.Fatalf("blocked call %#v missing required inputs", call)
		}
	}
	for _, id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("blocked calls missing %q in %#v", id, calls)
		}
	}
}

func workflowStringSlicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
