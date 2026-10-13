// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// EntityPage is one page of a budgeted entity walk. Hits holds the page rows
// in key order with undecayed raw metadata; the query layer decodes each
// through its one metadata decode into querycontract.EntitySearchPage. More
// reports, for a complete answer, that a row exists past the page. Partial is
// non-nil when the budget ended the search first.
type EntityPage struct {
	Hits    []querycontract.EntitySearchHit
	More    bool
	Partial *querycontract.SearchPartial
}

// SearchEntities runs one budgeted entity-content search with no repository
// filter. It is the entity twin of Search: the same probe, capped
// continuation, trigram tail, and continuation phases over content_entities
// in (repo_id, relative_path, start_line, entity_id) order. limit is the page
// size and offset the number of matches to skip; cursor, when non-zero,
// starts the walk strictly after that entity key.
//
// The whole search is one REPEATABLE READ READ ONLY transaction whose first
// statement is the substring-index readiness check; a readiness failure is
// returned with its SQLSTATE intact so the caller can map it to the 503-class
// answer. A page the budget could not complete is returned with a non-nil
// Partial, never as an error.
func (s *Searcher) SearchEntities(
	ctx context.Context,
	pattern string,
	limit, offset int,
	cursor querycontract.SearchCursor,
) (EntityPage, error) {
	if pattern == "" {
		return EntityPage{}, errors.New("unscoped entity search: pattern is required")
	}
	if limit < 1 {
		return EntityPage{}, fmt.Errorf("unscoped entity search: limit %d must be at least 1", limit)
	}
	if offset < 0 {
		return EntityPage{}, fmt.Errorf("unscoped entity search: offset %d must not be negative", offset)
	}
	hits, more, p, err := search(ctx, s, &entityShape, pattern, limit, offset, cursor)
	if err != nil {
		return EntityPage{}, err
	}
	return entityPage(hits, more, p), nil
}
