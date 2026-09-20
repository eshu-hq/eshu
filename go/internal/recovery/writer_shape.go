// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// WriterShapeKey is the marker key tracking which graph-writer shape
// version the deployment's graph was last re-projected for. There is one
// key for all writers: writer-semantics fixes are rare, and a single
// version keeps the upgrade check to one marker read on the common path.
const WriterShapeKey = "graph-writer-shape"

// WriterShapeClaimLease bounds how long a won upgrade claim suppresses
// competing claimants. A crashed winner's claim expires so the next
// startup retries; a failed refinalize releases its claim immediately
// (see EnsureGraphWriterShape) instead of waiting out the lease.
const WriterShapeClaimLease = 5 * time.Minute

// WriterShapeStore persists the applied graph-writer shape version so a
// deployment retires stale generations exactly once per writer-semantics
// upgrade, no matter how many binaries restart into it.
//
// Conflict domain: one row per marker key. ClaimVersion is the only
// writer that advances claimed_at, and MarkAppliedVersion the only writer
// that advances applied_version; concurrent starters converge on one
// winner through the claim predicate, and the losers skip the refinalize.
type WriterShapeStore interface {
	// AppliedVersion reports the last retired writer shape version, or 0
	// when no retirement has ever been recorded.
	AppliedVersion(ctx context.Context, key string) (int, error)
	// ClaimVersion attempts to take the upgrade claim for version. It
	// reports true only when no version at least this new is applied and
	// no unexpired competing claim exists. Implementations must evaluate
	// that predicate atomically per claimant (second claimants block on
	// the winner's row and re-evaluate against it).
	ClaimVersion(ctx context.Context, key string, version int, lease time.Duration) (bool, error)
	// MarkAppliedVersion records version as retired. Callers invoke it
	// only after the upgrade refinalize succeeds.
	MarkAppliedVersion(ctx context.Context, key string, version int) error
	// ReleaseClaim clears this key's claim so the next starter retries
	// immediately instead of waiting out the lease. Callers invoke it
	// when the upgrade refinalize fails.
	ReleaseClaim(ctx context.Context, key string) error
}

// EnsureGraphWriterShape retires every active scope's generations when the
// binary's writer shape version is newer than the deployment's applied
// marker (issue #6868). Ordinary operation never reopens completed work,
// so a graph that persisted stale writer output keeps serving it after an
// upgrade until something reprojects: this is that something. The winner
// of the atomic claim runs one all-scopes refinalize through the same
// dedup-reset sequence the operator path uses, so the next drain
// reprojects with the fixed writers; every other starter is a no-op.
//
// It returns whether this caller performed the retirement. A failed
// refinalize releases the claim and reports retired=false with the error,
// so the next startup retries instead of waiting out the lease.
func (h *Handler) EnsureGraphWriterShape(
	ctx context.Context,
	shapes WriterShapeStore,
	key string,
	currentVersion int,
) (bool, error) {
	if h == nil {
		return false, fmt.Errorf("recovery handler is required")
	}
	if shapes == nil {
		return false, fmt.Errorf("writer shape store is required")
	}
	if strings.TrimSpace(key) == "" {
		return false, fmt.Errorf("writer shape key is required")
	}
	if currentVersion <= 0 {
		return false, fmt.Errorf("writer shape version must be positive, got %d", currentVersion)
	}

	applied, err := shapes.AppliedVersion(ctx, key)
	if err != nil {
		return false, fmt.Errorf("read applied writer shape version: %w", err)
	}
	if applied >= currentVersion {
		return false, nil
	}

	won, err := shapes.ClaimVersion(ctx, key, currentVersion, WriterShapeClaimLease)
	if err != nil {
		return false, fmt.Errorf("claim writer shape version %d: %w", currentVersion, err)
	}
	if !won {
		slog.InfoContext(ctx, "graph writer shape retirement already claimed",
			slog.String("key", key), slog.Int("version", currentVersion))
		return false, nil
	}

	slog.InfoContext(ctx, "retiring generations for graph writer shape upgrade",
		slog.String("key", key),
		slog.Int("applied_version", applied),
		slog.Int("current_version", currentVersion))
	result, err := h.Refinalize(ctx, RefinalizeFilter{AllScopes: true})
	if err != nil {
		if releaseErr := shapes.ReleaseClaim(ctx, key); releaseErr != nil {
			slog.WarnContext(ctx, "release writer shape claim after failed refinalize",
				slog.String("key", key), slog.String("error", releaseErr.Error()))
		}
		return false, fmt.Errorf("refinalize for writer shape version %d: %w", currentVersion, err)
	}
	if err := shapes.MarkAppliedVersion(ctx, key, currentVersion); err != nil {
		return false, fmt.Errorf("mark writer shape version %d applied: %w", currentVersion, err)
	}
	slog.InfoContext(ctx, "graph writer shape retirement refinalized",
		slog.String("key", key),
		slog.Int("version", currentVersion),
		slog.Int("enqueued", result.Enqueued),
		slog.Int("generations_retired", result.GenerationsRetired))
	return true, nil
}
