// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package packageregistry normalizes package-registry evidence before it enters
// the durable fact envelope.
//
// The package belongs to the collector boundary: it validates bounded
// package-registry target config, registers ecosystem-native metadata parsers,
// observes and normalizes package/feed metadata through internal/packageidentity,
// parses local npm, PyPI, Go module, Maven, NuGet, Generic/JFrog, and
// Artifactory package-wrapper fixture metadata into observations, including
// source-native advisory hints and registry events, then emits
// reported-confidence facts through the package, version, dependency,
// artifact, source-hint, vulnerability-hint,
// registry-event, hosting, and warning envelope builders. Artifactory wrapper
// parsing can use a caller-supplied MetadataParserRegistry so runtime-owned
// ecosystem registrations remain in force. The packageruntime subpackage owns
// the
// claim-driven HTTP metadata fetch path and returns these facts through the
// shared collector commit boundary.
// NormalizePackageIdentity keeps ecosystem identity rules separate, including
// Swift package identity aliases even before a Swift registry metadata parser
// exists, so package facts stay idempotent across retries and replay, while
// PURL, BOMRef, package-manager, and source-debug fields stay available to
// reducers and read surfaces. Envelope builders keep StableFactKey
// source-stable while making FactID scope- and generation-specific, and they
// emit correlation anchors for reducer joins.
// The package does not claim ownership, dependency truth, graph nodes, or query
// answers; reducers must corroborate registry facts with source, build,
// lockfile, or runtime evidence before promoting canonical relationships.
package packageregistry
