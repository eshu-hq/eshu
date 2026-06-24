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

func TestDocumentationHandlerListsOCRSectionFactsWithMetadata(t *testing.T) {
	t.Parallel()

	row := []byte(`{
		"fact_id": "fact:ocr-section",
		"fact_kind": "documentation_section",
		"scope_id": "doc-source:git:repo-ocr",
		"generation_id": "gen-ocr-1",
		"payload": {
			"document_id": "doc:git:repo-ocr:docs/architecture.png",
			"section_id": "ocr:title",
			"content": "Architecture dashboard",
			"content_format": "text/plain",
			"source_metadata": {
				"format_family": "image_ocr",
				"incident_media_source_class": "ocr_region",
				"bounds_x": "0.1000",
				"confidence_bucket": "high",
				"source_hash": "sha256:fixture"
			}
		}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows:    [][]driver.Value{{row}},
		queryContains: []string{
			"fact_records.payload->>'source_id'",
			"fact_records.payload->>'document_id'",
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
		"/api/v0/documentation/facts?source_id=doc-source:git:repo-ocr&document_id=doc:git:repo-ocr:docs/architecture.png&fact_kind=section&q=Architecture&limit=1",
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
	rows := data["facts"].([]any)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(facts) = %d, want %d", got, want)
	}
	payload := rows[0].(map[string]any)["payload"].(map[string]any)
	metadata := payload["source_metadata"].(map[string]any)
	if got, want := metadata["format_family"], "image_ocr"; got != want {
		t.Fatalf("format_family = %#v, want %#v", got, want)
	}
	if got, want := metadata["confidence_bucket"], "high"; got != want {
		t.Fatalf("confidence_bucket = %#v, want %#v", got, want)
	}
	if got, want := metadata["incident_media_source_class"], "ocr_region"; got != want {
		t.Fatalf("incident_media_source_class = %#v, want %#v", got, want)
	}
}
