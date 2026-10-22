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
	"neo4j/unscoped/cloud_category":    "a7ba6ae742e1385a8ce5c98737c507b42ff7df7862227abc98da7d701e412837",
	"neo4j/unscoped/equality":          "36be530120d6e5f7812c1e08243fe9a90553efb61d8f4126abdeab4361d09354",
	"neo4j/unscoped/free_text":         "178da3a7576fe14a9276c6200620b61cbf82aae1e4cb0afa7587206ce0733b25",
	"neo4j/unscoped/structured":        "27e2f3eba5be0b10645f65d55d326eb363e8a59c814b59bb96703d815c29b634",
	"neo4j/scoped/argocd_only":         "5452dc9ee860bc94f3071c9c1e3a927c8aabae7706327060f22244f6a770ea6a",
	"neo4j/scoped/category_kind":       "6f4aba4a725bbd30554c54569d0e2f44a6515eb91568b44fc0735458341ee813",
	"neo4j/scoped/cloud_category":      "01ee95006a36b2b64a98f6b3950c621c9e9f9ed2aa39a942ee313ee06c86c331",
	"neo4j/scoped/equality":            "5c944055fca37865e9da8c629efe2528f47923dd71c04da0dabd5bcaa18671b9",
	"neo4j/scoped/free_text":           "94677114df67787430a441a05cdfdc3ff5b82a08a592cd3ed0480de3bea88c43",
	"neo4j/scoped/structured":          "6bae2c85d23032283697962074a23e3bbe400c2340eb80e0f260d1c687a123d1",
	"nornicdb/unscoped/argocd_only":    "e748810b0efd23fc1f7baaa5ea219bc3298463e981bf4faa747ee6722cb0710b",
	"nornicdb/unscoped/category_kind":  "e63ea821f3f8aa0efa46085d3d4eaf8b831cb0983daee967850311e4d578d355",
	"nornicdb/unscoped/cloud_category": "a7ba6ae742e1385a8ce5c98737c507b42ff7df7862227abc98da7d701e412837",
	"nornicdb/unscoped/equality":       "36be530120d6e5f7812c1e08243fe9a90553efb61d8f4126abdeab4361d09354",
	"nornicdb/unscoped/free_text":      "178da3a7576fe14a9276c6200620b61cbf82aae1e4cb0afa7587206ce0733b25",
	"nornicdb/unscoped/structured":     "27e2f3eba5be0b10645f65d55d326eb363e8a59c814b59bb96703d815c29b634",
	"nornicdb/scoped/argocd_only":      "18203f66ff98a41457a971668f56cff6a8e350dedb77fa6a85b479584f5c5202",
	"nornicdb/scoped/category_kind":    "b0d0ab17e233b22786d1c9145be38c525467ada73e708713fc5db92bc6ee224d",
	"nornicdb/scoped/cloud_category":   "0ff2483e3ccead615df3dd040020dc9690c6ce78d1cf78f9b7390376aef74ccb",
	"nornicdb/scoped/equality":         "cdd250f11c55b243a9de71158bbf6c8cb620e4174d562201bbf4fb89623decf0",
	"nornicdb/scoped/free_text":        "ab9adc4f06e7f360630f58ab962de1e893faea4dd5361debbf50e6bf2969b53a",
	"nornicdb/scoped/structured":       "1ab1760f279413131d01177032ac8d42b77ad4bd6c26e25df4b6c789a8a59ed7",
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
