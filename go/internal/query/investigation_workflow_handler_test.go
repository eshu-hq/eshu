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
