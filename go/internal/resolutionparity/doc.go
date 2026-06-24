// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package resolutionparity holds the per-language call-resolution accuracy
// goldens and CI parity gates for code-edge resolution provenance.
//
// Once code-call edges carry a resolution_method (ADR #2222, emitted by the
// reducer in #2223), the distribution of resolution tiers per language becomes
// measurable. This package parses the per-language fixture corpora with the real
// parser engine, runs the reducer's code-call extraction, and tallies the
// resolution_method of every emitted row. A checked-in golden pins the expected
// per-language distribution; the test fails when a parser or resolver change
// shifts the tiers, which is how a per-language resolution regression is caught
// in the normal `go test ./...` CI matrix.
//
// The exact caller-to-callee gate is separate from the tier snapshot. It uses
// source-authored fixture truth and the parser/goldenaudit scorer to fail when
// a call resolves to the wrong target, even if its provenance tier is unchanged.
// The tier golden remains a regression snapshot of deterministic pipeline
// output; the exact-edge fixture truth must stay independent of Eshu output.
package resolutionparity
