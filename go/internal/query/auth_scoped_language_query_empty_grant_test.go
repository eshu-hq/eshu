// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
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
			auth := querytestutil.CodeGrantScopedAuthContext(nil)
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
			// reasonEmptyGrantNoBackendRead on purpose. Comparing the response
			// to the same constant that produced it passes whatever the
			// constant says -- including "no results", which was tried, and
			// which the documented contract does NOT allow. A literal is what
			// makes a reword red this test and send whoever rewords it to the
			// page that promises the old words.
			var envelope struct {
				Truth struct {
					Basis  string `json:"basis"`
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
			// postgres_content_store for a content_index page, which is why the
			// empty page carries its own no-read basis instead (#6544). Written
			// out, not read from noBackendReadSourceBackend: an expectation
			// taken from the value under test passes whatever that value
			// becomes.
			if got, want := data["source_backend"], "no_backend_read"; got != want {
				t.Fatalf("source_backend = %v, want %q; no backend served this page", got, want)
			}
			// The basis says the same thing as source_backend rather than
			// borrowing content_index, which claimed a content-store read this
			// page never issued (#6544). Without this assertion the basis could
			// regress to authoritative_graph and everything above would still
			// pass.
			if got, want := envelope.Truth.Basis, "no_backend_read"; got != want {
				t.Fatalf("truth.basis = %q, want %q; an unread page must not claim a backend it never read", got, want)
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

// TestLanguageQueryEmptyGrantWithRepoIDIsRejectedNotAnsweredEmpty pins the one
// boundary the `no_backend_read` truth basis rests on: an empty grant carrying a
// non-empty repo_id must be REJECTED in the selector, and must never reach the
// empty page.
//
// Without this the label can quietly become a lie. `no_backend_read` asserts
// that nothing was read, and the empty-grant page is only reachable with an
// empty repo_id, where queryselector.ResolveExactForAccess short-circuits with
// zero reads. Selector resolution itself DOES read (content.MatchRepositories
// plus two Cypher lookups), so if anyone later relaxes the fail-closed
// behaviour and lets an unresolvable selector fall through to the empty page
// instead of erroring, the page would be answered AFTER a read while still
// claiming no backend was touched.
//
// The existing coverage does not reach this: the 400 test uses a NON-empty
// grant (codeGrantOtherRepo), and the empty-grant tests send NO repo_id. This
// covers the intersection, on both code routes, for a canonical and a
// non-canonical id — they take different paths in, and both must end at 400.
// Raised as a P2 in review of PR #6602.
func TestLanguageQueryEmptyGrantWithRepoIDIsRejectedNotAnsweredEmpty(t *testing.T) {
	t.Parallel()

	repoIDs := map[string]string{
		// Canonical: LooksCanonicalRepositoryID is true, so it is checked
		// against the grant rather than resolved.
		"canonical": "repo://live-alpha/service",
		// Non-canonical: runs the grant-filtered lookup, which yields nothing
		// under an empty grant and must still end in NotFoundError.
		"non_canonical": "some-service",
	}

	for _, branch := range languageQueryGrantBranches() {
		for idKind, repoID := range repoIDs {
			t.Run(branch.name+"/"+idKind, func(t *testing.T) {
				t.Parallel()

				handler, _ := newLanguageQueryGrantHandler(branch, &languageQueryPlainContentStore{})
				auth := querytestutil.CodeGrantScopedAuthContext(nil)
				body := languageQueryGrantBody(branch.entityType)
				body["repo_id"] = repoID
				rec := runLanguageQueryGrantRequest(t, handler, body, &auth)

				if got := rec.Code; got != http.StatusBadRequest {
					t.Fatalf("status = %d, want %d; an empty grant naming %s repo_id %q must be rejected "+
						"in the selector, never answered as the empty no_backend_read page (a page answered "+
						"after a selector read would make that basis a lie); body = %s",
						got, http.StatusBadRequest, idKind, repoID, rec.Body.String())
				}

				// Belt and braces: a 400 body must not carry the empty-page
				// shape, so a future refactor cannot satisfy the status check
				// while still serving the page.
				var envelope map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
					return // a non-JSON error body is fine; only the page shape is forbidden
				}
				data, ok := envelope["data"].(map[string]any)
				if !ok {
					return
				}
				if _, hasResults := data["results"]; hasResults {
					t.Fatalf("rejected request still returned a results page: %s", rec.Body.String())
				}
			})
		}
	}
}
