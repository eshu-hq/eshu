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

# Fixture repositories define their own bases; the caller's pinned SHA is absent.
unset ESHU_PRE_PUSH_BASE

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
require "scopes to the documented categories" '--category exactness,telemetry,hygiene,docs,security,frontend'
require "runs blocking-only (advisory gates stay out of the mandatory floor)" '--blocking-only'
require "narrows self-tests to changed paths" '--self-tests changed'
require "requests the data-backed pre-push gate deferral" '--pre-push'
forbid "the whole-module prelude (pre-pr/pre-pr-full's job, not pre-push's)" '--pre-pr-whole-module'
forbid "any stamp write" 'eshu-prepr-stamp'
forbid "any stamp bypass variable" 'UNSTAMPED_PUSH'
require "runs the advisory docs-contradiction gate unconditionally" 'verify-docs-contradiction.sh'
require "states the merge rule on success: both Go umbrellas green" 'go-core-complete and go-race-complete must be green to merge'
require "states the merge rule on success: no undispositioned red gate" 'no completed red gate may be left undispositioned'
forbid "the retired claim that the aggregate must be green to merge (#9094)" 'required-gates-complete must still be green to merge'
# The header comment wraps across lines, so fold it before the same checks.
flat="$(sed 's/^# *//' "${script}" | tr '\n' ' ' | tr -s ' ')"
[[ "${flat}" != *'A merge still requires required-gates-complete green'* ]] ||
	fail "header states the retired merge rule (aggregate green) beside the success line (#9094)"
[[ "${flat}" == *'A merge requires go-core-complete and go-race-complete green'* ]] ||
	fail "header must state the same merge rule as the success line (#9094)"

# ---------------------------------------------------------------------------
# Behavioral: execute the real driver against a hermetic fixture repo.
# ---------------------------------------------------------------------------
temp_root="$(mktemp -d)"
# #8673: keep fixture repos out of the host git-ai daemon's Trace2 tracking.
# The daemon keys on its Trace2 socket env; any git process that emits to it
# teaches the daemon the fixture path, and the daemon may then hold a fixture
# .git/ai entry open across teardown (ENOTEMPTY masking green assertions).
# GIT_TRACE2_EVENT=0 disables the event stream (same convention as
# test-verify-moved-file-refs.sh); PERF likewise. The #8671 teardown retries
# stay as belt-and-braces.
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
# Retry teardown: an async host indexer (git-ai daemon via Trace2) may hold a
# fixture .git/ai entry open in the teardown window; a single rm then fails
# with ENOTEMPTY and masks green assertions. Retry briefly, then fail loud.
# Budget assumes release within ~5s; a longer hold still fails loud,
# surfacing the slow indexer instead of masking it.
clean_temp_root() {
	local i
	for i in 1 2 3 4 5; do
		rm -rf "${temp_root}" 2>/dev/null && return 0
		sleep 1
	done
	rm -rf "${temp_root}" || exit 1
}
trap 'clean_temp_root' EXIT

# build_fixture creates a throwaway git repo with fake toolchain stand-ins and
# copies the REAL driver plus its REAL sourced libs into it, so the assertions
# below exercise pre-push.sh's actual control flow, not a paraphrase of it.
build_fixture() {
	local name="$1" fixture
	fixture="${temp_root}/${name}"
	rm -rf "${fixture}"
	mkdir -p "${fixture}/scripts/dev" "${fixture}/scripts/lib" "${fixture}/go" "${fixture}/bin"
	cp "${script}" "${fixture}/scripts/dev/pre-push.sh"
	cp "${repo_root}"/scripts/lib/pre-pr-*.sh "${fixture}/scripts/lib/"
	cp "${repo_root}/scripts/lib/cgo-cflags.sh" "${fixture}/scripts/lib/"
	cp "${repo_root}/scripts/lib/go-toolchain.sh" "${fixture}/scripts/lib/"
	cp "${repo_root}"/scripts/lib/pre-push-*.sh "${fixture}/scripts/lib/" 2>/dev/null || true
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
printf 'toolchain-env GOTOOLCHAIN=%s GOROOT=%s\n' "${GOTOOLCHAIN-unset}" "${GOROOT-unset}" >> "${DRIVER_ARGS_LOG}"
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
	printf 'module example.invalid/fixture\n\ngo 1.26.10\n' > "${fixture}/go/go.mod"
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

# ── Case B2: the floor pins the toolchain from go/go.mod (#9098). A host whose
# go is newer than the module makes golangci-lint and gosec fail on export data;
# the caller here starts from GOTOOLCHAIN=local and a stale GOROOT, exactly the
# failing starting point, and the gates must see go1.26.10 with GOROOT unset.
fixture="$(build_fixture case-b2)"
status=0
: > "${fixture}.args"
GOTOOLCHAIN=local GOROOT=/nonexistent/goroot DRIVER_ARGS_LOG="${fixture}.args" DRIVER_GATE_STATUS=0 \
	PATH="${fixture}/bin:${PATH}" bash "${fixture}/scripts/dev/pre-push.sh" > "${fixture}.log" 2>&1 || status=$?
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case B2: expected exit 0, got ${status}"; }
rg -q -- '^toolchain-env GOTOOLCHAIN=go1\.26\.10 GOROOT=unset$' "${fixture}.args" \
	|| { cat "${fixture}.args" >&2; fail "case B2: gates did not see the go.mod toolchain pin with GOROOT unset"; }

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
	cp "${repo_root}"/scripts/lib/pre-pr-*.sh "${fixture}/scripts/lib/"
	cp "${repo_root}/scripts/lib/cgo-cflags.sh" "${fixture}/scripts/lib/"
	cp "${repo_root}/scripts/lib/go-toolchain.sh" "${fixture}/scripts/lib/"
	cp "${repo_root}"/scripts/lib/pre-push-*.sh "${fixture}/scripts/lib/" 2>/dev/null || true
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

# ── Case J: changed_go_dirs skips the dirs `go build ./...` ignores ──
# `go help packages`: names beginning with "." or "_" are ignored by the go
# tool, as are directories named "testdata". CI builds `./...`, so it never
# compiles a fixture under go/**/testdata; a named dir is built anyway, so the
# focused selector must drop those dirs itself (#7978: a `package main`
# fixture without main() failed `go build` on the floor only).
case_j_out="$(
	go_dir="${temp_root}/case-j/go"
	mkdir -p "${go_dir}/internal/pkg/testdata/src" "${go_dir}/internal/pkg/_scratch" \
		"${go_dir}/internal/pkg/.hidden" "${go_dir}/internal/pkg"
	# shellcheck disable=SC2329  # called by the sourced changed_go_files
	collect_changed_paths() {
		printf '%s\n' go/root.go go/internal/pkg/a.go go/internal/pkg/testdata/src/main.go \
			go/internal/pkg/_scratch/x.go go/internal/pkg/.hidden/y.go
	}
	# shellcheck source=lib/pre-pr-go-paths.sh
	source "${repo_root}/scripts/lib/pre-pr-go-paths.sh"
	changed_go_dirs
)"
[[ "${case_j_out}" == $'./.\n./internal/pkg' ]] || fail "case J: changed_go_dirs = [${case_j_out}], want ./. and ./internal/pkg only (testdata, _ and . dirs are skipped like ./...; the module root stays)"

# ── Case K: no blocking `pre_push: floor` row may sit outside the floor's ──
# category list (#8326). The list is a filter in front of the allowlist, so a
# floor row in a category it omits is silently never run: gosec-changed was one
# (`security`), and cassette-author claimed the floor for the same reason while
# never running there. floor_rows_outside_categories prints "<id> <category>"
# for each such row; the seeded pass drops `security` and must name gosec-changed.
floor_rows_outside_categories() {
	local cats="$1" f
	for f in "${repo_root}"/specs/ci-gates.d/*.yaml; do
		awk -v cats="${cats}" '
			/^  - id: / { id = $3; cat = ""; blocking = "" }
			/^    category: / { cat = $2 }
			/^    blocking: / { blocking = $2 }
			/^      pre_push: floor/ {
				if (blocking == "true" && index("," cats ",", "," cat ",") == 0) { print id, cat }
			}' "${f}"
	done
}
floor_cats="$(rg -o -- '--category [a-z,]+' "${script}" | head -1 | sed 's/--category //')"
[[ -n "${floor_cats}" ]] || fail "case K: could not read the floor category list from pre-push.sh"
case_k_out="$(floor_rows_outside_categories "${floor_cats}")"
[[ -z "${case_k_out}" ]] || fail "case K: blocking pre_push: floor rows outside the floor categories (${floor_cats}): ${case_k_out}"
case_k_seeded="$(floor_rows_outside_categories "${floor_cats//security,/}")"
printf '%s\n' "${case_k_seeded}" | rg -q -- '^gosec-changed security$' ||
	fail "case K: seeded run without security did not name gosec-changed (got: ${case_k_seeded})"

# Single-command builds must not dirty the source tree; multiple packages
# already discard output. The stand-in models the real Go behavior proved
# with a standalone module, while the real driver selects and builds packages.
for scenario in main library multiple broken existing; do
    fixture="$(build_fixture "build-output-${scenario}")"
    if [[ "${scenario}" == library ]]; then
        mkdir -p "${fixture}/go/internal/library"
        printf 'package library\n' > "${fixture}/go/internal/library/value.go"
    else
        mkdir -p "${fixture}/go/cmd/one"
        printf 'package main\nfunc main() {}\n' > "${fixture}/go/cmd/one/main.go"
        if [[ "${scenario}" == multiple ]]; then
            mkdir -p "${fixture}/go/cmd/two"
            printf 'package main\nfunc main() {}\n' > "${fixture}/go/cmd/two/main.go"
        fi
    fi
    if [[ "${scenario}" == existing ]]; then
        printf 'existing executable\n' > "${fixture}/go/one"
    fi
    git -C "${fixture}" -c core.hooksPath=/dev/null add go
    cat > "${fixture}/bin/go" <<'BUILD_OUTPUT'
#!/usr/bin/env bash
printf 'go %s\n' "$*" >> "${DRIVER_ARGS_LOG}"
[[ "${1:-}" == build ]] || exit 0
shift
[[ "${BUILD_OUTPUT_FAIL:-0}" == 0 ]] || exit 17
output=
if [[ "${1:-}" == -o ]]; then
    output="$2"
    shift 2
    [[ "$#" == 1 ]] || exit 18
fi
if [[ "$#" == 1 && "$1" == ./cmd/one ]]; then
    output="${output:-one}"
    [[ "${output}" == /dev/null ]] || : > "${output}"
fi
exit 0
BUILD_OUTPUT
    chmod +x "${fixture}/bin/go"
    if [[ "${scenario}" == broken ]]; then
        status="$(BUILD_OUTPUT_FAIL=1 run_fixture "${fixture}" 0)"
        [[ "${status}" != 0 ]] || fail "broken build was accepted"
    else
        status="$(run_fixture "${fixture}" 0)"
        [[ "${status}" == 0 ]] || { cat "${fixture}.log" >&2; fail "${scenario} build failed"; }
    fi
    if [[ "${scenario}" == existing ]]; then
        [[ "$(cat "${fixture}/go/one")" == "existing executable" ]] || fail "build altered an existing executable"
    fi
    [[ -z "$(git -C "${fixture}" ls-files --others --exclude-standard -- go)" ]] || fail "single main build left untracked output"
    rg -q '^go build ' "${fixture}.args" || fail "${scenario}: build never ran"
    rg -q '^go vet ' "${fixture}.args" || fail "${scenario}: vet never ran"
done

# ── Cases E-I: the merge-tree step (#7111 F5) ──
# shellcheck source=lib/test-pre-push-merge-cases.sh
source "${repo_root}/scripts/lib/test-pre-push-merge-cases.sh"

printf 'test-pre-push: pass\n'
