// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
)

// RouteMounter is anything that can mount its routes on a mux, such as the
// admin Handler. The harness takes the interface instead of the concrete
// handler so white-box handler tests can share it without an import cycle.
type RouteMounter interface {
	Mount(*http.ServeMux)
}

// MountAdminHandler mounts a handler on a fresh mux so spec and live proofs
// can drive the moved handlers over HTTP without duplicating the harness in
// every test package that needs it.
func MountAdminHandler(h RouteMounter) *http.ServeMux {
	mux := http.NewServeMux()
	h.Mount(mux)
	return mux
}

// PostJSON posts a JSON body to path on mux and returns the recorder, so
// handler proofs share one request shape.
func PostJSON(mux *http.ServeMux, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}
