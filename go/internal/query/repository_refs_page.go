// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// repositoryRefPageDefaultLimit is the page size applied when the caller
// supplies no limit query param. #5503 (option A): the branches/tags endpoint
// always bounds its response, even with no params, rather than returning the
// full unbounded ref list.
const repositoryRefPageDefaultLimit = 100

// repositoryRefPageMaxLimit is the largest limit a caller may request.
const repositoryRefPageMaxLimit = 500

// repositoryRefPageCursorVersion is the only cursor payload version this
// build understands. A cursor from an unknown version is rejected as
// invalid rather than partially trusted.
const repositoryRefPageCursorVersion = 1

// repositoryRefPageCursor is the forward-only keyset cursor for the combined
// branches+tags ref stream exposed by GET .../branches. It encodes the full
// sort key -- default-rank, kind, name -- of the last-emitted ref, mirroring
// the store's `ORDER BY is_default DESC, ref_kind, name` order
// (content_reader_repository_refs.go). Encoding the full key (not just the
// name) matters: the first component is the default-rank, not alphabetical
// order, so a cursor placed right after the default ref must not skip an
// alphabetically-earlier branch (see repositoryRefKeyLess).
type repositoryRefPageCursor struct {
	Version int    `json:"v"`
	RepoID  string `json:"repo"`
	Default int    `json:"d"` // 1 for the default ref, 0 otherwise; mirrors is_default DESC
	Kind    string `json:"k"` // "branch" or "tag"
	Name    string `json:"n"`
}

// encodeRepositoryRefPageCursor renders ref's full sort key as an opaque
// forward-only page token scoped to repoID.
func encodeRepositoryRefPageCursor(repoID string, ref RepositoryRef) string {
	cursor := repositoryRefPageCursor{
		Version: repositoryRefPageCursorVersion,
		RepoID:  repoID,
		Default: repositoryRefDefaultRank(ref),
		Kind:    ref.Kind,
		Name:    ref.Name,
	}
	// cursor fields are a bounded struct of ints/strings; Marshal cannot fail.
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeRepositoryRefPageCursor validates and decodes an opaque cursor
// produced by encodeRepositoryRefPageCursor. It returns an error on any
// malformed base64/JSON, unknown version, unknown kind, or cross-repository
// cursor -- callers MUST turn a non-nil error into a 400, never a silently
// empty or reset page.
func decodeRepositoryRefPageCursor(raw, repoID string) (repositoryRefPageCursor, error) {
	var cursor repositoryRefPageCursor
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return cursor, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return cursor, fmt.Errorf("invalid cursor payload: %w", err)
	}
	if cursor.Version != repositoryRefPageCursorVersion {
		return cursor, fmt.Errorf("unsupported cursor version %d", cursor.Version)
	}
	if cursor.Kind != "branch" && cursor.Kind != "tag" {
		return cursor, fmt.Errorf("unsupported cursor kind %q", cursor.Kind)
	}
	if cursor.RepoID != repoID {
		return cursor, fmt.Errorf("cursor repo mismatch")
	}
	return cursor, nil
}

// repositoryRefDefaultRank returns the cursor default-rank component: 1 for
// the default ref, 0 otherwise. It mirrors the store's `is_default DESC`
// clause so windowing never falls back to comparing the default ref's name
// alphabetically against the rest of the stream.
func repositoryRefDefaultRank(ref RepositoryRef) int {
	if ref.Default {
		return 1
	}
	return 0
}

// repositoryRefSortKey projects ref onto the same (default-rank, kind, name)
// key shape as a decoded cursor, so a ref and a cursor can be compared with
// repositoryRefKeyLess.
func repositoryRefSortKey(ref RepositoryRef) repositoryRefPageCursor {
	return repositoryRefPageCursor{
		Default: repositoryRefDefaultRank(ref),
		Kind:    ref.Kind,
		Name:    ref.Name,
	}
}

// repositoryRefKeyLess reports whether key a sorts strictly before key b
// under the store's (is_default DESC, ref_kind ASC, name ASC) order. Default
// rank compares DESCENDING (rank 1 first); kind and name compare ASCENDING.
// This asymmetry is the ordering trap the design guards against: comparing
// only ref names would let a cursor placed after the default ref "main" skip
// an alphabetically-earlier branch "alpha", because "alpha" < "main" by name
// alone but sorts AFTER the default ref in the store's real order.
func repositoryRefKeyLess(a, b repositoryRefPageCursor) bool {
	if a.Default != b.Default {
		return a.Default > b.Default
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Name < b.Name
}

// repositoryRefPageWindow returns the bounded page of refs strictly after
// cursor (or from the start when cursor is nil), assuming refs is already in
// the store's (is_default DESC, ref_kind, name) order. window is at most
// limit entries; remainder is whatever in refs follows window, used by
// callers to derive the deprecated tags_truncated field without a second
// pass over the full list. truncated and nextCursor follow the
// WorkItemEvidencePage convention (work_item_evidence_page.go): truncated is
// true exactly when more refs exist beyond window, and nextCursor -- set
// only when truncated -- is window's last ref's own sort key, so paging
// forward never skips or repeats a ref regardless of concurrent ref churn.
func repositoryRefPageWindow(repoID string, refs []RepositoryRef, cursor *repositoryRefPageCursor, limit int) (window, remainder []RepositoryRef, truncated bool, nextCursor string) {
	start := 0
	if cursor != nil {
		for start < len(refs) && !repositoryRefKeyLess(*cursor, repositoryRefSortKey(refs[start])) {
			start++
		}
	}
	after := refs[start:]
	if len(after) > limit {
		window = after[:limit]
		remainder = after[limit:]
		truncated = true
		nextCursor = encodeRepositoryRefPageCursor(repoID, window[len(window)-1])
		return window, remainder, truncated, nextCursor
	}
	return after, nil, false, ""
}

// repositoryRefWindowEntries splits one paged window of refs into branch and
// tag wire entries, preserving the window's relative order. A window may
// span the branch/tag boundary (branches sort before tags within each
// default-rank group), so both slices are populated from a single pass.
func repositoryRefWindowEntries(window []RepositoryRef) (branches, tags []map[string]any) {
	branches = make([]map[string]any, 0)
	tags = make([]map[string]any, 0)
	for _, ref := range window {
		switch ref.Kind {
		case "tag":
			tags = append(tags, repositoryRefEntry(ref, false))
		default:
			branches = append(branches, repositoryRefEntry(ref, true))
		}
	}
	return branches, tags
}

// repositoryRefsContainTag reports whether refs contains at least one tag
// entry. Used to derive the deprecated tags_truncated field from the exact
// in-memory remainder rather than re-deriving a separate tag-only cap.
func repositoryRefsContainTag(refs []RepositoryRef) bool {
	for _, ref := range refs {
		if ref.Kind == "tag" {
			return true
		}
	}
	return false
}
