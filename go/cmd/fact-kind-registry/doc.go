// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command fact-kind-registry generates the core fact-kind registry contract.
//
// It statically discovers typed fact-family contract declarations
// (contract.Declare beside fact definitions, contract.Bind beside
// reducer implementations) with a go/parser-only scan that never
// executes scanned packages, validates ownership, bindings, producers,
// dispositions, and schema references with file:line diagnostics,
// verifies the derived registry against the live internal/facts family
// helpers, and emits the compact specs/fact-kind-registry.v1.yaml, the
// specs/fact-kind-registry-contracts.d shards and catalog pages, and
// the generated Go and Markdown artifacts consumed by admission and
// documentation drift gates.
package main
