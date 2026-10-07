#!/usr/bin/env bash
# test-ci-go-install-retry.sh - hermetic checks for
# scripts/ci/go-install-retry.sh.
#
# A `bench script contract` run died on `go install
# golang.org/x/perf/cmd/benchstat@latest` with a proxy.golang.org 502 and only
# passed on rerun: every CI tool install was a bare `go install` with no retry,
# unlike the module pre-warm that go-mod-download-retry.sh already covers. This
# mirror pins the wrapper's retry, validation, and argument contract, and that
# no workflow reintroduces a bare `go install`.
#
# Runs entirely off the network: a fake `go` on PATH
# (scripts/lib/test-ci-go-install-retry-fake-go.sh) stands in for the real
# one, logs each invocation's arguments and working directory, and simulates
# failure/success via env vars it reads. No real proxy.golang.org call happens
# in this test.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${repo_root}/scripts/ci/go-install-retry.sh"
fake_go="${repo_root}/scripts/lib/test-ci-go-install-retry-fake-go.sh"
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

if [ ! -x "${target}" ]; then
	echo "test-ci-go-install-retry: missing executable script at ${target}" >&2
	exit 1
fi

pass=0
fail=0
check() {
	local desc="$1"
	local status="$2"
	if [ "${status}" -eq 0 ]; then
		printf 'PASS: %s\n' "${desc}"
		pass=$((pass + 1))
	else
		printf 'FAIL: %s\n' "${desc}"
		fail=$((fail + 1))
	fi
}

# new_case <name> prints a fresh, unique case directory. mktemp, not $RANDOM:
# a repeated name would reuse a stale invocation count file.
new_case() {
	mktemp -d "${tmp_root}/$1.XXXXXX"
}

# run_case <case-dir> [env assignments...] -- <script args...>
# Installs the fake go under <case-dir>/bin, runs the target with delay 0 from
# <case-dir>/cwd, and stores the exit code in $rc and output in
# <case-dir>/out.log.
run_case() {
	local dir="$1"
	shift
	local -a envs=()
	while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do
		envs+=("$1")
		shift
	done
	[ "$#" -gt 0 ] && shift
	mkdir -p "${dir}/bin" "${dir}/cwd"
	cp "${fake_go}" "${dir}/bin/go"
	chmod +x "${dir}/bin/go"
	rc=0
	(
		cd "${dir}/cwd"
		env PATH="${dir}/bin:${PATH}" \
			FAKE_GO_LOG="${dir}/go.log" \
			FAKE_GO_PWD_LOG="${dir}/pwd.log" \
			FAKE_GO_COUNT_FILE="${dir}/count" \
			ESHU_GO_DOWNLOAD_RETRY_DELAY=0 \
			${envs[@]+"${envs[@]}"} \
			"${target}" "$@"
	) >"${dir}/out.log" 2>&1 || rc=$?
}

invocations() {
	if [ -f "$1/count" ]; then cat "$1/count"; else echo 0; fi
}

pkg_a="golang.org/x/perf/cmd/benchstat@latest"
pkg_b="github.com/securego/gosec/v2/cmd/gosec@v2.27.1"

# --- (a) no arguments: usage error, go never invoked. ---
case_a="$(new_case a)"
run_case "${case_a}" --
check "no arguments exits 2" "$([ "${rc}" -eq 2 ] && echo 0 || echo 1)"
check "no arguments never invokes go" "$([ "$(invocations "${case_a}")" -eq 0 ] && echo 0 || echo 1)"

# --- (b) an unversioned package is refused: inside a module it would resolve
#     through that go.mod instead of the requested version. Refused before ANY
#     install runs, even when a valid package precedes it. ---
case_b="$(new_case b)"
run_case "${case_b}" -- "${pkg_a}" "golang.org/x/vuln/cmd/govulncheck"
check "unversioned package exits 2" "$([ "${rc}" -eq 2 ] && echo 0 || echo 1)"
check "unversioned package never invokes go (validated up front)" "$([ "$(invocations "${case_b}")" -eq 0 ] && echo 0 || echo 1)"
if rg -q 'golang.org/x/vuln/cmd/govulncheck' "${case_b}/out.log"; then
	check "unversioned package error names the argument" 0
else
	check "unversioned package error names the argument" 1
fi

# --- (b2) an empty version or empty package path is refused too. ---
for bad in "golang.org/x/perf/cmd/benchstat@" "@v1.0.0" "a@b@c"; do
	case_b2="$(new_case b2)"
	run_case "${case_b2}" -- "${bad}"
	check "malformed package \"${bad}\" exits 2" "$([ "${rc}" -eq 2 ] && echo 0 || echo 1)"
	check "malformed package \"${bad}\" never invokes go" "$([ "$(invocations "${case_b2}")" -eq 0 ] && echo 0 || echo 1)"
done

# --- (c) invalid attempts/delay knobs exit 2, as go-mod-download-retry.sh
#     does, instead of silently installing nothing. ---
for bad_attempts in 0 abc -1; do
	case_c="$(new_case c-attempts)"
	run_case "${case_c}" "ESHU_GO_DOWNLOAD_ATTEMPTS=${bad_attempts}" -- "${pkg_a}"
	check "ESHU_GO_DOWNLOAD_ATTEMPTS=\"${bad_attempts}\" exits 2" "$([ "${rc}" -eq 2 ] && echo 0 || echo 1)"
	check "ESHU_GO_DOWNLOAD_ATTEMPTS=\"${bad_attempts}\" never invokes go" "$([ "$(invocations "${case_c}")" -eq 0 ] && echo 0 || echo 1)"
done
for bad_delay in x -1 1.5; do
	case_c="$(new_case c-delay)"
	run_case "${case_c}" "ESHU_GO_DOWNLOAD_RETRY_DELAY=${bad_delay}" -- "${pkg_a}"
	check "ESHU_GO_DOWNLOAD_RETRY_DELAY=\"${bad_delay}\" exits 2" "$([ "${rc}" -eq 2 ] && echo 0 || echo 1)"
	check "ESHU_GO_DOWNLOAD_RETRY_DELAY=\"${bad_delay}\" never invokes go" "$([ "$(invocations "${case_c}")" -eq 0 ] && echo 0 || echo 1)"
done

# --- (d) two simulated proxy failures then success still exits 0, installs
#     the exact versioned argument, and runs in the caller's directory. ---
case_d="$(new_case d)"
run_case "${case_d}" FAKE_GO_FAIL_UNTIL=2 ESHU_GO_DOWNLOAD_ATTEMPTS=3 -- "${pkg_a}"
check "two transient failures then success exits 0" "$([ "${rc}" -eq 0 ] && echo 0 || echo 1)"
check "retried exactly 3 times" "$([ "$(invocations "${case_d}")" -eq 3 ] && echo 0 || echo 1)"
if rg -q 'succeeded on attempt 3/3' "${case_d}/out.log"; then
	check "reports which attempt succeeded" 0
else
	check "reports which attempt succeeded" 1
fi
if [ "$(sort -u "${case_d}/go.log")" = "install ${pkg_a}" ]; then
	check "invokes 'go install <pkg@version>' unchanged" 0
else
	check "invokes 'go install <pkg@version>' unchanged" 1
fi
if [ "$(sort -u "${case_d}/pwd.log")" = "$(cd "${case_d}/cwd" && pwd)" ]; then
	check "runs go install from the caller's directory" 0
else
	check "runs go install from the caller's directory" 1
fi

# --- (e) exhausting every attempt fails loud after exactly N attempts,
#     naming the package. ---
case_e="$(new_case e)"
run_case "${case_e}" FAKE_GO_FAIL_UNTIL=99 ESHU_GO_DOWNLOAD_ATTEMPTS=2 -- "${pkg_a}"
check "a package that never installs exits 1" "$([ "${rc}" -eq 1 ] && echo 0 || echo 1)"
check "gives up after exactly 2 attempts" "$([ "$(invocations "${case_e}")" -eq 2 ] && echo 0 || echo 1)"
if rg -q 'still failing after 2 attempt' "${case_e}/out.log" && rg -qF "${pkg_a}" "${case_e}/out.log"; then
	check "exhausted-retry message names attempts and package" 0
else
	check "exhausted-retry message names attempts and package" 1
fi

# --- (f) multiple packages: each installed once, in argument order. ---
case_f="$(new_case f)"
run_case "${case_f}" -- "${pkg_a}" "${pkg_b}"
check "multiple packages exit 0" "$([ "${rc}" -eq 0 ] && echo 0 || echo 1)"
if [ "$(cat "${case_f}/go.log")" = "$(printf 'install %s\ninstall %s' "${pkg_a}" "${pkg_b}")" ]; then
	check "multiple packages each installed once, in order" 0
else
	check "multiple packages each installed once, in order" 1
fi

# --- (g) a later package that never installs fails the run, with its own
#     full retry budget, and the message names that package. ---
case_g="$(new_case g)"
run_case "${case_g}" "FAKE_GO_FAIL_PKG=${pkg_b}" ESHU_GO_DOWNLOAD_ATTEMPTS=3 -- "${pkg_a}" "${pkg_b}"
check "failing second package exits 1" "$([ "${rc}" -eq 1 ] && echo 0 || echo 1)"
check "second package gets its own 3 attempts (1 + 3 invocations)" "$([ "$(invocations "${case_g}")" -eq 4 ] && echo 0 || echo 1)"
if rg -qF "${pkg_b}" "${case_g}/out.log" && rg -q 'still failing after 3 attempt' "${case_g}/out.log"; then
	check "failing second package message names it" 0
else
	check "failing second package message names it" 1
fi

# check_workflow_wiring <workflows-dir> is the CI-wiring guard: no
# non-comment workflow line may run a bare `go install` (each install goes
# through the wrapper), and <workflows-dir>/test.yml must run this mirror
# from a non-comment line. Prints findings; returns 0 clean, 1 on a
# violation, 2 when the scan itself fails, so an unreadable tree fails
# closed instead of reading as "no bare installs".
check_workflow_wiring() {
	local wf_dir="$1"
	local found=""
	local scan_rc=0
	found="$(rg -n '^[^#]*\bgo[[:space:]]+install\b' "${wf_dir}" 2>&1)" || scan_rc=$?
	case "${scan_rc}" in
	0)
		printf 'bare go install:\n%s\n' "${found}"
		return 1
		;;
	1) ;;
	*)
		printf 'workflow scan failed (rg exit %s): %s\n' "${scan_rc}" "${found}"
		return 2
		;;
	esac
	if ! rg -q '^[^#]*run: bash scripts/test-ci-go-install-retry\.sh' "${wf_dir}/test.yml" 2>/dev/null; then
		printf '%s/test.yml does not run scripts/test-ci-go-install-retry.sh\n' "${wf_dir}"
		return 1
	fi
	return 0
}

# wiring_case <name> <file> [filter-command...]: copies the real workflows
# into a fresh dir, pipes <file> there through the filter (stdin to stdout;
# never touching the real tree), and stores the guard's exit code in
# $wiring_rc. No filter means an unedited copy.
wiring_case() {
	local dir
	dir="$(new_case "wiring-$1")"
	local file="$2"
	shift 2
	cp -R "${repo_root}/.github/workflows" "${dir}/workflows"
	if [ "$#" -gt 0 ]; then
		"$@" <"${dir}/workflows/${file}" >"${dir}/edited"
		if cmp -s "${dir}/edited" "${dir}/workflows/${file}"; then
			# The seed did not apply (the real step text moved): report a
			# value no case expects rather than testing an unplanted defect.
			printf 'seed %s left %s unchanged\n' "${dir##*/}" "${file}"
			wiring_rc=99
			return 0
		fi
		mv "${dir}/edited" "${dir}/workflows/${file}"
	fi
	wiring_rc=0
	check_workflow_wiring "${dir}/workflows" >"${dir}/out.log" 2>&1 || wiring_rc=$?
}

benchstat_step='run: scripts/ci/go-install-retry.sh golang.org/x/perf/cmd/benchstat@latest'

# --- (h) CI wiring on the real tree. ---
wiring_rc=0
check_workflow_wiring "${repo_root}/.github/workflows" || wiring_rc=$?
check "real workflows: no bare 'go install', test.yml runs this mirror" "${wiring_rc}"

# --- (i) seeded violations: the guard must fail on each planted defect in a
#     copy of the real workflows, and pass on the unedited copy. ---
wiring_case clean test.yml
check "seeded GREEN: unedited workflow copy passes the guard" "$([ "${wiring_rc}" -eq 0 ] && echo 0 || echo 1)"
wiring_case bare-run bench.yml sed -e "s|${benchstat_step}|run: go install foo@v1|"
check "seeded RED: planted 'run: go install foo@v1' fails the guard" "$([ "${wiring_rc}" -eq 1 ] && echo 0 || echo 1)"
wiring_case bare-spaces bench.yml sed -e "s|${benchstat_step}|run: go   install foo@v1|"
check "seeded RED: planted 'go   install' (extra spaces) fails the guard" "$([ "${wiring_rc}" -eq 1 ] && echo 0 || echo 1)"
# shellcheck disable=SC2016 # The awk program must stay literal.
wiring_case bare-block bench.yml awk -v step="${benchstat_step}" '
	index($0, step) { sub(/run: .*/, "run: |"); print; sub(/run: \|/, "  go install foo@v1"); print; next }
	{ print }'
check "seeded RED: bare 'go install' inside a run: | block fails the guard" "$([ "${wiring_rc}" -eq 1 ] && echo 0 || echo 1)"
# shellcheck disable=SC2016 # The awk program must stay literal.
wiring_case comment-only bench.yml awk -v step="${benchstat_step}" '
	{ print }
	index($0, step) { sub(/run: .*/, "# go install foo@v1 was the old step"); print }'
check "seeded GREEN: a commented-out 'go install' does not trip the guard" "$([ "${wiring_rc}" -eq 0 ] && echo 0 || echo 1)"
wiring_case no-mirror test.yml sed -e '/run: bash scripts\/test-ci-go-install-retry\.sh/d'
check "seeded RED: removing the mirror step from test.yml fails the guard" "$([ "${wiring_rc}" -eq 1 ] && echo 0 || echo 1)"
wiring_case commented-mirror test.yml sed -e 's|run: bash scripts/test-ci-go-install-retry\.sh|# &|'
check "seeded RED: commenting out the mirror step fails the guard" "$([ "${wiring_rc}" -eq 1 ] && echo 0 || echo 1)"
wiring_rc=0
check_workflow_wiring "${tmp_root}/does-not-exist" >/dev/null 2>&1 || wiring_rc=$?
check "seeded RED: an unreadable workflows dir fails closed (exit 2), not clean" "$([ "${wiring_rc}" -eq 2 ] && echo 0 || echo 1)"

echo
echo "test-ci-go-install-retry: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
