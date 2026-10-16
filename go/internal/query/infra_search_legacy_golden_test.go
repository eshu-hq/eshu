// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// infraSearchLegacyCase is one request shape whose graph statements the #7245
// read cutover must leave byte for byte unchanged whenever the full-text path
// is not selected.
type infraSearchLegacyCase struct {
	name    string
	backend querycontract.GraphBackend
	scoped  bool
	body    string
}

// infraSearchLegacyCases covers every dispatch of searchResources: unscoped
// and scoped (both dialects), free text, the "::" equality branch, the
// structured (empty-query) search, a category plus filter set, and the ArgoCD
// category-only read.
func infraSearchLegacyCases() []infraSearchLegacyCase {
	bodies := map[string]string{
		"free_text":      `{"query":"aws_iam_role"}`,
		"equality":       `{"query":"arn:aws:sns::123:topic"}`,
		"structured":     `{"kind":"aws_iam_role","environment":"qa"}`,
		"category_kind":  `{"query":"svc-1","category":"terraform","kind":"aws_iam_role","provider":"aws","resource_service":"iam","resource_category":"identity","limit":16}`,
		"argocd_only":    `{"category":"argocd"}`,
		"cloud_category": `{"query":"svc","category":"cloud"}`,
	}
	names := make([]string, 0, len(bodies))
	for name := range bodies {
		names = append(names, name)
	}
	sort.Strings(names)
	var cases []infraSearchLegacyCase
	for _, backend := range []querycontract.GraphBackend{querycontract.GraphBackendNeo4j, querycontract.GraphBackendNornicDB} {
		for _, scoped := range []bool{false, true} {
			scope := "unscoped"
			if scoped {
				scope = "scoped"
			}
			for _, name := range names {
				cases = append(cases, infraSearchLegacyCase{
					name:    string(backend) + "/" + scope + "/" + name,
					backend: backend,
					scoped:  scoped,
					body:    bodies[name],
				})
			}
		}
	}
	return cases
}

// infraSearchLegacyDigests pins the statements and params of every legacy
// search shape, computed from origin/main e0e7f03f9 before the #7245 read
// cutover. A change here is a wire change to the legacy search and needs its
// own review.
var infraSearchLegacyDigests = map[string]string{
	"neo4j/unscoped/argocd_only":       "e748810b0efd23fc1f7baaa5ea219bc3298463e981bf4faa747ee6722cb0710b",
	"neo4j/unscoped/category_kind":     "e63ea821f3f8aa0efa46085d3d4eaf8b831cb0983daee967850311e4d578d355",
	"neo4j/unscoped/cloud_category":    "8d187ebe095faaeddb2a369a26d61b07e9c60809e0648757801ccf54aa1c7093",
	"neo4j/unscoped/equality":          "89cbc7786e244cceef8a293b43db02f41b3737c9a6f33141b16dd1de0258f86e",
	"neo4j/unscoped/free_text":         "2e01aa03a3aebabc379a888cebd4f78d3cf625a4e14e402d874a47a63603b4ed",
	"neo4j/unscoped/structured":        "01192917fac1a40ca1873b574d5363c616ebd0aaa1602478be2dd2703af1bedc",
	"neo4j/scoped/argocd_only":         "5452dc9ee860bc94f3071c9c1e3a927c8aabae7706327060f22244f6a770ea6a",
	"neo4j/scoped/category_kind":       "6f4aba4a725bbd30554c54569d0e2f44a6515eb91568b44fc0735458341ee813",
	"neo4j/scoped/cloud_category":      "011ac80e11008890a237ff7565c8dc07811aee6b28decb6c9e54aac20e8727f6",
	"neo4j/scoped/equality":            "c022d6b7ed0bb4f68ff0740733282be91dfb9b36eac1a4c6f0a66e3caf80286e",
	"neo4j/scoped/free_text":           "59b2d19e69a27c4bec5bddc8b49aef249a274cf9fc35da20b5af194e04404070",
	"neo4j/scoped/structured":          "8ca24aeae6804922737e2aaf3f4f26db104235d8429a9cdd01398c3aef71b47e",
	"nornicdb/unscoped/argocd_only":    "e748810b0efd23fc1f7baaa5ea219bc3298463e981bf4faa747ee6722cb0710b",
	"nornicdb/unscoped/category_kind":  "e63ea821f3f8aa0efa46085d3d4eaf8b831cb0983daee967850311e4d578d355",
	"nornicdb/unscoped/cloud_category": "8d187ebe095faaeddb2a369a26d61b07e9c60809e0648757801ccf54aa1c7093",
	"nornicdb/unscoped/equality":       "89cbc7786e244cceef8a293b43db02f41b3737c9a6f33141b16dd1de0258f86e",
	"nornicdb/unscoped/free_text":      "2e01aa03a3aebabc379a888cebd4f78d3cf625a4e14e402d874a47a63603b4ed",
	"nornicdb/unscoped/structured":     "01192917fac1a40ca1873b574d5363c616ebd0aaa1602478be2dd2703af1bedc",
	"nornicdb/scoped/argocd_only":      "18203f66ff98a41457a971668f56cff6a8e350dedb77fa6a85b479584f5c5202",
	"nornicdb/scoped/category_kind":    "b0d0ab17e233b22786d1c9145be38c525467ada73e708713fc5db92bc6ee224d",
	"nornicdb/scoped/cloud_category":   "5156bbb334e417817988206720936b78e5703efd78b80ca631e682d9b6299909",
	"nornicdb/scoped/equality":         "f62f5b1d77ca18b9d379badb1893eb620c7dbbba77c055714320a0194318defc",
	"nornicdb/scoped/free_text":        "504e5befb8eb12e99230818b79bc95280e0ba2d4c21e6227127a878770c4cc3b",
	"nornicdb/scoped/structured":       "12d5b13acbd0fd09c982d91b4ec4ff3cc27ee3e7099a54a0290ce8aa8b5e6549",
}

// serveInfraSearchLegacy drives one legacy case through the real mounted
// handler, letting the caller configure the handler before the request, and
// returns every graph statement the request issued.
func serveInfraSearchLegacy(t *testing.T, c infraSearchLegacyCase, configure func(*InfraHandler)) []recordedInfraCall {
	t.Helper()
	graph := &dialectRecordingGraph{}
	handler := &InfraHandler{GraphBackend: c.backend, Neo4j: graph}
	if configure != nil {
		configure(handler)
	}
	var auth *AuthContext
	if c.scoped {
		a := dialectGrants()[0].auth()
		auth = &a
	}
	serveInfraSearchRequest(t, handler, auth, c.body)
	return graph.calls
}

// serveInfraSearchRequest sends one search body through handler's mounted
// route, under auth when it is not nil, and fails the test on a non-200.
func serveInfraSearchRequest(t *testing.T, handler *InfraHandler, auth *AuthContext, body string) {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, infraSearchPath, strings.NewReader(body))
	if auth != nil {
		req = req.WithContext(ContextWithAuthContext(req.Context(), *auth))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%.80s: status %d body %.300s", body, rec.Code, rec.Body.String())
	}
}

func TestInfraSearchLegacyStatementsByteIdentical(t *testing.T) {
	t.Parallel()
	for _, c := range infraSearchLegacyCases() {
		got := statementDigest(t, serveInfraSearchLegacy(t, c, nil))
		want, ok := infraSearchLegacyDigests[c.name]
		if !ok {
			t.Errorf("no golden for %s: %q", c.name, got)
			continue
		}
		if got != want {
			t.Errorf("%s digest = %s, want %s", c.name, got, want)
		}
	}
}
