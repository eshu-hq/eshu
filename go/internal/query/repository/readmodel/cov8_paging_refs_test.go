// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package readmodel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
)

// TestCov8ListPageFromRequest pins limit/offset clamping: defaults,
// negatives, and over-limit values resolve silently in range.
func TestCov8ListPageFromRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		target     string
		wantLimit  int
		wantOffset int
	}{
		{"defaults", "/repos", ListDefaultLimit, 0},
		{"explicit", "/repos?limit=10&offset=20", 10, 20},
		{"zero limit", "/repos?limit=0", ListDefaultLimit, 0},
		{"negative limit", "/repos?limit=-5", ListDefaultLimit, 0},
		{"over max limit", "/repos?limit=99999", ListMaxLimit, 0},
		{"negative offset", "/repos?offset=-3", ListDefaultLimit, 0},
		{"over max offset", "/repos?offset=99999999", ListDefaultLimit, ListMaxOffset},
		{"non-numeric", "/repos?limit=abc&offset=xyz", ListDefaultLimit, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ListPageFromRequest(httptest.NewRequest("GET", tc.target, nil))
			if got.Limit != tc.wantLimit || got.Offset != tc.wantOffset {
				t.Errorf("ListPageFromRequest(%q) = %+v, want limit=%d offset=%d",
					tc.target, got, tc.wantLimit, tc.wantOffset)
			}
		})
	}
}

// TestCov8ListResponseAndPaging pins the paged envelope and the
// (name, id) window slicer, including the past-the-end empty page.
func TestCov8ListResponseAndPaging(t *testing.T) {
	t.Parallel()

	repos := []map[string]any{
		{"id": "b", "name": "same"},
		{"id": "a", "name": "same"},
		{"id": "c", "name": "aaa"},
	}
	page, truncated := PageRepositoryMaps(repos, ListPage{Limit: 2, Offset: 0})
	if truncated != true || len(page) != 2 {
		t.Fatalf("first window = %d repos truncated=%v, want 2 true", len(page), truncated)
	}
	if page[0]["id"] != "c" || page[1]["id"] != "a" {
		t.Errorf("window order = %v, want [c a] (name, then id)", page)
	}
	// The input slice is sorted in place.
	if repos[0]["id"] != "c" {
		t.Errorf("repos[0] after paging = %v, want id c", repos[0])
	}

	rest, truncated := PageRepositoryMaps(repos, ListPage{Limit: 2, Offset: 2})
	if truncated || len(rest) != 1 || rest[0]["id"] != "b" {
		t.Errorf("second window = %v truncated=%v, want [b] false", rest, truncated)
	}
	past, truncated := PageRepositoryMaps(repos, ListPage{Limit: 2, Offset: 99})
	if truncated || len(past) != 0 {
		t.Errorf("past-the-end window = %v truncated=%v, want empty false", past, truncated)
	}

	env := ListResponse(page, ListPage{Limit: 2, Offset: 0}, true, 3)
	if env["count"] != 2 || env["total"] != 3 || env["limit"] != 2 ||
		env["offset"] != 0 || env["truncated"] != true {
		t.Errorf("ListResponse() = %v, want count=2 total=3 limit=2 offset=0 truncated=true", env)
	}
	if got, ok := env["repositories"].([]map[string]any); !ok || len(got) != 2 {
		t.Errorf("ListResponse() repositories = %v, want the 2-row page", env["repositories"])
	}
}

// encodeCov8Cursor renders an arbitrary cursor payload so negative decode
// shapes (old versions, unknown kinds) can be constructed directly.
func encodeCov8Cursor(t *testing.T, version int, repoID, kind, name string) string {
	t.Helper()
	raw, err := json.Marshal(RefPageCursor{Version: version, RepoID: repoID, Kind: kind, Name: name})
	if err != nil {
		t.Fatalf("Marshal(cursor) = %v, want nil", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// TestCov8RefCursorRoundTrip pins the opaque page token: encode/decode
// round-trips, and every malformed shape errors instead of resetting.
func TestCov8RefCursorRoundTrip(t *testing.T) {
	t.Parallel()

	ref := repository.RepositoryRef{Kind: "branch", Name: "main"}
	token := EncodeRepositoryRefPageCursor("repo-1", ref)
	cursor, err := DecodeRepositoryRefPageCursor(token, "repo-1")
	if err != nil {
		t.Fatalf("Decode(Encode()) = %v, want nil", err)
	}
	if cursor.Version != RefPageCursorVersion || cursor.Kind != "branch" ||
		cursor.Name != "main" || cursor.RepoID != "repo-1" {
		t.Errorf("decoded cursor = %+v, want the encoded key", cursor)
	}

	if _, err := DecodeRepositoryRefPageCursor("!!!not-base64!!!", "repo-1"); err == nil {
		t.Error("Decode(bad base64) = nil, want error")
	}
	if _, err := DecodeRepositoryRefPageCursor("aGk=", "repo-1"); err == nil {
		t.Error("Decode(bad json) = nil, want error")
	}
	// A v1-versioned cursor (see the RefPageCursorVersion history) is
	// rejected so a client restarts from page 1.
	v1 := encodeCov8Cursor(t, 1, "repo-1", "branch", "main")
	if _, err := DecodeRepositoryRefPageCursor(v1, "repo-1"); err == nil {
		t.Error("Decode(v1 cursor) = nil, want unsupported-version error")
	}
	unknownKind := encodeCov8Cursor(t, RefPageCursorVersion, "repo-1", "note", "x")
	if _, err := DecodeRepositoryRefPageCursor(unknownKind, "repo-1"); err == nil {
		t.Error("Decode(unknown kind) = nil, want error")
	}
	if _, err := DecodeRepositoryRefPageCursor(token, "repo-2"); err == nil {
		t.Error("Decode(cross-repo cursor) = nil, want mismatch error")
	}
}

// TestCov8RefPagingOrder pins the immutable (kind, name) comparator and
// the keyset window: branches precede tags, windows chain without
// skip or repeat, and entries split by kind.
func TestCov8RefPagingOrder(t *testing.T) {
	t.Parallel()

	a := RefPageCursor{Kind: "branch", Name: "b"}
	b := RefPageCursor{Kind: "branch", Name: "a"}
	tag := RefPageCursor{Kind: "tag", Name: "a"}
	if !RefKeyLess(b, a) || RefKeyLess(a, b) {
		t.Error("RefKeyLess must order names ascending within a kind")
	}
	if !RefKeyLess(a, tag) || RefKeyLess(tag, a) {
		t.Error("RefKeyLess must order every branch before every tag")
	}
	if RefKeyLess(a, a) {
		t.Error("RefKeyLess must be strict")
	}
	key := RefSortKey(repository.RepositoryRef{Kind: "tag", Name: "v1"})
	if key.Kind != "tag" || key.Name != "v1" {
		t.Errorf("RefSortKey() = %+v, want tag/v1", key)
	}

	refs := []repository.RepositoryRef{
		{Kind: "tag", Name: "v2"},
		{Kind: "branch", Name: "main"},
		{Kind: "tag", Name: "v1"},
		{Kind: "branch", Name: "dev"},
	}
	SortRepositoryRefsForPaging(refs)
	wantOrder := []string{"branch/dev", "branch/main", "tag/v1", "tag/v2"}
	for i, want := range wantOrder {
		if got := refs[i].Kind + "/" + refs[i].Name; got != want {
			t.Fatalf("sorted refs = %v, want %v", refs, wantOrder)
		}
	}

	window, remainder, truncated, next := RefPageWindow("repo-1", refs, nil, 3)
	if len(window) != 3 || len(remainder) != 1 || !truncated || next == "" {
		t.Fatalf("first window = %d+%d truncated=%v next=%q, want 3+1 true token",
			len(window), len(remainder), truncated, next)
	}
	cursor, err := DecodeRepositoryRefPageCursor(next, "repo-1")
	if err != nil {
		t.Fatalf("Decode(next) = %v, want nil", err)
	}
	window2, remainder2, truncated2, next2 := RefPageWindow("repo-1", refs, &cursor, 3)
	if len(window2) != 1 || len(remainder2) != 0 || truncated2 || next2 != "" {
		t.Fatalf("second window = %d+%d truncated=%v next=%q, want 1+0 false empty",
			len(window2), len(remainder2), truncated2, next2)
	}
	if window2[0].Name != "v2" {
		t.Errorf("second window = %v, want the tag v2 tail", window2)
	}

	branches, tags := RefWindowEntries(window)
	if len(branches) != 2 || len(tags) != 1 {
		t.Errorf("RefWindowEntries(2 branches + 1 tag) = %d+%d, want 2+1", len(branches), len(tags))
	}
	if !RefsContainTag(remainder) || RefsContainTag(window[:2]) {
		t.Error("RefsContainTag must see the tag tail only")
	}
	if RefsContainTag(nil) {
		t.Error("RefsContainTag(nil) = true, want false")
	}
}

// cov8RefListStore is a ContentStore double that serves a canned ref list:
// ContentStore is embedded so only the asserted lister method is defined.
type cov8RefListStore struct {
	querycontract.ContentStore
	refs []repository.RepositoryRef
	err  error
}

// ListRepositoryRefs implements the repositoryRefLister port Refs asserts.
func (s cov8RefListStore) ListRepositoryRefs(context.Context, string) ([]repository.RepositoryRef, error) {
	return s.refs, s.err
}

// cov8BareContentStore implements ContentStore with no lister port.
type cov8BareContentStore struct {
	querycontract.ContentStore
}

// TestCov8RefsListing pins the unpaged ref read: nil and non-lister
// stores stay quiet, the lister error propagates, rows pass through.
func TestCov8RefsListing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	if refs, err := Refs(ctx, nil, "repo-1"); refs != nil || err != nil {
		t.Errorf("Refs(nil store) = %v, %v; want nil, nil", refs, err)
	}
	// A ContentStore that does not implement the lister port stays quiet.
	if refs, err := Refs(ctx, cov8BareContentStore{}, "repo-1"); refs != nil || err != nil {
		t.Errorf("Refs(non-lister) = %v, %v; want nil, nil", refs, err)
	}
	wantErr := errors.New("store down")
	if _, err := Refs(ctx, cov8RefListStore{err: wantErr}, "repo-1"); !errors.Is(err, wantErr) {
		t.Errorf("Refs(failing lister) = %v, want %v", err, wantErr)
	}
	rows := []repository.RepositoryRef{
		{Kind: "branch", Name: "main", HeadSHA: "abc", Default: true},
		{Kind: "tag", Name: "v1", HeadSHA: "def"},
	}
	got, err := Refs(ctx, cov8RefListStore{refs: rows}, "repo-1")
	if err != nil || len(got) != 2 {
		t.Fatalf("Refs() = %v, %v; want 2 rows, nil", got, err)
	}
	if branch := RefsDefaultBranch(got); branch != "main" {
		t.Errorf("RefsDefaultBranch() = %q, want main", branch)
	}
	if branch := RefsDefaultBranch(got[1:]); branch != "" {
		t.Errorf("RefsDefaultBranch(tags only) = %q, want empty", branch)
	}
	if branch := RefsDefaultBranch(nil); branch != "" {
		t.Errorf("RefsDefaultBranch(nil) = %q, want empty", branch)
	}
}

// TestCov8RefEntryShape pins the wire entry: branches carry is_default,
// tags never do, and zero timestamps stay absent.
func TestCov8RefEntryShape(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	branch := RefEntry(repository.RepositoryRef{
		Kind: "branch", Name: "main", HeadSHA: "abc",
		Default: true, ObservedAt: at, IndexedAt: at,
	}, true)
	if branch["name"] != "main" || branch["kind"] != "branch" || branch["head_sha"] != "abc" {
		t.Errorf("branch entry identity = %v, want main/branch/abc", branch)
	}
	if branch["is_default"] != true {
		t.Errorf("branch entry is_default = %v, want true", branch["is_default"])
	}
	if _, ok := branch["observed_at"]; !ok {
		t.Error("branch entry misses observed_at")
	}
	if _, ok := branch["last_indexed_at"]; !ok {
		t.Error("branch entry misses last_indexed_at")
	}
	tag := RefEntry(repository.RepositoryRef{Kind: "tag", Name: "v1", HeadSHA: "def"}, false)
	if _, ok := tag["is_default"]; ok {
		t.Errorf("tag entry carries is_default = %v, want absent", tag["is_default"])
	}
	if _, ok := tag["observed_at"]; ok {
		t.Errorf("tag entry carries observed_at with zero time = %v, want absent", tag["observed_at"])
	}
}

// TestCov8ValidateSelectedRepositoryRef pins ref selection: blank and
// direct-commit selections pass without a read, matches verify against
// the indexed commit, and misses fail with caller-facing statuses.
func TestCov8ValidateSelectedRepositoryRef(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	if code, _, err := ValidateSelectedRepositoryRef(ctx, nil, "repo-1", "  ", "abc"); code != 0 || err != nil {
		t.Errorf("blank selection = %d, %v; want 0, nil", code, err)
	}
	if code, _, err := ValidateSelectedRepositoryRef(ctx, nil, "repo-1", "abc", "abc"); code != 0 || err != nil {
		t.Errorf("direct commit selection = %d, %v; want 0, nil", code, err)
	}
	storeErr := errors.New("store down")
	if _, _, err := ValidateSelectedRepositoryRef(ctx, cov8RefListStore{err: storeErr}, "repo-1", "main", "zzz"); !errors.Is(err, storeErr) {
		t.Errorf("failing store err = %v, want %v", err, storeErr)
	}
	// No refs at all: metadata unavailable.
	if code, _, err := ValidateSelectedRepositoryRef(ctx, cov8RefListStore{}, "repo-1", "main", "zzz"); code != 409 || err != nil {
		t.Errorf("empty store = %d, %v; want 409, nil", code, err)
	}
	rows := []repository.RepositoryRef{
		{Kind: "branch", Name: "main", HeadSHA: "abc"},
		{Kind: "tag", Name: "v1", HeadSHA: "def"},
	}
	store := cov8RefListStore{refs: rows}
	if code, _, err := ValidateSelectedRepositoryRef(ctx, store, "repo-1", "main", "abc"); code != 0 || err != nil {
		t.Errorf("indexed match = %d, %v; want 0, nil", code, err)
	}
	if code, _, err := ValidateSelectedRepositoryRef(ctx, store, "repo-1", "def", "def"); code != 0 || err != nil {
		t.Errorf("sha match = %d, %v; want 0, nil", code, err)
	}
	if code, msg, err := ValidateSelectedRepositoryRef(ctx, store, "repo-1", "main", "zzz"); code != 409 || err != nil || msg == "" {
		t.Errorf("stale match = %d, %q, %v; want 409, message, nil", code, msg, err)
	}
	if code, _, err := ValidateSelectedRepositoryRef(ctx, store, "repo-1", "nope", "zzz"); code != 404 || err != nil {
		t.Errorf("miss = %d, %v; want 404, nil", code, err)
	}
}

// cov8GraphQuery is a GraphQuery double serving canned rows.
type cov8GraphQuery struct {
	querycontract.GraphQuery
	rows []map[string]any
	err  error
}

func (g cov8GraphQuery) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return g.rows, g.err
}

func (g cov8GraphQuery) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	if g.err != nil {
		return nil, g.err
	}
	if len(g.rows) == 0 {
		return nil, nil
	}
	return g.rows[0], nil
}

// TestCov8QueryRepositoryNamesByID pins the name lookup: nil graph and
// empty ids skip the query, id-less rows drop, errors wrap.
func TestCov8QueryRepositoryNamesByID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	if names, err := QueryRepositoryNamesByID(ctx, nil, []string{"r1"}); names != nil || err != nil {
		t.Errorf("nil graph = %v, %v; want nil, nil", names, err)
	}
	graph := cov8GraphQuery{rows: []map[string]any{{"repo_id": "r1", "repo_name": "one"}}}
	if names, err := QueryRepositoryNamesByID(ctx, graph, nil); names != nil || err != nil {
		t.Errorf("empty ids = %v, %v; want nil, nil", names, err)
	}
	wantErr := errors.New("graph down")
	if _, err := QueryRepositoryNamesByID(ctx, cov8GraphQuery{err: wantErr}, []string{"r1"}); !errors.Is(err, wantErr) {
		t.Errorf("graph error = %v, want wrap of %v", err, wantErr)
	}
	names, err := QueryRepositoryNamesByID(ctx, cov8GraphQuery{rows: []map[string]any{
		{"repo_id": "r1", "repo_name": "one"},
		{"repo_id": "", "repo_name": "ghost"},
		{"repo_id": "r2", "repo_name": ""},
		{"repo_id": "r3", "repo_name": "three"},
	}}, []string{"r1", "r2", "r3"})
	if err != nil {
		t.Fatalf("QueryRepositoryNamesByID() = %v, want nil", err)
	}
	if len(names) != 2 || names["r1"] != "one" || names["r3"] != "three" {
		t.Errorf("names = %v, want r1=one r3=three only", names)
	}
}
