// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/contentread"
)

// ContentHandler serves HTTP endpoints for reading file and entity content
// from the Postgres content store. The implementation moved to
// internal/query/contentread (#6060); this alias preserves the compatibility
// surface for cmd/api, cmd/mcp-server, and root's own tests, none of which
// import the family package directly. The alias carries ContentHandler's
// exported methods (Mount) and fields unchanged; it cannot forward unexported
// helpers, but none of those cross this boundary.
type ContentHandler = contentread.ContentHandler

// ContentResultReranker reorders bounded content-search result rows by hybrid
// relevance. See contentread.ContentResultReranker for the full contract.
type ContentResultReranker = contentread.ContentResultReranker

// ContentHybridRanker reorders lexical content-search results by hybrid
// relevance. See contentread.ContentHybridRanker for the full contract.
type ContentHybridRanker = contentread.ContentHybridRanker

// FileContent is one file from the content store.
// See contentread.FileContent for the full field contract.
type FileContent = contentread.FileContent

// Content-search page bounds. The implementation moved to
// internal/query/contentread (#6060); these forwarders keep root's OpenAPI
// sweep test asserting the emitted schema against the handler's own limits.
const (
	ContentSearchDefaultLimit = contentread.ContentSearchDefaultLimit
	ContentSearchMaxLimit     = contentread.ContentSearchMaxLimit
	ContentSearchMaxOffset    = contentread.ContentSearchMaxOffset
)

// NewContentHybridRanker builds a content-search hybrid re-ranker. cmd/api
// and cmd/mcp-server call this through package query rather than contentread
// directly (#6060); it forwards unchanged to
// contentread.NewContentHybridRanker.
func NewContentHybridRanker(enabled bool) *ContentHybridRanker {
	return contentread.NewContentHybridRanker(enabled)
}
