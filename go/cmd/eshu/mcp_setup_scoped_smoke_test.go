// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAPIQueryProberAcceptsScopedIndexStatusShape pins the `eshu mcp setup
// --verify` first-query smoke against the scoped index-status payload (#5167).
// A personal token now gets the scoped shape -- a grant-bound repository_count
// plus withheld_sections, with no status or queue sections -- instead of the
// 403 it got while the route sat on the pending row-filtering ledger, so the
// smoke must treat a 200 with that body as a pass and not require any
// deployment-wide field.
func TestAPIQueryProberAcceptsScopedIndexStatusShape(t *testing.T) {
	t.Parallel()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/index-status" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"dev","scoped":true,"repository_count":1,` +
			`"completeness_state":"scoped_repository_count_only",` +
			`"withheld_sections":["status","reasons","queue","queue_blockages","coordinator",` +
			`"scope_activity","aws_materialization","semantic_extraction","terraform_state"]}`))
	}))
	t.Cleanup(srv.Close)

	client := NewAPIClient(srv.URL, "personal-token", "")
	if err := (apiQueryProber{client: client}).Smoke(); err != nil {
		t.Fatalf("Smoke() error = %v, want nil for the scoped index-status shape", err)
	}
	if gotAuth != "Bearer personal-token" {
		t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer personal-token")
	}
}
