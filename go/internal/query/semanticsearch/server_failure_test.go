// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticsearch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

const (
	semanticSearchPath        = "/api/v0/search/semantic"
	semanticSearchKeywordBody = `{"repo_id":"repo-payments","query":"payment","mode":"keyword","limit":5,"timeout_ms":250}`
)

// TestSemanticSearchAnswersFixedServerFailures is the #7674 regression for
// POST /api/v0/search/semantic. A failed scope resolution answered 503 with
// the resolver's error text, a failed index retrieval answered 500 with the
// wrapped backend text, both through the package's own writer, which the
// error text guard could not see, and a client cancel answered like a server
// fault. Both reads go through the guarded PostgreSQL reader, so a stale
// reader answers the retryable 503. The route opens its own handler span, so
// the suite swaps semanticSearchTracer and must not run in parallel.
func TestSemanticSearchAnswersFixedServerFailures(t *testing.T) {
	failure.Run(t, failure.Suite{HandlerTracer: &semanticSearchTracer, Routes: []failure.Route{
		{
			Name: "scope resolution", Method: http.MethodPost, Path: semanticSearchPath, Body: semanticSearchKeywordBody,
			Message: semanticSearchScopeResolutionFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &SemanticSearchHandler{
					Index:         &fakeSemanticSearchIndexStore{},
					ScopeResolver: &fakeSemanticSearchScopeResolver{err: err},
					Profile:       querycontract.ProfileProduction,
				}
			},
		},
		{
			Name: "index retrieval", Method: http.MethodPost, Path: semanticSearchPath, Body: semanticSearchKeywordBody,
			Message: semanticSearchRetrievalFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &SemanticSearchHandler{
					Index:         &fakeSemanticSearchIndexStore{err: err},
					ScopeResolver: &fakeSemanticSearchScopeResolver{scopeID: "git-repository-scope:repo-payments"},
					Profile:       querycontract.ProfileProduction,
				}
			},
		},
		{
			Name: "local hybrid retrieval", Method: http.MethodPost, Path: semanticSearchPath,
			Body:    `{"repo_id":"repo-payments","query":"payment","mode":"hybrid","limit":5,"timeout_ms":250}`,
			Message: semanticSearchRetrievalFailedMessage,
			Handler: func(err error) failure.Mounter {
				return &SemanticSearchHandler{
					Index:       &fakeSemanticSearchIndexStore{},
					LocalHybrid: &fakeSemanticSearchIndexStore{err: err},
					Profile:     querycontract.ProfileProduction,
				}
			},
		},
	}})
}

// serveSemanticSearch posts body to the route through a real mux, with the
// envelope media type when envelope is true.
func serveSemanticSearch(t *testing.T, handler *SemanticSearchHandler, body string, envelope bool) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, semanticSearchPath, strings.NewReader(body))
	if envelope {
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// semanticSearchErrorBody decodes the route's plain error shape.
type semanticSearchErrorBody struct {
	ErrorCode  querycontract.ErrorCode `json:"error_code"`
	Message    string                  `json:"message"`
	Capability string                  `json:"capability"`
}

// TestSemanticSearchServerFailureKeepsThePlainShape pins that a caller that
// does not negotiate the envelope still gets the route's own plain error
// shape for a server fault, with only the message fixed.
func TestSemanticSearchServerFailureKeepsThePlainShape(t *testing.T) {
	t.Parallel()
	rec := serveSemanticSearch(t, &SemanticSearchHandler{
		Index:   &fakeSemanticSearchIndexStore{err: fmt.Errorf("backend: %s", failure.Canary)},
		Profile: querycontract.ProfileProduction,
	}, semanticSearchKeywordBody, false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	var body semanticSearchErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	want := semanticSearchErrorBody{querycontract.ErrorCodeInternalError, semanticSearchRetrievalFailedMessage, Capability}
	if body != want {
		t.Fatalf("body = %+v, want %+v", body, want)
	}
}

// TestSemanticSearchConfigurationVerdictsAnswerFixed503 pins the two
// configuration 503s: semantic mode with no embedder wired, and no persisted
// index wired. Each answers backend_unavailable with its fixed text and no
// Retry-After, because retrying cannot help until an operator wires it.
func TestSemanticSearchConfigurationVerdictsAnswerFixed503(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		handler *SemanticSearchHandler
		body    string
		want    string
	}{
		{
			name:    "semantic mode without an embedder",
			handler: &SemanticSearchHandler{Index: &fakeSemanticSearchIndexStore{}, Profile: querycontract.ProfileProduction},
			body:    `{"repo_id":"repo-payments","query":"payment","mode":"semantic","limit":5,"timeout_ms":250}`,
			want:    semanticSearchEmbedderRequiredMessage,
		},
		{
			name:    "no persisted index",
			handler: &SemanticSearchHandler{Profile: querycontract.ProfileProduction},
			body:    semanticSearchKeywordBody,
			want:    semanticSearchIndexNotConfiguredMessage,
		},
	} {
		rec := serveSemanticSearch(t, tc.handler, tc.body, true)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503; body = %s", tc.name, rec.Code, rec.Body.String())
			continue
		}
		var envelope querycontract.ResponseEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Error == nil {
			t.Errorf("%s: body = %s (decode err %v)", tc.name, rec.Body.String(), err)
			continue
		}
		if envelope.Error.Code != querycontract.ErrorCodeBackendUnavailable || envelope.Error.Message != tc.want {
			t.Errorf("%s: error = %+v, want backend_unavailable %q", tc.name, envelope.Error, tc.want)
		}
		if got := rec.Header().Get("Retry-After"); got != "" {
			t.Errorf("%s: Retry-After = %q, want none for a configuration 503", tc.name, got)
		}
	}
}

// TestSemanticSearchAmbiguousScopeAnswersSentinelText pins the 409: it names
// only the ambiguity sentinel, never the text an error wrapping it carries.
func TestSemanticSearchAmbiguousScopeAnswersSentinelText(t *testing.T) {
	t.Parallel()
	rec := serveSemanticSearch(t, &SemanticSearchHandler{
		Index: &fakeSemanticSearchIndexStore{},
		ScopeResolver: &fakeSemanticSearchScopeResolver{
			err: fmt.Errorf("resolver: %s: %w", failure.Canary, ErrSemanticSearchScopeAmbiguous),
		},
		Profile: querycontract.ProfileProduction,
	}, semanticSearchKeywordBody, true)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
	if got, want := failure.Message(rec.Body.Bytes()), ErrSemanticSearchScopeAmbiguous.Error(); got != want {
		t.Fatalf("body message = %q, want %q", got, want)
	}
}

// TestSemanticSearchScopeResolutionFailureAnswers500Not503 pins a wire
// change (#7674): a scope-resolver failure that is neither a reader fence nor
// an ambiguity answered 503 backend_unavailable, which reads as an
// availability signal. It is a server-side fault, so it answers 500
// internal_error.
func TestSemanticSearchScopeResolutionFailureAnswers500Not503(t *testing.T) {
	t.Parallel()
	rec := serveSemanticSearch(t, &SemanticSearchHandler{
		Index:         &fakeSemanticSearchIndexStore{},
		ScopeResolver: &fakeSemanticSearchScopeResolver{err: fmt.Errorf("permission denied for relation ingestion_scopes")},
		Profile:       querycontract.ProfileProduction,
	}, semanticSearchKeywordBody, true)
	if rec.Code == http.StatusServiceUnavailable {
		t.Fatalf("status = 503, the pre-#7674 answer; want 500; body = %s", rec.Body.String())
	}
	assertSemanticSearchInternalError(t, rec, semanticSearchScopeResolutionFailedMessage)
}

// TestSemanticSearchResultNormalizationFailureAnswers500Not400 pins a wire
// change (#7674): a candidate the index returned that fails response
// normalization answered 400 invalid_argument quoting the stored document id.
// The document comes from the server's own index, not the request, so the
// fault is the server's: it answers 500 internal_error with the fixed text.
func TestSemanticSearchResultNormalizationFailureAnswers500Not400(t *testing.T) {
	t.Parallel()
	const storedID = "doc-stored-canary_secret_table"
	index := &fakeSemanticSearchIndexStore{result: SemanticSearchIndexResult{Candidates: []searchretrieval.Candidate{{
		Document: searchdocs.Document{ID: storedID, RepoID: "repo-payments"},
		Score:    1,
	}}}}
	rec := serveSemanticSearch(t, &SemanticSearchHandler{Index: index, Profile: querycontract.ProfileProduction},
		semanticSearchKeywordBody, true)
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("status = 400, the pre-#7674 answer; want 500; body = %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), storedID) {
		t.Fatalf("body quotes the stored document id: %s", rec.Body.String())
	}
	assertSemanticSearchInternalError(t, rec, semanticSearchRetrievalFailedMessage)
}

// assertSemanticSearchInternalError checks for a 500 internal_error envelope
// carrying message and the route capability.
func assertSemanticSearchInternalError(t *testing.T, rec *httptest.ResponseRecorder, message string) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body = %s", rec.Code, rec.Body.String())
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Error == nil {
		t.Fatalf("body = %s (decode err %v)", rec.Body.String(), err)
	}
	if envelope.Error.Code != querycontract.ErrorCodeInternalError || envelope.Error.Message != message ||
		envelope.Error.Capability != Capability {
		t.Fatalf("error = %+v, want internal_error %q under %s", envelope.Error, message, Capability)
	}
}
