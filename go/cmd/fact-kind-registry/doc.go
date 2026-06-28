// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command fact-kind-registry generates the core fact-kind registry contract.
//
// It reads specs/fact-kind-registry.v1.yaml, verifies it against the live
// internal/facts family helpers, and emits the generated Go and Markdown
// artifacts consumed by admission and documentation drift gates.
package main
