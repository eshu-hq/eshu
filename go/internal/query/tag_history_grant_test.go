// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/taghistory"
)

// tagHistoryGrantGraphCall records one GraphQuery.Run invocation.
type tagHistoryGrantGraphCall struct {
	cypher string
	params map[string]any
}

// fakeTagHistoryGrantGraph serves the tag-history read and the BUILT_FROM
// lookup from separate canned row sets, dispatching on the statement text, so
// a handler test can seed the #6564 matrix: granted/other repositories, images
// d1..d4, and tag observations t1..t4 on one image_ref.
type fakeTagHistoryGrantGraph struct {
	tagRows       []map[string]any
	builtFromRows []map[string]any
	builtFromErr  error
	calls         []tagHistoryGrantGraphCall
}

func (f *fakeTagHistoryGrantGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.calls = append(f.calls, tagHistoryGrantGraphCall{cypher: cypher, params: params})
	if strings.Contains(cypher, "BUILT_FROM") {
		if f.builtFromErr != nil {
			return nil, f.builtFromErr
		}
		return f.builtFromRows, nil
	}
	return f.tagRows, nil
}

func (*fakeTagHistoryGrantGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// tagHistoryGrantMatrix is the seed the orchestrator probed on the pinned
// NornicDB: t1 resolves d1 (granted) with previous d2 (other), t2 resolves d2
// (other), t3 resolves d3 (no BUILT_FROM edge), t4 resolves d4 (granted AND
// other).
func tagHistoryGrantMatrix() *fakeTagHistoryGrantGraph {
	return &fakeTagHistoryGrantGraph{
		tagRows: []map[string]any{
			tagHistoryRowMap("t1", "sha256:d1", "sha256:d2", "2026-06-01T00:00:00Z", true),
			tagHistoryRowMap("t2", "sha256:d2", "", "2026-06-02T00:00:00Z", false),
			tagHistoryRowMap("t3", "sha256:d3", "", "2026-06-03T00:00:00Z", false),
			tagHistoryRowMap("t4", "sha256:d4", "", "2026-06-04T00:00:00Z", false),
		},
		builtFromRows: []map[string]any{
			{"digest": "sha256:d1", "repository_id": "repo-granted"},
			{"digest": "sha256:d2", "repository_id": "repo-other"},
			{"digest": "sha256:d4", "repository_id": "repo-granted"},
			{"digest": "sha256:d4", "repository_id": "repo-other"},
		},
	}
}

const tagHistoryGrantTarget = "/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/demo&tag=1.0.0"

// serveTagHistoryAs serves one request against a handler wired the way a
// deployment with a DEK is: cursors sealed with tagHistoryTestCursorKeyring.
// Every served page in these tests uses a fresh handler, so a token that
// round-trips here round-trips across restarts and replicas too.
func serveTagHistoryAs(t *testing.T, graph GraphQuery, auth *AuthContext, target string) *httptest.ResponseRecorder {
	t.Helper()
	return serveTagHistoryWithSealer(t, tagHistoryTestCursorKeyring, graph, auth, target)
}

// serveTagHistoryWithSealer is serveTagHistoryAs with an explicit sealer, so a
// test can serve the no-DEK deployment by passing nil.
func serveTagHistoryWithSealer(
	t *testing.T,
	sealer taghistory.Sealer,
	graph GraphQuery,
	auth *AuthContext,
	target string,
) *httptest.ResponseRecorder {
	t.Helper()
	return serveTagHistoryHandler(
		t,
		&TagHistoryHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative, Cursors: sealer},
		auth,
		target,
	)
}

// serveTagHistoryWithProfile is serveTagHistoryAs against a handler running
// profile, so a test can reach the capability refusal every other helper here
// is deliberately wired to avoid.
func serveTagHistoryWithProfile(
	t *testing.T,
	profile QueryProfile,
	graph GraphQuery,
	auth *AuthContext,
	target string,
) *httptest.ResponseRecorder {
	t.Helper()
	return serveTagHistoryHandler(
		t,
		&TagHistoryHandler{Neo4j: graph, Profile: profile, Cursors: tagHistoryTestCursorKeyring},
		auth,
		target,
	)
}

// serveTagHistoryHandler mounts handler on a fresh mux and serves one request
// as auth -- the part every serve helper above shares.
func serveTagHistoryHandler(
	t *testing.T,
	handler *TagHistoryHandler,
	auth *AuthContext,
	target string,
) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := newTagHistoryRequest(target)
	if auth != nil {
		req = req.WithContext(ContextWithAuthContext(req.Context(), *auth))
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func scopedTagHistoryAuth(repositoryIDs ...string) *AuthContext {
	return &AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant_a",
		WorkspaceID:          "workspace_a",
		AllowedRepositoryIDs: repositoryIDs,
	}
}

// tagHistoryScopedAudience is the cursor audience the handler derives for a
// caller scoped to repositoryIDs, and tagHistoryUnscopedAudience the one it
// derives for a caller with no scope.
//
// Both run the SAME two steps listTagHistory runs -- build the access filter
// from an auth context, then AudienceOf -- rather than reconstructing the
// digest, so a change to either step cannot leave these helpers agreeing with
// a handler that has moved on.
func tagHistoryScopedAudience(repositoryIDs ...string) taghistory.Audience {
	ctx := ContextWithAuthContext(context.Background(), *scopedTagHistoryAuth(repositoryIDs...))
	return taghistory.AudienceOf(querycontract.RepositoryAccessFilterFromContext(ctx).WithCanonicalScopeRepositories())
}

func tagHistoryUnscopedAudience() taghistory.Audience {
	return taghistory.AudienceOf(querycontract.RepositoryAccessFilterFromContext(context.Background()).WithCanonicalScopeRepositories())
}

func tagHistoryResultTags(t *testing.T, data map[string]any) []string {
	t.Helper()
	history, ok := data["tag_history"].([]any)
	if !ok {
		t.Fatalf("tag_history = %#v, want array", data["tag_history"])
	}
	tags := make([]string, 0, len(history))
	for _, entry := range history {
		tags = append(tags, entry.(map[string]any)["tag"].(string))
	}
	return tags
}

// TestTagHistoryScopedCallerKeepsOnlyGrantedBuiltFromRows covers the positive
// (t1), negative (t2), no-edge (t3) and multi-source (t4) cases in one page, and
// proves the grant lookup is ONE single-clause BUILT_FROM read keyed by the
// page's distinct resolved and previous digests.
func TestTagHistoryScopedCallerKeepsOnlyGrantedBuiltFromRows(t *testing.T) {
	t.Parallel()

	graph := tagHistoryGrantMatrix()
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeTagHistoryBody(t, w)

	if got, want := tagHistoryResultTags(t, data), []string{"t1", "t4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped tags = %v, want %v (t2 is BUILT_FROM another tenant, t3 has no BUILT_FROM edge)", got, want)
	}
	if got, want := data["count"], float64(2); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got := data["grant_filtered"]; got != true {
		t.Fatalf("grant_filtered = %#v, want true for a scoped caller", got)
	}
	if len(graph.calls) != 2 {
		t.Fatalf("graph calls = %d, want 2 (tag read + one BUILT_FROM lookup)", len(graph.calls))
	}
	lookup := graph.calls[1]
	if !strings.Contains(lookup.cypher, "BUILT_FROM") {
		t.Fatalf("second read = %q, want the BUILT_FROM lookup", lookup.cypher)
	}
	if got, want := lookup.params["digests"], []string{"sha256:d1", "sha256:d2", "sha256:d3", "sha256:d4"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("BUILT_FROM digests = %#v, want %#v (distinct, sorted resolved + previous digests)", got, want)
	}
	if strings.Count(strings.ToUpper(lookup.cypher), "MATCH") != 1 {
		t.Fatalf("BUILT_FROM lookup must be single-clause (multi-MATCH returned 0 rows on NornicDB): %q", lookup.cypher)
	}
}

// TestTagHistoryScopedCallerBlanksUngrantedPreviousDigest proves t1 keeps its
// row (d1 is granted) but loses previous_digest d2, which only another tenant's
// repository was built into.
func TestTagHistoryScopedCallerBlanksUngrantedPreviousDigest(t *testing.T) {
	t.Parallel()

	w := serveTagHistoryAs(t, tagHistoryGrantMatrix(), scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget)
	data := decodeTagHistoryBody(t, w)
	history := data["tag_history"].([]any)
	if len(history) == 0 {
		t.Fatalf("tag_history empty; body = %s", w.Body.String())
	}
	first := history[0].(map[string]any)
	if got, want := first["tag"], "t1"; got != want {
		t.Fatalf("history[0].tag = %#v, want %#v", got, want)
	}
	if got, present := first["previous_digest"]; present {
		t.Fatalf("history[0].previous_digest = %#v, want omitted (d2 is BUILT_FROM an ungranted repository)", got)
	}
}

// TestTagHistoryScopedCallerKeepsGrantedPreviousDigest is the positive twin: a
// previous digest whose image is BUILT_FROM a granted repository survives.
func TestTagHistoryScopedCallerKeepsGrantedPreviousDigest(t *testing.T) {
	t.Parallel()

	graph := tagHistoryGrantMatrix()
	graph.tagRows = []map[string]any{
		tagHistoryRowMap("t5", "sha256:d4", "sha256:d1", "2026-06-05T00:00:00Z", true),
	}
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget)
	data := decodeTagHistoryBody(t, w)
	history := data["tag_history"].([]any)
	if len(history) != 1 {
		t.Fatalf("tag_history = %d rows, want 1; body = %s", len(history), w.Body.String())
	}
	if got, want := history[0].(map[string]any)["previous_digest"], "sha256:d1"; got != want {
		t.Fatalf("previous_digest = %#v, want %#v", got, want)
	}
}

// TestTagHistoryUnscopedCallerUnchanged proves shared-key, unauthenticated,
// and all-scope callers still issue exactly one statement and get every row,
// with previous_digest intact and no grant_filtered marker.
func TestTagHistoryUnscopedCallerUnchanged(t *testing.T) {
	t.Parallel()

	cases := map[string]*AuthContext{
		"no auth context": nil,
		"all scopes": {
			Mode:        AuthModeScoped,
			TenantID:    "tenant_a",
			WorkspaceID: "workspace_a",
			AllScopes:   true,
		},
	}
	for name, auth := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			graph := tagHistoryGrantMatrix()
			w := serveTagHistoryAs(t, graph, auth, tagHistoryGrantTarget)
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			data := decodeTagHistoryBody(t, w)
			if got, want := tagHistoryResultTags(t, data), []string{"t1", "t2", "t3", "t4"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("tags = %v, want %v", got, want)
			}
			if len(graph.calls) != 1 {
				t.Fatalf("graph calls = %d, want 1 (unscoped path must stay one statement)", len(graph.calls))
			}
			if _, present := data["grant_filtered"]; present {
				t.Fatalf("grant_filtered present for an unscoped caller: %#v", data["grant_filtered"])
			}
			first := data["tag_history"].([]any)[0].(map[string]any)
			if got, want := first["previous_digest"], "sha256:d2"; got != want {
				t.Fatalf("previous_digest = %#v, want %#v", got, want)
			}
		})
	}
}

// TestTagHistoryScopedCallerEmptyPageSkipsBuiltFromRead proves an empty tag
// page issues no BUILT_FROM lookup.
func TestTagHistoryScopedCallerEmptyPageSkipsBuiltFromRead(t *testing.T) {
	t.Parallel()

	graph := tagHistoryGrantMatrix()
	graph.tagRows = nil
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeTagHistoryBody(t, w)
	if got, want := data["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if len(graph.calls) != 1 {
		t.Fatalf("graph calls = %d, want 1 (no BUILT_FROM lookup for an empty page)", len(graph.calls))
	}
}

// TestTagHistoryScopedCallerEmptyGrantShortCircuits proves a scoped caller
// holding no grant gets an empty page without any graph read.
func TestTagHistoryScopedCallerEmptyGrantShortCircuits(t *testing.T) {
	t.Parallel()

	graph := tagHistoryGrantMatrix()
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth(), tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeTagHistoryBody(t, w)
	if got, want := data["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got := data["truncated"]; got != false {
		t.Fatalf("truncated = %#v, want false", got)
	}
	if len(graph.calls) != 0 {
		t.Fatalf("graph calls = %d, want 0 for an empty grant", len(graph.calls))
	}
}

// TestTagHistoryScopedCallerBuiltFromErrorFailsClosed proves a failure of the
// second read never falls back to serving the unfiltered page.
func TestTagHistoryScopedCallerBuiltFromErrorFailsClosed(t *testing.T) {
	t.Parallel()

	graph := tagHistoryGrantMatrix()
	graph.builtFromErr = errors.New("boom")
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sha256:d2") {
		t.Fatalf("error body leaked an ungranted digest: %s", w.Body.String())
	}
}

// TestTagHistoryScopedWindowExcludesLimitPlusOneSentinel proves each refill
// window reads MaxLimit+1 rows only to learn whether history continues: the
// sentinel row is trimmed before the grant filter, so it is never looked up,
// never counted, never kept, and never named by a cursor.
//
// The window is MaxLimit-sized regardless of the caller's limit (#6564
// re-review finding 1, RefillScopedPage), which is why this seeds MaxLimit+1
// rows at limit=2 rather than 3: the sentinel is the 201st row, not the 3rd.
//
// This replaces TestTagHistoryScopedCallerTruncationFollowsUnfilteredWindow,
// which pinned the pre-#6564-review contract where truncated and next_cursor
// came from the raw pre-filter window. That contract is gone: a scoped page is
// now refilled (tag_history_refill_test.go).
func TestTagHistoryScopedWindowExcludesLimitPlusOneSentinel(t *testing.T) {
	t.Parallel()

	graph := &fakeTagHistoryGrantGraph{}
	for i := 0; i <= taghistory.MaxLimit; i++ {
		digest := fmt.Sprintf("sha256:s%04d", i)
		graph.tagRows = append(graph.tagRows, tagHistoryRowMap(
			fmt.Sprintf("s%04d", i), digest, "", fmt.Sprintf("17600000%05d", i), false,
		))
		graph.builtFromRows = append(graph.builtFromRows, map[string]any{
			"digest": digest, "repository_id": "repo-granted",
		})
	}
	sentinel := fmt.Sprintf("sha256:s%04d", taghistory.MaxLimit)

	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=2")
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := graph.calls[0].params["limit"], taghistory.MaxLimit+1; got != want {
		t.Fatalf("tag read limit = %#v, want %#v (MaxLimit+1 sentinel)", got, want)
	}
	digests, ok := graph.calls[1].params["digests"].([]string)
	if !ok {
		t.Fatalf("BUILT_FROM digests = %#v, want []string", graph.calls[1].params["digests"])
	}
	if got, want := len(digests), taghistory.MaxLimit; got != want {
		t.Fatalf("BUILT_FROM keys = %d, want %d (the window minus its sentinel row)", got, want)
	}
	if slices.Contains(digests, sentinel) {
		t.Fatalf("BUILT_FROM digests include the sentinel row %q, which must be trimmed before the grant filter", sentinel)
	}
	if got, want := tagHistoryResultTags(t, decodeTagHistoryBody(t, w)), []string{"s0000", "s0001"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
}

// TestTagHistoryRouteIsGrantBoundAllowlisted proves the route left the #5167
// pending ledger for the scoped-token allowlist as a grant-bound route.
func TestTagHistoryRouteIsGrantBoundAllowlisted(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/v0/images/tag-history", nil)
	if !scopedHTTPRouteSupportsTenantFilter(req) {
		t.Fatal("scopedHTTPRouteSupportsTenantFilter(tag-history) = false, want true")
	}
	if IsPendingRowFilteringRoute(req) {
		t.Fatal("tag-history is still on pendingRowFilteringRoutes")
	}
	class, ok := scopedTokenAdvertisedRoutes["GET /api/v0/images/tag-history"]
	if !ok || class != scopedRouteGrantBound {
		t.Fatalf("scopedTokenAdvertisedRoutes[tag-history] = (%d, %v), want (scopedRouteGrantBound, true)", class, ok)
	}
}
