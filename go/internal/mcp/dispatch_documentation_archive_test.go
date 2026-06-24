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

func TestDispatchToolListsArchiveContainedDocumentationFacts(t *testing.T) {
	t.Parallel()

	archivePath := "docs/support-packet.tar.gz"
	documentID := "doc:git:repository:r_archive:" + archivePath + "!/runbook.md"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/documentation/facts", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), query.EnvelopeMIMEType; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		for key, want := range map[string]string{
			"fact_kind":   "documentation_section",
			"repo":        "repository:r_archive",
			"document_id": documentID,
			"q":           "Restore",
			"limit":       "10",
		} {
			if got := r.URL.Query().Get(key); got != want {
				t.Fatalf("query %q = %q, want %q", key, got, want)
			}
		}
		writeArchiveDocumentationEnvelope(w, documentID, archivePath)
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_documentation_facts",
		map[string]any{
			"fact_kind":   "documentation_section",
			"repo":        "repository:r_archive",
			"document_id": documentID,
			"q":           "Restore",
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
	metadata := mcpMapValue(payload, "source_metadata")
	if got, want := query.StringVal(metadata, "archive_member_path"), "runbook.md"; got != want {
		t.Fatalf("payload.source_metadata.archive_member_path = %q, want %q", got, want)
	}
}

func writeArchiveDocumentationEnvelope(w http.ResponseWriter, documentID string, archivePath string) {
	w.Header().Set("Content-Type", query.EnvelopeMIMEType)
	_ = json.NewEncoder(w).Encode(query.ResponseEnvelope{
		Data: map[string]any{
			"facts": []map[string]any{{
				"fact_id":   "fact:archive:section:1",
				"fact_kind": "documentation_section",
				"payload": map[string]any{
					"document_id":    documentID,
					"section_id":     "section:restore-service",
					"content":        "Follow the recovery checklist.",
					"content_format": "markdown",
					"source_metadata": map[string]any{
						"archive_path":        archivePath,
						"archive_member_path": "runbook.md",
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
