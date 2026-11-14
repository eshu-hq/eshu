// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package manifest serves GET /api/v0/repository/manifest-dependencies, the
// bounded repository-scoped inventory of declared manifest dependencies:
// package name, declared version or range, manifest file, ecosystem, and the
// dev-or-runtime signal when the fact says so.
//
// It reads the active-generation git content-entity facts through the same
// delta-overlay snapshot the reducer's pair-probe read uses
// (storage/postgres ListActiveRepositoryManifestDependencyFacts), so query
// truth and reducer truth agree row for row. The truth envelope says the rows
// are declared, not resolved: no version resolution runs here, and no
// repository ownership beyond the scope anchor is asserted.
package manifest
