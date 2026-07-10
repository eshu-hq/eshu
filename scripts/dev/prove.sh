#!/usr/bin/env bash
# prove: the credential-free local entry point that mirrors the Ifá CI gate
# (design doc docs/internal/design/4389-ifa-conformance-platform.md,
# "Contributor contract" / "make prove", ~lines 708-744). Run before opening
# or updating a PR that touches Ifá:
#
#   bash scripts/dev/prove.sh        # or: make prove
#
# Two layers:
#   1. Credential-free common path (ALWAYS runs, independent of changed
#      paths): the Ifá contract-layer test, the hermetic structural mirrors
#      for both Docker-backed determinism matrices, and the `ifa coverage`
#      reconcile against the manifest so a new fact kind or surface cannot
#      land silently uncovered. This is the path the prove-latency budget
#      below is measured against.
#   2. Docker matrix (Layer 2), path-selected via the SAME `ci-gates select`
#      registry every other gate uses (specs/ci-gates.v1.yaml). The
#      registry's own `local.command` for the `ifa-determinism` /
#      `ifa-dead-letter-matrix` gate ids IS the hermetic mirror already run
#      in step 1 — not the real Docker matrix — so this script never runs
#      `ci-gates run` for this layer; it invokes
#      scripts/verify-ifa-determinism.sh and
#      scripts/verify-ifa-dead-letter-matrix.sh directly, only when the
#      changed paths select the corresponding gate id AND Docker is present.
#      When Docker is absent, it prints operator guidance and defers
#      non-fatally (mirroring scripts/dev/trivy-fs-local.sh's loud-defer
#      pattern) — it never silently reports a pass as though the matrix ran.
#   prove.sh registers no new gate id in specs/ci-gates.v1.yaml: it is a thin
#   composition over gates that already exist there, not a new one.
#
# Flake policy — NO retry-to-green, ever
# (scripts/verify-ifa-determinism.sh, "Per this platform's flake policy...
# a real divergence here must be root-caused, never normalized away by
# lowering N, retrying, or reducing worker counts", ~line 45). A
# nondeterministic failure IS a determinism defect, the exact class this
# platform exists to catch. This script never re-runs a failed step to turn
# it green — every step below executes exactly once.
#
# Prove-latency budget — the common path (step 1) carries a measured
# wall-time budget, read from docs/public/reference/local-performance-
# envelope.md (the same doc go/internal/perfcontract's localEnvelopeThresholds
# binds in lockstep) so the shell budget and the Go contract test can never
# silently disagree. The Docker matrix's wall time varies by machine/Docker
# state and is reported informationally only, never budgeted. Exceeding the
# budget WARNS — this script does not fail on it (EnforcementOperatorGated,
# not a hermetic gate; a slow prove path is how test frameworks rot into
# being skipped, so the warning stays loud).
#
# Portability: plain indexed arrays only, no associative arrays, matching
# scripts/dev/pre-pr.sh — the "bash" resolved by a Makefile recipe's PATH is
# not guaranteed to be bash 4+ (macOS ships /bin/bash 3.2 as the system
# default), and associative arrays are a bash-4-only feature.
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
go_dir="${repo_root}/go"
registry="${repo_root}/specs/ci-gates.v1.yaml"
envelope_doc="${repo_root}/docs/public/reference/local-performance-envelope.md"

log() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }

base="origin/main"
git -C "${repo_root}" fetch --no-tags origin main >/dev/null 2>&1 || true
git -C "${repo_root}" rev-parse --verify "${base}" >/dev/null 2>&1 || base="HEAD~1"

overall=0
# step_labels/step_statuses are index-aligned parallel arrays, appended in a
# fixed run order (never reordered, never appended twice for the same step),
# so printing them in append order is already the deterministic report order.
step_labels=()
step_statuses=()
record_status() { step_labels+=("$1"); step_statuses+=("$2"); }
status_of() {
	local want="$1" i
	for i in "${!step_labels[@]}"; do
		[[ "${step_labels[${i}]}" == "${want}" ]] && { printf '%s' "${step_statuses[${i}]}"; return 0; }
	done
	return 1
}

run_common_step() {
	local label="$1"
	shift
	log "${label}"
	if "$@"; then
		record_status "${label}" "PASS"
	else
		record_status "${label}" "FAIL"
		overall=1
	fi
}

step_contract_layer() { (cd "${go_dir}" && go test ./internal/ifa ./cmd/ifa -count=1); }
step_determinism_mirror() { bash "${repo_root}/scripts/test-verify-ifa-determinism.sh"; }
step_deadletter_mirror() { bash "${repo_root}/scripts/test-verify-ifa-dead-letter-matrix.sh"; }
# step_coverage runs `ifa coverage` in its default advisory mode, without the
# `-blocking` flag, on purpose: an uncovered surface is the expected P2+
# backfill worklist (specs/ifa-coverage-manifest.v1.yaml's header), not a
# defect, so make prove reports gaps without failing on them. The step still
# fails loudly on a real tooling error (an unreadable manifest, a malformed
# registry) — it tolerates advisory coverage gaps, not a broken reconcile. The
# blocking Ifá contract gate is `ifa-contract-layer`'s `go test ./internal/ifa`
# (step_contract_layer above), which make prove already runs.
step_coverage() {
	(cd "${go_dir}" && go run ./cmd/ifa coverage \
		-specs-dir "${repo_root}/specs" \
		-snapshot "${repo_root}/testdata/golden/e2e-20repo-snapshot.json")
}

common_path_start=${SECONDS}
run_common_step "ifa contract-layer tests" step_contract_layer
run_common_step "hermetic determinism structural mirror" step_determinism_mirror
run_common_step "hermetic dead-letter-matrix structural mirror" step_deadletter_mirror
run_common_step "ifa coverage reconcile (advisory)" step_coverage
common_path_wall=$((SECONDS - common_path_start))

# ---------------------------------------------------------------------------
# Layer 2: Docker matrix, path-selected via `ci-gates select` (the same
# registry-driven selection every other local gate uses), never via
# `ci-gates run` — see header. --explain prints one SELECTED/SKIPPED line per
# registry gate id; only the two Layer 2 gate ids are read from it.
# ---------------------------------------------------------------------------
explain="$(go -C "${go_dir}" run ./cmd/ci-gates select \
	--registry "${registry}" --base "${base}" --tier pre-pr --category exactness --explain 2>&1)"

gate_selected() {
	# rg --quiet exits as soon as it confirms a match (it does not need to
	# scan the rest of stdin), which can close its read end before `printf`
	# finishes writing and SIGPIPE the upstream `printf` — under `pipefail`
	# that 141 (128+SIGPIPE), not rg's own 0/1 result, would become this
	# pipeline's reported status. Read rg's own exit code from PIPESTATUS
	# instead of the pipefail-tainted overall pipeline status, so a SIGPIPE'd
	# `printf` can never masquerade as "not selected".
	printf '%s\n' "${explain}" | rg --quiet --pcre2 "^SELECTED\s+${1}\b"
	local rg_status=${PIPESTATUS[1]}
	return "${rg_status}"
}

# safe_bash resolves a bash binary new enough for scripts/verify-ifa-*.sh and
# scripts/lib/ifa_determinism_common.sh, which use bash-4+-only constructs
# (associative arrays, and bash 4.4's fix for expanding an empty array under
# `set -u`). Plain PATH-resolved "bash" is NOT safe to assume: macOS ships
# /bin/bash 3.2 as the system default, and running these scripts under it
# does not just fail loudly — bash 3.2's nounset+empty-array bug, combined
# with these scripts' own `ifa_det_build_bin ... || die ...` pattern, was
# observed in practice to make a real mid-script crash get reported as a
# CLEAN PASS (the `trap cleanup EXIT` handler's own `$?` capture read 0, not
# the crash). A silently-wrong PASS is worse than deferring, so this is
# checked as a hard precondition before ever trusting this layer's result,
# not assumed from the ambient PATH.
safe_bash() {
	local candidate resolved major
	for candidate in bash /opt/homebrew/bin/bash /usr/local/bin/bash; do
		command -v "${candidate}" >/dev/null 2>&1 || continue
		resolved="$(command -v "${candidate}")"
		major="$("${resolved}" -c 'printf "%s" "${BASH_VERSINFO[0]}"' 2>/dev/null || printf '0')"
		if [[ "${major}" =~ ^[0-9]+$ ]] && [[ "${major}" -ge 4 ]]; then
			printf '%s' "${resolved}"
			return 0
		fi
	done
	return 1
}

run_layer2() {
	local gate_id="$1" label="$2" script_rel="$3"
	if ! gate_selected "${gate_id}"; then
		record_status "${label}" "SKIP (not selected for changed paths)"
		return
	fi
	if ! command -v docker >/dev/null 2>&1; then
		printf 'prove: docker not found — deferring %s.\n' "${label}"
		printf 'prove: changed paths select this gate, but the real Docker matrix needs Docker.\n'
		printf 'prove: install Docker (https://docs.docker.com/get-docker/) and re-run, or run it\n'
		printf 'prove: directly once Docker is available: bash %s\n' "${script_rel}"
		printf 'prove: CI runs the authoritative Docker matrix; this defer is informational, not a pass.\n'
		record_status "${label}" "DEFER (docker unavailable)"
		return
	fi
	local bash_bin
	if ! bash_bin="$(safe_bash)"; then
		printf 'prove: no bash >= 4 found — deferring %s.\n' "${label}"
		printf 'prove: %s (and scripts/lib/ifa_determinism_common.sh) needs bash 4+; the\n' "${script_rel}"
		printf 'prove: "bash" this PATH resolves to is too old to run it safely.\n'
		printf 'prove: install a newer bash (e.g. `brew install bash`) and put it earlier on PATH,\n'
		printf 'prove: or run it directly with that bash: /opt/homebrew/bin/bash %s\n' "${script_rel}"
		printf 'prove: CI runs the authoritative Docker matrix; this defer is informational, not a pass.\n'
		record_status "${label}" "DEFER (bash too old)"
		return
	fi
	log "${label}"
	if "${bash_bin}" "${repo_root}/${script_rel}"; then
		record_status "${label}" "PASS"
	else
		record_status "${label}" "FAIL"
		overall=1
	fi
}

run_layer2 "ifa-determinism" "docker matrix: graph-determinism (Layer 2)" "scripts/verify-ifa-determinism.sh"
run_layer2 "ifa-dead-letter-matrix" "docker matrix: dead-letter-set determinism (Layer 2)" "scripts/verify-ifa-dead-letter-matrix.sh"

# ---------------------------------------------------------------------------
# Deterministic report: fixed step order, fixed vocabulary, zero per-run
# tokens (no wall time, no PID, no tmpdir path) — two runs against the same
# repo state MUST produce a byte-identical block here. Wall-clock numbers
# live only in the TIMING block that follows, never in this one.
# ---------------------------------------------------------------------------
printf '\n==== PROVE REPORT (deterministic) ====\n'
for i in "${!step_labels[@]}"; do
	printf '%-46s %s\n' "${step_labels[${i}]}" "${step_statuses[${i}]}"
done
printf '==== END PROVE REPORT ====\n'

# prove_budget_seconds is read from the doc, not hardcoded here, so the shell
# budget and go/internal/perfcontract's doc-lockstep test can never silently
# disagree — both derive from the same phrase in envelope_doc.
prove_budget_seconds="$(rg -o --pcre2 '(?<=common path stays under `)\d+(?=s`)' "${envelope_doc}" 2>/dev/null | head -1 || true)"

printf '\n==== PROVE TIMING (informational; not part of the deterministic report) ====\n'
if [[ -n "${prove_budget_seconds}" ]]; then
	printf 'credential-free common path wall time: %ss (budget: %ss, from %s)\n' \
		"${common_path_wall}" "${prove_budget_seconds}" "${envelope_doc#"${repo_root}"/}"
	if [[ ${common_path_wall} -gt ${prove_budget_seconds} ]]; then
		printf 'prove: WARN — common path exceeded the %ss budget. This is operator-gated, not a hermetic\n' "${prove_budget_seconds}"
		printf 'prove: failure; investigate before a slow prove path rots into being skipped.\n'
	fi
else
	printf 'credential-free common path wall time: %ss (budget phrase not found in %s)\n' \
		"${common_path_wall}" "${envelope_doc#"${repo_root}"/}"
fi
if [[ "$(status_of "docker matrix: graph-determinism (Layer 2)")" == "PASS" || "$(status_of "docker matrix: dead-letter-set determinism (Layer 2)")" == "PASS" ]]; then
	printf 'docker matrix wall time: see the per-cell "wall=Ns" lines above (varies by machine/Docker state; never budgeted)\n'
fi
printf '==== END PROVE TIMING ====\n'

if [[ ${overall} -ne 0 ]]; then
	printf '\n\033[31mprove: failures above — fix and re-run before opening or updating a PR.\033[0m\n'
else
	printf '\n\033[32mprove: credential-free path passed (Docker matrix per the report above).\033[0m\n'
fi
exit ${overall}
