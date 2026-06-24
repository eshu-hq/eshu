// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package json parses JSON and JSONC configuration, CloudFormation, and
// data-intelligence documents for the parent parser engine.
//
// Parse reads one file, preserves legacy JSON payload buckets, and returns
// deterministic rows for dependency manifests, npm, Composer, NuGet, and
// SwiftPM lockfile versions, TypeScript configs, `.jsonc` config files,
// CloudFormation templates, dbt manifests, and replay fixture documents.
// JSONC normalization strips comments
// and trailing commas with bounded scans before strict JSON decoding. The
// package depends on shared parser helpers and CloudFormation extraction, but
// it does not import the parent parser package; parent-owned dbt SQL lineage is
// supplied through Config and converted at the parent wrapper boundary.
//
// DependencyCoverage publishes the per-ecosystem repository dependency parser
// coverage matrix that the supply-chain impact reducer relies on. Each entry
// records whether a manifest or lockfile is parsed into content_entity
// dependency facts (Covered) or is still a Gap; gap entries preserve the safety
// rule that missing dependency evidence is neither safe nor affected. The
// dependency coverage emit and fixture tests exercise JSON-owned covered files
// and gap files, while parent-parser tests prove covered exact-name entries
// owned by other parser packages. npm package manifests preserve runtime, dev,
// optional, and peer range scopes; package-lock rows preserve exact installed
// versions, dependency chains, and npm-recorded runtime/dev/optional/peer scope
// where available. Composer manifests preserve runtime/dev range scopes, while
// composer.lock rows preserve exact installed versions, runtime/dev scope, and
// dependency paths when the lockfile proves package-to-package requirements.
// Pub coverage is YAML-owned and still listed here so public readiness tables
// have one sorted source.
// SwiftPM Package.resolved rows are limited to remote source-control pins with
// an exact version; branch-only, revision-only, and local pins remain
// non-evidence.
package json
