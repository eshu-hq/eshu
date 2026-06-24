// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	defaultSearchDocumentSweepInterval = 30 * time.Second
	defaultSearchDocumentSweepLimit    = 200
)

// PendingSearchDocumentScope is one repository scope whose active generation has
// no curated search-document projection yet.
type PendingSearchDocumentScope struct {
	ScopeID      string
	GenerationID string
	SourceSystem string
}

// PendingSearchDocumentLister lists repository scopes whose active generation
// still needs a curated search-document projection. Implementations must bound
// the result by limit and select only scopes that have indexed content.
type PendingSearchDocumentLister interface {
	ListPendingSearchDocumentScopes(ctx context.Context, limit int) ([]PendingSearchDocumentScope, error)
}

// SearchDocumentProjectionSweeper periodically enqueues DomainEshuSearchDocument
// reducer intents for repository generations that have indexed content but no
// curated search documents yet (design 430). It is decoupled from the projector
// hot path so per-generation projection behavior is unchanged.
//
// Enqueue is idempotent: the reducer queue keys work items by
// scope+generation+domain+entity and inserts ON CONFLICT DO NOTHING, so
// re-enqueuing a still-pending scope each tick is a no-op, and an advanced
// active generation produces a fresh work item. The sweeper therefore needs no
// lease; concurrent sweepers converge on the same idempotent inserts.
type SearchDocumentProjectionSweeper struct {
	Pending  PendingSearchDocumentLister
	Intents  ReducerIntentWriter
	Limit    int
	Interval time.Duration
	Wait     func(context.Context, time.Duration) error
	Logger   *slog.Logger
}

func (s SearchDocumentProjectionSweeper) limit() int {
	if s.Limit <= 0 {
		return defaultSearchDocumentSweepLimit
	}
	return s.Limit
}

func (s SearchDocumentProjectionSweeper) interval() time.Duration {
	if s.Interval <= 0 {
		return defaultSearchDocumentSweepInterval
	}
	return s.Interval
}

// Run sweeps on an interval until the context is canceled.
func (s SearchDocumentProjectionSweeper) Run(ctx context.Context) error {
	wait := s.Wait
	if wait == nil {
		wait = sleepWithContext
	}
	for {
		if _, err := s.RunOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.logError(ctx, err)
		}
		if err := wait(ctx, s.interval()); err != nil {
			return err
		}
	}
}

// RunOnce lists pending repository scopes and enqueues one curated
// search-document intent per scope. It returns the number of intents enqueued.
func (s SearchDocumentProjectionSweeper) RunOnce(ctx context.Context) (int, error) {
	if s.Pending == nil || s.Intents == nil {
		return 0, nil
	}
	started := time.Now()
	scopes, err := s.Pending.ListPendingSearchDocumentScopes(ctx, s.limit())
	if err != nil {
		return 0, err
	}
	if len(scopes) == 0 {
		return 0, nil
	}
	intents := make([]ReducerIntent, 0, len(scopes))
	for _, pending := range scopes {
		intents = append(intents, ReducerIntent{
			ScopeID:      pending.ScopeID,
			GenerationID: pending.GenerationID,
			Domain:       reducer.DomainEshuSearchDocument,
			EntityKey:    "eshu_search_document:" + pending.ScopeID,
			Reason:       "search lane projection catch-up sweep",
			SourceSystem: pending.SourceSystem,
		})
	}
	result, err := s.Intents.Enqueue(ctx, intents)
	if err != nil {
		return 0, err
	}
	s.logSweep(ctx, len(scopes), result.Count, started)
	return result.Count, nil
}

func (s SearchDocumentProjectionSweeper) logSweep(ctx context.Context, pending int, enqueued int, startedAt time.Time) {
	if s.Logger == nil {
		return
	}
	s.Logger.InfoContext(
		ctx, "eshu search document projection sweep completed",
		slog.Int("pending_scopes", pending),
		slog.Int("enqueued_intents", enqueued),
		slog.Float64("duration_seconds", time.Since(startedAt).Seconds()),
		slog.String("domain", string(reducer.DomainEshuSearchDocument)),
	)
}

func (s SearchDocumentProjectionSweeper) logError(ctx context.Context, err error) {
	if s.Logger == nil {
		return
	}
	s.Logger.ErrorContext(ctx, "eshu search document projection sweep failed", slog.String("error", err.Error()))
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
