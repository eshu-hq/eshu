// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"fmt"
)

// LanguageResultMatchKey identifies one entity by where it sits in a file. It
// is shared with the entity and code-search enrichments (root's
// entity_metadata.go, code_search_metadata.go), which anchor their own reads
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
	SearchEntitiesByLanguageAndTypeForAccess(context.Context, LanguageEntitySearch) ([]EntityContent, error)
}
