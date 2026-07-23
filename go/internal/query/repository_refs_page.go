// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
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
//
// Bumped to 2 (#5503 T3): v1 encoded the mutable is_default flag as part of
// the sort key, which corrupts windowing when the default branch changes
// between two page fetches (a ref can be duplicated or skipped even though
// nothing was added or removed -- see
// TestGetRepositoryBranchesPagingDefaultChurnNoDupSkip). A v1 cursor now
// fails the version check below and decodes to an error; the client simply
// restarts from page 1. This is a deploy-boundary-only event (a cursor
// in flight across the exact moment of deploy) and safe because the
// endpoint has no side effects.
const repositoryRefPageCursorVersion = 2

// repositoryRefPageCursor is the forward-only keyset cursor for the combined
// branches+tags ref stream exposed by GET .../branches. It encodes the
// (kind, name) sort key of the last-emitted ref. The store persists rows
// ordered `is_default DESC, ref_kind, name`
// (content_reader_repository_refs.go), but the cursor deliberately does NOT
// include is_default: that flag is mutable (a repository's default branch
// can change), and encoding it into the key corrupts windowing across a
// default-branch change between page fetches (T3; see
// repositoryRefKeyLess). The handler re-sorts refs by (kind, name) before
// paging (sortRepositoryRefsForPaging) so the cursor's key always matches
// the order actually paged.
type repositoryRefPageCursor struct {
	Version int    `json:"v"`
	RepoID  string `json:"repo"`
	Kind    string `json:"k"` // "branch" or "tag"
	Name    string `json:"n"`
}

// encodeRepositoryRefPageCursor renders ref's (kind, name) sort key as an
// opaque forward-only page token scoped to repoID.
func encodeRepositoryRefPageCursor(repoID string, ref RepositoryRef) string {
	cursor := repositoryRefPageCursor{
		Version: repositoryRefPageCursorVersion,
		RepoID:  repoID,
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

// repositoryRefSortKey projects ref onto the same (kind, name) key shape as a
// decoded cursor, so a ref and a cursor can be compared with
// repositoryRefKeyLess.
func repositoryRefSortKey(ref RepositoryRef) repositoryRefPageCursor {
	return repositoryRefPageCursor{
		Kind: ref.Kind,
		Name: ref.Name,
	}
}

// repositoryRefKeyLess reports whether key a sorts strictly before key b
// under the paging order: kind ASCENDING (all branches precede all tags),
// then name ASCENDING. This is deliberately immutable per ref -- unlike
// is_default, a ref's kind and name do not change while it exists, so a
// cursor built from this key stays valid across concurrent ref churn
// (additions, deletions, or a default-branch change) between page fetches.
func repositoryRefKeyLess(a, b repositoryRefPageCursor) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Name < b.Name
}

// sortRepositoryRefsForPaging re-sorts refs in place by the exact (kind,
// name) Go byte-wise comparator the cursor's keyset math uses. This is
// LOAD-BEARING, not cosmetic: it replaces reliance on the store's `ORDER BY
// name` (Postgres collation/ICU-dependent, which can order case-mixed or
// non-ASCII ref names differently than Go's byte-wise string comparison)
// with the comparator windowing actually applies, closing a latent
// collation-vs-Go-order dup/skip for such names.
func sortRepositoryRefsForPaging(refs []RepositoryRef) {
	sort.Slice(refs, func(i, j int) bool {
		return repositoryRefKeyLess(repositoryRefSortKey(refs[i]), repositoryRefSortKey(refs[j]))
	})
}

// repositoryRefPageWindow returns the bounded page of refs strictly after
// cursor (or from the start when cursor is nil), assuming refs is already
// sorted by (kind, name) -- callers MUST call sortRepositoryRefsForPaging
// first. window is at most limit entries; remainder is whatever in refs
// follows window, used by
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
// span the branch/tag boundary (all branches sort before all tags -- kind
// ASCENDING), so both slices are populated from a single pass.
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
