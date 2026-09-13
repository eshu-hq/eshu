// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/taghistory"
)

// seededTagHistoryGraph is an offset-aware GraphQuery double. Unlike
// fakeTagHistoryGrantGraph it honors the $offset and $limit parameters of
// taghistory.Cypher, so a test can observe what a refilling, cursor-paged
// handler actually reads across successive windows rather than receiving the
// same canned slice for every window.
type seededTagHistoryGraph struct {
	history    []map[string]any
	builtFrom  map[string][]string
	tagReads   int
	builtReads int
	windows    [][2]int
}

func (g *seededTagHistoryGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if strings.Contains(cypher, "BUILT_FROM") {
		g.builtReads++
		digests, _ := params["digests"].([]string)
		rows := make([]map[string]any, 0, len(digests))
		for _, digest := range digests {
			for _, repositoryID := range g.builtFrom[digest] {
				rows = append(rows, map[string]any{"digest": digest, "repository_id": repositoryID})
			}
		}
		return rows, nil
	}
	g.tagReads++
	offset, _ := params["offset"].(int)
	limit, _ := params["limit"].(int)
	g.windows = append(g.windows, [2]int{offset, limit})
	if offset >= len(g.history) {
		return nil, nil
	}
	end := offset + limit
	if end > len(g.history) {
		end = len(g.history)
	}
	return append([]map[string]any(nil), g.history[offset:end]...), nil
}

func (*seededTagHistoryGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// newSeededTagHistoryGraph builds an ordered history from one spec per
// observation: "granted" (BUILT_FROM repo-granted), "other" (BUILT_FROM
// repo-other) or "none" (no BUILT_FROM edge at all, the unattributed case).
func newSeededTagHistoryGraph(specs ...string) *seededTagHistoryGraph {
	graph := &seededTagHistoryGraph{builtFrom: map[string][]string{}}
	for i, spec := range specs {
		digest := fmt.Sprintf("sha256:d%02d", i)
		graph.history = append(graph.history, tagHistoryRowMap(
			fmt.Sprintf("t%02d", i),
			digest,
			"",
			fmt.Sprintf("2026-06-01T00:%02d:00Z", i),
			false,
		))
		switch spec {
		case "granted":
			graph.builtFrom[digest] = []string{"repo-granted"}
		case "other":
			graph.builtFrom[digest] = []string{"repo-other"}
		}
	}
	return graph
}

// visibleTags returns the tags a scoped caller holding repo-granted is
// entitled to, in history order.
func (g *seededTagHistoryGraph) visibleTags() []string {
	tags := make([]string, 0, len(g.history))
	for _, row := range g.history {
		if repos := g.builtFrom[StringVal(row, "resolved_digest")]; len(repos) == 1 && repos[0] == "repo-granted" {
			tags = append(tags, StringVal(row, "tag"))
		}
	}
	return tags
}

// tagHistoryCursorString returns next_cursor as the opaque token string the
// #6564 review requires, failing the test if it is absent or not a string.
func tagHistoryCursorString(t *testing.T, data map[string]any) string {
	t.Helper()
	raw, present := data["next_cursor"]
	if !present {
		t.Fatalf("next_cursor absent; page = %#v", data)
	}
	token, ok := raw.(string)
	if !ok {
		t.Fatalf("next_cursor = %#v (%T), want an opaque token string", raw, raw)
	}
	return token
}

// TestTagHistoryScopedPageRefillsPastWithheldRows is the primary #6564 review
// fix: a page whose leading window is entirely another tenant's history must
// keep reading until it holds `limit` visible rows, so `limit - count` no
// longer measures how many rows were withheld.
func TestTagHistoryScopedPageRefillsPastWithheldRows(t *testing.T) {
	t.Parallel()

	graph := newSeededTagHistoryGraph("other", "other", "other", "other", "granted", "granted", "granted")
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=2")
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeTagHistoryBody(t, w)
	if got, want := tagHistoryResultTags(t, data), []string{"t04", "t05"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v (the page must refill past the four withheld rows)", got, want)
	}
	if got, want := data["count"], float64(2); got != want {
		t.Fatalf("count = %#v, want %#v: a filled page must not disclose the withheld rows through limit-count", got, want)
	}
	if got := data["truncated"]; got != true {
		t.Fatalf("truncated = %#v, want true (t06 is still unread)", got)
	}
}

// TestTagHistoryScopedShortPageMeansHistoryEnded proves a scoped page shorter
// than limit is the END of the visible history, not a window the filter
// shortened: with refill, limit-count carries no withheld-row information.
func TestTagHistoryScopedShortPageMeansHistoryEnded(t *testing.T) {
	t.Parallel()

	graph := newSeededTagHistoryGraph("granted", "other", "none", "granted", "other")
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=50")
	data := decodeTagHistoryBody(t, w)
	if got, want := tagHistoryResultTags(t, data), []string{"t00", "t03"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	if got := data["truncated"]; got != false {
		t.Fatalf("truncated = %#v, want false (the history ended inside this page)", got)
	}
	if _, present := data["next_cursor"]; present {
		t.Fatalf("next_cursor = %#v, want absent on a complete page", data["next_cursor"])
	}
}

// TestTagHistoryNextCursorIsOpaqueToken proves next_cursor stopped being the
// raw offset object a scoped caller could read the withheld-row frontier from.
func TestTagHistoryNextCursorIsOpaqueToken(t *testing.T) {
	t.Parallel()

	graph := newSeededTagHistoryGraph("granted", "granted", "granted", "granted")
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=2")
	data := decodeTagHistoryBody(t, w)
	token := tagHistoryCursorString(t, data)
	if token == "" {
		t.Fatal("next_cursor = \"\", want a non-empty opaque token")
	}
	if _, err := strconv.Atoi(token); err == nil {
		t.Fatalf("next_cursor = %q, want an opaque token rather than a bare row offset", token)
	}
	if _, present := data["offset"]; present {
		t.Fatalf("offset = %#v echoed to a grant-filtered caller, want it omitted: it is the raw pre-filter frontier", data["offset"])
	}
}

// flipTagHistoryCursorChar returns a base64url character that is never the one
// it was given, so an "edited cursor" case cannot accidentally reproduce the
// original token and pass vacuously.
func flipTagHistoryCursorChar(c byte) string {
	if c == 'Z' {
		return "Y"
	}
	return "Z"
}

// TestTagHistoryMalformedCursorIsRejected proves a cursor that is not a cursor
// at all, carries an unusable payload, or was issued for another image_ref or
// another limit fails with a 400 rather than being partially trusted.
//
// It is named for what it proves (#6564 re-review finding 1). The earlier name
// said "Tampered", which claimed more than the code does: taghistory.Cursor
// carries no MAC, so a WELL-FORMED edit -- a payload minted at any offset for
// the caller's own image_ref and limit -- passes every check here. The
// "edited payload" case below flips a base64 character and is caught because
// that corrupts the encoding or the JSON, not because tampering is detected.
func TestTagHistoryMalformedCursorIsRejected(t *testing.T) {
	t.Parallel()

	graph := newSeededTagHistoryGraph("granted", "granted", "granted", "granted")
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=2")
	token := tagHistoryCursorString(t, decodeTagHistoryBody(t, w))

	foreign := serveTagHistoryAs(
		t,
		newSeededTagHistoryGraph("granted", "granted", "granted", "granted"),
		scopedTagHistoryAuth("repo-granted"),
		"/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/other&tag=1.0.0&limit=2&cursor="+url.QueryEscape(token),
	)
	if got, want := foreign.Code, http.StatusBadRequest; got != want {
		t.Fatalf("foreign-image_ref cursor status = %d, want %d; body = %s", got, want, foreign.Body.String())
	}

	for name, bad := range map[string]string{
		"not base64":      "!!!not-a-cursor!!!",
		"edited payload":  token[:len(token)-1] + flipTagHistoryCursorChar(token[len(token)-1]),
		"empty payload":   "e30",
		"unknown version": base64.RawURLEncoding.EncodeToString([]byte(`{"v":99,"ref":"ghcr.io/eshu-hq/demo:1.0.0","l":2,"o":4}`)),
		"negative offset": base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"ref":"ghcr.io/eshu-hq/demo:1.0.0","l":2,"o":-1}`)),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := serveTagHistoryAs(
				t,
				newSeededTagHistoryGraph("granted"),
				scopedTagHistoryAuth("repo-granted"),
				tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(bad),
			)
			if got.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", got.Code, http.StatusBadRequest, got.Body.String())
			}
		})
	}

	replay := serveTagHistoryAs(
		t,
		newSeededTagHistoryGraph("granted", "granted", "granted", "granted"),
		scopedTagHistoryAuth("repo-granted"),
		tagHistoryGrantTarget+"&limit=1&cursor="+url.QueryEscape(token),
	)
	if got, want := replay.Code, http.StatusBadRequest; got != want {
		t.Fatalf("limit-replay cursor status = %d, want %d; body = %s", got, want, replay.Body.String())
	}
}

// TestTagHistoryScopedCallerCannotPageByRawOffset proves the limit=1 offset
// walk the review named is closed: a scoped caller continues with next_cursor,
// and a raw offset is refused rather than silently honored.
func TestTagHistoryScopedCallerCannotPageByRawOffset(t *testing.T) {
	t.Parallel()

	graph := newSeededTagHistoryGraph("granted", "other", "granted")
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=1&offset=1")
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if graph.tagReads != 0 {
		t.Fatalf("tag reads = %d, want 0: a refused offset must not reach the graph", graph.tagReads)
	}
}

// TestTagHistoryUnscopedCallerKeepsOffsetPaging proves the offset contract is
// preserved for the callers that had it: an unscoped caller still pages by
// offset and still receives its echoed offset.
func TestTagHistoryUnscopedCallerKeepsOffsetPaging(t *testing.T) {
	t.Parallel()

	graph := newSeededTagHistoryGraph("granted", "other", "none", "granted")
	w := serveTagHistoryAs(t, graph, nil, tagHistoryGrantTarget+"&limit=2&offset=1")
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeTagHistoryBody(t, w)
	if got, want := tagHistoryResultTags(t, data), []string{"t01", "t02"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	if got, want := data["offset"], float64(1); got != want {
		t.Fatalf("offset = %#v, want %#v echoed for an unscoped caller", got, want)
	}
	if graph.tagReads != 1 {
		t.Fatalf("tag reads = %d, want 1: the unscoped path must stay one statement", graph.tagReads)
	}
}

// pageTagHistoryByCursor walks every page a scoped caller can reach at the
// given limit, returning the concatenated tags and the page count. It fails
// the test on a non-200, on a missing cursor for a truncated page, or on a
// page budget overrun, so a non-terminating cursor cannot pass as success.
func pageTagHistoryByCursor(t *testing.T, graph GraphQuery, limit, maxPages int) ([]string, int) {
	t.Helper()
	var tags []string
	target := fmt.Sprintf("%s&limit=%d", tagHistoryGrantTarget, limit)
	for pages := 1; pages <= maxPages; pages++ {
		w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), target)
		if w.Code != http.StatusOK {
			t.Fatalf("page %d status = %d, want 200; body = %s", pages, w.Code, w.Body.String())
		}
		data := decodeTagHistoryBody(t, w)
		tags = append(tags, tagHistoryResultTags(t, data)...)
		if data["truncated"] != true {
			return tags, pages
		}
		target = fmt.Sprintf("%s&limit=%d&cursor=%s", tagHistoryGrantTarget, limit, url.QueryEscape(tagHistoryCursorString(t, data)))
	}
	t.Fatalf("cursor paging did not terminate within %d pages; tags so far = %v", maxPages, tags)
	return nil, 0
}

// TestTagHistoryCursorPagingReachesEveryVisibleRowExactlyOnce walks a history
// that mixes granted, ungranted and unattributed observations and proves the
// cursor contract has no duplicate and no skip at several page sizes.
func TestTagHistoryCursorPagingReachesEveryVisibleRowExactlyOnce(t *testing.T) {
	t.Parallel()

	specs := []string{
		"none", "granted", "other", "granted", "other", "other", "none", "granted",
		"granted", "other", "none", "none", "granted", "other", "granted", "granted",
	}
	for _, limit := range []int{1, 2, 3, 5, 50} {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			t.Parallel()

			graph := newSeededTagHistoryGraph(specs...)
			want := graph.visibleTags()
			got, _ := pageTagHistoryByCursor(t, graph, limit, 4*len(specs))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("paged tags = %v, want %v (every visible row exactly once, in order)", got, want)
			}
		})
	}
}

// TestTagHistoryFullyWithheldWindowStillPages proves a page whose every read
// row belongs to another tenant returns a usable page: an honest empty page
// with a cursor that resumes correctly, never a silent short page presented as
// complete and never an endpoint the caller cannot advance past.
func TestTagHistoryFullyWithheldWindowStillPages(t *testing.T) {
	t.Parallel()

	specs := make([]string, 0, 41)
	for i := 0; i < 40; i++ {
		specs = append(specs, "other")
	}
	specs = append(specs, "granted")

	graph := newSeededTagHistoryGraph(specs...)
	got, pages := pageTagHistoryByCursor(t, graph, 2, 40)
	if want := []string{"t40"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paged tags = %v, want %v", got, want)
	}
	if pages < 2 {
		t.Fatalf("pages = %d, want more than one: a fully withheld window must page on rather than end the history", pages)
	}
}

// TestTagHistoryRefillHonoursReadCap proves the refill loop is bounded: a
// history that is entirely another tenant's stops after
// taghistory.MaxRefillReads windows and reports that honestly, with a cursor
// that resumes exactly where the scan stopped rather than a silent short page
// presented as complete.
func TestTagHistoryRefillHonoursReadCap(t *testing.T) {
	t.Parallel()

	specs := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		specs = append(specs, "other")
	}
	graph := newSeededTagHistoryGraph(specs...)

	const limit = 5
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), fmt.Sprintf("%s&limit=%d", tagHistoryGrantTarget, limit))
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := graph.tagReads, taghistory.MaxRefillReads; got != want {
		t.Fatalf("tag reads = %d, want %d (the per-request refill cap)", got, want)
	}
	if got, want := graph.builtReads, taghistory.MaxRefillReads; got != want {
		t.Fatalf("BUILT_FROM reads = %d, want %d (one lookup per window)", got, want)
	}

	data := decodeTagHistoryBody(t, w)
	if got, want := data["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got := data["truncated"]; got != true {
		t.Fatalf("truncated = %#v, want true: a capped page is not a complete page", got)
	}
	offset, err := taghistory.DecodeCursor(tagHistoryCursorString(t, data), "ghcr.io/eshu-hq/demo:1.0.0", limit)
	if err != nil {
		t.Fatalf("taghistory.DecodeCursor() error = %v", err)
	}
	if got, want := offset, taghistory.MaxRefillReads*limit; got != want {
		t.Fatalf("cursor offset = %d, want %d (resume exactly where the capped scan stopped)", got, want)
	}
}

// TestTagHistoryBuiltFromFanOutOverflowFailsClosed closes review finding 2:
// taghistory.BuiltFromCypher's result set is not bounded by its key count, so
// overflow must fail the read closed rather than serve a page built from a
// partially read edge set. It also pins #6564 re-review finding 4: the 500 body
// carries no repository id AND no row or key count, both of which are taken
// over the raw pre-filter window across every tenant.
func TestTagHistoryBuiltFromFanOutOverflowFailsClosed(t *testing.T) {
	t.Parallel()

	graph := tagHistoryGrantMatrix()
	graph.builtFromRows = make([]map[string]any, 0, taghistory.BuiltFromMaxRows+1)
	for i := 0; i <= taghistory.BuiltFromMaxRows; i++ {
		graph.builtFromRows = append(graph.builtFromRows, map[string]any{
			"digest":        "sha256:d1",
			"repository_id": fmt.Sprintf("repo-%04d", i),
		})
	}

	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget)
	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	// Neither the repository id nor either COUNT may reach the caller: both
	// counts are taken over the raw pre-filter window, which spans every
	// tenant's images on this image_ref (#6564 re-review finding 4).
	body := w.Body.String()
	for _, leaked := range []string{
		"repo-0001",
		strconv.Itoa(taghistory.BuiltFromMaxRows + 1),
		strconv.Itoa(taghistory.BuiltFromMaxRows),
	} {
		if strings.Contains(body, leaked) {
			t.Fatalf("error body leaked %q, which describes the raw pre-filter window: %s", leaked, body)
		}
	}
}

// TestTagHistoryBuiltFromBoundsAreBelowTheRegisteredFanOut keeps the Go bound
// and the queryplan registration in agreement: the entry registered for
// LookupBuiltFromRepositories in
// go/internal/queryplan/testdata/query-source-coverage.yaml is only honest
// because these two constants enforce it.
func TestTagHistoryBuiltFromBoundsAreBelowTheRegisteredFanOut(t *testing.T) {
	t.Parallel()

	if got, want := taghistory.BuiltFromMaxKeys, 2*taghistory.MaxLimit; got != want {
		t.Fatalf("taghistory.BuiltFromMaxKeys = %d, want %d (resolved + previous digest per row)", got, want)
	}
	if taghistory.BuiltFromMaxRows <= taghistory.BuiltFromMaxKeys {
		t.Fatalf(
			"taghistory.BuiltFromMaxRows = %d, want above the %d key bound: a digest built from several repositories fans out past the key count",
			taghistory.BuiltFromMaxRows, taghistory.BuiltFromMaxKeys,
		)
	}
}

// TestTagHistoryScopeOnlyGrantMatchesCanonicalRepository closes review finding
// 3: deleting the WithCanonicalScopeRepositories() call in listTagHistory left
// every test green. A token holding ONLY a git-repository-scope grant must
// still match the canonical Repository.id a BUILT_FROM edge lands on.
func TestTagHistoryScopeOnlyGrantMatchesCanonicalRepository(t *testing.T) {
	t.Parallel()

	const canonicalRepositoryID = "repository:r_payments"
	graph := newSeededTagHistoryGraph("granted", "other")
	graph.builtFrom["sha256:d00"] = []string{canonicalRepositoryID}

	auth := &AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant_a",
		WorkspaceID:     "workspace_a",
		AllowedScopeIDs: []string{"git-repository-scope:" + canonicalRepositoryID},
	}
	w := serveTagHistoryAs(t, graph, auth, tagHistoryGrantTarget+"&limit=10")
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeTagHistoryBody(t, w)
	if got, want := tagHistoryResultTags(t, data), []string{"t00"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v: a scope-only grant must resolve to its canonical repository id", got, want)
	}
	if got := data["grant_filtered"]; got != true {
		t.Fatalf("grant_filtered = %#v, want true", got)
	}
}
