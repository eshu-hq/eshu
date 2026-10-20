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
// config_artifact_reconcile_cursor. Migration 186 adds the content_files
// version fence, which bumps a repository's version once per content
// transaction at COMMIT, and migration 187 seeds one version-0 row per
// repository. The derive exists; ContentWriter.Write calls it as its last
// step (PR3b-2c-2), and no reader exists yet.
//
// This package defines the contracts the later PRs build on:
//
//   - [CheckEncoding], the start-up check that the database is UTF8. The
//     row_digest column hashes config_artifact_utf8(text), which is IMMUTABLE
//     only per database; on another encoding the read model stays off.
//   - [CheckFence], the start-up check that the five migration 186 fence
//     triggers exist on their tables and fire for an ordinary session
//     (tgenabled 'O' or 'A'); otherwise the read model stays off with
//     [ReasonVersionFenceMissing]. The deriving binaries run both checks
//     once at process start.
//   - [Reader], [Repository], [Row] and [Classify], the read-model port and the
//     reader's trust condition: stored rows are served only for the current
//     content version and extractor version after a successful derive.
//   - [ClaimPage] and [LoadCursor], the walker's shared cursor claim over the
//     versions table, and [ListDirty], the walker's dirty join.
//   - [NewDeriver], [Deriver] and [Deriver.DeriveRepository], the derive of
//     one repository under protocol section 3: read the content version
//     first, list, read and extract with no lock and no transaction, then
//     write in one short transaction under the repository advisory lock only
//     when the version or extractor version advances. An error, a panic or a
//     timeout writes a failure-only state update and never fails the
//     caller; [Result] carries the outcome for the wiring slice to count.
//   - [Trigger] and [Outcome], the derive labels, and the status values
//     [StatusNotInstalled], [StatusBackfilling] and [StatusReady].
//
// Version-row rule: no statement deletes or resets a version row except one
// that also deletes the repository's state row. A guard test enumerates every
// Go literal expression and migration in the module and enforces it; it
// cannot prove that both deletes of a retirement statement name the same
// repository.
package artifacts
