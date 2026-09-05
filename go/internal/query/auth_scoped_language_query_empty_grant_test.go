// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"testing"
)

// #5167 code-family batch 2a: what POST /api/v0/code/language-query answers
// when the caller's grant admits nothing, and what a canonical repo_id does on
// the way in. Split out of auth_scoped_language_query_grant_test.go, which the
// truth-envelope assertion below pushed over the repository's 500-line file
// cap; the fixtures and handler helpers still live in that file.

// TestLanguageQueryEmptyGrantAnswersWithArraysNotNull is the batch-1
// TestCodeRoutesEmptyGrantAnswersWithArraysNotNull rule on this route: the
// empty-grant short-circuit is the one path that never runs the loop building
// the results slice, and a nil slice serializes as `null` where the OpenAPI
// schema declares an array.
func TestLanguageQueryEmptyGrantAnswersWithArraysNotNull(t *testing.T) {
	t.Parallel()

	for _, branch := range languageQueryGrantBranches() {
		t.Run(branch.name, func(t *testing.T) {
			t.Parallel()

			handler, _ := newLanguageQueryGrantHandler(branch, &languageQueryPlainContentStore{})
			auth := codeGrantScopedAuthContext(nil)
			rec := runLanguageQueryGrantRequest(t, handler, languageQueryGrantBody(branch.entityType), &auth)

			data := decodeEnvelopeData(t, rec.Body.Bytes())
			value, ok := data["results"]
			if !ok {
				t.Fatalf("response has no results field: %s", rec.Body.String())
			}
			rows, ok := value.([]any)
			if !ok {
				t.Fatalf("results = %#v, want an empty JSON array, not null: %s", value, rec.Body.String())
			}
			if len(rows) != 0 {
				t.Fatalf("results = %#v, want no rows for a grantless caller", rows)
			}

			// The empty page is NOT indistinguishable from a granted search
			// that matched nothing: the truth envelope's reason names the
			// grantless case, and language-query-dsl.md tells callers so.
			//
			// The wanted text is written out here rather than compared against
			// reasonLanguageQueryEmptyGrant on purpose. Comparing the response
			// to the same constant that produced it passes whatever the
			// constant says -- including "no results", which was tried, and
			// which the documented contract does NOT allow. A literal is what
			// makes a reword red this test and send whoever rewords it to the
			// page that promises the old words.
			var envelope struct {
				Truth struct {
					Reason string `json:"reason"`
				} `json:"truth"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode the truth envelope: %v; body = %s", err, rec.Body.String())
			}
			const wantReason = "the caller's grant admits no repository, so no backend was read"
			if got := envelope.Truth.Reason; got != wantReason {
				t.Fatalf("truth.reason = %q, want %q; the grantless page must name its own case, and "+
					"docs/public/reference/language-query-dsl.md quotes this sentence to callers", got, wantReason)
			}
			// This page reads nothing, so it must not name a backend that
			// served it. Deriving source_backend from the basis answers
			// postgres_content_store here, which is why the empty page does not
			// go through writeLanguageQueryResult. Written out, not read from
			// languageQueryNoBackendRead: an expectation taken from the value
			// under test passes whatever that value becomes.
			if got, want := data["source_backend"], "unavailable"; got != want {
				t.Fatalf("source_backend = %v, want %q; no backend served this page", got, want)
			}
		})
	}
}

// TestLanguageQueryCanonicalRepoIDIsTakenAsGiven pins the half of the
// selector contract that language-query-dsl.md's error table describes and
// nothing else covered: a canonical id is never resolved, so an UNSCOPED
// caller naming one that is not indexed gets the route's ordinary empty page
// rather than the 400 a non-canonical selector would earn.
//
// queryselector.LooksCanonicalRepositoryID is what makes the two differ, and
// the difference is caller-class-sensitive: for a SCOPED token the same
// canonical id is still checked against the grant, which is
// TestLanguageQueryUngrantedRepositorySelectorIsRejected's case (it passes
// codeGrantOtherRepo, itself a `repo://` id, and asserts 400). Both halves of
// the page's row therefore have a test.
func TestLanguageQueryCanonicalRepoIDIsTakenAsGiven(t *testing.T) {
	t.Parallel()

	store := &languageQueryPlainContentStore{}
	handler := &LanguageQueryHandler{Content: store, Profile: ProfileLocalAuthoritative}
	body := languageQueryGrantBody("variable")
	body["repo_id"] = "repo://never-indexed/service"
	rec := runLanguageQueryGrantRequest(t, handler, body, nil)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; a canonical repo_id is taken as given, not resolved, so an "+
			"unindexed one must not become the 400 a non-canonical selector earns; body = %s", got, want, rec.Body.String())
	}
	data := decodeEnvelopeData(t, rec.Body.Bytes())
	rows, ok := data["results"].([]any)
	if !ok {
		t.Fatalf("results = %#v, want an empty JSON array: %s", data["results"], rec.Body.String())
	}
	if len(rows) != 0 {
		t.Fatalf("results = %#v, want no rows for a repository that is not indexed", rows)
	}
}
