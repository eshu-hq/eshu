// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package artifacts holds the skeleton of the reducer walker for the
// config-artifact read model (#7126, decision record
// docs/internal/design/7126-ingest-time-config-artifacts.md).
//
// The walker repairs and backfills the stored config_paths rows: it finds
// work through the dirty join, then walks every repository from a persisted
// cursor, and derives each repository with the same protocol a content Write
// uses. PR3b-1a ships the shape only: [Config] with the record's defaults
// (K = 4 workers, skip after 5 failures), the [Claimer] and [Deriver] ports,
// and [Assign], which splits a claimed page over K workers by a stable hash
// of repo_id so one process never derives one repository twice at once.
// Nothing here runs a derive, reads an environment variable, or is wired
// into cmd/reducer; PR3b-2 adds the runner loop, its telemetry, and its
// variables.
package artifacts
