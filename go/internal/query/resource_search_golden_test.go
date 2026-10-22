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
	"neo4j/unscoped/cloud_category":    "9ec7dce594f4f7d9f146a908f255b058f92845b21b684330e2024eab86dc637c",
	"neo4j/unscoped/equality":          "074fde02d3c2e059b13fc77cfec8f0988245a34896171ccdc14096cb3840db41",
	"neo4j/unscoped/free_text":         "0c5906d1ab1467af9c3c61217ff85bcdb991899591a1d4d741887a3aeade1e3c",
	"neo4j/unscoped/structured":        "1ea587744cd936e26afe5fd7784e77ab6c56faf0598880c4cccb934eada139dc",
	"neo4j/scoped/argocd_only":         "5452dc9ee860bc94f3071c9c1e3a927c8aabae7706327060f22244f6a770ea6a",
	"neo4j/scoped/category_kind":       "6f4aba4a725bbd30554c54569d0e2f44a6515eb91568b44fc0735458341ee813",
	"neo4j/scoped/cloud_category":      "f038147097f9cfef4bd1e33ea8ae6195eb30283b1e1131b86d2b725988f749bc",
	"neo4j/scoped/equality":            "37d9de071d4a4444370c3259c89d298f7f1ad68f74803a0666cb831ef4f578c8",
	"neo4j/scoped/free_text":           "87f8b5bf7872ab3d2c33b8efb08bd94eb25d26e9dd07c79490520d316f6efa08",
	"neo4j/scoped/structured":          "040e2cacc26cfe5b381382c3ba64cb2c92c7f6868cc1d6cbb640549f692bc632",
	"nornicdb/unscoped/argocd_only":    "e748810b0efd23fc1f7baaa5ea219bc3298463e981bf4faa747ee6722cb0710b",
	"nornicdb/unscoped/category_kind":  "e63ea821f3f8aa0efa46085d3d4eaf8b831cb0983daee967850311e4d578d355",
	"nornicdb/unscoped/cloud_category": "9ec7dce594f4f7d9f146a908f255b058f92845b21b684330e2024eab86dc637c",
	"nornicdb/unscoped/equality":       "074fde02d3c2e059b13fc77cfec8f0988245a34896171ccdc14096cb3840db41",
	"nornicdb/unscoped/free_text":      "0c5906d1ab1467af9c3c61217ff85bcdb991899591a1d4d741887a3aeade1e3c",
	"nornicdb/unscoped/structured":     "1ea587744cd936e26afe5fd7784e77ab6c56faf0598880c4cccb934eada139dc",
	"nornicdb/scoped/argocd_only":      "18203f66ff98a41457a971668f56cff6a8e350dedb77fa6a85b479584f5c5202",
	"nornicdb/scoped/category_kind":    "b0d0ab17e233b22786d1c9145be38c525467ada73e708713fc5db92bc6ee224d",
	"nornicdb/scoped/cloud_category":   "ce1652b91617b15d743d33fc4d1c06ce01e05aae376df9b889f215e791fa84a4",
	"nornicdb/scoped/equality":         "5451d53e303387e9051ac5515b26b59e995e501a876e3aa8f1b6fe7ea284ba50",
	"nornicdb/scoped/free_text":        "ef0e320d76abec70ffa669547e0b809d20d6361c89a2585b24fe06dda9f2b807",
	"nornicdb/scoped/structured":       "18c24ae983b56ccd2f32c83bd87a529a036f3caab1e7ce91124ae12c585db9bf",
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
