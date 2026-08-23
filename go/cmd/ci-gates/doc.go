// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command ci-gates is the CLI for the CI gate registry (#4213).
//
// It provides six subcommands that give local workflows and the trusted CI
// publisher one source of truth for path-selected verification:
//
//	ci-gates select   — print or explain which gates match the changed paths
//	ci-gates run      — execute the selected gates and report PASS/FAIL/SKIP
//	ci-gates await    — wait for exact blocking checks on an exact PR head
//	ci-gates contexts — print the required-status context manifest
//	ci-gates validate — verify that every registry entry's script and workflow exist
//	ci-gates uncovered — print changed paths without local category coverage
//
// The backing registry is specs/ci-gates.v1.yaml, loaded and validated by the
// internal/cigates package. All but await are credential-free and work offline
// once the repo is cloned. Await uses GitHub's pull-request files and check
// rollup APIs; no subcommand requires Docker directly.
//
// # await and contexts
//
// Await verifies an exact PR head, selects every matching blocking gate without
// applying the local tier ceiling, resolves concrete workflow/check identities
// from a trusted default-branch checkout, and fails closed until every selected
// check passes. Its exit code tells the required-gates publisher which kind of
// non-success it saw: 10 a gate concluded failure (the only code that may
// publish `failure`), 11 gates still running, 12 aggregation broke, and 13
// every selected gate terminal with at least one that never produced a verdict
// (#6189) -- CANCELLED, STALE, or SKIPPED because the workflow run that owned
// the job was cancelled. That is infrastructure state, not a gate result, so
// it publishes `error`. A gate GitHub skipped for its own reasons is NOT in
// that set and still publishes `failure`; separating the two is the one thing
// await needs the `actions: read` scope for, and a failed lookup keeps the
// skip a failure. Contexts exposes the repository-owned required-status manifest,
// including pinned GitHub App integration IDs for live ruleset verification.
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
// object with selected, skipped, and ci_only arrays. --category <list> filters
// to a comma-separated set of categories (e.g. exactness,telemetry); gates
// outside the set are reported as skipped rather than dropped.
//
// # run
//
// Runs each selected gate's local.command and non-empty local.test_command via
// /bin/sh -c, deduplicates byte-identical pairs, accumulates all results, and
// exits non-zero if any blocking gate failed. Advisory failures are printed
// but do not affect the exit code. --category applies the same filter as
// select; `make pre-pr` uses exactness, telemetry, hygiene, and docs for its
// credential-free registry lane (#4214).
//
// When a gate command shells out to "bash scripts/verify-*.sh", run resolves
// a bash >= 4.4 (checking PATH, then /opt/homebrew/bin/bash, then
// /usr/local/bin/bash) and prepends its directory to the subprocess's PATH
// so the inner "bash" token does not silently resolve to macOS's bash 3.2,
// which lacks bash 4.0+ features such as `declare -A` (#5050). If no
// candidate qualifies, PATH is left unchanged.
//
// run also strips GOROOT from every gate subprocess's environment. The
// wrapper launches this binary via `go run` from go/, and when go/go.mod
// requests a newer Go than the host, the GOTOOLCHAIN=auto switch exports the
// downloaded toolchain's GOROOT to this process; passing it on would make any
// gate that runs `go` in a module the host toolchain satisfies (the SDK
// modules, the scorecard example) pair the host go driver with the switched
// toolchain's tools and fail with a compile/tool version mismatch. With
// GOROOT cleared, each gate's go command resolves its own toolchain from its
// own go.mod (#6113 fixed the same leak per `go install` in
// scripts/dev/precommit-go.sh).
//
// # validate
//
// Loads the registry and calls (*cigates.Registry).Validate against the repo
// root, checking that every script and workflow file referenced in the registry
// exists on disk. Exits non-zero on any integrity error.
//
// With --drift it additionally runs (*cigates.Registry).DriftCheck (#4220),
// which fails if .pre-commit-config.yaml or .github/workflows/ have drifted from
// the registry — an unregistered local hook, a gate hook_id missing or at the
// wrong stage, or a workflow that is in neither a gate nor non_gate_workflows.
//
// # uncovered
//
//	ci-gates uncovered --registry specs/ci-gates.v1.yaml --category race \
//	                   --tier pre-pr [--base origin/main] [--paths-from <file|->]
//
// Prints the changed paths that no locally-runnable gate in the requested
// categories (at tier <= ceiling) covers via a trigger. `make pre-pr`'s scoped
// race lane (#4215) uses --category race to find the changed packages no race
// gate already runs, so it races exactly those without double-racing
// registry-owned packages. A CI-only gate (no local command) does not count as
// covering.
package main
