// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDocumentationHandlerListsGoogleWorkspaceSectionFacts(t *testing.T) {
	t.Parallel()

	row := []byte(`{
		"fact_id": "fact:gws:section:1",
		"fact_kind": "documentation_section",
		"scope_id": "doc-source:google_workspace:synthetic",
		"generation_id": "gws-gen-readback",
		"source_system": "google_workspace",
		"source_uri": "redacted:sha256:synthetic",
		"source_record_id": "gws-file:sha256:synthetic",
		"payload": {
			"document_id": "doc:google_workspace:sha256:synthetic",
			"revision_id": "rev-readback",
			"section_id": "export:body",
			"heading_text": "Runbook",
			"content": "Synthetic workspace runbook",
			"source_metadata": {
				"export_mime": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
				"file_id_hash": "sha256:synthetic"
			}
		}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows:    [][]driver.Value{{row}},
		queryContains: []string{
			"fact_records.fact_kind = $1",
			"fact_records.payload->>'source_id' = $2",
			"fact_records.payload->>'document_id' = $3",
			"LOWER(",
		},
	}})
	handler := &DocumentationHandler{
		Content: NewContentReader(db),
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/documentation/facts?fact_kind=section&source_id=doc-source:google_workspace:synthetic&document_id=doc:google_workspace:sha256:synthetic&q=workspace&limit=1",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	factRows := data["facts"].([]any)
	if got, want := len(factRows), 1; got != want {
		t.Fatalf("len(facts) = %d, want %d", got, want)
	}
	payload := factRows[0].(map[string]any)["payload"].(map[string]any)
	metadata := payload["source_metadata"].(map[string]any)
	if got, want := metadata["export_mime"], "application/vnd.openxmlformats-officedocument.wordprocessingml.document"; got != want {
		t.Fatalf("export_mime = %#v, want %#v", got, want)
	}
	if got, want := data["missing_evidence"], false; got != want {
		t.Fatalf("missing_evidence = %#v, want %#v", got, want)
	}
	if got, want := resp.Truth.Capability, documentationFactsCapability; got != want {
		t.Fatalf("truth.capability = %#v, want %#v", got, want)
	}
}
