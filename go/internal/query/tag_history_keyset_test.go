// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/taghistory"
)

// TestTagHistoryCursorCarriesNoRowPosition is the #6564 re-review finding 1
// regression. The defect it pins is that the continuation token WAS a raw row
// offset in readable JSON, so a scoped caller could read the pre-filter
// frontier off the wire and could mint a token at any position to walk another
// tenant's withheld history one row at a time.
//
// It asserts the two halves that close it together, and it is deliberately
// written against the token BYTES rather than taghistory.Cursor so it compiles
// and runs against both the offset design and the keyset one:
//
//  1. No issued token carries a row position. A keyset token names the
//     (first_observed_at, uid) of a row the caller was actually shown, which
//     is information the caller already holds -- and since #6564 round 3 it is
//     sealed, so the caller cannot read even that off the wire.
//  2. A minted offset token is refused. There is no position to forge, so the
//     old payload shape is simply not a continuation this build understands.
func TestTagHistoryCursorCarriesNoRowPosition(t *testing.T) {
	t.Parallel()

	graph := newSeededTagHistoryGraph("granted", "granted", "granted", "granted", "granted", "granted", "granted")
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=2")
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	token := tagHistoryCursorString(t, decodeTagHistoryBody(t, w))
	if raw, err := base64.RawURLEncoding.DecodeString(token); err == nil {
		var payload map[string]any
		if json.Unmarshal(raw, &payload) == nil {
			t.Fatalf("next_cursor decodes to readable JSON %#v; it must be a sealed envelope", payload)
		}
	}
	// taghistory.Key has no position-shaped field at all, so opening the token
	// is the whole assertion: what comes out is a row key or nothing.
	if got, want := openTagHistoryCursor(t, token, tagHistoryScopedAudience("repo-granted")).UID, "uid-sha256:d01"; got != want {
		t.Fatalf("cursor uid = %q, want %q (the last row the caller was shown)", got, want)
	}

	forged := base64.RawURLEncoding.EncodeToString([]byte(
		`{"v":1,"ref":"ghcr.io/eshu-hq/demo:1.0.0","l":2,"o":4}`,
	))
	got := serveTagHistoryAs(
		t,
		newSeededTagHistoryGraph("other", "other", "other", "other", "granted", "granted"),
		scopedTagHistoryAuth("repo-granted"),
		tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(forged),
	)
	if got.Code != http.StatusBadRequest {
		t.Fatalf(
			"minted offset cursor status = %d, want %d: a caller must not be able to address a raw row position; body = %s",
			got.Code, http.StatusBadRequest, got.Body.String(),
		)
	}
}

// TestTagHistoryCursorNamesARowTheCallerWasShown proves the token's key is the
// key of a row that was actually returned on that page, for a page that filled
// normally. That is what makes the key disclose nothing: the caller already has
// the row it names.
func TestTagHistoryCursorNamesARowTheCallerWasShown(t *testing.T) {
	t.Parallel()

	graph := newSeededTagHistoryGraph("granted", "other", "granted", "other", "granted", "granted")
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=2")
	data := decodeTagHistoryBody(t, w)
	if got, want := tagHistoryResultTags(t, data), []string{"t00", "t02"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	key := openTagHistoryCursor(t, tagHistoryCursorString(t, data), tagHistoryScopedAudience("repo-granted"))
	// newSeededTagHistoryGraph mints uid-<resolved_digest> per row, and t02
	// resolves sha256:d02.
	if got, want := key.UID, "uid-sha256:d02"; got != want {
		t.Fatalf("cursor uid = %q, want %q (the last row the caller was shown)", got, want)
	}
	if got, want := key.At, seededTagHistoryObservedAt(2); got != want {
		t.Fatalf("cursor at = %q, want %q (that row's first_observed_at)", got, want)
	}
}

// TestTagHistoryCursorSurvivesALimitChangeMidWalk proves the token is valid at
// any page size. The offset token was bound to the limit it was issued for,
// which was a guard against an offset walk; a keyset token has no position to
// re-aim, so that binding is gone and a caller may change limit mid-walk
// without being 400ed (the RepositoryRefPageCursor contract).
func TestTagHistoryCursorSurvivesALimitChangeMidWalk(t *testing.T) {
	t.Parallel()

	specs := []string{"granted", "granted", "granted", "granted", "granted", "granted"}
	graph := newSeededTagHistoryGraph(specs...)
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=2")
	token := tagHistoryCursorString(t, decodeTagHistoryBody(t, w))

	next := serveTagHistoryAs(
		t,
		newSeededTagHistoryGraph(specs...),
		scopedTagHistoryAuth("repo-granted"),
		tagHistoryGrantTarget+"&limit=3&cursor="+url.QueryEscape(token),
	)
	if got, want := next.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, next.Body.String())
	}
	data := decodeTagHistoryBody(t, next)
	if got, want := tagHistoryResultTags(t, data), []string{"t02", "t03", "t04"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v (the walk continues from the key at the new page size)", got, want)
	}
}

// TestTagHistoryKeysetCursorRejections pins that every PRE-SEAL token shape is
// dead on this route. Each payload below was a usable continuation at some
// point in this issue's history -- a v1 offset, a v2 keyset key -- and each is
// now refused because it is not an envelope this deployment sealed. The
// plaintext shape checks that used to live here moved to
// taghistory.TestDecodeCursorRefusesAMalformedSealedPlaintext, where a test can
// still construct the sealed payload they guard against.
func TestTagHistoryKeysetCursorRejections(t *testing.T) {
	t.Parallel()

	const ref = "ghcr.io/eshu-hq/demo:1.0.0"
	for name, payload := range map[string]string{
		"v1 offset token":          `{"v":1,"ref":"` + ref + `","l":2,"o":4}`,
		"v2 keyset token":          `{"v":2,"ref":"` + ref + `","at":"x","uid":"u"}`,
		"v3 plaintext, unsealed":   `{"v":3,"ref":"` + ref + `","at":"x","uid":"u"}`,
		"foreign image_ref":        `{"v":2,"ref":"ghcr.io/eshu-hq/other:1.0.0","at":"x","uid":"u"}`,
		"empty uid":                `{"v":2,"ref":"` + ref + `","at":"x","uid":""}`,
		"null tail with timestamp": `{"v":2,"ref":"` + ref + `","at":"x","nt":true,"uid":"u"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := serveTagHistoryAs(
				t,
				newSeededTagHistoryGraph("granted"),
				scopedTagHistoryAuth("repo-granted"),
				tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(base64.RawURLEncoding.EncodeToString([]byte(payload))),
			)
			if got.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", got.Code, http.StatusBadRequest, got.Body.String())
			}
		})
	}
}

// TestTagHistoryRefillWindowIsFixedNotLimitSized proves the refill reads
// MaxLimit-sized raw windows regardless of the caller's limit. That is what
// makes the cap-reached count:0 signal a CONSTANT 800-raw-row span rather than
// 4*limit: at limit=1 the old loop told a caller that four specific consecutive
// observations were someone else's.
func TestTagHistoryRefillWindowIsFixedNotLimitSized(t *testing.T) {
	t.Parallel()

	specs := make([]string, 0, 4*taghistory.MaxLimit+1)
	for range 4 * taghistory.MaxLimit {
		specs = append(specs, "other")
	}
	specs = append(specs, "granted")
	graph := newSeededTagHistoryGraph(specs...)

	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=1")
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeTagHistoryBody(t, w)
	if got, want := data["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	for _, window := range graph.windows {
		if got, want := window[1], taghistory.MaxLimit+1; got != want {
			t.Fatalf("window limit = %d, want %d (MaxLimit plus the sentinel) in %v", got, want, graph.windows)
		}
	}
	if got, want := len(graph.windows), taghistory.MaxRefillReads; got != want {
		t.Fatalf("windows read = %d, want %d", got, want)
	}
	// The whole point: at limit=1 the scan covered 800 consumed raw rows, not
	// 4. Each window also reads one sentinel row it never consumes, so the
	// backend returned MaxRefillReads*(MaxLimit+1).
	if got, want := graph.rawRowsRead, taghistory.MaxRefillReads*(taghistory.MaxLimit+1); got != want {
		t.Fatalf("raw rows read = %d, want %d: the refill window must not shrink with limit", got, want)
	}
}

// TestTagHistoryCapReachedEmptyPageAdvances covers the §3 residue head-on: when
// a capped page kept NOTHING the cursor must name the last RAW row scanned, or
// the caller re-reads the same 800 rows forever. The disclosure this costs is
// one withheld observation's key per fully-withheld span, and it is stated on
// every caller-facing surface.
func TestTagHistoryCapReachedEmptyPageAdvances(t *testing.T) {
	t.Parallel()

	specs := make([]string, 0, 4*taghistory.MaxLimit+1)
	for range 4 * taghistory.MaxLimit {
		specs = append(specs, "other")
	}
	specs = append(specs, "granted")

	graph := newSeededTagHistoryGraph(specs...)
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=2")
	data := decodeTagHistoryBody(t, w)
	if got := data["truncated"]; got != true {
		t.Fatalf("truncated = %#v, want true on a capped page", got)
	}
	key := openTagHistoryCursor(t, tagHistoryCursorString(t, data), tagHistoryScopedAudience("repo-granted"))
	lastRaw := fmt.Sprintf("uid-sha256:d%02d", 4*taghistory.MaxLimit-1)
	if key.UID != lastRaw {
		t.Fatalf("cursor uid = %q, want %q (the last RAW row scanned, so the walk can advance)", key.UID, lastRaw)
	}

	// And following it must reach the granted row rather than re-scan.
	next := serveTagHistoryAs(
		t,
		graph,
		scopedTagHistoryAuth("repo-granted"),
		tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(tagHistoryCursorString(t, data)),
	)
	if got, want := tagHistoryResultTags(t, decodeTagHistoryBody(t, next)), []string{fmt.Sprintf("t%d", 4*taghistory.MaxLimit)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second page tags = %v, want %v", got, want)
	}
}

// TestTagHistoryKeysetPagesTheNullTail proves statement C: rows written before
// #5459 shipped first_observed_at have NO such property, they sort last on both
// backends, and a cursor inside that tail pages through it by uid alone. A
// string key cannot express "no timestamp", which is why the token carries the
// nt flag.
func TestTagHistoryKeysetPagesTheNullTail(t *testing.T) {
	t.Parallel()

	graph := newSeededTagHistoryGraph("granted", "granted", "granted", "granted")
	// t02 and t03 are pre-#5459 rows: the property is absent, not empty.
	for _, row := range graph.history[2:] {
		delete(row, "first_observed_at")
	}

	tags, _ := pageTagHistoryByCursor(t, graph, 2, 8)
	if got, want := tags, []string{"t00", "t01", "t02", "t03"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paged tags = %v, want %v (the null tail pages without duplicate or skip)", got, want)
	}

	// The cursor issued from inside the tail must declare it, so the next read
	// uses the null-tail statement rather than a string comparison against "".
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=3")
	key := openTagHistoryCursor(t, tagHistoryCursorString(t, decodeTagHistoryBody(t, w)), tagHistoryScopedAudience("repo-granted"))
	if !key.NullAt {
		t.Fatalf("cursor NullAt = false, want true for a key inside the null tail: %#v", key)
	}
	if key.At != "" {
		t.Fatalf("cursor carries At = %q alongside NullAt; a null-tail key has no timestamp", key.At)
	}
}

// TestTagHistoryKeysetIsStableUnderAnInsertBehindTheFrontier proves the
// property an offset cursor never had: a row inserted BEFORE the cursor's key
// while a walk is in progress does not shift the walk, so no row is returned
// twice. The offset design returned a duplicate here.
func TestTagHistoryKeysetIsStableUnderAnInsertBehindTheFrontier(t *testing.T) {
	t.Parallel()

	graph := newSeededTagHistoryGraph("granted", "granted", "granted", "granted")
	w := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=2")
	data := decodeTagHistoryBody(t, w)
	if got, want := tagHistoryResultTags(t, data), []string{"t00", "t01"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first page tags = %v, want %v", got, want)
	}
	token := tagHistoryCursorString(t, data)

	// A backdated observation lands at the FRONT of the order between requests.
	graph.insertGranted("t-backdated", "1759999999999")

	next := serveTagHistoryAs(
		t,
		graph,
		scopedTagHistoryAuth("repo-granted"),
		tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(token),
	)
	if got, want := tagHistoryResultTags(t, decodeTagHistoryBody(t, next)), []string{"t02", "t03"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second page tags = %v, want %v: an insert behind the key must not shift the walk", got, want)
	}
}

// TestTagHistoryKeysetAheadOfHistoryIsAnEmptyPage proves a key past the end of
// the history -- a forged one, or one left behind by a retract wave -- is not
// an error and discloses nothing beyond "no visible rows after this point".
func TestTagHistoryKeysetAheadOfHistoryIsAnEmptyPage(t *testing.T) {
	t.Parallel()

	ahead, err := taghistory.EncodeCursor(
		tagHistoryTestCursorKeyring,
		tagHistoryTestImageRef,
		tagHistoryScopedAudience("repo-granted"),
		taghistory.Key{At: "2999-01-01T00:00:00Z", UID: "uid-zzz"},
	)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	w := serveTagHistoryAs(
		t,
		newSeededTagHistoryGraph("granted", "granted", "granted"),
		scopedTagHistoryAuth("repo-granted"),
		tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(ahead),
	)
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
	if _, present := data["next_cursor"]; present {
		t.Fatalf("next_cursor = %#v, want absent", data["next_cursor"])
	}
}

// TestTagHistoryUnscopedCallerEmitsTheSameTokenFormat proves there is ONE token
// format on the wire wherever a sealing key exists. An unscoped caller keeps
// offset/SKIP paging (a test pins that), but its next_cursor is the same sealed
// keyset token, so a client library never has to know which kind of caller it
// is holding a token for. The one divergence is the no-DEK deployment, where
// only the unscoped caller still gets a token at all
// (TestTagHistoryUnscopedPagingSurvivesAnAbsentSealingKey).
func TestTagHistoryUnscopedCallerEmitsTheSameTokenFormat(t *testing.T) {
	t.Parallel()

	graph := newSeededTagHistoryGraph("granted", "other", "none", "granted")
	w := serveTagHistoryAs(t, graph, nil, tagHistoryGrantTarget+"&limit=2&offset=1")
	data := decodeTagHistoryBody(t, w)
	token := tagHistoryCursorString(t, data)
	if !strings.HasPrefix(token, "ESK1.") {
		t.Fatalf("unscoped next_cursor = %q, want the same sealed envelope a scoped caller gets", token)
	}
	if got, want := openTagHistoryCursor(t, token, tagHistoryUnscopedAudience()).UID, "uid-sha256:d02"; got != want {
		t.Fatalf("cursor uid = %q, want %q (the last row this page returned)", got, want)
	}
	// And the token continues the walk for that caller too.
	next := serveTagHistoryAs(
		t,
		graph,
		nil,
		tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(tagHistoryCursorString(t, data)),
	)
	if got, want := tagHistoryResultTags(t, decodeTagHistoryBody(t, next)), []string{"t03"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unscoped cursor page tags = %v, want %v", got, want)
	}
}

// TestTagHistoryScopedStatementsStaySingleClause is the NornicDB shape guard.
// The pinned build corrupts the projection of any read with a clause between
// the anchoring MATCH and the RETURN, and it mis-evaluates an empty-string
// guarded OR disjunct and a literal-default coalesce inequality. Every
// statement this route runs must stay clear of all three.
func TestTagHistoryScopedStatementsStaySingleClause(t *testing.T) {
	t.Parallel()

	for name, statement := range map[string]string{
		"first page": taghistory.FirstPageCypher,
		"after key":  taghistory.AfterKeyCypher,
		"null tail":  taghistory.NullTailCypher,
		"offset":     taghistory.OffsetCypher,
		"built from": taghistory.BuiltFromCypher,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := strings.Count(statement, "MATCH"); got != 1 {
				t.Fatalf("statement has %d MATCH clauses, want exactly 1:\n%s", got, statement)
			}
			for _, banned := range []string{"OPTIONAL MATCH", "WITH ", "CALL", "UNION", "UNWIND", "coalesce(", "<> ''", "= ''"} {
				if strings.Contains(statement, banned) {
					t.Fatalf("statement contains %q, which the pinned NornicDB build mishandles:\n%s", banned, statement)
				}
			}
		})
	}
}
