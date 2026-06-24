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

func TestDispatchToolListsStructuredDiagramDocumentationFacts(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/documentation/facts", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), query.EnvelopeMIMEType; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		for key, want := range map[string]string{
			"fact_kind":   "documentation_section",
			"repo":        "repository:r_diagram",
			"document_id": "doc:git:repository:r_diagram:docs/architecture.svg",
			"q":           "SVG Runbook",
			"limit":       "10",
		} {
			if got := r.URL.Query().Get(key); got != want {
				t.Fatalf("query %q = %q, want %q", key, got, want)
			}
		}
		writeStructuredDiagramDocumentationEnvelope(w)
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_documentation_facts",
		map[string]any{
			"fact_kind":   "documentation_section",
			"repo":        "repository:r_diagram",
			"document_id": "doc:git:repository:r_diagram:docs/architecture.svg",
			"q":           "SVG Runbook",
			"limit":       10,
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
	if got, want := query.StringVal(payload, "content_format"), "svg"; got != want {
		t.Fatalf("payload.content_format = %q, want %q", got, want)
	}
}

func writeStructuredDiagramDocumentationEnvelope(w http.ResponseWriter) {
	w.Header().Set("Content-Type", query.EnvelopeMIMEType)
	_ = json.NewEncoder(w).Encode(query.ResponseEnvelope{
		Data: map[string]any{
			"facts": []map[string]any{{
				"fact_id":   "fact:structured-diagram:section:1",
				"fact_kind": "documentation_section",
				"payload": map[string]any{
					"document_id":    "doc:git:repository:r_diagram:docs/architecture.svg",
					"section_id":     "section:diagram",
					"content":        "Documentation Graph\nSVG Runbook",
					"content_format": "svg",
					"source_metadata": map[string]any{
						"format_family":  "diagram",
						"diagram_format": "svg",
					},
				},
			}},
			"count":            1,
			"limit":            10,
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
