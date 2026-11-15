// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package manifest serves GET /api/v0/repository/manifest-dependencies, the
// bounded repository-scoped inventory of declared manifest dependencies:
// package name, declared version or range, manifest file, ecosystem, and the
// normalized dependency scope (runtime, dev, or test). Runtime dependencies
// are the default; dev and test dependencies appear only with the
// include_dev_dependencies opt-in.
//
// It reads the active-generation git content-entity facts through the same
// delta-overlay snapshot the reducer's pair-probe read uses
// (storage/postgres ListActiveRepositoryManifestDependencyFacts), so query
// truth and reducer truth agree row for row. The truth envelope says the rows
// are declared, not resolved: no version resolution runs here, and no
// repository ownership beyond the scope anchor is asserted.
//
// The same rows appear as a bounded manifest_dependencies section of the
// repository context response (LoadContextSection), carrying a truncation
// marker and a pointer back to this route for the full paged list.
package manifest
