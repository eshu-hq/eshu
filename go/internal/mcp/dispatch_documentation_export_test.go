// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestDispatchToolListsOfflineExportDocumentationFacts(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/documentation/facts", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), query.EnvelopeMIMEType; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		for key, want := range map[string]string{
			"fact_kind":   "documentation_section",
			"source_id":   "doc-source:documentation_export:synthetic",
			"document_id": "doc:documentation_export:sha256:synthetic",
			"q":           "queue",
			"limit":       "1",
		} {
			if got := r.URL.Query().Get(key); got != want {
				t.Fatalf("query %q = %q, want %q", key, got, want)
			}
		}
		writeOfflineExportDocumentationEnvelope(w)
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_documentation_facts",
		map[string]any{
			"fact_kind":   "documentation_section",
			"source_id":   "doc-source:documentation_export:synthetic",
			"document_id": "doc:documentation_export:sha256:synthetic",
			"q":           "queue",
			"limit":       1,
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	data := mcpEnvelopeData(t, result)
	factRows := mcpMapSliceValue(data, "facts")
	if got, want := len(factRows), 1; got != want {
		t.Fatalf("len(facts) = %d, want %d", got, want)
	}
	payload := mcpMapValue(factRows[0], "payload")
	if got, want := query.StringVal(payload, "content"), "Wait for queue zero before deploy."; got != want {
		t.Fatalf("payload.content = %q, want %q", got, want)
	}
}

func writeOfflineExportDocumentationEnvelope(w http.ResponseWriter) {
	w.Header().Set("Content-Type", query.EnvelopeMIMEType)
	_ = json.NewEncoder(w).Encode(query.ResponseEnvelope{
		Data: map[string]any{
			"facts": []map[string]any{{
				"fact_id":   "fact:export:section:1",
				"fact_kind": "documentation_section",
				"payload": map[string]any{
					"document_id": "doc:documentation_export:sha256:synthetic",
					"section_id":  "export:1",
					"content":     "Wait for queue zero before deploy.",
					"source_metadata": map[string]any{
						"source_system": "slack",
					},
				},
			}},
			"count":            1,
			"limit":            1,
			"truncated":        false,
			"missing_evidence": false,
			"states":           []string{},
		},
		Truth: &query.TruthEnvelope{
			Capability: "documentation_facts.list",
			Profile:    "production",
			Basis:      "semantic_facts",
			Freshness:  query.TruthFreshness{State: query.FreshnessFresh},
		},
		Error: nil,
	})
}
