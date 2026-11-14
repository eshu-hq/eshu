// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

// contextEvidenceDetailReader is a counting graph fake: the base repository
// row plus empty auxiliary panels. It records reads so the unknown-value test
// can prove the 400 ran before any read.
type contextEvidenceDetailReader struct {
	reads *int
}

func (r contextEvidenceDetailReader) RunSingle(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
	*r.reads++
	if !strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
		return nil, nil
	}
	return map[string]any{
		"id": "repo-context", "name": "context-service", "path": "/repos/context-service",
		"local_path": "/repos/context-service", "repo_slug": "acme/context-service",
		"remote_url": "https://github.com/acme/context-service.git", "has_remote": true,
	}, nil
}

func (r contextEvidenceDetailReader) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	*r.reads++
	return nil, nil
}

func serveRepositoryContextDetail(t *testing.T, query string, envelope bool) (*httptest.ResponseRecorder, int) {
	t.Helper()

	reads := 0
	handler := &Handler{
		Neo4j:   contextEvidenceDetailReader{reads: &reads},
		Content: content.FakePortContentStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-context/context"+query, nil)
	if envelope {
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	got := reads
	if query == "?evidence_detail=compact" {
		t.Cleanup(func() {
			if got != 0 {
				t.Fatalf("graph reads = %d before the evidence_detail 400, want 0", got)
			}
		})
	}
	return w, got
}

// TestGetRepositoryContextEvidenceDetailParameter proves the context route
// reads evidence_detail like the story route: absent means full, handles is
// recorded on the body, and an unknown value is a 400 before any graph read
// runs.
func TestGetRepositoryContextEvidenceDetailParameter(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ query, want string }{
		{"", "full"},
		{"?evidence_detail=full", "full"},
		{"?evidence_detail=handles", "handles"},
	} {
		w, _ := serveRepositoryContextDetail(t, tc.query, false)
		if w.Code != http.StatusOK {
			t.Fatalf("%q: status = %d, want 200; body = %s", tc.query, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%q: json.Unmarshal: %v", tc.query, err)
		}
		if got := querycontract.StringVal(body, "evidence_detail"); got != tc.want {
			t.Fatalf("%q: evidence_detail = %q, want %q", tc.query, got, tc.want)
		}
	}

	w, _ := serveRepositoryContextDetail(t, "?evidence_detail=compact", false)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown evidence_detail status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

// TestGetRepositoryContextEvidenceDetailDefaultIsFull proves the HTTP default
// is byte-identical to an explicit full.
func TestGetRepositoryContextEvidenceDetailDefaultIsFull(t *testing.T) {
	t.Parallel()

	def, _ := serveRepositoryContextDetail(t, "", false)
	full, _ := serveRepositoryContextDetail(t, "?evidence_detail=full", false)
	if def.Code != http.StatusOK || full.Code != http.StatusOK {
		t.Fatalf("status = %d/%d, want 200/200", def.Code, full.Code)
	}
	if def.Body.String() != full.Body.String() {
		t.Fatal("default response differs from explicit full; want byte-identical")
	}
}

// TestGetRepositoryContextHandlesOmitsNothingOnEmpty proves an empty context
// still shapes to a valid body with no omissions.
func TestGetRepositoryContextHandlesOmitsNothingOnEmpty(t *testing.T) {
	t.Parallel()

	w, _ := serveRepositoryContextDetail(t, "?evidence_detail=handles", true)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := querycontract.StringVal(envelope.Data, "evidence_detail"); got != "handles" {
		t.Fatalf("evidence_detail = %q, want handles", got)
	}
}
