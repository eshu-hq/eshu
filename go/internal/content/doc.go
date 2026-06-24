// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package content defines the source-local content write contract and the
// canonical content-entity identifier used by Postgres-backed writers.
//
// Writer is the narrow per-scope-generation interface; Materialization,
// Record, EntityRecord, and RepositoryRef are its inputs. WriterConfig and
// LoadWriterConfig expose the ESHU_CONTENT_ENTITY_BATCH_SIZE tunable.
// CanonicalEntityID hashes (repoID, relativePath, entityType, entityName,
// lineNumber) into a stable "content-entity:e_<12-hex>" identifier with
// BLAKE2s.
package content
