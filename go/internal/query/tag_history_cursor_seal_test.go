// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/taghistory"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

// tagHistoryTestImageRef is the image_ref tagHistoryGrantTarget selects.
const tagHistoryTestImageRef = "ghcr.io/eshu-hq/demo:1.0.0"

// tagHistoryTestCursorKeyring is the deployment DEK these tests run with. It is
// a package-level FIXED key rather than a per-test one on purpose: every served
// page builds its own handler, so a token that survives a round trip here has
// survived what a restart or a second replica holding the same DEK does to it.
var tagHistoryTestCursorKeyring = mustTagHistoryTestKeyring("cursor-test", 0x11)

// tagHistoryRotatedCursorKeyring is the key a rotation left behind: different
// material under a different id, which is what KeyringFromEnv produces when
// ESHU_AUTH_SECRET_ENC_KEY is replaced (the id defaults to a fingerprint of the
// key material).
var tagHistoryRotatedCursorKeyring = mustTagHistoryTestKeyring("cursor-test-rotated", 0x22)

// mustTagHistoryTestKeyring builds a deterministic single-key DEK, panicking
// rather than returning an error so it can initialize a package var. The key
// material is test-only and deliberately not read from the environment.
func mustTagHistoryTestKeyring(id string, fill byte) *secretcrypto.Keyring {
	key := make([]byte, 32)
	for i := range key {
		key[i] = fill ^ byte(i)
	}
	keyring, err := secretcrypto.NewKeyring(
		secretcrypto.KeyID(id),
		map[secretcrypto.KeyID][]byte{secretcrypto.KeyID(id): key},
	)
	if err != nil {
		panic("build tag-history test keyring: " + err.Error())
	}
	return keyring
}

// tagHistoryTruthReason returns the response's truth.reason, which is where a
// scoped caller is told what its page does and does not disclose.
func tagHistoryTruthReason(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v; body = %s", err, w.Body.String())
	}
	truth, ok := body["truth"].(map[string]any)
	if !ok {
		t.Fatalf("truth = %#v, want an object; body = %s", body["truth"], w.Body.String())
	}
	reason, ok := truth["reason"].(string)
	if !ok {
		t.Fatalf("truth.reason = %#v, want a string", truth["reason"])
	}
	return reason
}

// openTagHistoryCursor returns the key inside a sealed token, failing the test
// if it does not open under the test keyring. Tests assert on the KEY rather
// than on a decoded payload map, because the payload is no longer readable from
// the wire -- which is the point.
func openTagHistoryCursor(t *testing.T, token string, audience taghistory.Audience) taghistory.Key {
	t.Helper()
	key, err := taghistory.DecodeCursor(tagHistoryTestCursorKeyring, token, tagHistoryTestImageRef, audience)
	if err != nil {
		t.Fatalf("next_cursor %q did not open under the serving deployment's key: %v", token, err)
	}
	return key
}

// forgedTagHistoryCursor is the payload a scoped caller mints for its OWN
// image_ref: a well-formed unsealed keyset token naming a start of the
// caller's choosing. "zzz" sorts after every uid the seeded graph mints, so
// the forged key lands cleanly after row start and the scanned span is rows
// start+1 .. start+800.
func forgedTagHistoryCursor(start int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(
		`{"v":2,"ref":"ghcr.io/eshu-hq/demo:1.0.0","at":%q,"uid":"zzz"}`,
		seededTagHistoryObservedAt(start),
	)))
}

// TestTagHistoryForgedCursorIsRefused is the #6564 round-3 finding 1
// regression: the keyset cursor carried no MAC, so a scoped caller could mint
// a token naming ANY start for its own image_ref. That is the whole oracle.
// A fully-withheld capped page answers with the key of the 800th raw row
// after the caller's chosen start, so f(start) is a monotone step function the
// caller can binary-search -- recovering every withheld row's
// (first_observed_at, uid) in ~40 requests each, not "one key per 800-row
// span" as six caller-facing surfaces claimed.
//
// The fix seals the token, so the reachable start set is exactly {page one} U
// {tokens this server issued}. This test asserts the refusal and that it costs
// no graph read; the failure message prints the leaked keys so a regression
// shows the oracle rather than only a status code.
func TestTagHistoryForgedCursorIsRefused(t *testing.T) {
	t.Parallel()

	specs := make([]string, 0, 1000)
	for range 1000 {
		specs = append(specs, "other")
	}

	for _, start := range []int{0, 1, 50} {
		t.Run(fmt.Sprintf("start=%d", start), func(t *testing.T) {
			t.Parallel()

			graph := newSeededTagHistoryGraph(specs...)
			w := serveTagHistoryAs(
				t,
				graph,
				scopedTagHistoryAuth("repo-granted"),
				tagHistoryGrantTarget+"&limit=1&cursor="+url.QueryEscape(forgedTagHistoryCursor(start)),
			)
			if w.Code != http.StatusBadRequest {
				data := decodeTagHistoryBody(t, w)
				t.Fatalf(
					"forged start %d: status = %d, want %d; the page answered count=%#v truncated=%#v with next_cursor=%#v -- "+
						"the caller aimed the scan and read back the key of raw row %d, which it may not see; body = %s",
					start, w.Code, http.StatusBadRequest,
					data["count"], data["truncated"], data["next_cursor"], start+800, w.Body.String(),
				)
			}
			if graph.tagReads != 0 {
				t.Fatalf("tag reads = %d, want 0: a refused cursor must not reach the graph", graph.tagReads)
			}
		})
	}
}

// TestTagHistoryServerIssuedCursorRoundTrips is the other half of the same
// contract: sealing must not break the walk. A token this server issued is
// accepted back and continues from the row it names.
func TestTagHistoryServerIssuedCursorRoundTrips(t *testing.T) {
	t.Parallel()

	specs := []string{"granted", "granted", "granted", "granted", "granted"}
	graph := newSeededTagHistoryGraph(specs...)
	first := serveTagHistoryAs(t, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=2")
	if got, want := first.Code, http.StatusOK; got != want {
		t.Fatalf("first page status = %d, want %d; body = %s", got, want, first.Body.String())
	}
	data := decodeTagHistoryBody(t, first)
	token := tagHistoryCursorString(t, data)

	next := serveTagHistoryAs(
		t,
		graph,
		scopedTagHistoryAuth("repo-granted"),
		tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(token),
	)
	if got, want := next.Code, http.StatusOK; got != want {
		t.Fatalf("continuation status = %d, want %d; body = %s", got, want, next.Body.String())
	}
	if got, want := tagHistoryResultTags(t, decodeTagHistoryBody(t, next)), []string{"t02", "t03"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("continuation tags = %v, want %v", got, want)
	}
	if !strings.HasPrefix(token, "ESK1.") {
		t.Fatalf("next_cursor = %q, want the sealed ESK1 envelope shape", token)
	}
	if got, want := openTagHistoryCursor(t, token, tagHistoryScopedAudience("repo-granted")).UID, "uid-sha256:d01"; got != want {
		t.Fatalf("sealed cursor uid = %q, want %q (the last row this page returned)", got, want)
	}
}

// TestTagHistoryCursorFromAnotherDeploymentIsRefused covers the two key-state
// failures an operator can actually produce: a token sealed under a key this
// process no longer holds (a rotation mid-walk), and a token sealed by a peer
// for a DIFFERENT image_ref. Both are 400s that cost no graph read, so the
// client restarts from page one rather than reading someone else's history.
func TestTagHistoryCursorFromAnotherDeploymentIsRefused(t *testing.T) {
	t.Parallel()

	key := taghistory.Key{At: seededTagHistoryObservedAt(1), UID: "uid-sha256:d01"}
	audience := tagHistoryScopedAudience("repo-granted")
	rotated, err := taghistory.EncodeCursor(tagHistoryRotatedCursorKeyring, tagHistoryTestImageRef, audience, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	foreign, err := taghistory.EncodeCursor(tagHistoryTestCursorKeyring, "ghcr.io/eshu-hq/other:1.0.0", audience, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	valid, err := taghistory.EncodeCursor(tagHistoryTestCursorKeyring, tagHistoryTestImageRef, audience, key)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}

	for name, token := range map[string]string{
		"sealed under a rotated key":   rotated,
		"sealed for another image_ref": foreign,
		"truncated envelope":           valid[:len(valid)-4],
		"garbage":                      "not-a-cursor",
		"unsealed v2 token":            base64.RawURLEncoding.EncodeToString([]byte(`{"v":2,"ref":"` + tagHistoryTestImageRef + `","at":"x","uid":"u"}`)),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			graph := newSeededTagHistoryGraph("granted", "granted", "granted")
			w := serveTagHistoryAs(
				t,
				graph,
				scopedTagHistoryAuth("repo-granted"),
				tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(token),
			)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if graph.tagReads != 0 {
				t.Fatalf("tag reads = %d, want 0: an unopenable cursor must not reach the graph", graph.tagReads)
			}
		})
	}

	// The control: the same key, sealed by THIS deployment, is accepted. Without
	// it every refusal above would also pass against a route that refused
	// everything.
	w := serveTagHistoryAs(
		t,
		newSeededTagHistoryGraph("granted", "granted", "granted"),
		scopedTagHistoryAuth("repo-granted"),
		tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(valid),
	)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("control status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

// TestTagHistoryScopedPagingFailsClosedWithoutASealingKey pins the absent-key
// contract. A deployment with no DEK cannot issue a token a scoped caller
// could not re-aim, so it issues none: the page is still served, still correct
// and still honestly truncated, but next_cursor is omitted and truth.reason
// names the variable to set. It is NOT a 500 and NOT a silent downgrade to an
// unsealed token.
func TestTagHistoryScopedPagingFailsClosedWithoutASealingKey(t *testing.T) {
	t.Parallel()

	t.Run("truncated page omits the token and says why", func(t *testing.T) {
		t.Parallel()

		graph := newSeededTagHistoryGraph("granted", "granted", "granted", "granted")
		w := serveTagHistoryWithSealer(t, nil, graph, scopedTagHistoryAuth("repo-granted"), tagHistoryGrantTarget+"&limit=2")
		if got, want := w.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
		}
		data := decodeTagHistoryBody(t, w)
		if got, want := tagHistoryResultTags(t, data), []string{"t00", "t01"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("tags = %v, want %v: the page itself must still be correct", got, want)
		}
		if got := data["truncated"]; got != true {
			t.Fatalf("truncated = %#v, want true: the page must stay honest about the rows it did not return", got)
		}
		if token, present := data["next_cursor"]; present {
			t.Fatalf("next_cursor = %#v, want absent: an unsealed token here is the oracle sealing closes", token)
		}
		reason := tagHistoryTruthReason(t, w)
		if !strings.Contains(reason, "ESHU_AUTH_SECRET_ENC_KEY") {
			t.Fatalf("truth.reason = %q, want it to name the variable an operator must set", reason)
		}
	})

	t.Run("a request carrying a cursor is refused 503", func(t *testing.T) {
		t.Parallel()

		key := taghistory.Key{At: seededTagHistoryObservedAt(1), UID: "uid-sha256:d01"}
		token, err := taghistory.EncodeCursor(tagHistoryTestCursorKeyring, tagHistoryTestImageRef, tagHistoryScopedAudience("repo-granted"), key)
		if err != nil {
			t.Fatalf("EncodeCursor() error = %v", err)
		}
		graph := newSeededTagHistoryGraph("granted", "granted", "granted")
		w := serveTagHistoryWithSealer(
			t, nil, graph,
			scopedTagHistoryAuth("repo-granted"),
			tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(token),
		)
		if got, want := w.Code, http.StatusServiceUnavailable; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
		}
		body := w.Body.String()
		for _, want := range []string{"ESHU_AUTH_SECRET_ENC_KEY", "capability_degraded"} {
			if !strings.Contains(body, want) {
				t.Fatalf("body = %s, want it to carry %q", body, want)
			}
		}
		if graph.tagReads != 0 {
			t.Fatalf("tag reads = %d, want 0", graph.tagReads)
		}
	})
}

// TestTagHistoryUnscopedPagingSurvivesAnAbsentSealingKey is the other half of
// the owner's decision: a caller with nothing withheld from it is UNAFFECTED by
// the missing key. Its cursor still works, because an aimed start buys it
// nothing the offset parameter does not already give it.
func TestTagHistoryUnscopedPagingSurvivesAnAbsentSealingKey(t *testing.T) {
	t.Parallel()

	graph := newSeededTagHistoryGraph("granted", "other", "none", "granted")
	w := serveTagHistoryWithSealer(t, nil, graph, nil, tagHistoryGrantTarget+"&limit=2")
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeTagHistoryBody(t, w)
	token := tagHistoryCursorString(t, data)
	next := serveTagHistoryWithSealer(
		t, nil, graph, nil,
		tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(token),
	)
	if got, want := tagHistoryResultTags(t, decodeTagHistoryBody(t, next)), []string{"t02", "t03"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unscoped continuation tags = %v, want %v", got, want)
	}
}

// TestTagHistorySealedCursorWalksEveryTimestampState is the paging-correctness
// proof sealing must not break: every visible row exactly once, in order, with
// no duplicate and no skip, across a history that holds all three timestamp
// states plus a shared-millisecond tie -- at four page sizes, with the cursor
// sealed and re-opened between every page.
func TestTagHistorySealedCursorWalksEveryTimestampState(t *testing.T) {
	t.Parallel()

	for _, limit := range []int{1, 2, 3, 7} {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			t.Parallel()

			graph := newSeededTagHistoryGraph(
				"granted", "other", "granted", "granted", "other",
				"granted", "none", "granted", "granted", "other",
			)
			// Two rows share one millisecond, so only the uid tiebreak
			// separates them.
			graph.history[2]["first_observed_at"] = graph.history[1]["first_observed_at"]
			// Two rows carry the STORED empty timestamp ociTagObservedAtValue
			// writes for a zero ObservedAt; they sort first.
			graph.history[4]["first_observed_at"] = ""
			graph.history[5]["first_observed_at"] = ""
			// Two rows predate #5459 and have no property at all; they sort
			// last and page by uid alone.
			delete(graph.history[8], "first_observed_at")
			delete(graph.history[9], "first_observed_at")

			want := graph.visibleTags()
			if len(want) < 4 {
				t.Fatalf("seed produced only %d visible rows; the walk would not prove anything", len(want))
			}
			got, pages := pageTagHistoryByCursor(t, graph, limit, 4*len(graph.history))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("paged tags = %v, want %v over %d pages", got, want, pages)
			}
		})
	}
}

// TestTagHistoryCursorFromAnotherAudienceIsRefused is the end-to-end form of
// the audience binding (#6564 review finding, taghistory/cursor.go).
//
// Sealing alone bounded the reachable starts to tokens THIS SERVER issued, but
// said nothing about WHO they were issued to, so a token minted in one
// authorization context opened cleanly in another. That matters most for an
// unscoped caller, which keeps the offset parameter: it can ask for offset=N,
// read the next_cursor back, and so mint a token at essentially any raw
// position in the history. Handing one to a scoped caller restored exactly the
// caller-chosen start the seal exists to remove, and falsified the design
// comment's claim that such a caller "cannot choose where such a span starts".
//
// Each subtest carries its own control, because a 400 proves nothing on its own
// -- the same token must be accepted by the audience it was issued to, or the
// refusal could be any other cursor defect.
func TestTagHistoryCursorFromAnotherAudienceIsRefused(t *testing.T) {
	t.Parallel()

	newGraph := func() GraphQuery {
		return newSeededTagHistoryGraph("granted", "granted", "granted", "granted", "granted")
	}
	issue := func(t *testing.T, auth *AuthContext) string {
		t.Helper()
		w := serveTagHistoryAs(t, newGraph(), auth, tagHistoryGrantTarget+"&limit=2")
		if got, want := w.Code, http.StatusOK; got != want {
			t.Fatalf("issuing page status = %d, want %d; body = %s", got, want, w.Body.String())
		}
		return tagHistoryCursorString(t, decodeTagHistoryBody(t, w))
	}
	replay := func(t *testing.T, auth *AuthContext, token string) int {
		t.Helper()
		return serveTagHistoryAs(
			t, newGraph(), auth,
			tagHistoryGrantTarget+"&limit=2&cursor="+url.QueryEscape(token),
		).Code
	}

	t.Run("an unscoped caller's cursor is refused for a scoped caller", func(t *testing.T) {
		t.Parallel()

		token := issue(t, nil)
		if got, want := replay(t, scopedTagHistoryAuth("repo-granted"), token), http.StatusBadRequest; got != want {
			t.Fatalf("status = %d, want %d: an unscoped caller can aim a cursor with offset, so a scoped caller must not be able to replay one", got, want)
		}
		if got, want := replay(t, nil, token), http.StatusOK; got != want {
			t.Fatalf("control status = %d, want %d: the issuing audience must still be able to continue", got, want)
		}
	})

	t.Run("another tenant's cursor is refused", func(t *testing.T) {
		t.Parallel()

		token := issue(t, scopedTagHistoryAuth("repo-granted"))
		if got, want := replay(t, scopedTagHistoryAuth("repo-other"), token), http.StatusBadRequest; got != want {
			t.Fatalf("status = %d, want %d: a cursor must not start a differently-granted caller mid-history", got, want)
		}
		if got, want := replay(t, scopedTagHistoryAuth("repo-granted"), token), http.StatusOK; got != want {
			t.Fatalf("control status = %d, want %d", got, want)
		}
	})

	t.Run("a grant change mid-walk stops the cursor", func(t *testing.T) {
		t.Parallel()

		// The accepted cost of binding to the grant SET rather than to the
		// credential: widening a grant between two pages ends the walk and the
		// client restarts from page one. That is correct -- the filter's answer
		// changed underneath it -- and it is the same deploy boundary a key
		// rotation and a version bump already carry.
		token := issue(t, scopedTagHistoryAuth("repo-granted"))
		if got, want := replay(t, scopedTagHistoryAuth("repo-granted", "repo-extra"), token), http.StatusBadRequest; got != want {
			t.Fatalf("status = %d, want %d", got, want)
		}
	})

	t.Run("a reordered grant set keeps paging", func(t *testing.T) {
		t.Parallel()

		// The other side of that cost, and the one that would be a real bug:
		// the SAME grants arriving in a different order must not end the walk.
		token := issue(t, scopedTagHistoryAuth("repo-granted", "repo-extra"))
		if got, want := replay(t, scopedTagHistoryAuth("repo-extra", "repo-granted"), token), http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d: grant order is not part of the audience", got, want)
		}
	})
}
