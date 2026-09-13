// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taghistory

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// CursorVersion is the only tag-history cursor payload version this build
// understands. A cursor carrying any other version is rejected rather than
// partially trusted, the rule RepositoryRefPageCursorVersion already applies in
// go/internal/query/repositoryreadmodel/repository_refs_page.go. Restarting
// paging is safe: the route has no side effects.
const CursorVersion = 1

// Cursor is the continuation token GET /api/v0/images/tag-history returns in
// place of the raw row offset (#6564 review finding 1).
//
// Before this change next_cursor was {"offset": n} taken from the raw
// pre-filter window, so a grant-filtered caller could subtract its own count
// from the advance and learn exactly how many rows of another tenant's history
// the filter had withheld, and could walk the same history at limit=1 to map
// where those rows sat in time. Two things close that: the route refills a
// scoped page to `limit` visible rows (RefillScopedPage), so limit-count
// carries no withheld-row information, and the continuation point leaves the
// wire as this token instead of an integer a caller can read, increment, or
// binary-search.
//
// The payload binds the token to the image_ref and the limit it was issued
// for, so an ISSUED cursor cannot be replayed against another image's history
// or re-aimed at a smaller page size to resume the limit=1 walk. Every
// rejection is a 400, never a silently reset or silently empty page.
//
// Those checks constrain a token this server issued; they do not authenticate
// one. The encoding is reversible and carries no MAC, so a caller can mint
// {"v":1,"ref":"<its own image_ref>","l":1,"o":N} for any N and every check
// above passes -- the limit=1 walk over another tenant's history is narrowed to
// callers willing to forge, not closed.
//
// THIS IS AN OPEN DEFECT, not an accepted residual (#6564 re-review finding 1).
// Do not describe the token as tamper-rejecting, and do not add wording anywhere
// that presents the leak as a disclosed-and-accepted limitation: a replacement
// is being designed (signed, keyset, or server-side cursor), and a keyset cursor
// naming the last VISIBLE row would remove the raw frontier from the token
// altogether rather than authenticate it. Do not build further on the raw-offset
// payload below. Tracked in
// docs/internal/evidence/6564-tag-history-grant-binding.md.
type Cursor struct {
	Version  int    `json:"v"`
	ImageRef string `json:"ref"`
	Limit    int    `json:"l"`
	Offset   int    `json:"o"`
}

// EncodeCursor renders the continuation offset for imageRef at limit as the
// token next_cursor carries.
func EncodeCursor(imageRef string, limit, offset int) string {
	// The payload is a bounded struct of ints and strings; Marshal cannot fail.
	raw, _ := json.Marshal(Cursor{
		Version:  CursorVersion,
		ImageRef: imageRef,
		Limit:    limit,
		Offset:   offset,
	})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeCursor validates a token produced by EncodeCursor and returns its
// continuation offset. It errors on malformed base64 or JSON, an unknown
// version, a negative offset, a cursor issued for another image_ref, and a
// cursor issued for another limit; callers MUST turn a non-nil error into a
// 400.
func DecodeCursor(raw, imageRef string, limit int) (int, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	var cursor Cursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return 0, fmt.Errorf("invalid cursor payload: %w", err)
	}
	if cursor.Version != CursorVersion {
		return 0, fmt.Errorf("unsupported cursor version %d", cursor.Version)
	}
	if cursor.Offset < 0 {
		return 0, fmt.Errorf("cursor offset %d is negative", cursor.Offset)
	}
	if cursor.ImageRef != imageRef {
		return 0, fmt.Errorf("cursor was issued for another image_ref")
	}
	if cursor.Limit != limit {
		return 0, fmt.Errorf("cursor was issued for limit %d, not %d", cursor.Limit, limit)
	}
	return cursor.Offset, nil
}
