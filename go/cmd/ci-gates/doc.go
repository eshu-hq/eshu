// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command ci-gates is the CLI for the CI gate registry (#4213).
//
// It provides three subcommands that together give any local workflow
// a single source of truth for which CI verifiers apply to a given set
// of changed paths:
//
//	ci-gates select   — print or explain which gates match the changed paths
//	ci-gates run      — execute the selected gates and report PASS/FAIL/SKIP
//	ci-gates validate — verify that every registry entry's script and workflow exist
//
// The backing registry is specs/ci-gates.v1.yaml, loaded and validated by the
// internal/cigates package. All subcommands are credential-free and
// Docker-free; they work offline once the repo is cloned.
//
// # select
//
//	ci-gates select --registry specs/ci-gates.v1.yaml \
//	                --tier pre-pr \
//	                [--base origin/main] \
//	                [--paths-from paths.txt] \
//	                [--explain] [--json]
//
// Without --paths-from the changed paths are derived from git (committed vs
// --base, staged, and unstaged), mirroring scripts/dev/pre-pr.sh. Pass
// --paths-from=- to inject paths from stdin for hermetic tests.
//
// Default output: one selected gate id per line (registry order). --explain
// adds a human-readable reason for each gate. --json emits a structured
// object with selected, skipped, and ci_only arrays.
//
// # run
//
// Runs each selected gate's local.command via /bin/sh -c, accumulates all
// results, and exits non-zero if any blocking gate failed. Advisory failures
// are printed but do not affect the exit code.
//
// # validate
//
// Loads the registry and calls (*cigates.Registry).Validate against the repo
// root, checking that every script and workflow file referenced in the registry
// exists on disk. Exits non-zero on any integrity error.
package main
