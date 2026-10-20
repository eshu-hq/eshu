// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package artifacts holds the skeleton of the reducer walker for the
// config-artifact read model (#7126, decision record
// docs/internal/design/7126-ingest-time-config-artifacts.md).
//
// The walker repairs and backfills the stored config_paths rows: it finds
// work through the dirty join, then walks every repository from a persisted
// cursor, and derives each repository with the same protocol a content Write
// uses. [Config] carries the record's defaults (K = 4 workers, skip after 5
// failures), [Assign] splits a claimed page over K workers by a stable hash
// of repo_id so one process never derives one repository twice at once, and
// the [Claimer], [Deriver], [Lister] and [Marker] ports keep this leaf from
// importing storage: cmd/reducer adapts the storage functions to them. The
// [Runner] loop derives the dirty join's repositories with trigger repair,
// then one claimed walk page with trigger backfill, and records the backfill
// marker when the walk wraps with nothing dirty left. The walker's
// environment variables live in the env registry; the loader sits beside the
// derive loader in internal/content.
package artifacts
