// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Relationships direction validation proof. It stays in codequery: it drives
// the moved CodeHandler with no content store at all. Split from
// code_relationships_content_fallback_test.go at the lane-A move (#6060); the
// SQL-backed fallback proofs live in package query under the same basename.

func TestHandleRelationshipsRejectsInvalidDirection(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/relationships",
		bytes.NewBufferString(`{"entity_id":"function-1","direction":"sideways"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}
