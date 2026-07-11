// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"
)

// SearchVectorBuildScopeProgress records keyset progress produced for one
// scope by a successful bounded vector build.
type SearchVectorBuildScopeProgress struct {
	ScopeID        string
	GenerationID   string
	DocumentCount  int
	LastDocumentID string
}

// vectorScopeStateKey joins scope_id and generation_id for a fence map key.
func vectorScopeStateKey(scopeID, generationID string) string {
	return scopeID + "\x00" + generationID
}

func (r *SearchVectorBuildRunner) buildRequests(
	scopes []SearchVectorBuildPendingScope,
	documentLimit int,
	fences map[string]int64,
) []SearchVectorBuildRequest {
	reqs := make([]SearchVectorBuildRequest, 0, len(scopes))
	for _, pending := range scopes {
		reqs = append(reqs, SearchVectorBuildRequest{
			ScopeID:            pending.ScopeID,
			GenerationID:       pending.GenerationID,
			RepoID:             pending.RepoID,
			AfterDocumentID:    pending.DocumentCursor,
			ProviderProfileID:  r.Config.ProviderProfileID,
			SourceClass:        r.Config.SourceClass,
			EmbeddingModelID:   r.Config.EmbeddingModelID,
			VectorIndexVersion: r.Config.VectorIndexVersion,
			Limit:              documentLimit,
			ProjectionRevision: pending.ProjectionRevision,
			BuildFence:         fences[vectorScopeStateKey(pending.ScopeID, pending.GenerationID)],
		})
	}
	return reqs
}

// finalizeVectorScopeStates checks per-scope vector completeness and
// CAS-publishes ready state for scopes that are fully built. It is called
// from both the batch fast path and the serial per-scope path.
func (r *SearchVectorBuildRunner) finalizeVectorScopeStates(
	ctx context.Context,
	scopes []SearchVectorBuildPendingScope,
	identity SearchVectorBuildIdentity,
	fences map[string]int64,
	progress []SearchVectorBuildScopeProgress,
	documentLimit int,
) int {
	if r.ScopeState == nil {
		return 0
	}
	finalized := 0
	progressByScope := make(map[string]SearchVectorBuildScopeProgress, len(progress))
	for _, item := range progress {
		progressByScope[vectorScopeStateKey(item.ScopeID, item.GenerationID)] = item
	}
	for _, scope := range scopes {
		key := vectorScopeStateKey(scope.ScopeID, scope.GenerationID)
		fence := fences[key]
		item, hasProgress := progressByScope[key]
		effectiveCursor := scope.DocumentCursor
		if item.LastDocumentID != "" {
			effectiveCursor = item.LastDocumentID
		}
		if hasProgress && item.LastDocumentID != "" {
			advanced, err := r.ScopeState.AdvanceDocumentCursor(ctx, scope.ScopeID, scope.GenerationID, identity, scope.ProjectionRevision, fence, item.LastDocumentID)
			if err != nil {
				slog.Warn("search vector document cursor advance failed",
					"scope_id", scope.ScopeID, "generation_id", scope.GenerationID, "error", err)
				continue
			}
			if !advanced {
				slog.Warn("search vector document cursor advance skipped",
					"scope_id", scope.ScopeID, "generation_id", scope.GenerationID, "reason", "cas_rejected")
				continue
			}
		}
		complete, err := r.ScopeState.ScopeVectorComplete(ctx, scope.ScopeID, scope.GenerationID, identity)
		if err != nil {
			slog.Warn("search vector scope complete check failed",
				"scope_id", scope.ScopeID,
				"generation_id", scope.GenerationID,
				"error", err,
			)
			continue
		}
		if !complete {
			if hasProgress && effectiveCursor != "" && item.DocumentCount < documentLimit {
				reset, err := r.ScopeState.ResetDocumentCursor(ctx, scope.ScopeID, scope.GenerationID, identity, scope.ProjectionRevision, fence)
				if err != nil {
					slog.Warn("search vector document cursor reset failed",
						"scope_id", scope.ScopeID, "generation_id", scope.GenerationID, "error", err)
				} else if !reset {
					slog.Warn("search vector document cursor reset skipped",
						"scope_id", scope.ScopeID, "generation_id", scope.GenerationID, "reason", "cas_rejected")
				} else {
					slog.Info("search vector document cursor wrapped",
						"scope_id", scope.ScopeID, "generation_id", scope.GenerationID,
						"document_count", item.DocumentCount, "document_limit", documentLimit)
				}
			}
			continue
		}
		ok, err := r.ScopeState.FinalizeReady(ctx, scope.ScopeID, scope.GenerationID, identity, scope.ProjectionRevision, fence)
		if err != nil {
			slog.Warn("search vector scope finalize ready failed",
				"scope_id", scope.ScopeID,
				"generation_id", scope.GenerationID,
				"error", err,
			)
			continue
		}
		if !ok {
			slog.Warn("search vector scope finalize skipped",
				"scope_id", scope.ScopeID,
				"generation_id", scope.GenerationID,
				"reason", "cas_rejected",
			)
			continue
		}
		finalized++
	}
	return finalized
}
