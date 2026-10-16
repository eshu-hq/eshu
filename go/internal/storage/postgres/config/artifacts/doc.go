// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package artifacts owns the Postgres config-artifact read model (#7126):
// the name-free config_paths rows of each repository, stored at content-write
// time so the story and context routes read them with one statement instead
// of re-parsing every related repository per request. The decision record is
// docs/internal/design/7126-ingest-time-config-artifacts.md; the protocol
// companion holds the tables and the derive protocol.
//
// Migration 170 creates the tables: config_artifact_content_versions (one
// content version per repository), config_artifact_repo_state (one derive
// state per repository), config_artifact_paths (the rows, keyed on repo_id
// and a generated row_digest), config_artifact_backfill_markers, and
// config_artifact_reconcile_cursor. Migration 182 adds the content_files
// version-fence triggers, which bump a repository's version once per content
// transaction, and migration 183 seeds one version-0 row per repository. No
// derive or reader exists yet.
//
// This package defines the contracts the later PRs build on:
//
//   - [CheckEncoding], the start-up check that the database is UTF8. The
//     row_digest column hashes config_artifact_utf8(text), which is IMMUTABLE
//     only per database; on another encoding the read model stays off.
//   - [Reader], [Repository], [Row] and [Classify], the read-model port and the
//     reader's trust condition: stored rows are served only for the current
//     content version and extractor version after a successful derive.
//   - [ClaimPage] and [LoadCursor], the walker's shared cursor claim over the
//     versions table, and [ListDirty], the walker's dirty join.
//   - [Trigger] and [Outcome], the derive labels, and the status values
//     [StatusNotInstalled], [StatusBackfilling] and [StatusReady].
//
// Version-row rule: no statement deletes or resets a version row except one
// that also deletes the repository's state row. A guard test enumerates every
// Go literal expression and migration in the module and enforces it; it
// cannot prove that both deletes of a retirement statement name the same
// repository.
package artifacts
