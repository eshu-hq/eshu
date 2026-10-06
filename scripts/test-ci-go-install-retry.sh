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
# Runs entirely off the network: a fake `go` on PATH stands in for the real
# one, logs each invocation's arguments and working directory, and simulates
# failure/success via env vars the fake script reads. No real
# proxy.golang.org call happens in this test.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${repo_root}/scripts/ci/go-install-retry.sh"
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

# install_fake_go writes a `go` stub to $1/bin that only understands
# `install <pkg>`: it appends "<args>" to FAKE_GO_LOG and its cwd to
# FAKE_GO_PWD_LOG once per invocation, counts invocations in
# FAKE_GO_COUNT_FILE, fails every invocation at or below FAKE_GO_FAIL_UNTIL
# (default 0, meaning "never fail"), and always fails an install of
# FAKE_GO_FAIL_PKG.
install_fake_go() {
	local bin_dir="$1"
	mkdir -p "${bin_dir}"
	cat >"${bin_dir}/go" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "install" ]]; then
	printf '%s\n' "$*" >>"${FAKE_GO_LOG:?}"
	pwd >>"${FAKE_GO_PWD_LOG:?}"
	count_file="${FAKE_GO_COUNT_FILE:?}"
	n=0
	[[ -f "${count_file}" ]] && n="$(cat "${count_file}")"
	n=$((n + 1))
	printf '%s' "${n}" >"${count_file}"
	if [[ -n "${FAKE_GO_FAIL_PKG:-}" && "${2:-}" == "${FAKE_GO_FAIL_PKG}" ]]; then
		echo "fake go: simulated 502 for ${2}" >&2
		exit 1
	fi
	fail_until="${FAKE_GO_FAIL_UNTIL:-0}"
	if (( n <= fail_until )); then
		echo "fake go: simulated proxy 502, attempt ${n}" >&2
		exit 1
	fi
	exit 0
fi
exit 0
STUB
	chmod +x "${bin_dir}/go"
}

# run_case <case-dir> [env assignments...] -- <script args...>
# Installs a fake go under <case-dir>/bin, runs the target with delay 0 from
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
	install_fake_go "${dir}/bin"
	mkdir -p "${dir}/cwd"
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
case_a="${tmp_root}/case-a"
run_case "${case_a}" --
check "no arguments exits 2" "$([ "${rc}" -eq 2 ] && echo 0 || echo 1)"
check "no arguments never invokes go" "$([ "$(invocations "${case_a}")" -eq 0 ] && echo 0 || echo 1)"

# --- (b) an unversioned package is refused: inside a module it would resolve
#     through that go.mod instead of the pinned version. Refused before ANY
#     install runs, even when a valid package precedes it. ---
case_b="${tmp_root}/case-b"
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
	case_b2="${tmp_root}/case-b2-${RANDOM}"
	run_case "${case_b2}" -- "${bad}"
	check "malformed package \"${bad}\" exits 2" "$([ "${rc}" -eq 2 ] && echo 0 || echo 1)"
	check "malformed package \"${bad}\" never invokes go" "$([ "$(invocations "${case_b2}")" -eq 0 ] && echo 0 || echo 1)"
done

# --- (c) invalid attempts/delay knobs exit 2, as go-mod-download-retry.sh
#     does, instead of silently installing nothing. ---
for bad_attempts in 0 abc -1; do
	case_c="${tmp_root}/case-c-${RANDOM}"
	run_case "${case_c}" "ESHU_GO_DOWNLOAD_ATTEMPTS=${bad_attempts}" -- "${pkg_a}"
	check "ESHU_GO_DOWNLOAD_ATTEMPTS=\"${bad_attempts}\" exits 2" "$([ "${rc}" -eq 2 ] && echo 0 || echo 1)"
	check "ESHU_GO_DOWNLOAD_ATTEMPTS=\"${bad_attempts}\" never invokes go" "$([ "$(invocations "${case_c}")" -eq 0 ] && echo 0 || echo 1)"
done
for bad_delay in x -1 1.5; do
	case_c="${tmp_root}/case-c-${RANDOM}"
	run_case "${case_c}" "ESHU_GO_DOWNLOAD_RETRY_DELAY=${bad_delay}" -- "${pkg_a}"
	check "ESHU_GO_DOWNLOAD_RETRY_DELAY=\"${bad_delay}\" exits 2" "$([ "${rc}" -eq 2 ] && echo 0 || echo 1)"
	check "ESHU_GO_DOWNLOAD_RETRY_DELAY=\"${bad_delay}\" never invokes go" "$([ "$(invocations "${case_c}")" -eq 0 ] && echo 0 || echo 1)"
done

# --- (d) two simulated proxy failures then success still exits 0, installs
#     the exact versioned argument, and runs in the caller's directory. ---
case_d="${tmp_root}/case-d"
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
case_e="${tmp_root}/case-e"
run_case "${case_e}" FAKE_GO_FAIL_UNTIL=99 ESHU_GO_DOWNLOAD_ATTEMPTS=2 -- "${pkg_a}"
check "a package that never installs exits 1" "$([ "${rc}" -eq 1 ] && echo 0 || echo 1)"
check "gives up after exactly 2 attempts" "$([ "$(invocations "${case_e}")" -eq 2 ] && echo 0 || echo 1)"
if rg -q 'still failing after 2 attempt' "${case_e}/out.log" && rg -qF "${pkg_a}" "${case_e}/out.log"; then
	check "exhausted-retry message names attempts and package" 0
else
	check "exhausted-retry message names attempts and package" 1
fi

# --- (f) multiple packages: each installed once, in argument order. ---
case_f="${tmp_root}/case-f"
run_case "${case_f}" -- "${pkg_a}" "${pkg_b}"
check "multiple packages exit 0" "$([ "${rc}" -eq 0 ] && echo 0 || echo 1)"
if [ "$(cat "${case_f}/go.log")" = "$(printf 'install %s\ninstall %s' "${pkg_a}" "${pkg_b}")" ]; then
	check "multiple packages each installed once, in order" 0
else
	check "multiple packages each installed once, in order" 1
fi

# --- (g) a later package that never installs fails the run, with its own
#     full retry budget, and the message names that package. ---
case_g="${tmp_root}/case-g"
run_case "${case_g}" "FAKE_GO_FAIL_PKG=${pkg_b}" ESHU_GO_DOWNLOAD_ATTEMPTS=3 -- "${pkg_a}" "${pkg_b}"
check "failing second package exits 1" "$([ "${rc}" -eq 1 ] && echo 0 || echo 1)"
check "second package gets its own 3 attempts (1 + 3 invocations)" "$([ "$(invocations "${case_g}")" -eq 4 ] && echo 0 || echo 1)"
if rg -qF "${pkg_b}" "${case_g}/out.log" && rg -q 'still failing after 3 attempt' "${case_g}/out.log"; then
	check "failing second package message names it" 0
else
	check "failing second package message names it" 1
fi

# --- (h) CI wiring: no workflow runs a bare `go install` any more (each one
#     goes through this wrapper), and test.yml runs this mirror. ---
bare_installs="$(rg -n '^[^#]*\bgo install\b' "${repo_root}/.github/workflows" || true)"
if [ -z "${bare_installs}" ]; then
	check "no workflow runs a bare 'go install'" 0
else
	printf '%s\n' "${bare_installs}"
	check "no workflow runs a bare 'go install'" 1
fi
if rg -qF 'run: bash scripts/test-ci-go-install-retry.sh' "${repo_root}/.github/workflows/test.yml"; then
	check "test.yml runs this mirror" 0
else
	check "test.yml runs this mirror" 1
fi

echo
echo "test-ci-go-install-retry: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
