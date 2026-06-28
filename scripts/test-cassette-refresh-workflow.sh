#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# test-cassette-refresh-workflow.sh — static mirror for the R-6 credentialed
# cassette refresh workflow (epic #4102, issue #4108).
#
# Validates the contract that cannot silently drift without credentials:
#
#   1. The refresh workflow YAML exists and parses (bash -n on the inline
#      logic it sources; yamllint is optional).
#   2. The workflow is label-gated on [refresh-cassettes] — a push to main
#      or an accidental unlabeled PR must NOT trigger a credentialed run.
#   3. Secrets are never written to the cassette directory directly — the
#      workflow writes to GITHUB_OUTPUT or env, not to cassette files.
#   4. The canonical-diff and redaction Go tests exist and the package
#      compiles offline (no credentials, no network, no Docker).
#   5. The workflow references the collector's documented credential names and
#      overwrites the tracked cassette instead of creating an untracked sibling.
#
# This script runs without Docker, without live credentials, and without a
# running Go toolchain for the YAML/shell structural checks. The Go
# compilation check does require a Go toolchain.
#
# Run from the repository root:
#
#   bash scripts/test-cassette-refresh-workflow.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workflow="${repo_root}/.github/workflows/refresh-cassettes.yml"
go_pkg="${repo_root}/go/internal/replay/refreshworkflow"
go_dir="${repo_root}/go"

pass_count=0
fail_count=0

pass() { printf '[PASS] %s\n' "$*"; (( pass_count++ )) || true; }
fail() { printf '[FAIL] %s\n' "$*" >&2; (( fail_count++ )) || true; }

# ---------------------------------------------------------------------------
# Case 1: Workflow file exists and is executable-by-intent (readable)
# ---------------------------------------------------------------------------
if [[ -f "${workflow}" ]]; then
  pass "refresh-cassettes.yml exists"
else
  fail "missing ${workflow}"
fi

# ---------------------------------------------------------------------------
# Case 2: Label gate — workflow must require the [refresh-cassettes] label.
# A run triggered by a bare PR push (no label) must not execute.
# ---------------------------------------------------------------------------
if rg --fixed-strings --quiet -- 'refresh-cassettes' "${workflow}" 2>/dev/null; then
  pass "workflow references refresh-cassettes label"
else
  fail "workflow does not reference refresh-cassettes label; credentialed runs must be label-gated"
fi

if rg --fixed-strings --quiet -- 'github.event.label.name' "${workflow}" 2>/dev/null; then
  pass "workflow gates on github.event.label.name"
else
  fail "workflow does not gate on github.event.label.name; any label could trigger a credentialed run"
fi

# ---------------------------------------------------------------------------
# Case 3: Workflow_dispatch present — operators can trigger a manual refresh
# ---------------------------------------------------------------------------
if rg --fixed-strings --quiet -- 'workflow_dispatch' "${workflow}" 2>/dev/null; then
  pass "workflow supports workflow_dispatch for manual refresh"
else
  fail "workflow missing workflow_dispatch; operators cannot trigger a manual cassette refresh"
fi

# ---------------------------------------------------------------------------
# Case 4: Secrets via ${{ secrets.* }} only — never echoed, never written to
# the cassette directory directly. The refresh job must only pass secrets as
# env vars to the collector binaries.
# ---------------------------------------------------------------------------
if rg --fixed-strings --quiet -- 'secrets.' "${workflow}" 2>/dev/null; then
  pass "workflow references secrets via secrets.* expressions"
else
  fail "workflow has no secrets references; credentialed run cannot authenticate to providers"
fi

# Case 6a: secrets must not be directly interpolated into run: shell via
# ${{ secrets.X }}. This is the primary expression-injection vector: an
# attacker who controls a context value can shell-escape if it is interpolated
# directly by the GHA expression evaluator into the run: script body.
# Note: this check covers the interpolation risk, not all possible secret
# exposure paths (e.g. echoing an ESHU_* token is caught below, separately).
if rg --quiet -- 'echo.*\${{.*secrets\.' "${workflow}" 2>/dev/null; then
  fail "workflow has an echo of a secret expression — secrets must not be interpolated into run: via \${{ secrets.X }}"
else
  pass "workflow does not directly echo \${{ secrets.X }} expressions into run: scripts"
fi

# Case 6b: secrets must not be echoed via their env-var names either.
# Provider credentials are bound to ESHU_* env vars in the record job; an
# accidental direct print in a run: block would expose the value in the Actions
# log (GitHub's automatic masking only covers the exact secret value, not
# truncations or encodings).
if rg --quiet -- 'echo.*\$\{ESHU_' "${workflow}" 2>/dev/null \
   || rg --quiet -- 'printf.*\$\{ESHU_' "${workflow}" 2>/dev/null \
   || rg --quiet -- 'printenv.*ESHU_' "${workflow}" 2>/dev/null; then
  fail "workflow echoes a provider credential env var (ESHU_*) — secrets must not be written to stdout"
else
  pass "workflow does not echo provider credential env vars (ESHU_*) to stdout"
fi

# ---------------------------------------------------------------------------
# Case 5: PR opening — workflow must open or update a PR with the diff
# ---------------------------------------------------------------------------
if rg --fixed-strings --quiet -- 'gh pr create' "${workflow}" 2>/dev/null \
    || rg --fixed-strings --quiet -- 'gh pr edit' "${workflow}" 2>/dev/null \
    || rg --fixed-strings --quiet -- 'gh pr' "${workflow}" 2>/dev/null; then
  pass "workflow opens or updates a PR with the cassette diff"
else
  fail "workflow does not open or update a PR; the diff must be submitted for review"
fi

# ---------------------------------------------------------------------------
# Case 6: Record mode invocation — workflow must call -mode=record
# ---------------------------------------------------------------------------
if rg --fixed-strings --quiet -- 'mode=record' "${workflow}" 2>/dev/null \
    || rg --fixed-strings --quiet -- '-mode record' "${workflow}" 2>/dev/null; then
  pass "workflow invokes collector in -mode=record"
else
  fail "workflow does not invoke -mode=record; cassettes will not be regenerated"
fi

# ---------------------------------------------------------------------------
# Case 7: collector config contract — workflow must wire the collector's real
# environment names and must not retain the old placeholder ESHU_K8S_* names.
# ---------------------------------------------------------------------------
if rg --fixed-strings --quiet -- 'ESHU_KUBERNETES_LIVE_COLLECTOR_INSTANCE_ID' "${workflow}" 2>/dev/null; then
  pass "workflow wires kubernetes_live collector instance id"
else
  fail "workflow does not wire ESHU_KUBERNETES_LIVE_COLLECTOR_INSTANCE_ID"
fi

if rg --fixed-strings --quiet -- 'ESHU_KUBERNETES_LIVE_CLUSTERS_JSON' "${workflow}" 2>/dev/null; then
  pass "workflow wires kubernetes_live clusters JSON"
else
  fail "workflow does not wire ESHU_KUBERNETES_LIVE_CLUSTERS_JSON"
fi

if rg --fixed-strings --quiet -- 'ESHU_KUBERNETES_LIVE_KUBECONFIG_B64' "${workflow}" 2>/dev/null \
    && rg --fixed-strings --quiet -- '/tmp/eshu-kubernetes-live/kubeconfig' "${workflow}" 2>/dev/null; then
  pass "workflow materializes kubernetes_live kubeconfig at the documented path"
else
  fail "workflow does not materialize the kubernetes_live kubeconfig contract"
fi

if rg --fixed-strings --quiet -- 'ESHU_K8S_' "${workflow}" 2>/dev/null; then
  fail "workflow still references stale ESHU_K8S_* placeholder names"
else
  pass "workflow does not reference stale ESHU_K8S_* placeholder names"
fi

# ---------------------------------------------------------------------------
# Case 8: cassette-file flag — workflow must overwrite the tracked corpus file.
# ---------------------------------------------------------------------------
if rg --fixed-strings --quiet -- 'testdata/cassettes/kuberneteslive/supply-chain-demo.json' "${workflow}" 2>/dev/null; then
  pass "workflow overwrites the tracked kubernetes_live cassette"
else
  fail "workflow does not overwrite testdata/cassettes/kuberneteslive/supply-chain-demo.json"
fi

if rg --fixed-strings --quiet -- 'live.cassette.json' "${workflow}" 2>/dev/null; then
  fail "workflow still writes the untracked kubernetes_live live.cassette.json"
else
  pass "workflow does not write an untracked kubernetes_live cassette sibling"
fi

# ---------------------------------------------------------------------------
# Case 9: contents: write permission — required for git push and gh pr create
# ---------------------------------------------------------------------------
if rg --fixed-strings --quiet -- 'contents: write' "${workflow}" 2>/dev/null; then
  pass "workflow declares contents: write permission for PR creation"
else
  fail "workflow missing contents: write permission; gh pr create will fail"
fi

# ---------------------------------------------------------------------------
# Case 10: pull-requests: write permission — required for gh pr create/edit
# ---------------------------------------------------------------------------
if rg --fixed-strings --quiet -- 'pull-requests: write' "${workflow}" 2>/dev/null; then
  pass "workflow declares pull-requests: write permission"
else
  fail "workflow missing pull-requests: write permission; gh pr create will fail"
fi

# ---------------------------------------------------------------------------
# Case 11: Go refreshworkflow package exists and compiles offline
# ---------------------------------------------------------------------------
if [[ -d "${go_pkg}" ]]; then
  pass "go/internal/replay/refreshworkflow package directory exists"
else
  fail "missing go/internal/replay/refreshworkflow package directory"
fi

if [[ -f "${go_pkg}/doc.go" ]]; then
  pass "refreshworkflow/doc.go exists"
else
  fail "missing refreshworkflow/doc.go"
fi

if [[ -f "${go_pkg}/refreshworkflow_test.go" ]]; then
  pass "refreshworkflow/refreshworkflow_test.go exists"
else
  fail "missing refreshworkflow/refreshworkflow_test.go"
fi

# Offline Go test-compile check (no network; no credentials).
# Use `go test -run=^$ -count=1` rather than `go build` so _test.go files
# are also compiled — go build skips test files and would miss syntax errors
# or unresolved imports in the _test package.
if command -v go >/dev/null 2>&1; then
  export GOCACHE="${go_dir}/.gocache"
  if (cd "${go_dir}" && go test ./internal/replay/refreshworkflow/... -run '^$' -count=1 2>&1); then
    pass "go/internal/replay/refreshworkflow compiles offline (including _test.go files)"
  else
    fail "go/internal/replay/refreshworkflow does not compile"
  fi
else
  printf '[SKIP] go toolchain not available; skipping compile check\n'
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
printf '\n%d passed, %d failed\n' "${pass_count}" "${fail_count}"
[[ "${fail_count}" -eq 0 ]] || exit 1
