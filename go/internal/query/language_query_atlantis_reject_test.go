// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleLanguageQueryRejectsAtlantisEntityType is the failing-first
// regression for the #5369 codex P1: registering atlantis_project /
// atlantis_workflow for resolve_entity must NOT advertise them as
// language-queryable. Atlantis entities carry language "yaml", which
// supportedLanguages does not accept, so a language-query for an atlantis type
// must be rejected with 400 "unsupported entity_type" rather than dispatched to
// a graph query that filters to a non-yaml language and returns 200-empty
// (a claimed-but-unqueryable capability). Before the fix these types were in
// graphFirstContentBackedEntityTypes -> allSupportedEntityTypes(), so the
// handler dispatched them and returned 200 with zero rows.
func TestHandleLanguageQueryRejectsAtlantisEntityType(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j:   &mockLanguageQueryGraphReader{},
		Content: &languageQueryContentStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, entityType := range []string{"atlantis_project", "atlantis_workflow"} {
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/v0/code/language-query",
			// hcl is a valid supported language, so the request passes language
			// validation and reaches the entity_type check — proving the 400 is
			// about the unqueryable entity_type, not the language.
			bytes.NewBufferString(`{"language":"hcl","entity_type":"`+entityType+`","query":"x"}`),
		)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if got, want := w.Code, http.StatusBadRequest; got != want {
			t.Fatalf("entity_type %q: status = %d, want %d body=%s", entityType, got, want, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "unsupported entity_type") {
			t.Fatalf("entity_type %q: body = %q, want it to contain %q", entityType, w.Body.String(), "unsupported entity_type")
		}
	}
}
