// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// semanticSearchRoute is the path every request this package builds is posted
// to.
//
// It is deliberately NOT bound to the handler's own route literal in
// semanticsearch.Mount — this package must not import a handler family. If the
// two ever diverge, the tests that route through a real mux fail with a 404
// rather than silently exercising a path nothing serves; the ones that call a
// handler directly would not notice, which is a reason to prefer the mux.
const semanticSearchRoute = "/api/v0/search/semantic"

// semanticSearchFixtureUpdatedAt is the fixed UpdatedAt every fixture document
// carries. It is a constant instant, not time.Now(): freshness assertions
// compare against it, and a moving clock would make them pass or fail by wall
// time rather than by the code under test.
var semanticSearchFixtureUpdatedAt = time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

// SemanticSearchDocumentFixture builds one curated search document for a
// semantic-search test. The four parameters are what assertions vary; every
// other field is held fixed so a diff in a response body points at the handler
// rather than at fixture drift.
//
// It lives here rather than in a _test.go file because two packages need it
// and Go never compiles a package's _test.go files into anything another
// package can import: the handler family moved to
// internal/query/semanticsearch for #6060, while root package query's
// session-permission tests still drive that handler as their vehicle. Two
// copies would drift, and a fixture that no longer matches the corpus the
// handler reads keeps passing while proving nothing.
//
// The document is derived truth from the read model, fresh, and scoped to
// repoID — the ordinary shape, so a test asserting on a degraded or
// out-of-scope case must set that case up explicitly rather than inherit it.
func SemanticSearchDocumentFixture(id string, repoID string, title string, contextText string) searchdocs.Document {
	return searchdocs.Document{
		ID:          id,
		RepoID:      repoID,
		SourceKind:  searchdocs.SourceKindRuntimeSummary,
		Title:       title,
		Path:        "docs/runbook.md",
		ContextText: contextText,
		UpdatedAt:   semanticSearchFixtureUpdatedAt,
		TruthScope: searchdocs.TruthScope{
			Level: searchdocs.TruthLevelDerived,
			Basis: searchdocs.TruthBasisReadModel,
		},
		Freshness:   searchdocs.Freshness{State: searchdocs.FreshnessFresh},
		AccessScope: searchdocs.AccessScope{RepoID: repoID},
		GraphHandles: []searchdocs.GraphHandle{
			{Kind: "repository", ID: repoID},
			{Kind: "service", ID: "svc-payments"},
		},
		Labels: []string{"runtime", "payments"},
		Provenance: searchdocs.Provenance{
			SourceTable: "service_runtime_summaries",
			SourceIDs:   []string{id},
		},
	}
}

// SemanticSearchHTTPRequest builds a POST to the semantic-search route with
// body as JSON and the envelope Accept header set.
//
// The Accept header is not optional decoration: without it the handler writes
// the bare error shape instead of the envelope, so a test that omitted it
// would decode an empty ResponseEnvelope and read a real error as no error at
// all. Setting it here means no caller can forget.
//
// Shared for the same reason as SemanticSearchDocumentFixture: the handler
// family and root package query's session-permission tests both post to this
// route from different packages (#6060).
func SemanticSearchHTTPRequest(t *testing.T, body map[string]any) *http.Request {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, semanticSearchRoute, strings.NewReader(string(encoded)))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	return req
}
