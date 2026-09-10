// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The CodeHandler cases (TestHandleCallChain_..., TestHandleRelationshipsTransitiveCallers_...)
// moved to codequery/contract_endpoint_test.go at the #6060 CodeHandler
// move: they drive unexported handleCallChain and handleRelationships, which
// cannot be called from another package.

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

	handler.GetRepositoryStory(w, req)

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

	handler.GetWorkloadContext(w, req)

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

	handler.GetWorkloadStory(w, req)

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
