// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// documentationRepositoryScopeLookup finds the ingestion scope behind one
// repository anchor. ingestion_scopes is a hundreds-of-rows table, so the
// four-way OR with LIMIT 2 is a bounded probe, never a corpus scan. LIMIT 2
// exists so an ambiguous name stays ambiguous instead of silently picking a
// winner.
const documentationRepositoryScopeLookup = `SELECT scope_id,
	COALESCE(NULLIF(payload->>'repo', ''), NULLIF(payload->>'repo_id', ''), NULLIF(source_key, ''), scope_id)
FROM ingestion_scopes
WHERE scope_id = $1 OR source_key = $1 OR payload->>'repo' = $1 OR payload->>'repo_id' = $1
LIMIT 2`

// resolveDocumentationRepositoryScope binds a scope-less documentation-facts
// filter to its repository's scope before SQL is built (#7098). A name-form
// repository (or any repository value that names exactly one ingestion scope)
// becomes a ScopeID read, which binds through the scope's active generation
// scalar subquery over the keyset index, instead of an active_join that probes
// every ingestion scope while its payload containment matches zero rows.
//
// The rewrite is conservative: an already-scoped filter needs no lookup, an
// ambiguous or unknown repository keeps the filter byte-identical, and a
// lookup failure keeps the current SQL rather than failing the read. The
// repository anchor itself is canonicalized to the scope's stored id only when
// the caller did not already name the scope id, so id-form callers see no
// change at all. A repository combined with a service, target, or explicit
// generation anchor stays untouched: those anchors shadow the repository in
// the SQL, so binding a scope would silently narrow result content.
func resolveDocumentationRepositoryScope(
	ctx context.Context,
	store db.ReadStore,
	filter documentationFactFilter,
) (documentationFactFilter, bool) {
	if strings.TrimSpace(filter.ScopeID) != "" {
		return filter, false
	}
	if strings.TrimSpace(filter.ServiceID) != "" ||
		strings.TrimSpace(filter.TargetID) != "" ||
		strings.TrimSpace(filter.GenerationID) != "" {
		return filter, false
	}
	repository := strings.TrimSpace(filter.Repository)
	if repository == "" {
		return filter, false
	}
	rows, err := store.QueryContext(ctx, documentationRepositoryScopeLookup, repository)
	if err != nil {
		return filter, false
	}
	defer func() { _ = rows.Close() }()
	type scopeMatch struct {
		scopeID   string
		canonical string
	}
	matches := make([]scopeMatch, 0, 2)
	for rows.Next() {
		var match scopeMatch
		if err := rows.Scan(&match.scopeID, &match.canonical); err != nil {
			return filter, false
		}
		matches = append(matches, match)
	}
	if err := rows.Err(); err != nil || len(matches) != 1 {
		return filter, false
	}
	match := matches[0]
	filter.ScopeID = match.scopeID
	if repository != match.scopeID && match.canonical != "" && match.canonical != repository {
		filter.Repository = match.canonical
	}
	return filter, true
}
