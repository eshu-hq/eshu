// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
