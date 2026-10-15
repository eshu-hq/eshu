// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collisions

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// basenameCollisionPathSampleLimit caps the number of colliding paths reported
// in a single warning log entry to keep the log line bounded.
const basenameCollisionPathSampleLimit = 5

// ReportRepositoryBasenameCollisions inspects the discovered repository IDs
// from one collector cycle and emits a structured warning log and metric when
// the same repository basename appears at more than one distinct path. This is
// a pure observability call — it does not change which repositories are indexed.
//
// The signal is a HEURISTIC for accidental corpus nesting, not a true
// repository-identity check. Identity here is only the last path segment
// (basename) of each repoID — a cheap, label-free signal computed without
// reading .git/config on every discovered root. Distinct repositories can
// legitimately share a basename (org-a/utils and org-b/utils, or monorepo
// common/ directories), so a collision is a prompt to inspect the logged paths,
// not proof of duplication. The motivating case is accidental nesting or
// recursive copies (e.g. repos/repos/repos/… as in issue #3677): there the same
// basename appears at multiple depths and the counter advances, making the
// 4× inflation visible from metrics and logs rather than requiring post-hoc
// database forensics.
//
// Metric: eshu_dp_repository_basename_collision_total — incremented by the
// number of surplus (non-first) occurrences of each colliding basename. No path
// or basename labels are attached; those details are in the structured log.
//
// Log: "repository basename collision detected (possible accidental corpus
// nesting)" at WARN level, with fields:
//
//	identity          — the colliding basename
//	path_count        — total paths that share this basename
//	surplus_count     — paths beyond the first (matches the metric delta)
//	path_sample       — up to basenameCollisionPathSampleLimit paths (bounded)
//	total_path_count  — full count when the sample is truncated
func ReportRepositoryBasenameCollisions(
	ctx context.Context,
	repoIDs []string,
	logger *slog.Logger,
	inst *telemetry.Instruments,
) {
	if len(repoIDs) == 0 {
		return
	}

	// Group repoIDs by their basename.
	byBasename := make(map[string][]string, len(repoIDs))
	for _, id := range repoIDs {
		if id == "" {
			continue
		}
		basename := filepath.Base(id)
		if basename == "" || basename == "." {
			continue
		}
		byBasename[basename] = append(byBasename[basename], id)
	}

	var totalSurplus int64
	for basename, paths := range byBasename {
		if len(paths) <= 1 {
			continue
		}
		// Each path beyond the first is a surplus collision.
		surplus := int64(len(paths) - 1)
		totalSurplus += surplus

		if logger != nil {
			sample := paths
			truncated := false
			if len(sample) > basenameCollisionPathSampleLimit {
				sample = sample[:basenameCollisionPathSampleLimit]
				truncated = true
			}
			attrs := []any{
				slog.String("identity", basename),
				slog.Int("path_count", len(paths)),
				slog.Int("surplus_count", len(paths)-1),
				slog.Any("path_sample", sample),
			}
			if truncated {
				attrs = append(attrs, slog.Int("total_path_count", len(paths)))
			}
			logger.WarnContext(ctx, "repository basename collision detected (possible accidental corpus nesting)", attrs...)
		}
	}

	if totalSurplus > 0 && inst != nil {
		inst.RepositoryBasenameCollision.Add(ctx, totalSurplus)
	}
}
