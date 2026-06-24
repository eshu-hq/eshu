// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package scannerworker defines scanner-worker claims, output validation,
// failure payloads, analyzer ports, and hosted claim processing.
//
// The package owns the narrow boundary that lets workflow-owned work items
// carry bounded repository, image, or artifact target scope and resource limits
// into isolated CPU and memory heavy security analyzers. Scanner workers emit
// allowlisted source fact families only, including image/rootfs coverage and
// package evidence when a configured analyzer proves it from package
// databases; reducers remain the truth owners for finding admission,
// prioritization, and graph projection. Unsupported analyzer coverage is
// explicit scanner_worker.warning evidence, not a safe, affected, or scanned
// result. Target kind derivation stays bounded to repository, image, or
// artifact enums so telemetry and failure payloads do not leak raw locators.
// Hosted workers carry tenant boundaries into commit mutations and must either
// commit source evidence under the active grant or record a bounded retry or
// dead-letter payload.
package scannerworker
