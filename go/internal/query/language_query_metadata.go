// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
)

// languageEntitySearch is one content-store entity lookup for
// POST /api/v0/code/language-query, carrying the caller's repository grant
// alongside the filters.
//
// AllowedRepositoryIDs is never populated from the request body: the handler
// fills it from the caller's AuthContext through codeContentGrantScope
// (code_repository_selector.go). It restricts a corpus-wide read (empty RepoID)
// at the SQL WHERE, before the LIMIT page boundary. Empty leaves the read
// unrestricted, which is what an unscoped shared, admin, or local caller wants.
type languageEntitySearch struct {
	RepoID               string
	Language             string
	EntityType           string
	Query                string
	Limit                int
	AllowedRepositoryIDs []string
}

// languageEntityContentSearcher is the grant-bound content read this route
// prefers. *ContentReader implements it; a store that does not gets the
// per-repository fallback in searchLanguageEntities below, which is bound but
// issues one statement per granted repository.
type languageEntityContentSearcher interface {
	SearchEntitiesByLanguageAndTypeForAccess(context.Context, languageEntitySearch) ([]EntityContent, error)
}

// enrichLanguageResultsWithContentMetadata merges Postgres content-index
// metadata into graph-sourced results, keyed by file path/label/name/start
// line. merged reports true whenever a matched row's content metadata was
// non-empty and was merged into that row via mergeGraphFirstMetadata -- every
// no-op path below (nil Content, unmapped label, zero content rows, no key
// match, or a matched key whose content metadata is empty) reports
// merged=false. This is not the same as "at least one content VALUE actually
// changed the row": mergeGraphFirstMetadata lets any non-nil graph-derived
// value in the row's existing metadata override the content value at the
// same key, so a row whose content metadata keys are all shadowed by
// non-nil graph values still reports merged=true even though the final
// metadata map is unchanged from the graph-only answer. The direction stays
// safe either way -- this can only over-claim toward a hybrid/derived truth
// basis when a plain graph read would have been equally accurate, never
// launder a content-served answer as authoritative-graph-only (#5761 P1-1).
func (h *LanguageQueryHandler) enrichLanguageResultsWithContentMetadata(
	ctx context.Context,
	results []map[string]any,
	language string,
	label string,
	query string,
	repoID string,
	limit int,
	grant languageQueryGrant,
) ([]map[string]any, bool, error) {
	if h == nil || h.Content == nil || len(results) == 0 {
		return results, false, nil
	}

	entityType := graphLabelToContentEntityType(label)
	if entityType == "" {
		return results, false, nil
	}

	for i := range results {
		attachSemanticSummary(results[i])
	}

	// #5167 batch 2a: this is a SECOND content read, issued after the graph
	// already answered. Left unbound it reads every tenant's rows to build the
	// merge-key map below, so a key collision on file path/label/name/start
	// line would merge another tenant's metadata into a granted row.
	rows, err := h.searchLanguageEntities(ctx, languageEntitySearch{
		RepoID:               repoID,
		Language:             language,
		EntityType:           entityType,
		Query:                query,
		Limit:                limit,
		AllowedRepositoryIDs: grant.allowedRepositoryIDs,
	})
	if err != nil {
		return nil, false, fmt.Errorf("enrich language results with content metadata: %w", err)
	}
	if len(rows) == 0 {
		return results, false, nil
	}

	metadataByKey := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		metadataByKey[languageResultMatchKey(
			row.RelativePath,
			row.EntityType,
			row.EntityName,
			row.StartLine,
		)] = row.Metadata
	}

	merged := false
	for i := range results {
		key := languageResultMatchKey(
			StringVal(results[i], "file_path"),
			label,
			StringVal(results[i], "name"),
			IntVal(results[i], "start_line"),
		)
		metadata, ok := metadataByKey[key]
		if !ok || len(metadata) == 0 {
			continue
		}
		results[i]["metadata"] = mergeGraphFirstMetadata(results[i]["metadata"], metadata)
		attachSemanticSummary(results[i])
		merged = true
	}

	return results, merged, nil
}

func languageResultMatchKey(filePath string, entityType string, name string, startLine int) string {
	return fmt.Sprintf("%s|%s|%s|%d", filePath, entityType, name, startLine)
}

func mergeGraphFirstMetadata(existing any, fallback map[string]any) map[string]any {
	if len(fallback) == 0 {
		if metadata, ok := existing.(map[string]any); ok {
			return metadata
		}
		return nil
	}
	merged := make(map[string]any, len(fallback))
	for key, value := range fallback {
		merged[key] = value
	}
	if current, ok := existing.(map[string]any); ok {
		for key, value := range current {
			if value == nil {
				continue
			}
			merged[key] = value
		}
	}
	return merged
}

// searchLanguageEntities runs one content-store entity lookup with the grant
// bound.
//
// A store that satisfies languageEntityContentSearcher takes the grant into its
// own statement, so one read serves the whole granted set and the LIMIT page is
// taken from it. A store that does not -- the shape a test fake or an older
// implementation has -- can only be asked about one repository at a time, so a
// corpus-wide scoped search iterates the granted repositories rather than
// asking for repository "", which the unrestricted statement answers with every
// tenant's rows. That is the same fallback shape symbolNameFallbackEntities
// (code_symbol.go) uses on POST /api/v0/code/symbols/search.
func (h *LanguageQueryHandler) searchLanguageEntities(
	ctx context.Context,
	search languageEntitySearch,
) ([]EntityContent, error) {
	if h == nil || h.Content == nil {
		return nil, fmt.Errorf("content reader is required for %s queries", search.EntityType)
	}
	if searcher, ok := h.Content.(languageEntityContentSearcher); ok {
		return searcher.SearchEntitiesByLanguageAndTypeForAccess(ctx, search)
	}
	if search.RepoID != "" || len(search.AllowedRepositoryIDs) == 0 {
		return h.Content.SearchEntitiesByLanguageAndType(
			ctx, search.RepoID, search.Language, search.EntityType, search.Query, search.Limit,
		)
	}
	entities := make([]EntityContent, 0, search.Limit)
	for _, repoID := range search.AllowedRepositoryIDs {
		if len(entities) >= search.Limit {
			break
		}
		rows, err := h.Content.SearchEntitiesByLanguageAndType(
			ctx, repoID, search.Language, search.EntityType, search.Query, search.Limit-len(entities),
		)
		if err != nil {
			return nil, err
		}
		entities = append(entities, rows...)
	}
	return entities, nil
}

// queryContentByLanguage answers one dispatch branch entirely from the content
// store, with the caller's grant bound at the read.
func (h *LanguageQueryHandler) queryContentByLanguage(
	ctx context.Context,
	language, entityType, query, repoID string,
	limit int,
	grant languageQueryGrant,
) ([]map[string]any, error) {
	rows, err := h.searchLanguageEntities(ctx, languageEntitySearch{
		RepoID:               repoID,
		Language:             language,
		EntityType:           entityType,
		Query:                query,
		Limit:                limit,
		AllowedRepositoryIDs: grant.allowedRepositoryIDs,
	})
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result := map[string]any{
			"entity_id":  row.EntityID,
			"name":       row.EntityName,
			"labels":     []string{row.EntityType},
			"file_path":  row.RelativePath,
			"repo_id":    row.RepoID,
			"language":   row.Language,
			"start_line": row.StartLine,
			"end_line":   row.EndLine,
			"metadata":   row.Metadata,
		}
		attachSemanticSummary(result)
		results = append(results, result)
	}

	return results, nil
}

// *ContentReader is the only production content store (cmd/api/wiring.go and
// cmd/mcp-server/wiring.go both wire NewContentReader(db)), and the type
// assertion in searchLanguageEntities silently falls back to the
// one-repository-at-a-time path if it ever stops satisfying this interface.
// This line fails `go build`, not only `go test`, the moment that happens.
var _ languageEntityContentSearcher = (*ContentReader)(nil)
