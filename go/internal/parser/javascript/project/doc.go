// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package project resolves a JavaScript or TypeScript source file's project
// context from the surrounding repository layout: the nearest tsconfig.json
// and its baseUrl/paths import-alias mappings (TSConfigImportResolver), the
// nearest package.json and the entry points it declares (PackageFileRootKinds,
// NearestPackageRoot, PackagePublicSourcePaths), repo-relative path
// normalization (RelativeSlashPath, CleanPath, PathWithin), and the
// stat-keyed cache (ScopeCache) that keeps those filesystem lookups off the
// parser's hot path (issue #4515 P2a, issue #6771).
//
// This package is a leaf: it must not import the parent javascript package.
// The javascript adapter calls in the other direction while walking one
// file's AST, using the exported resolvers here to answer "which tsconfig.json
// and package.json own this file, and what do they say."
//
// tsconfig.json, package.json, and the scope cache are one mutually recursive
// unit: NewTSConfigImportResolver reads a cached tsconfig.json through
// ScopeCache, PackageFileRootKinds reads a cached package.json through the
// same cache type, and both walk the filesystem with CleanPath/PathWithin.
// Splitting them into separate packages would only replace one internal call
// graph with an import cycle, so they stay together here.
package project
