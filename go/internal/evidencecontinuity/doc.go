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
//
// The verifier also checks its own CI gate's reach: every Go package a
// `go test` proof ref names, every input ValidateRepository reads, and every
// Go package the verifier is built from must be spanned by the
// evidence-continuity triggers in specs/ci-gates.v1.yaml and by the "evidence"
// path filter in .github/workflows/static-contract-gates.yml (finding
// gate_trigger_gap). The inputs are the contract spec, the capability matrix
// and its fragments, the generated surface inventory, and the two files the
// check itself reads — the CI gate registry and the workflow; they are listed
// in validatorInputs, which a new input must be added to. The built-from
// packages are this one and internal/cigates, whose MatchGlob and DornyFilters
// decide what the check reports; they are listed in validatorCodeDeps, which a
// test derives from this package's own imports.
// Before #6131 those trigger sets were disjoint from the referenced packages,
// so renaming a referenced test never ran this gate and unrelated PRs were
// the first place the stale ref failed.
package evidencecontinuity
