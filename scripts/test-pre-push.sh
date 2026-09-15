#!/usr/bin/env bash
# Behavioral hermetic test for scripts/dev/pre-push.sh, modeled on
# scripts/test-pre-pr-whole-module-gates.sh's assert_driver_lane: it executes
# the REAL driver against a synthetic git repo with fake `go`,
# `run-selected-gates.sh`, `precommit-go.sh`, and `verify-docs-contradiction.sh`
# stand-ins, and asserts on its actual exit code and captured argv — not on the
# driver's source text.
#
# What this pins (the fast local floor before every push, replacing the
# per-SHA `make pre-pr` push stamp):
#   - it runs the registry-selected gates via run-selected-gates.sh WITHOUT
#     --pre-pr-whole-module (pre-push never runs the whole-module Go lanes);
#   - it exits non-zero when any gate fails;
#   - it writes no stamp (no .git/eshu-prepr-stamp directory, ever);
#   - it always runs the advisory docs-contradiction gate, which has no CI
#     workflow and would otherwise have no local enforcement at all.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/dev/pre-push.sh"

fail() { printf 'test-pre-push: %s\n' "$*" >&2; exit 1; }

[[ -f "${script}" ]] || fail "missing ${script}"
[[ -x "${script}" ]] || fail "pre-push.sh must be executable"
bash -n "${script}" || fail "pre-push.sh has a syntax error"

lines="$(wc -l <"${script}" | tr -d '[:space:]')"
[[ "${lines}" -lt 500 ]] || fail "pre-push.sh must stay under 500 lines (has ${lines})"

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

require "sources the shared Go-path helpers" 'scripts/lib/pre-pr-go-paths.sh'
require "sources the fixture-consumer helpers" 'scripts/lib/pre-pr-fixture-consumers.sh'
require "sources the focused test-selection helper" 'scripts/lib/pre-pr-test-selection.sh'
require "runs the registry-selected gates at tier pre-pr" '--tier pre-pr'
require "scopes to the documented categories" '--category exactness,telemetry,hygiene,docs'
require "runs blocking-only (advisory gates stay out of the mandatory floor)" '--blocking-only'
require "narrows self-tests to changed paths" '--self-tests changed'
require "requests the data-backed pre-push gate deferral" '--pre-push'
forbid "the whole-module prelude (pre-pr/pre-pr-full's job, not pre-push's)" '--pre-pr-whole-module'
forbid "any stamp write" 'eshu-prepr-stamp'
forbid "any stamp bypass variable" 'UNSTAMPED_PUSH'
require "runs the advisory docs-contradiction gate unconditionally" 'verify-docs-contradiction.sh'
require "names the CI required-gates aggregate on success" 'required-gates-complete'

# ---------------------------------------------------------------------------
# Behavioral: execute the real driver against a hermetic fixture repo.
# ---------------------------------------------------------------------------
temp_root="$(mktemp -d)"
trap 'rm -rf "${temp_root}"' EXIT

# build_fixture creates a throwaway git repo with fake toolchain stand-ins and
# copies the REAL driver plus its REAL sourced libs into it, so the assertions
# below exercise pre-push.sh's actual control flow, not a paraphrase of it.
build_fixture() {
	local name="$1" fixture
	fixture="${temp_root}/${name}"
	rm -rf "${fixture}"
	mkdir -p "${fixture}/scripts/dev" "${fixture}/scripts/lib" "${fixture}/go" "${fixture}/bin"
	cp "${script}" "${fixture}/scripts/dev/pre-push.sh"
	cp "${repo_root}"/scripts/lib/pre-pr-lane.sh "${fixture}/scripts/lib/"
	cp "${repo_root}"/scripts/lib/pre-pr-fixture-consumers.sh "${fixture}/scripts/lib/"
	cp "${repo_root}"/scripts/lib/pre-pr-test-selection.sh "${fixture}/scripts/lib/"
	cp "${repo_root}"/scripts/lib/pre-pr-go-paths.sh "${fixture}/scripts/lib/"
	printf '#!/bin/sh\nexit 0\n' > "${fixture}/bin/go"
	chmod +x "${fixture}/bin/go"
	cat > "${fixture}/scripts/dev/precommit-go.sh" <<'PRECOMMIT'
#!/usr/bin/env bash
printf 'precommit-go %s\n' "$*" >> "${DRIVER_ARGS_LOG}"
exit 0
PRECOMMIT
	chmod +x "${fixture}/scripts/dev/precommit-go.sh"
	cat > "${fixture}/scripts/dev/run-selected-gates.sh" <<'GATE'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >> "${DRIVER_ARGS_LOG}"
exit "${DRIVER_GATE_STATUS}"
GATE
	chmod +x "${fixture}/scripts/dev/run-selected-gates.sh"
	mkdir -p "${fixture}/scripts"
	cat > "${fixture}/scripts/verify-docs-contradiction.sh" <<'DOCS'
#!/usr/bin/env bash
printf 'docs-contradiction ran\n' >> "${DRIVER_ARGS_LOG}"
exit 0
DOCS
	chmod +x "${fixture}/scripts/verify-docs-contradiction.sh"
	printf 'baseline\n' > "${fixture}/README.md"
	git -C "${fixture}" init -q
	git -C "${fixture}" -c core.hooksPath=/dev/null add .
	git -C "${fixture}" -c core.hooksPath=/dev/null -c user.name=Test -c user.email=test@example.invalid commit -qm baseline
	local head
	head="$(git -C "${fixture}" rev-parse HEAD)"
	git -C "${fixture}" update-ref refs/remotes/origin/main "${head}"
	printf 'changed\n' >> "${fixture}/README.md"
	printf '%s\n' "${fixture}"
}

run_fixture() {
	local fixture="$1" gate_status="$2" status=0
	: > "${fixture}.args"
	DRIVER_ARGS_LOG="${fixture}.args" DRIVER_GATE_STATUS="${gate_status}" \
		PATH="${fixture}/bin:${PATH}" bash "${fixture}/scripts/dev/pre-push.sh" \
		> "${fixture}.log" 2>&1 || status=$?
	printf '%s' "${status}"
}

# ── Case A: every gate passes → exit 0, no stamp, docs-contradiction ran ──
fixture="$(build_fixture case-a)"
status="$(run_fixture "${fixture}" 0)"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case A: expected exit 0, got ${status}"; }
[[ ! -e "${fixture}/.git/eshu-prepr-stamp" ]] || fail "case A: pre-push must never write a stamp directory"
rg -q -- '--self-tests' "${fixture}.args" || fail "case A: driver never reached the selected exactness gates"
rg -q -- '^changed$' "${fixture}.args" || fail "case A: driver did not pass --self-tests changed"
rg -q -- '--blocking-only' "${fixture}.args" || fail "case A: driver did not run blocking-only"
rg -q -- '--pre-pr-whole-module' "${fixture}.args" && fail "case A: driver must never request the whole-module prelude"
rg -q -- 'docs-contradiction ran' "${fixture}.args" || fail "case A: driver never ran the advisory docs-contradiction gate"
rg -q -- '--category race' "${fixture}.args" && fail "case A: driver must not run a race lane"

# ── Case B: a selected gate fails → exit non-zero, still no stamp ──
fixture="$(build_fixture case-b)"
status="$(run_fixture "${fixture}" 23)"
[[ "${status}" != "0" ]] || fail "case B: expected a non-zero exit when a gate fails, got 0"
[[ ! -e "${fixture}/.git/eshu-prepr-stamp" ]] || fail "case B: a failed run must never write a stamp directory"

# ── Case C: an unresolvable ESHU_PRE_PUSH_BASE fails instead of narrowing ──
# Silently falling back to HEAD~1 would check only the last commit's paths and
# still report success.
fixture="$(build_fixture case-c)"
status=0
: > "${fixture}.args"
ESHU_PRE_PUSH_BASE="refs/heads/does-not-exist-pre-push-base" DRIVER_ARGS_LOG="${fixture}.args" DRIVER_GATE_STATUS=0 \
	PATH="${fixture}/bin:${PATH}" bash "${fixture}/scripts/dev/pre-push.sh" > "${fixture}.log" 2>&1 || status=$?
[[ "${status}" == "2" ]] || { cat "${fixture}.log" >&2; fail "case C: expected exit 2 for an unresolvable base, got ${status}"; }
rg -q -- 'does not resolve to a commit' "${fixture}.log" || fail "case C: failure did not name the unresolvable base"
rg -q -- '--self-tests' "${fixture}.args" && fail "case C: gates ran against a narrowed base"

# ── Case D: a commit deletes the last Go file in a package → fmt/lint must
# not be handed the now-nonexistent path (#6712 review). build_fixture's
# `precommit-go.sh` stand-in only logs args; this one also fails fmt/lint
# exactly the way golangci-lint fails on a deleted directory (`lstat: no such
# file or directory`) so the assertion is on the real failure mode, not a
# paraphrase of it.
build_fixture_deleted_package() {
	local fixture="${temp_root}/case-d"
	rm -rf "${fixture}"
	mkdir -p "${fixture}/scripts/dev" "${fixture}/scripts/lib" "${fixture}/go/internal/deleted" "${fixture}/bin"
	cp "${script}" "${fixture}/scripts/dev/pre-push.sh"
	cp "${repo_root}"/scripts/lib/pre-pr-lane.sh "${fixture}/scripts/lib/"
	cp "${repo_root}"/scripts/lib/pre-pr-fixture-consumers.sh "${fixture}/scripts/lib/"
	cp "${repo_root}"/scripts/lib/pre-pr-test-selection.sh "${fixture}/scripts/lib/"
	cp "${repo_root}"/scripts/lib/pre-pr-go-paths.sh "${fixture}/scripts/lib/"
	printf '#!/bin/sh\nexit 0\n' > "${fixture}/bin/go"
	chmod +x "${fixture}/bin/go"
	cat > "${fixture}/scripts/dev/precommit-go.sh" <<'PRECOMMIT'
#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
printf 'precommit-go %s\n' "$*" >> "${DRIVER_ARGS_LOG}"
cmd="${1:-}"; shift || true
if [[ "${cmd}" == "fmt" || "${cmd}" == "lint" ]]; then
	for f in "$@"; do
		if [[ ! -f "${root}/${f}" ]]; then
			printf 'precommit-go %s: lstat %s: no such file or directory\n' "${cmd}" "${f}" >&2
			exit 3
		fi
	done
fi
exit 0
PRECOMMIT
	chmod +x "${fixture}/scripts/dev/precommit-go.sh"
	cat > "${fixture}/scripts/dev/run-selected-gates.sh" <<'GATE'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" >> "${DRIVER_ARGS_LOG}"
exit 0
GATE
	chmod +x "${fixture}/scripts/dev/run-selected-gates.sh"
	cat > "${fixture}/scripts/verify-docs-contradiction.sh" <<'DOCS'
#!/usr/bin/env bash
printf 'docs-contradiction ran\n' >> "${DRIVER_ARGS_LOG}"
exit 0
DOCS
	chmod +x "${fixture}/scripts/verify-docs-contradiction.sh"
	printf 'package deleted\n' > "${fixture}/go/internal/deleted/pkg.go"
	printf 'baseline\n' > "${fixture}/README.md"
	git -C "${fixture}" init -q
	git -C "${fixture}" -c core.hooksPath=/dev/null add .
	git -C "${fixture}" -c core.hooksPath=/dev/null -c user.name=Test -c user.email=test@example.invalid commit -qm baseline
	local head
	head="$(git -C "${fixture}" rev-parse HEAD)"
	git -C "${fixture}" update-ref refs/remotes/origin/main "${head}"
	# Delete the package's only Go file and its now-empty directory —
	# reproduces "a commit deletes the last Go file in a package". Staged
	# (not committed) so collect_changed_paths' `--cached` half picks it up,
	# the same way case A/B/C rely on an uncommitted README.md edit.
	git -C "${fixture}" rm -q go/internal/deleted/pkg.go
	rmdir "${fixture}/go/internal/deleted" 2>/dev/null || true
	printf '%s\n' "${fixture}"
}

fixture="$(build_fixture_deleted_package)"
status="$(run_fixture "${fixture}" 0)"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case D: a deleted package must not fail fmt/lint with an lstat error, got exit ${status}"; }
# filecap is allowed to still see the deleted path (filecap_check_file skips a
# missing file internally and never errors) — only fmt/lint must not.
rg -q -- '^precommit-go (fmt|lint) .*go/internal/deleted/pkg\.go' "${fixture}.args" && \
	{ cat "${fixture}.args" >&2; fail "case D: the deleted file must never reach precommit-go.sh fmt/lint"; }
true

printf 'test-pre-push: pass\n'
