// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
)

// scopeSelectorQuery resolves the operator's scope selector to candidate
// scope ids. It accepts the raw scope id or the scope's source key. LIMIT 2
// is the ambiguity detector: one row is an exact resolution, two rows mean
// one scope's id collided with another scope's source key (#7732).
const scopeSelectorQuery = `
SELECT scope.scope_id
FROM ingestion_scopes AS scope
WHERE scope.scope_id = $1
   OR scope.source_key = $1
ORDER BY scope.scope_id
LIMIT 2
`

// resolveScopeID resolves the operator's scope selector to the canonical
// scope id. Skip and reopen share this resolver so the two routes cannot
// diverge on the same input: zero matches return
// admin.ErrScopeSelectorNotFound, one match returns the scope id, and two
// matches return admin.ScopeSelectorAmbiguousError naming both scopes.
func (s *postgresStore) resolveScopeID(ctx context.Context, selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	rows, err := s.database.QueryContext(ctx, scopeSelectorQuery, selector)
	if err != nil {
		return "", fmt.Errorf("resolve scope selector: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var matched []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return "", fmt.Errorf("scan scope selector: %w", err)
		}
		matched = append(matched, scopeID)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("read scope selector: %w", err)
	}
	switch len(matched) {
	case 0:
		return "", admin.ErrScopeSelectorNotFound
	case 1:
		return matched[0], nil
	default:
		return "", admin.ScopeSelectorAmbiguousError{Selector: selector, ScopeIDs: matched}
	}
}
