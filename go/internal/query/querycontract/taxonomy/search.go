// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taxonomy

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/rowvalue"
)

// LanguageResultMatchKey identifies one entity by where it sits in a file. It
// is shared with the entity and code-search enrichments (entity/metadata.go,
// codequery/search/enrich.go), which anchor their own reads
// differently, so a caller adding the repository to the key does so itself
// rather than this shape changing. The implementation moved from root's
// language_query_metadata.go for #6060 so a handler-family subpackage can
// build the same match key without importing root.
func LanguageResultMatchKey(filePath string, entityType string, name string, startLine int) string {
	return fmt.Sprintf("%s|%s|%s|%d", filePath, entityType, name, startLine)
}

// LanguageEntitySearch is one content-store entity lookup for
// POST /api/v0/code/language-query, carrying the caller's repository grant
// alongside the filters.
//
// AllowedRepositoryIDs is never populated from the request body: the handler
// fills it from the caller's AuthContext through its own repository grant
// resolution. It restricts a corpus-wide read (empty RepoID) at the SQL
// WHERE, before the LIMIT page boundary. Empty leaves the read unrestricted,
// which is what an unscoped shared, admin, or local caller wants. The
// implementation moved from root's language_query_metadata.go for #6060 so a
// handler-family subpackage can build the same search input without
// importing root.
type LanguageEntitySearch struct {
	RepoID               string
	Language             string
	EntityType           string
	Query                string
	Limit                int
	AllowedRepositoryIDs []string
}

// LanguageEntityContentSearcher is the grant-bound content read a
// language-query-shaped route prefers. The implementation moved from root's
// language_query_metadata.go for #6060 so a handler-family subpackage can
// name the same interface without importing root.
type LanguageEntityContentSearcher interface {
	SearchEntitiesByLanguageAndTypeForAccess(context.Context, LanguageEntitySearch) ([]querycontract.EntityContent, error)
}

// LanguageEntityIDLookup is one bounded content-metadata read for
// POST /api/v0/code/language-query, keyed by the entity ids the graph already
// returned rather than by a second filtered page (#7247).
//
// EntityIDs are graph entity ids whose label shares the content store's
// primary key (see the language package's contentIDAnchoredLabels). RepoID and
// AllowedRepositoryIDs carry the caller's repository scope exactly as
// LanguageEntitySearch does: a non-empty RepoID binds the read to that one
// repository, otherwise a non-empty AllowedRepositoryIDs binds it to the
// caller's grant, and both empty leaves it to the id set alone (the unscoped
// shared, admin, and local callers). Neither is ever taken from the request
// body.
//
// A lookup carries at most MaxLanguageEntityIDLookup ids; the reader rejects a
// larger set rather than issuing an unbounded statement.
type LanguageEntityIDLookup struct {
	EntityIDs            []string
	RepoID               string
	AllowedRepositoryIDs []string
}

// MaxLanguageEntityIDLookup bounds one LanguageEntityIDLookup to the largest
// language-query page (the route clamps limit to 200), so the id set, and the
// rows the lookup can return, never exceed one page.
const MaxLanguageEntityIDLookup = 200

// LanguageEntityMetadataByIDReader is the id-anchored content read the
// language-query enrichment prefers for a content-id-anchored label (#7247).
// It returns at most one row per id, with the repository scope above bound in
// the statement. Rows carry no source_cache: the enrichment merges metadata
// only. A store that does not implement it gets today's filtered page read.
type LanguageEntityMetadataByIDReader interface {
	ListEntityMetadataByIDsForAccess(context.Context, LanguageEntityIDLookup) ([]querycontract.EntityContent, error)
}

// ResultContentEntityType resolves a result row's content-entity type from
// its graph labels. The implementation moved from root's
// code_search_metadata.go for #6060 so a handler-family subpackage can
// resolve a row's content type without importing root.
func ResultContentEntityType(result map[string]any) string {
	labels := rowvalue.StringSliceVal(result, "labels")
	for _, label := range labels {
		if entityType := GraphLabelToContentEntityType(label); entityType != "" {
			return entityType
		}
	}
	return ""
}
