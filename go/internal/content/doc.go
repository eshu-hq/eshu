// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package content defines the source-local content write contract and the
// canonical content-entity identifiers used by Postgres-backed writers.
//
// Writer is the narrow per-scope-generation interface; Materialization,
// Record, EntityRecord, and RepositoryRef are its inputs. WriterConfig and
// LoadWriterConfig expose the ESHU_CONTENT_ENTITY_BATCH_SIZE tunable.
// CanonicalEntityID hashes (repoID, relativePath, entityType, entityName,
// lineNumber) into a stable "content-entity:e_<12-hex>" identifier with
// BLAKE2s. CanonicalEntityIDWithMetadata routes in-scope manifest dependency
// Variables (see its doc comment for the exact gate, and
// dependency_identity.go for the full per-package_manager discriminator
// table) to CanonicalDependencyEntityID instead — a section-keyed,
// line-independent identity so reordering dependencies within a manifest
// section does not churn their content-entity id — and falls back to
// CanonicalEntityID for everything else. The in-scope package managers are
// npm, composer, cargo, gradle, maven, nuget, pypi, go (gomod), rubygems,
// pub, and hex; swift is deliberately excluded because its only producer is
// a lockfile (see dependencyIdentityPackageManagers).
package content
