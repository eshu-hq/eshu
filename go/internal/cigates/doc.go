// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cigates is the typed core of the CI gate registry (#4213, drift #4220).
//
// It answers both which credential-free verifiers should run locally and which
// path-selected blocking CI checks must pass before a pull request can merge.
//
// # Registry
//
// The gate registry lives at specs/ci-gates.v1.yaml. Load reads and structurally
// validates it (unique IDs, non-empty triggers, valid enum values, CI-only reason
// required when local is absent). A local-only gate can carry local_only_reason
// so callers that require proof orchestration can distinguish intentional local
// proofs from stale CI metadata. The result is a *Registry whose Gates slice
// preserves the YAML order for deterministic output. Local.TestCommand remains
// declarative in this package; cmd/ci-gates executes a distinct, non-empty
// self-test after the primary command.
//
// # Selection
//
// (*Registry).Select(changed []string, tier Tier) evaluates each gate in
// registry order and returns a []Selection. Each Selection records whether the
// gate was chosen, skipped (trigger mismatch or tier exceeded), or CI-only. The
// function is a pure, hermetic function of its inputs: selection never reads
// the tree, so `ci-gates select --paths-from` is reproducible from its inputs
// alone. Validate is the exception and only for trigger resolution — see
// Validation below.
//
// # Required checks
//
// (*Registry).RequiredGates(changed []string) selects every matching blocking
// CI job regardless of local tier or availability. The top-level required
// status manifest names the GitHub ruleset contexts and exactly one trusted
// aggregate publisher. Matrix jobs use explicit concrete check names.
//
// # Validation
//
// (*Registry).Validate(repoRoot string) checks that every local command's script
// file (and test_command, when present) and every CI workflow file exist on
// disk, and that every gate trigger still names something real. It accumulates
// all errors so a single pass surfaces every broken reference.
//
// The two trigger shapes carry different evidence. A literal trigger is
// stat-checked, with guards against one that escapes the repository root
// directly or through a symlink (#6055). A glob trigger must select at least
// one FILE git tracks at repoRoot (#6159); zero matches is an error, because a
// trigger that matches nothing can never select its gate, leaving the gate
// reading as wired for a surface it no longer guards. Both shapes reject a
// trigger naming a directory: Select is handed changed paths, and those are
// always files, so a trigger stopping at a directory can never select either.
// The spelling that works is "dir/**", which the error names. Resolving that is the one place this package runs git,
// and it is why Validate — unlike Select — is a reader of the work tree rather
// than a pure function; a tracked path set that cannot be read, or that comes
// back empty, is an error too, never a skip. That git call runs with the
// ambient repository pointers stripped, so an inherited GIT_DIR cannot make it
// verify triggers against a different checkout and still report success.
//
// # Drift (#4220)
//
// DriftCheck(repoRoot, *Registry) keeps .pre-commit-config.yaml and
// .github/workflows/ in lockstep with the registry. It fails when a local hook
// is neither a gate's HookID nor a declared HygieneHook, when a gate's HookID is
// missing from the hook config or sits at a stage inconsistent with its tier,
// when a workflow file is registered in neither a gate nor NonGateWorkflows (or
// in both, or is a stale allowlist entry), when a gate's CI.Job does not name a
// real check in its CI.Workflow — a job name, job key, or append_gate display,
// not the workflow title (#5010) — when a gate's literal (non-glob) trigger
// is not matched by its CI workflow's dorny/paths-filter block, for a gate
// whose filter key resolves either through an append_gate matrix dispatch
// (#5855) or through a job gated on a paths-filter output via
// needs.<job>.outputs.<key> (#5546) — or when a gate whose
// scripts/verify-*.sh is executed by exactly one workflow declares a
// different ci.workflow (#5748) — when scripts/lib/trivy-skip-dirs.sh, the
// shared skip-dirs derivation helper, is not provably wired to read
// specs/trivy-skip-dirs.txt, the single authoritative skip-dirs list, or
// scripts/dev/trivy-fs-local.sh and security-scan.yml's trivy-fs job are not
// each provably wired to INVOKE that helper — or when a gate's own local
// script, or a scripts/ file that script sources, is matched by none of that
// gate's triggers, which would let a PR editing only the verifier skip the
// gate locally and first fail in CI (#5762). Filter matching mirrors dorny's
// own rule:
// each pattern is compiled separately, a leading ! negates that pattern, and
// the predicate-quantifier decides whether ANY (default) or EVERY pattern must
// match. The script-correspondence check counts only executable `run:` blocks
// as running a script, since a paths filter watches a path rather than
// invoking it, and it skips rather than reports when no workflow runs the
// script (CI legitimately uses a different entrypoint than the local gate) or
// when several do (no single owner). It also validates the trusted aggregate
// publisher's workflow_run source, event boundary, serialized per-head
// concurrency, first-step pending invalidation, permissions, default-branch
// checkout, secret independence, status-publishing command, and
// cancellation-safe terminal condition. It also validates the cancelled-gate
// arm of that publisher's AGGREGATE_CODE branch (#6189): the arm must exist,
// must not map a cancelled dependency gate to state=failure, and must publish
// state=error, so a workflow-only revert cannot quietly restore "A required
// gate failed" for a head where nothing failed. Like the rest of the package
// it needs no network, Docker, or credentials.
//
// # Glob matching
//
// MatchGlob implements a small doublestar matcher (** crosses segments, * stays
// within one segment, all other characters are literal) with no external
// dependencies.
package cigates
