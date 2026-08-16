// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package firstrun holds the logic behind `eshu first-run` and its `report`
// subcommand: detecting the runtime shape, verifying it without starting
// anything, indexing the target repository (or reusing a proven index),
// running one bounded query, classifying failures into operator diagnostics,
// and projecting the outcome into a redacted evidence artifact.
//
// Execute walks the ordered steps and never reports success unless the
// bounded query actually returned; Result is the canonical envelope payload
// `eshu first-run --json` emits, and a failed step carries a classified
// Diagnostic whose preserved underlying error is never discarded.
// BuildEvidence projects a Result into an EvidenceReport, redacting every
// endpoint, path, and free-form field through internal/cli/evidredact before
// it lands in the model, so RenderEvidenceArtifact, RenderEvidenceTerminal,
// and WriteEvidenceArtifact only ever see redacted data. WriteEvidenceArtifact
// writes the artifact with owner-only (0600) permissions.
//
// Process contact is deliberately thin. APIHealthy dials <baseURL>/health with
// a three-second budget; WriteEvidenceArtifact writes one file; Truth reads
// ESHU_GRAPH_BACKEND through scan.CurrentGraphBackend; the scan-target step
// probes the workspace root for .eshu.yaml and .git through scan.TargetKind;
// and when Deps.ReposDir is left nil the scan.ReposDir fallback resolves the
// cache layout from the environment. Everything else -- cobra flags, the API
// client, PATH lookup, the config-backed MCP endpoint, the repository selector
// matcher, and the scan runtime -- arrives through Deps, resolved by the cobra
// wrapper in go/cmd/eshu. The split is mechanical rather than a design
// preference:
// cmd/eshu is package main, so nothing can import it, and any symbol that
// reads cobra flags, the environment, or the exit-code contract stays there.
package firstrun
