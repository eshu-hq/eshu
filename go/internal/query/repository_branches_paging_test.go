// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// buildPagedRefFixture returns refs in the exact store order (is_default
// DESC, ref_kind ASC, name ASC): one default branch "main", branchCount-1
// non-default branches "branch-NNN", then tagCount tags "tag-NNN", all
// zero-padded so string sort matches numeric sequence.
func buildPagedRefFixture(branchCount, tagCount int) []RepositoryRef {
	observedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	indexedAt := time.Date(2026, 6, 1, 9, 5, 0, 0, time.UTC)
	refs := []RepositoryRef{
		{Name: "main", Kind: "branch", HeadSHA: "sha-main", Default: true, ObservedAt: observedAt, IndexedAt: indexedAt},
	}
	for i := 0; i < branchCount-1; i++ {
		refs = append(refs, RepositoryRef{
			Name:       fmt.Sprintf("branch-%03d", i),
			Kind:       "branch",
			HeadSHA:    fmt.Sprintf("sha-b-%03d", i),
			ObservedAt: observedAt,
			IndexedAt:  indexedAt,
		})
	}
	for i := 0; i < tagCount; i++ {
		refs = append(refs, RepositoryRef{
			Name:       fmt.Sprintf("tag-%03d", i),
			Kind:       "tag",
			HeadSHA:    fmt.Sprintf("sha-t-%03d", i),
			ObservedAt: observedAt,
			IndexedAt:  indexedAt,
		})
	}
	return refs
}

func pagedBranchesResponse(t *testing.T, handler *RepositoryHandler, target string) map[string]any {
	t.Helper()
	w := requestRepositoryBranches(t, handler, target)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return resp
}

func refNames(t *testing.T, resp map[string]any, key string) []string {
	t.Helper()
	entries, ok := resp[key].([]any)
	if !ok {
		t.Fatalf("%s key missing or wrong type: %#v", key, resp[key])
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.(map[string]any)["name"].(string))
	}
	return names
}

// Test 1: first page of a 250-ref stream (1 default + 149 branches + 100
// tags) with no params returns exactly 100 entries (default limit), split
// across branches[]/tags[], truncated:true, and a next_cursor that decodes
// to the window's last ref. Ordering is (kind, name) only (T3): "main" is
// the default branch but is NOT first, because default-rank no longer
// participates in the sort key -- "branch-000".."branch-148" sort before
// "main" alphabetically ('b' < 'm').
func TestGetRepositoryBranchesPagingFirstPageDefaultLimit(t *testing.T) {
	t.Parallel()

	refs := buildPagedRefFixture(150, 100) // 1 default + 149 + 100 = 250
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories:   []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repositoryRefs: refs,
		},
	}

	resp := pagedBranchesResponse(t, handler, "/api/v0/repositories/repo-1/branches")

	branches := refNames(t, resp, "branches")
	tags := refNames(t, resp, "tags")
	if got, want := len(branches)+len(tags), repositoryRefPageDefaultLimit; got != want {
		t.Fatalf("total window = %d, want %d (default limit)", got, want)
	}
	if got, want := len(tags), 0; got != want {
		t.Fatalf("len(tags) = %d, want %d (100 branches fill the page first)", got, want)
	}
	if got, want := branches[0], "branch-000"; got != want {
		t.Fatalf("branches[0] = %q, want %q (alphabetically first branch)", got, want)
	}
	if truncated, _ := resp["truncated"].(bool); !truncated {
		t.Fatal("truncated = false, want true")
	}
	nextCursorRaw, ok := resp["next_cursor"].(string)
	if !ok || nextCursorRaw == "" {
		t.Fatal("next_cursor missing on a truncated first page")
	}
	decoded, err := decodeRepositoryRefPageCursor(nextCursorRaw, "repo-1")
	if err != nil {
		t.Fatalf("decodeRepositoryRefPageCursor: %v", err)
	}
	if got, want := decoded.Name, "branch-099"; got != want {
		t.Fatalf("next_cursor.Name = %q, want %q (window's last branch)", got, want)
	}
	if got, want := decoded.Kind, "branch"; got != want {
		t.Fatalf("next_cursor.Kind = %q, want %q", got, want)
	}
}

// Test 2 + 3: paging to completion via next_cursor visits every ref exactly
// once (no dup/skip) and the final page reports truncated:false with no
// next_cursor.
func TestGetRepositoryBranchesPagingCursorRoundTripCoversFullSet(t *testing.T) {
	t.Parallel()

	refs := buildPagedRefFixture(150, 100)
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories:   []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repositoryRefs: refs,
		},
	}

	seen := map[string]int{}
	cursor := ""
	pages := 0
	for {
		pages++
		if pages > 10 {
			t.Fatal("did not terminate within 10 pages")
		}
		target := "/api/v0/repositories/repo-1/branches"
		if cursor != "" {
			target += "?cursor=" + cursor
		}
		resp := pagedBranchesResponse(t, handler, target)
		for _, name := range refNames(t, resp, "branches") {
			seen[name]++
		}
		for _, name := range refNames(t, resp, "tags") {
			seen[name]++
		}
		truncated, _ := resp["truncated"].(bool)
		nextCursor, hasCursor := resp["next_cursor"].(string)
		if !truncated {
			if hasCursor && nextCursor != "" {
				t.Fatal("next_cursor present on a non-truncated (final) page")
			}
			break
		}
		if !hasCursor || nextCursor == "" {
			t.Fatal("truncated page missing next_cursor")
		}
		cursor = nextCursor
	}

	if got, want := len(seen), len(refs); got != want {
		t.Fatalf("union of paged names = %d unique, want %d (full ref set)", got, want)
	}
	for name, count := range seen {
		if count != 1 {
			t.Fatalf("ref %q appeared %d times across pages, want exactly 1 (no dup/skip)", name, count)
		}
	}
	if pages < 2 {
		t.Fatalf("pages = %d, want >1 to actually exercise cursor round-trip", pages)
	}
}

// Test 4: a page whose window crosses the branch/tag boundary populates both
// arrays in the same response.
func TestGetRepositoryBranchesPagingSpansBranchTagBoundary(t *testing.T) {
	t.Parallel()

	refs := buildPagedRefFixture(100, 50) // 100 branches (incl. default) + 50 tags
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories:   []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repositoryRefs: refs,
		},
	}

	resp := pagedBranchesResponse(t, handler, "/api/v0/repositories/repo-1/branches?limit=105")

	branches := refNames(t, resp, "branches")
	tags := refNames(t, resp, "tags")
	if got, want := len(branches), 100; got != want {
		t.Fatalf("len(branches) = %d, want %d", got, want)
	}
	if got, want := len(tags), 5; got != want {
		t.Fatalf("len(tags) = %d, want %d (boundary-spanning window)", got, want)
	}
}

// Test 5 (superseded by T3): the old default-branch ordering trap tested a
// v1-cursor property that no longer exists -- default-rank is not part of
// the sort key anymore, so there is nothing to "skip" based on default
// status. Its replacement is TestGetRepositoryBranchesPagingDefaultChurnNoDupSkip
// below, which covers the actual T3 regression: paging must stay correct
// when the default branch itself changes between page fetches.

// Test 6: limit validation table.
func TestGetRepositoryBranchesPagingLimitValidation(t *testing.T) {
	t.Parallel()

	refs := buildPagedRefFixture(150, 100)
	newHandler := func() *RepositoryHandler {
		return &RepositoryHandler{
			Content: fakePortContentStore{
				repositories:   []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
				repositoryRefs: refs,
			},
		}
	}

	cases := []struct {
		name       string
		limit      string
		wantStatus int
	}{
		{"zero", "0", http.StatusBadRequest},
		{"negative", "-1", http.StatusBadRequest},
		{"over_max", "501", http.StatusBadRequest},
		{"non_integer", "abc", http.StatusBadRequest},
		{"at_max", "500", http.StatusOK},
		{"blank", "", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target := "/api/v0/repositories/repo-1/branches"
			if tc.limit != "" {
				target += "?limit=" + tc.limit
			}
			w := requestRepositoryBranches(t, newHandler(), target)
			if got, want := w.Code, tc.wantStatus; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

// Test 7: invalid cursor table -- malformed base64, wrong version, wrong
// repo, unknown kind, and a deploy-boundary v1 cursor all reject with 400,
// never a silently empty page.
func TestGetRepositoryBranchesPagingInvalidCursor(t *testing.T) {
	t.Parallel()

	refs := buildPagedRefFixture(150, 100)
	newHandler := func() *RepositoryHandler {
		return &RepositoryHandler{
			Content: fakePortContentStore{
				repositories:   []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
				repositoryRefs: refs,
			},
		}
	}

	// Corrupt the encoded version by marshaling a cursor with an unsupported
	// future version (3; the current version is 2 -- see the dedicated
	// v1_cursor_rejected case below for the specific v1-deploy-boundary
	// scenario).
	badVersion := repositoryRefPageCursor{Version: 3, RepoID: "repo-1", Kind: "branch", Name: "main"}
	badVersionCursor := marshalCursorForTest(t, badVersion)

	wrongRepoCursor := encodeRepositoryRefPageCursor("repo-other", RepositoryRef{Name: "main", Kind: "branch", Default: true})

	unknownKind := repositoryRefPageCursor{Version: repositoryRefPageCursorVersion, RepoID: "repo-1", Kind: "commit", Name: "x"}
	unknownKindCursor := marshalCursorForTest(t, unknownKind)

	// T3 deploy-boundary case: a v1 cursor (issued before the is_default-key
	// bug was fixed) is hand-encoded as raw JSON, independent of the current
	// struct shape, so this case is meaningful whether or not the Go struct
	// still has a "d" field. Once the cursor version is bumped, decoding a v1
	// payload must reject on version mismatch -- the client simply restarts
	// from page 1, which is safe because the endpoint has no side effects.
	v1RawCursor := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"v":1,"repo":"repo-1","d":1,"k":"branch","n":"main"}`),
	)

	cases := []struct {
		name   string
		cursor string
	}{
		{"garbage_base64", "***not-base64***"},
		{"wrong_version", badVersionCursor},
		{"wrong_repo", wrongRepoCursor},
		{"unknown_kind", unknownKindCursor},
		{"v1_cursor_rejected", v1RawCursor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := requestRepositoryBranches(t, newHandler(), "/api/v0/repositories/repo-1/branches?cursor="+tc.cursor)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}

	// Sanity: a validly-encoded cursor for the right repo, right version,
	// known kind does NOT 400.
	validCursor := encodeRepositoryRefPageCursor("repo-1", RepositoryRef{Name: "main", Kind: "branch", Default: true})
	w := requestRepositoryBranches(t, newHandler(), "/api/v0/repositories/repo-1/branches?cursor="+validCursor)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("valid cursor status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

// marshalCursorForTest base64url-encodes a hand-built cursor struct so tests
// can construct structurally-valid-but-semantically-invalid cursors without
// duplicating the production encoder's validation.
func marshalCursorForTest(t *testing.T, cursor repositoryRefPageCursor) string {
	t.Helper()
	raw, err := json.Marshal(cursor)
	if err != nil {
		t.Fatalf("marshal cursor: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Test 8: a ref deleted between page 1 and page 2 (churn) must not corrupt
// page 2 or produce a duplicate -- the cursor is a forward-only key, not a
// row offset. Fixture refs are paged alphabetically (kind, name): branch-000,
// branch-001, main.
func TestGetRepositoryBranchesPagingChurnBetweenPages(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	indexedAt := time.Date(2026, 6, 1, 9, 5, 0, 0, time.UTC)
	branchA := RepositoryRef{Name: "branch-000", Kind: "branch", HeadSHA: "sha-a", ObservedAt: observedAt, IndexedAt: indexedAt}
	branchB := RepositoryRef{Name: "branch-001", Kind: "branch", HeadSHA: "sha-b", ObservedAt: observedAt, IndexedAt: indexedAt}
	main := RepositoryRef{Name: "main", Kind: "branch", HeadSHA: "sha-main", Default: true, ObservedAt: observedAt, IndexedAt: indexedAt}

	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories:   []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repositoryRefs: []RepositoryRef{main, branchA, branchB},
		},
	}
	page1 := pagedBranchesResponse(t, handler, "/api/v0/repositories/repo-1/branches?limit=1")
	got1 := refNames(t, page1, "branches")
	if len(got1) != 1 || got1[0] != "branch-000" {
		t.Fatalf("page1 branches = %v, want [\"branch-000\"] (alphabetically first)", got1)
	}
	cursor, _ := page1["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("page1 next_cursor missing")
	}

	// Simulate churn: branch-001 (not yet returned) is deleted before page 2.
	handler.Content = fakePortContentStore{
		repositories:   []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
		repositoryRefs: []RepositoryRef{main, branchA},
	}

	page2 := pagedBranchesResponse(t, handler, "/api/v0/repositories/repo-1/branches?limit=1&cursor="+cursor)
	got2 := refNames(t, page2, "branches")
	if len(got2) != 1 || got2[0] != "main" {
		t.Fatalf("page2 branches after churn = %v, want [\"main\"] (deleted branch-001 skipped cleanly, no dup)", got2)
	}
}

// TestGetRepositoryBranchesPagingDefaultChurnNoDupSkip is the T3 regression:
// under the v1 cursor (which encoded the mutable is_default flag as part of
// the sort key), the default branch changing between page 1 and page 2 shifts
// the encoded key's meaning and corrupts windowing -- a ref can be duplicated
// or skipped even though no ref was added or removed. Names/kinds are stable
// across the two fetches; only which ref is_default changes (main -> alpha),
// as a repository's default branch legitimately can between two page fetches.
func TestGetRepositoryBranchesPagingDefaultChurnNoDupSkip(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	indexedAt := time.Date(2026, 6, 1, 9, 5, 0, 0, time.UTC)

	// Page 1: "main" is the default branch. In real Postgres output this
	// sorts (is_default DESC, ref_kind, name) as [main, alpha].
	page1Refs := []RepositoryRef{
		{Name: "main", Kind: "branch", HeadSHA: "sha-main", Default: true, ObservedAt: observedAt, IndexedAt: indexedAt},
		{Name: "alpha", Kind: "branch", HeadSHA: "sha-alpha", Default: false, ObservedAt: observedAt, IndexedAt: indexedAt},
	}
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories:   []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repositoryRefs: page1Refs,
		},
	}
	page1 := pagedBranchesResponse(t, handler, "/api/v0/repositories/repo-1/branches?limit=1")
	got1 := refNames(t, page1, "branches")
	if len(got1) != 1 {
		t.Fatalf("page1 branches = %v, want exactly 1", got1)
	}
	cursor, _ := page1["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("page1 next_cursor missing")
	}

	// Between page 1 and page 2, the default branch flips main -> alpha (a
	// legitimate git operation). Names and kinds are unchanged; only Default
	// moves. Real Postgres output for this state sorts as [alpha, main].
	handler.Content = fakePortContentStore{
		repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
		repositoryRefs: []RepositoryRef{
			{Name: "alpha", Kind: "branch", HeadSHA: "sha-alpha", Default: true, ObservedAt: observedAt, IndexedAt: indexedAt},
			{Name: "main", Kind: "branch", HeadSHA: "sha-main", Default: false, ObservedAt: observedAt, IndexedAt: indexedAt},
		},
	}
	page2 := pagedBranchesResponse(t, handler, "/api/v0/repositories/repo-1/branches?limit=1&cursor="+cursor)
	got2 := refNames(t, page2, "branches")
	if len(got2) != 1 {
		t.Fatalf("page2 branches = %v, want exactly 1", got2)
	}

	all := append(append([]string{}, got1...), got2...)
	seen := map[string]int{}
	for _, name := range all {
		seen[name]++
	}
	if len(seen) != 2 {
		t.Fatalf("union across pages = %v, want both \"alpha\" and \"main\" exactly once each", all)
	}
	for name, count := range seen {
		if count != 1 {
			t.Fatalf("ref %q appeared %d times across pages (default churn caused dup/skip)", name, count)
		}
	}
}

// Test 10: the legacy single-indexed-commit fallback accepts limit/cursor
// params without erroring, and always reports truncated:false with no
// next_cursor since it never paginates.
func TestGetRepositoryBranchesPagingFallbackAcceptsParams(t *testing.T) {
	t.Parallel()

	indexedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories: []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repoFiles:    []FileContent{{RepoID: "repo-1", RelativePath: "main.go", CommitSHA: "abc123"}},
			coverage:     RepositoryContentCoverage{Available: true, FileCount: 1, FileIndexedAt: indexedAt},
		},
	}

	resp := pagedBranchesResponse(t, handler, "/api/v0/repositories/repo-1/branches?limit=50")
	branches, ok := resp["branches"].([]any)
	if !ok || len(branches) != 1 {
		t.Fatalf("branches = %#v, want 1 (fallback entry)", resp["branches"])
	}
	if truncated, _ := resp["truncated"].(bool); truncated {
		t.Fatal("truncated = true, want false (fallback never paginates)")
	}
	if _, ok := resp["next_cursor"]; ok {
		t.Fatal("next_cursor present on the fallback path, want absent")
	}
}
