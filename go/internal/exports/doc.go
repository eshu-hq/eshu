// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package exports renders bounded, deterministic, redacted Eshu evidence
// snapshots into standard wire formats.
//
// Callers assemble a [Snapshot] from already-authorized reducer-owned
// vulnerability findings, SBOM components, and optional scanner readiness
// status, then ask the [Registry] for an [Exporter] for the requested [Format].
// The exporter writes bytes; the package never reads the database, never
// expands scope, and never decides authorization. That ownership belongs to the
// calling handler.
//
// Three rules hold across every format:
//
//  1. Bounded scope. Every snapshot declares a [Scope] (a single repository,
//     image digest, package, or advisory). Exporters drop any finding or
//     component whose own scope identifiers disagree with the snapshot scope,
//     so a handler bug that mixes evidence from a second target cannot leak
//     through.
//  2. Determinism. Findings, components, advisory sources, and locations are
//     sorted before serialization so the same input bytes produce the same
//     output bytes. Fixture-locked golden files in testdata/ are the contract.
//  3. Redaction. An optional [FieldRedactor] rewrites manifest paths and
//     locator URIs before they leave the process so private source detail
//     beyond the requested scope cannot escape through a path string.
//
// SARIF v2.1.0 is implemented today. CycloneDX BOV, SPDX, and the GitHub
// dependency snapshot format are reserved [Format] values whose exporter
// implementations land in follow-up changes; the package surface is designed
// so each new format is one Exporter implementation plus a registry entry.
package exports
