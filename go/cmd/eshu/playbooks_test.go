// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/playbooks"
)

// TestPlaybookResolveThroughRealClient closes the seam between the concrete
// APIClient and internal/cli/playbooks: the real client must satisfy the
// package's EnvelopeClient interface, hit the resolve route with the envelope
// Accept header, and carry the parsed inputs in the request body.
func TestPlaybookResolveThroughRealClient(t *testing.T) {
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

	var out strings.Builder
	err := playbooks.RunResolve(&out, NewAPIClient(server.URL, "", ""), playbooks.ResolveOptions{
		PlaybookID: "service_story_citation",
		Inputs: map[string]string{
			"service_name": "payments-api",
			"environment":  "prod",
		},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
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
	if !strings.Contains(out.String(), `"playbook_id": "service_story_citation"`) {
		t.Fatalf("output = %q, want resolved playbook_id", out.String())
	}
}
