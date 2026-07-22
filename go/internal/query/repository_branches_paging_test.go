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
// to the window's last ref.
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
	if got, want := branches[0], "main"; got != want {
		t.Fatalf("branches[0] = %q, want %q (default ref first)", got, want)
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
	if got, want := decoded.Name, "branch-098"; got != want {
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

// Test 5: the default-branch ordering trap. A cursor placed right after the
// default ref "main" must not skip the alphabetically-earlier branch
// "alpha" -- the sort key's first component is default-rank, not name.
func TestGetRepositoryBranchesPagingDefaultRefOrderingTrap(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	indexedAt := time.Date(2026, 6, 1, 9, 5, 0, 0, time.UTC)
	refs := []RepositoryRef{
		// Store order: is_default DESC puts "main" first even though "alpha"
		// sorts earlier alphabetically.
		{Name: "main", Kind: "branch", HeadSHA: "sha-main", Default: true, ObservedAt: observedAt, IndexedAt: indexedAt},
		{Name: "alpha", Kind: "branch", HeadSHA: "sha-alpha", ObservedAt: observedAt, IndexedAt: indexedAt},
	}
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories:   []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repositoryRefs: refs,
		},
	}

	page1 := pagedBranchesResponse(t, handler, "/api/v0/repositories/repo-1/branches?limit=1")
	if got, want := refNames(t, page1, "branches"), []string{"main"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("page1 branches = %v, want %v", got, want)
	}
	cursor, _ := page1["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("page1 next_cursor missing")
	}

	page2 := pagedBranchesResponse(t, handler, "/api/v0/repositories/repo-1/branches?limit=1&cursor="+cursor)
	got := refNames(t, page2, "branches")
	if len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("page2 branches = %v, want [\"alpha\"] (must not skip the alphabetically-earlier branch)", got)
	}
}

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
// repo all reject with 400, never a silently empty page.
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

	// Corrupt the encoded version by marshaling a cursor with v:2.
	badVersion := repositoryRefPageCursor{Version: 2, RepoID: "repo-1", Default: 1, Kind: "branch", Name: "main"}
	badVersionCursor := marshalCursorForTest(t, badVersion)

	wrongRepoCursor := encodeRepositoryRefPageCursor("repo-other", RepositoryRef{Name: "main", Kind: "branch", Default: true})

	unknownKind := repositoryRefPageCursor{Version: 1, RepoID: "repo-1", Default: 0, Kind: "commit", Name: "x"}
	unknownKindCursor := marshalCursorForTest(t, unknownKind)

	cases := []struct {
		name   string
		cursor string
	}{
		{"garbage_base64", "***not-base64***"},
		{"wrong_version", badVersionCursor},
		{"wrong_repo", wrongRepoCursor},
		{"unknown_kind", unknownKindCursor},
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
// row offset.
func TestGetRepositoryBranchesPagingChurnBetweenPages(t *testing.T) {
	t.Parallel()

	fullRefs := buildPagedRefFixture(3, 0) // main, branch-000, branch-001
	handler := &RepositoryHandler{
		Content: fakePortContentStore{
			repositories:   []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
			repositoryRefs: fullRefs,
		},
	}
	page1 := pagedBranchesResponse(t, handler, "/api/v0/repositories/repo-1/branches?limit=1")
	cursor, _ := page1["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("page1 next_cursor missing")
	}

	// Simulate churn: branch-000 (not yet returned) is deleted before page 2.
	churnedRefs := []RepositoryRef{fullRefs[0], fullRefs[2]} // main, branch-001
	handler.Content = fakePortContentStore{
		repositories:   []RepositoryCatalogEntry{repositoryStatsCatalogEntry()},
		repositoryRefs: churnedRefs,
	}

	page2 := pagedBranchesResponse(t, handler, "/api/v0/repositories/repo-1/branches?limit=1&cursor="+cursor)
	got := refNames(t, page2, "branches")
	if len(got) != 1 || got[0] != "branch-001" {
		t.Fatalf("page2 branches after churn = %v, want [\"branch-001\"] (deleted branch-000 skipped cleanly, no dup)", got)
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
