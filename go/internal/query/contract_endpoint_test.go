package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleCallChain_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	handler := &CodeHandler{Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/call-chain", strings.NewReader(`{"start":"a","end":"b"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	handler.handleCallChain(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if body := w.Body.String(); !strings.Contains(body, `"unsupported_capability"`) {
		t.Fatalf("body = %s, want unsupported_capability envelope", body)
	}
}

func TestHandleRelationshipsTransitiveCallers_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	handler := &CodeHandler{Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships", strings.NewReader(`{"name":"helper","direction":"incoming","relationship_type":"CALLS","transitive":true}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	handler.handleRelationships(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if body := w.Body.String(); !strings.Contains(body, `"unsupported_capability"`) {
		t.Fatalf("body = %s, want unsupported_capability envelope", body)
	}
	if body := w.Body.String(); !strings.Contains(body, `"call_graph.transitive_callers"`) {
		t.Fatalf("body = %s, want transitive callers capability", body)
	}
}

func TestFindBlastRadius_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	handler := &ImpactHandler{Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/blast-radius", strings.NewReader(`{"target":"repo","target_type":"repository"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	handler.findBlastRadius(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if body := w.Body.String(); !strings.Contains(body, `"unsupported_capability"`) {
		t.Fatalf("body = %s, want unsupported_capability envelope", body)
	}
}

func TestTraceDeploymentChain_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	handler := &ImpactHandler{Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(`{"service_name":"payments"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	handler.traceDeploymentChain(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if body := w.Body.String(); !strings.Contains(body, `"unsupported_capability"`) {
		t.Fatalf("body = %s, want unsupported_capability envelope", body)
	}
}

func TestGetEcosystemOverview_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	handler := &InfraHandler{Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/ecosystem/overview", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	handler.getEcosystemOverview(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if body := w.Body.String(); !strings.Contains(body, `"unsupported_capability"`) {
		t.Fatalf("body = %s, want unsupported_capability envelope", body)
	}
}

func TestSearchResources_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	handler := &InfraHandler{Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/infra/resources/search", strings.NewReader(`{"query":"argocd"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	handler.searchResources(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if body := w.Body.String(); !strings.Contains(body, `"unsupported_capability"`) {
		t.Fatalf("body = %s, want unsupported_capability envelope", body)
	}
}

func TestGetRepositoryStory_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	handler := &RepositoryHandler{Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-1/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	handler.getRepositoryStory(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if body := w.Body.String(); !strings.Contains(body, `"unsupported_capability"`) {
		t.Fatalf("body = %s, want unsupported_capability envelope", body)
	}
	if body := w.Body.String(); !strings.Contains(body, `"platform_impact.context_overview"`) {
		t.Fatalf("body = %s, want context_overview capability", body)
	}
}

func TestGetWorkloadContext_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	handler := &EntityHandler{Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/w-1/context", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	handler.getWorkloadContext(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if body := w.Body.String(); !strings.Contains(body, `"unsupported_capability"`) {
		t.Fatalf("body = %s, want unsupported_capability envelope", body)
	}
	if body := w.Body.String(); !strings.Contains(body, `"platform_impact.context_overview"`) {
		t.Fatalf("body = %s, want context_overview capability", body)
	}
}

func TestGetWorkloadStory_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	handler := &EntityHandler{Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/w-1/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	handler.getWorkloadStory(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if body := w.Body.String(); !strings.Contains(body, `"unsupported_capability"`) {
		t.Fatalf("body = %s, want unsupported_capability envelope", body)
	}
	if body := w.Body.String(); !strings.Contains(body, `"platform_impact.context_overview"`) {
		t.Fatalf("body = %s, want context_overview capability", body)
	}
}

func TestCompareEnvironments_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	handler := &CompareHandler{Profile: ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/compare/environments", strings.NewReader(`{"workload_id":"w","left":"dev","right":"prod"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()

	handler.compareEnvironments(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if body := w.Body.String(); !strings.Contains(body, `"unsupported_capability"`) {
		t.Fatalf("body = %s, want unsupported_capability envelope", body)
	}
}
