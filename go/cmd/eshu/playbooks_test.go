// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchQueryPlaybookResolveUsesEnvelopeAPI(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotAccept string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"resolved":{"playbook_id":"service_story_citation","version":"1.0.0","prompt_family":"service.story","calls":[],"failure_modes":[]}},"truth":{"level":"exact","capability":"query.playbooks","basis":"runtime_state","freshness":{"state":"fresh"}},"error":null}`))
	}))
	defer server.Close()

	opts := queryPlaybookResolveOptions{
		PlaybookID: "service_story_citation",
		Inputs: map[string]string{
			"service_name": "payments-api",
			"environment":  "prod",
		},
	}
	envelope, err := fetchQueryPlaybookResolve(NewAPIClient(server.URL, "", ""), opts)
	if err != nil {
		t.Fatalf("fetch resolve: %v", err)
	}
	if got, want := gotPath, "/api/v0/query-playbooks/resolve"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := gotAccept, eshuEnvelopeMIMEType; got != want {
		t.Fatalf("Accept = %q, want %q", got, want)
	}
	if !strings.Contains(gotBody, `"service_name":"payments-api"`) {
		t.Fatalf("request body = %s, want service_name input", gotBody)
	}
	if got, want := envelope.Data.Resolved.PlaybookID, "service_story_citation"; got != want {
		t.Fatalf("resolved playbook_id = %q, want %q", got, want)
	}
}
