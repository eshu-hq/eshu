// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package evidencecontinuity validates the source-fact-to-answer evidence
// continuity contract.
//
// The package reads specs/evidence-continuity.v1.yaml, the capability matrix,
// and the generated surface inventory. It verifies that each evidence-centric
// GA or gated public capability row names a known capability, API route, MCP
// tool, deterministic source proof, projection or read-model proof, answer
// surface proof, empty/no-provider/no-collector behavior, and the closed
// negative evidence-loss cases. The verifier is intentionally static: it gates
// conformance coverage and points at the focused tests or golden-corpus proof
// that exercise runtime behavior.
package evidencecontinuity
