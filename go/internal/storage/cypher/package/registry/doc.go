// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package registry owns package-registry graph truth as statement-free
// parts: Cypher templates, UNWIND row builders, phase names, and the
// package-identity lock set for Package, PackageVersion, PackageDependency,
// PackageArtifact, and RegistryEvent nodes plus their HAS_VERSION,
// DECLARES_DEPENDENCY, DEPENDS_ON_PACKAGE, HAS_ARTIFACT, and
// HAS_REGISTRY_EVENT edges. The parent cypher package converts each part to
// its executable Statement.
//
// The leaf never imports the parent: parts carry plain Cypher, rows, label,
// and phase, and the parent stamps batching and statement metadata. The
// parent-facing writer API (CanonicalNodeWriter phases, the deferred second
// ExecuteGroup for edges) keeps its shape.
package registry
