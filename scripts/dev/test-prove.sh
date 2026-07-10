#!/usr/bin/env bash
# Static structural test for scripts/dev/prove.sh. `make prove` itself needs
# Docker for its Layer 2 matrix and a built Go toolchain for its
# credential-free path, so this mirror validates the contract that cannot
# silently drift: the script parses, the Makefile wires a `prove` target,
# every credential-free step is present with the exact command it must run,
# the Docker layer is path-selected via `ci-gates select` (never `ci-gates
# run`), the loud-defer-when-no-docker pattern is present, no new gate id is
# registered, the deterministic report is free of per-run tokens, the
# prove-latency budget is read from the doc (not hardcoded), and the flake
# policy (no retry-to-green) is stated and not violated by an actual retry
# loop. This is the credential-free lane; `make prove` itself is exercised
# directly (not via this mirror) when Docker and a full checkout are
# available.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/dev/prove.sh"
makefile="${repo_root}/Makefile"

fail() { printf 'test-prove: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "prove.sh must be executable"
bash -n "${script}" || fail "prove.sh has a syntax error"

lines="$(wc -l <"${script}" | tr -d '[:space:]')"
[[ "${lines}" -lt 500 ]] || fail "prove.sh must stay under 500 lines (has ${lines})"

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}
forbid() {
	local label="$1" needle="$2"
	if rg --fixed-strings --quiet -- "${needle}" "${script}"; then
		fail "must not contain ${label}: ${needle}"
	fi
}

# Strict mode, but no bash-4-only associative arrays: the "bash" a Makefile
# recipe resolves via PATH is not guaranteed to be bash 4+ (macOS ships
# /bin/bash 3.2 as the system default), matching scripts/dev/pre-pr.sh's own
# portability choice.
require "strict mode" "set -uo pipefail"
forbid "bash-4-only associative array" "declare -A"

# Makefile wiring: a documented `prove` target in .PHONY, invoking this script.
require_makefile() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${makefile}" || fail "missing ${label} (Makefile): ${needle}"
}
require_makefile "prove in .PHONY" ".PHONY:"
rg --pcre2 --quiet '^\.PHONY:.*\bprove\b' "${makefile}" || fail "Makefile .PHONY line must list prove"
rg --pcre2 --quiet '^prove:.*## ' "${makefile}" || fail "Makefile prove target must carry '## ' help text"
require_makefile "prove target invokes prove.sh" "scripts/dev/prove.sh"

# Credential-free common path: always runs, in this exact order, with the
# exact commands the ci-gates registry already pins for these gate ids
# (specs/ci-gates.v1.yaml: ifa-contract-layer, ifa-determinism,
# ifa-dead-letter-matrix local.command).
require "ifa contract-layer test command" "go test ./internal/ifa ./cmd/ifa -count=1"
require "hermetic determinism mirror invocation" "scripts/test-verify-ifa-determinism.sh"
require "hermetic dead-letter-matrix mirror invocation" "scripts/test-verify-ifa-dead-letter-matrix.sh"
require "ifa coverage reconcile invocation" "go run ./cmd/ifa coverage"
require "coverage specs-dir passed as an absolute path" '-specs-dir "${repo_root}/specs"'
require "coverage snapshot passed as an absolute path" '-snapshot "${repo_root}/testdata/golden/e2e-20repo-snapshot.json"'
# The advisory->blocking flip is a separate P4 slice: this step must run in
# ifa coverage's default advisory mode, not force -blocking (which would fail
# every run today given the current, still-growing coverage baseline).
forbid "coverage must not pass -blocking" " -blocking"

# Layer 2 selection: via `ci-gates select`, never `ci-gates run` (whose own
# local.command for these two gate ids is the hermetic mirror above, not the
# real Docker matrix).
require "path selection via ci-gates select" "run ./cmd/ci-gates select"
forbid "must not delegate the Docker matrix to ci-gates run" "cmd/ci-gates run"
require "explain-based SELECTED parsing" "^SELECTED"
require "ifa-determinism gate id read from selection" '"ifa-determinism"'
require "ifa-dead-letter-matrix gate id read from selection" '"ifa-dead-letter-matrix"'
require "real determinism matrix invoked directly" "scripts/verify-ifa-determinism.sh"
require "real dead-letter matrix invoked directly" "scripts/verify-ifa-dead-letter-matrix.sh"

# No new gate id is registered anywhere in this diff: prove.sh is a thin
# composition over existing gates, not a new one (per the design brief).
forbid "must not register a new ifa-prove gate id" "id: ifa-prove"
if rg --fixed-strings --quiet -- "id: ifa-prove" "${repo_root}/specs/ci-gates.v1.yaml"; then
	fail "specs/ci-gates.v1.yaml must not gain a new ifa-prove gate id"
fi

# Loud-defer-when-no-docker: mirrors scripts/dev/trivy-fs-local.sh's pattern
# (print guidance, exit non-fatally, never silently pass as if the matrix
# ran).
require "docker detection" "command -v docker"
require "defer status distinct from pass" "DEFER (docker unavailable)"
require "operator guidance on defer" "install Docker"
require "CI-remains-authoritative framing" "CI runs the authoritative Docker matrix"
require "defer is not a silent pass" "this defer is informational, not a pass"

# gate_selected must read rg's own PIPESTATUS, not the pipefail-tainted
# overall pipeline status: `rg --quiet` exits as soon as it confirms a
# match, which can SIGPIPE the upstream `printf` before it finishes writing,
# and under `pipefail` that 141 (not rg's real 0/1) would otherwise become
# gate_selected's reported result — silently turning a genuinely SELECTED
# gate into a false "not selected" (verified against a real repro on this
# box: a real ifa-dead-letter-matrix selection was reported SKIP without
# this fix). Regression-tested here, not just asserted.
require "gate_selected reads rg's own exit code via PIPESTATUS" "PIPESTATUS[1]"
require "SIGPIPE/pipefail hazard documented" "SIGPIPE"

# Same repro as above, executed directly against this script's own
# gate_selected shape rather than merely grepped for: build a synthetic
# multi-line `--explain`-shaped block with the SELECTED line near the START
# and thousands of filler lines AFTER it, so `rg --quiet` (which exits the
# instant it confirms a match) very likely quits while `printf` is still
# writing the filler — the exact race that SIGPIPE'd `printf` on a real
# `ifa-dead-letter-matrix` selection during manual proof-gathering on this
# box. Real newlines matter here (a double-quoted "...\n..." literal would
# not contain them), so the fixture is built as actual lines in a file, not
# an escaped string, and `explain=` is populated via `$(cat file)` exactly
# like prove.sh's own command-substitution capture of `ci-gates select`'s
# output. This is the same class of bug the PIPESTATUS require above guards
# structurally; this block guards it behaviorally.
gate_selected_explain_file="$(mktemp)"
gate_selected_repro="$(mktemp)"
trap 'rm -f "${gate_selected_explain_file}" "${gate_selected_repro}"' EXIT
{
	printf 'SELECTED  ifa-dead-letter-matrix — matched trigger on path\n'
	for _ in $(seq 1 5000); do printf 'SKIPPED some-other-gate — no trigger matched changed paths\n'; done
} >"${gate_selected_explain_file}"
{
	printf 'gate_selected() {\n'
	sed -n '/^gate_selected() {/,/^}/p' "${script}" | tail -n +2
	printf 'explain="$(cat %q)"\n' "${gate_selected_explain_file}"
	printf 'gate_selected "ifa-dead-letter-matrix"\n'
	printf 'exit $?\n'
} >"${gate_selected_repro}"
if ! bash "${gate_selected_repro}"; then
	fail "gate_selected regression: a genuinely SELECTED gate reported as not-selected (SIGPIPE/pipefail hazard reintroduced)"
fi

# safe_bash: scripts/verify-ifa-*.sh and scripts/lib/ifa_determinism_common.sh
# use bash-4+-only constructs. Plain PATH-resolved "bash" is not safe to
# assume (macOS ships /bin/bash 3.2 as the system default), and running
# those scripts under it was observed, on this box, to make a real
# mid-script crash get reported as a clean PASS (their own `trap cleanup
# EXIT` handler's `$?` capture read 0, not the crash) — a false green worse
# than deferring. prove.sh must verify a bash >= 4 before trusting this
# layer's result, not assume the ambient PATH is safe.
require "safe_bash resolves a bash >= 4 before running the real matrix" "safe_bash()"
require "safe_bash checks BASH_VERSINFO" "BASH_VERSINFO[0]"
require "safe_bash tries known Homebrew locations" "/opt/homebrew/bin/bash"
require "bash-too-old defer status distinct from pass" "DEFER (bash too old)"
require "bash-too-old guidance" "needs bash 4+"
require "real matrix invoked through the resolved safe bash, not ambient PATH bash" '"${bash_bin}" "${repo_root}/${script_rel}"'

# Flake policy: stated, and not contradicted by an actual retry-to-green
# loop. Each real gate is invoked exactly once: the two hermetic mirrors each
# have exactly one executing `bash ".../<script>"` call, and the two Layer 2
# scripts are each dispatched through exactly one run_layer2 call (whose own
# body contains the single shared `"${bash_bin}" "${repo_root}/${script_rel}"`
# invocation point — through the resolved safe bash, not ambient PATH bash —
# called at most once per script per run).
require "flake policy stated" "NO retry-to-green"
count_exact() { rg --fixed-strings --count-matches -- "$2" "${script}" 2>/dev/null || printf '0'; }
[[ "$(count_exact _ 'bash "${repo_root}/scripts/test-verify-ifa-determinism.sh"')" == "1" ]] \
	|| fail "hermetic determinism mirror must be invoked exactly once"
[[ "$(count_exact _ 'bash "${repo_root}/scripts/test-verify-ifa-dead-letter-matrix.sh"')" == "1" ]] \
	|| fail "hermetic dead-letter-matrix mirror must be invoked exactly once"
[[ "$(count_exact _ 'run_layer2 "ifa-determinism"')" == "1" ]] \
	|| fail "Layer 2 graph-determinism gate must be dispatched exactly once"
[[ "$(count_exact _ 'run_layer2 "ifa-dead-letter-matrix"')" == "1" ]] \
	|| fail "Layer 2 dead-letter-matrix gate must be dispatched exactly once"
[[ "$(count_exact _ '"${bash_bin}" "${repo_root}/${script_rel}"')" == "1" ]] \
	|| fail "run_layer2's own Docker-matrix invocation point must appear exactly once (shared by both gates, not duplicated per gate)"
forbid "no retry/backoff loop around a gate invocation" "for attempt in"
forbid "no until-retry loop" "until bash"

# Deterministic report: fixed vocabulary, no wall-time/PID/tmpdir token
# inside the report block itself (those live only in the separate TIMING
# block).
require "deterministic report marker" "PROVE REPORT (deterministic)"
require "report end marker" "END PROVE REPORT"
require "timing block is a separate section" "PROVE TIMING"
require "timing excluded from the deterministic report framing" "not part of the deterministic report"
# The wall-time variable must never be PRINTED inside the deterministic
# report block itself (computing it earlier, to have the value ready, is
# fine) — extract the lines between the two report markers and assert none
# of them mention common_path_wall.
report_start_line="$(rg -n --fixed-strings -- 'PROVE REPORT (deterministic)' "${script}" | head -1 | cut -d: -f1)"
report_end_line="$(rg -n --fixed-strings -- 'END PROVE REPORT' "${script}" | head -1 | cut -d: -f1)"
[[ -n "${report_start_line}" && -n "${report_end_line}" ]] || fail "could not locate the PROVE REPORT markers"
if sed -n "${report_start_line},${report_end_line}p" "${script}" | rg --fixed-strings --quiet -- 'common_path_wall'; then
	fail "common_path_wall must not be printed inside the PROVE REPORT block — it belongs only in the TIMING block"
fi

# Prove-latency budget: read from the doc via rg, not hardcoded as a second
# source of truth, so the shell and go/internal/perfcontract's doc-lockstep
# test can never silently disagree.
require "budget read from the envelope doc, not hardcoded" "local-performance-envelope.md"
require "budget extraction pattern" "common path stays under"
forbid "no bare numeric literal masquerading as a hardcoded budget" "prove_budget_seconds=\"90\""

# The referenced envelope doc must actually carry the phrase prove.sh looks
# for, and the same phrase go/internal/perfcontract/envelope.go pins.
envelope_doc="${repo_root}/docs/public/reference/local-performance-envelope.md"
envelope_go="${repo_root}/go/internal/perfcontract/envelope.go"
[[ -f "${envelope_doc}" ]] || fail "missing ${envelope_doc}"
[[ -f "${envelope_go}" ]] || fail "missing ${envelope_go}"
rg --pcre2 --quiet 'make prove.{0,80}common path stays under `\d+s`' "${envelope_doc}" \
	|| fail "local-performance-envelope.md is missing the make-prove budget phrase prove.sh extracts"
rg --fixed-strings --quiet -- "credential-free common path stays under" "${envelope_go}" \
	|| fail "go/internal/perfcontract/envelope.go is missing the make-prove Threshold bound to the same phrase"
rg --fixed-strings --quiet -- "EnforcementOperatorGated" "${envelope_go}" \
	|| fail "go/internal/perfcontract/envelope.go must exist and use EnforcementOperatorGated"

# No private data: hostnames, IPs, cloud account IDs, keys, internal paths.
private_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|arn:aws:|(^|[^0-9])[0-9]{12}([^0-9]|$)|/Users/|/home/[a-z]'
if rg --pcre2 --quiet -- "${private_pattern}" "${script}"; then
	fail "prove.sh looks like it contains private data"
fi

printf 'test-prove: pass\n'
