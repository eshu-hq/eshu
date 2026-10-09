// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Beginner opens the guarded read-only repeatable-read transaction one search
// runs in. db.ReadStore satisfies it.
type Beginner interface {
	BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error)
}

// Searcher answers file-content substring searches that carry no repository
// filter, inside a work budget. Fields other than Store and Budget are
// optional.
type Searcher struct {
	// Store opens the read transaction.
	Store Beginner
	// Budget is the requested SQL work budget; zero means DefaultBudget.
	Budget time.Duration
	// Now is the clock; nil means time.Now. Tests inject a fake clock.
	Now func() time.Time
	// Tracer records the search span; nil records nothing.
	Tracer trace.Tracer
	// Instruments records the outcome counters and the elapsed histogram; nil
	// records nothing.
	Instruments *telemetry.Instruments
	// Logger receives one line per partial result; nil means slog.Default().
	Logger *slog.Logger
}

// Search runs one budgeted search. limit is the page size and offset the
// number of matches to skip; cursor, when non-zero, starts the walk strictly
// after that key. pattern is the raw substring (ILIKE wildcards in it keep
// their meaning, as in every other content search).
//
// The whole search is one REPEATABLE READ READ ONLY transaction whose first
// statement is the substring-index readiness check; a readiness failure is
// returned with its SQLSTATE intact so the caller can map it to the 503-class
// answer. A page the budget could not complete is returned with a non-nil
// Partial, never as an error.
func (s *Searcher) Search(
	ctx context.Context,
	pattern string,
	limit, offset int,
	cursor querycontract.SearchCursor,
) (querycontract.FileSearchPage, error) {
	if pattern == "" {
		return querycontract.FileSearchPage{}, errors.New("unscoped search: pattern is required")
	}
	if limit < 1 {
		return querycontract.FileSearchPage{}, fmt.Errorf("unscoped search: limit %d must be at least 1", limit)
	}
	if offset < 0 {
		return querycontract.FileSearchPage{}, fmt.Errorf("unscoped search: offset %d must not be negative", offset)
	}
	budget := s.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}

	ctx, span := s.startSpan(ctx)
	defer span.End()

	tx, err := s.Store.BeginReadOnlySnapshot(ctx)
	if err != nil {
		span.RecordError(err)
		return querycontract.FileSearchPage{}, fmt.Errorf("begin unscoped search snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	w := &walk{
		tx:        tx,
		plan:      newPlan(budget),
		now:       now,
		pattern:   pattern,
		want:      offset + limit + 1,
		cursor:    cursor,
		requested: cursor,
		start:     now(),
	}
	if err := w.exec(ctx, readinessSQL); err != nil {
		span.RecordError(err)
		return querycontract.FileSearchPage{}, fmt.Errorf("check content substring index readiness: %w", err)
	}
	if err := w.run(ctx); err != nil {
		span.RecordError(err)
		return querycontract.FileSearchPage{}, fmt.Errorf("search unscoped file content: %w", err)
	}
	page := w.page(offset, limit)
	s.record(ctx, span, w, page, !cursor.IsZero())
	return page, nil
}
