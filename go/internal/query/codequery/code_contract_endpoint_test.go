// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// The CodeHandler cases of root's contract_endpoint_test.go moved here at
// the #6060 CodeHandler move: they drive unexported handleCallChain and
// handleRelationships directly, which cannot be called from another package.
// The InfraHandler, RepositoryHandler, EntityHandler, and CompareHandler
// cases in that shared unsupported-capability contract test stay in root.

func TestHandleCallChain_LocalLightweightReturnsStructuredUnsupportedCapability(t *testing.T) {
	handler := &CodeHandler{Profile: querycontract.ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/call-chain", strings.NewReader(`{"start":"a","end":"b"}`))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
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
	handler := &CodeHandler{Profile: querycontract.ProfileLocalLightweight}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/relationships", strings.NewReader(`{"name":"helper","direction":"incoming","relationship_type":"CALLS","transitive":true}`))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
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
