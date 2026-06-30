// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cigates is the typed core of the CI gate registry (#4213, drift #4220).
//
// It answers one question: given the set of paths changed in a PR and a tier
// ceiling, which credential-free CI verifiers should run locally — and which
// are registered but CI-only or out of scope?
//
// # Registry
//
// The gate registry lives at specs/ci-gates.v1.yaml. Load reads and structurally
// validates it (unique IDs, non-empty triggers, valid enum values, CI-only reason
// required when local is absent). A local-only gate can carry local_only_reason
// so callers that require proof orchestration can distinguish intentional local
// proofs from stale CI metadata. The result is a *Registry whose Gates slice
// preserves the YAML order for deterministic output.
//
// # Selection
//
// (*Registry).Select(changed []string, tier Tier) evaluates each gate in
// registry order and returns a []Selection. Each Selection records whether the
// gate was chosen, skipped (trigger mismatch or tier exceeded), or CI-only. The
// function is a pure, hermetic function of its inputs — git is touched only at
// the CLI boundary in cmd/ci-gates.
//
// # Validation
//
// (*Registry).Validate(repoRoot string) checks that every local command's script
// file (and test_command, when present) and every CI workflow file exist on
// disk. It accumulates all errors so a single pass surfaces every broken
// reference.
//
// # Drift (#4220)
//
// DriftCheck(repoRoot, *Registry) keeps .pre-commit-config.yaml and
// .github/workflows/ in lockstep with the registry. It fails when a local hook
// is neither a gate's HookID nor a declared HygieneHook, when a gate's HookID is
// missing from the hook config or sits at a stage inconsistent with its tier, or
// when a workflow file is registered in neither a gate nor NonGateWorkflows (or
// in both, or is a stale allowlist entry). Like the rest of the package it needs
// no network, Docker, or credentials.
//
// # Glob matching
//
// MatchGlob implements a small doublestar matcher (** crosses segments, * stays
// within one segment, all other characters are literal) with no external
// dependencies.
package cigates
