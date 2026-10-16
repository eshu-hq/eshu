// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/rebuild/reset"
)

// requestDeltaBaseMissingReindex records a per-repository reindex watermark
// for a base-missing refusal's scope, routing the refusal to the forced-full
// path: the git selector's next reconcile decision forces the full re-parse
// that restores the base (decideReindex, #7620). Only git default-branch
// scopes can carry such a watermark (recovery.IsGitDefaultBranchScope,
// shared with the refinalize path); other scopes rely on their own
// collector's next full generation.
//
// It runs after the refusal mark, as its own statement reusing the
// refinalize path's RequestReindexQuery semantics: a watermark failure never
// un-marks the refusal, and the next sync's delta refuses again and retries
// the watermark, so a transient failure self-heals instead of leaving the
// scope delta-active without a repair request. Failures log at WARN; the
// queue carries no logger, so this uses the default one.
func (q ProjectorQueue) requestDeltaBaseMissingReindex(ctx context.Context, scopeID string) {
	if !recovery.IsGitDefaultBranchScope(scopeID) {
		return
	}
	detached := context.WithoutCancel(ctx)
	rows, err := q.database.QueryContext(detached, reset.RequestReindexQuery, []string{scopeID})
	if err != nil {
		slog.Default().WarnContext(detached, "projector delta base-missing reindex request failed",
			slog.String("scope_id", scopeID), slog.String("error", err.Error()))
		return
	}
	defer func() { _ = rows.Close() }()
	stored := 0
	for rows.Next() {
		stored++
	}
	if err := rows.Err(); err != nil || stored != 1 {
		slog.Default().WarnContext(detached, "projector delta base-missing reindex request incomplete",
			slog.String("scope_id", scopeID), slog.Int("watermarks_stored", stored))
	}
}
