// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// #7170 (child B of #7129): the `matches` alias was byte-identical to
// `results` on the code search, symbol search, and structural inventory
// producers, doubling the response bytes that the MCP dispatcher counts against
// its budget. Removal is a wire-contract change, so each producer asserts the
// absence of the named field rather than comparing two fields for equality.

func TestCodeRoutesDoNotEmitMatchesAlias(t *testing.T) {
	t.Parallel()

	routes := []struct {
		name    string
		path    string
		body    string
		handler *CodeHandler
	}{
		{
			name: "code_search",
			path: "/api/v0/code/search",
			body: `{"query":"search","language":"typescript"}`,
			handler: &CodeHandler{Content: &recordingEntityNameSearcher{Rows: []querycontract.EntityContent{{
				EntityID: "func:ts:search", EntityName: "search", EntityType: "Function",
				RelativePath: "src/search.ts", RepoID: "repo-2", Language: "typescript",
				StartLine: 4, EndLine: 18,
			}}}},
		},
		{
			name:    "symbol_search",
			path:    "/api/v0/code/symbols/search",
			body:    `{"symbol":"RefreshSession"}`,
			handler: &CodeHandler{Content: &symbolSearchGrantStore{}, Profile: ProfileLocalAuthoritative},
		},
		{
			name:    "structural_inventory",
			path:    "/api/v0/code/structure/inventory",
			body:    `{"inventory_kind":"entity","language":"go"}`,
			handler: &CodeHandler{Content: &structuralInventoryGrantStore{}, Profile: ProfileLocalAuthoritative},
		},
	}

	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			mux := http.NewServeMux()
			route.handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, route.path, bytes.NewBufferString(route.body))
			req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			data := testutil.DecodeEnvelopeData(t, rec.Body.Bytes())
			results, ok := data["results"].([]any)
			if !ok || len(results) == 0 {
				t.Fatalf("data[results] = %#v, want a non-empty array; body = %s", data["results"], rec.Body.String())
			}
			if _, ok := data["matches"]; ok {
				t.Fatalf("data[matches] present, want the alias removed; body = %s", rec.Body.String())
			}
		})
	}
}
