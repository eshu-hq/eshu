// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestTraceExposurePathRouteRejectsEmptySource pins the
// POST /api/v0/impact/trace-exposure-path Mount registration and the
// handler's source validation: an empty request reaches traceExposurePath
// (full-stack profile clears the capability gate without a graph) and is
// rejected before any graph read. See #6060.
func TestTraceExposurePathRouteRejectsEmptySource(t *testing.T) {
	handler := &ImpactHandler{Profile: querycontract.ProfileLocalFullStack}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-exposure-path",
		bytes.NewBufferString(`{}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}
