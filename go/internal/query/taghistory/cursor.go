// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taghistory

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// CursorVersion is the only tag-history cursor payload version this build
// understands. A cursor carrying any other version is rejected rather than
// partially trusted, the rule RepositoryRefPageCursorVersion already applies in
// go/internal/query/repositoryreadmodel/repository_refs_page.go. Restarting
// paging is safe: the route has no side effects.
//
// Version 1 was a raw row offset and is refused by this build. A client holding
// one restarts from page one, the same deploy-boundary rule a
// RepositoryRefPageCursorVersion bump carries.
const CursorVersion = 2

// ErrCursorRequiresUID and the other decode errors below are the only reasons
// DecodeCursor refuses a payload. None of them is a tamper detection: the token
// carries no MAC. They stop an unusable payload from being partially trusted.
var ErrCursorRequiresUID = errors.New("cursor is missing its uid key")

// Cursor is the continuation token GET /api/v0/images/tag-history returns.
//
// It is a KEYSET cursor: it names the (first_observed_at, uid) position of one
// row in the statement's total order, and the next page is "your visible rows
// after this position". It replaced a raw row offset (#6564 re-review finding
// 1). An offset was a position in the PRE-FILTER history, so a scoped caller
// that base64-decoded its own token read the frontier the grant filter had
// advanced past, and one that re-encoded an edited offset could walk another
// tenant's withheld history a row at a time. A key cannot do either: there is
// no position-shaped field to read or increment, a forged key returns only the
// forger's own visible rows, and the key a page issues names a row that page
// already returned -- except on the fully-withheld capped page documented
// below, which is the one residue this design does not close.
//
// Three properties follow from the key rather than from a check:
//
//   - Forward-only and duplicate-free. An observation inserted behind the key
//     between two requests does not shift the walk, so no row is returned
//     twice; it is simply not seen, the same contract RepositoryRefPageCursor
//     documents. An offset shifted under exactly that insert.
//   - Valid at any page size. There is no Limit binding: it existed only to
//     stop an offset token being re-aimed at limit=1, and a key has no position
//     to re-aim. A caller may change limit mid-walk.
//   - Stable across replicas and restarts. The key is a property tuple any
//     replica can evaluate; no server-side secret exists or is needed.
//
// NullAt is needed because the store holds THREE timestamp states with three
// sort positions: a stored empty string (a zero ObservedAt, written by
// ociTagObservedAtValue in go/internal/storage/cypher), a real millisecond
// string, and no property at all (nodes created before #5459 shipped
// first_observed_at). The empty string needs no special case, because
// t.first_observed_at compares greater than it for every non-empty string. "No
// timestamp" cannot be expressed as a string value at all, and those rows sort
// LAST (measured on the pinned build, see
// docs/internal/evidence/6564-tag-history-keyset-pagination.md), so NullAt
// selects the null-tail statement instead of a string comparison.
//
// Residue this does NOT close, disclosed on every caller-facing surface: when a
// capped page kept no visible row at all, the token must name the last RAW row
// scanned or the caller re-reads the same span forever. That row may be one the
// caller may not see, so such a page discloses one withheld observation's
// first_observed_at and uid per fully-withheld span. uid is
// facts.StableID("OCIRegistryCanonicalNode", ...) -- a hash, not readable, but
// a caller already holding a candidate digest can confirm it by recomputing it.
type Cursor struct {
	Version  int    `json:"v"`
	ImageRef string `json:"ref"`
	At       string `json:"at,omitempty"`
	NullAt   bool   `json:"nt,omitempty"`
	UID      string `json:"uid"`
}

// EncodeCursor renders key as the token next_cursor carries for imageRef.
func EncodeCursor(imageRef string, key Key) string {
	// The payload is a bounded struct of a string, a bool and an int; Marshal
	// cannot fail.
	raw, _ := json.Marshal(Cursor{
		Version:  CursorVersion,
		ImageRef: imageRef,
		At:       key.At,
		NullAt:   key.NullAt,
		UID:      key.UID,
	})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor validates a token produced by EncodeCursor and returns the key
// it names. It errors on malformed base64 or JSON, an unknown version (which
// every version-1 offset token is), a cursor issued for another image_ref, a
// missing uid, and a null-tail key that also carries a timestamp; callers MUST
// turn a non-nil error into a 400.
//
// It does NOT error on a key that matches no current row. A retracted or
// re-projected observation is not a paging failure: the predicate is on values,
// not on that row still existing, so the next page is simply the rows after
// that position. A key beyond the end of the history yields an empty,
// untruncated page.
func DecodeCursor(raw, imageRef string) (Key, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Key{}, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	var cursor Cursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return Key{}, fmt.Errorf("invalid cursor payload: %w", err)
	}
	if cursor.Version != CursorVersion {
		return Key{}, fmt.Errorf("unsupported cursor version %d", cursor.Version)
	}
	if cursor.ImageRef != imageRef {
		return Key{}, errors.New("cursor was issued for another image_ref")
	}
	if cursor.UID == "" {
		return Key{}, ErrCursorRequiresUID
	}
	if cursor.NullAt && cursor.At != "" {
		return Key{}, errors.New("cursor claims no first_observed_at but carries one")
	}
	return Key{At: cursor.At, NullAt: cursor.NullAt, UID: cursor.UID}, nil
}
