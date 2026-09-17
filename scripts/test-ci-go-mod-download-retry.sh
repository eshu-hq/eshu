#!/usr/bin/env bash
# test-ci-go-mod-download-retry.sh - hermetic checks for
# scripts/ci/go-mod-download-retry.sh.
#
# #6615: the script hardcoded a `cd .../go`, so it could only ever warm the
# app module's cache. This repo also builds sdk/go/collector, sdk/go/
# factschema, and examples/collector-extensions/scorecard directly in CI
# (each its own go.mod / dependency graph), so a job compiling one of those
# got zero benefit from go-race's existing pre-warm step. The fix adds an
# optional module-dir argument (default "go", so every pre-#6615 call site
# keeps working unchanged).
#
# Runs entirely off the network: a fake `go` on PATH stands in for the real
# one, logs which directory it was invoked from, and simulates failure/
# success by attempt count via env vars the fake script reads. No real
# proxy.golang.org call happens in this test.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
target="${repo_root}/scripts/ci/go-mod-download-retry.sh"
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

if [ ! -x "${target}" ]; then
	echo "test-ci-go-mod-download-retry: missing executable script at ${target}" >&2
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

# install_fake_go writes a `go` stub to $1/bin that only understands `mod
# download`: it logs its cwd to FAKE_GO_PWD_LOG (once per invocation, so the
# last line is the final attempt's cwd), logs an invocation count to
# FAKE_GO_COUNT_FILE, and fails every attempt at or below
# FAKE_GO_FAIL_UNTIL (default 0, meaning "never fail").
install_fake_go() {
	local bin_dir="$1"
	mkdir -p "${bin_dir}"
	cat >"${bin_dir}/go" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "mod" && "${2:-}" == "download" ]]; then
	pwd >>"${FAKE_GO_PWD_LOG:?}"
	count_file="${FAKE_GO_COUNT_FILE:?}"
	n=0
	[[ -f "${count_file}" ]] && n="$(cat "${count_file}")"
	n=$((n + 1))
	printf '%s' "${n}" >"${count_file}"
	fail_until="${FAKE_GO_FAIL_UNTIL:-0}"
	if (( n <= fail_until )); then
		echo "fake go: simulated mod-download failure, attempt ${n}" >&2
		exit 1
	fi
	exit 0
fi
exit 0
STUB
	chmod +x "${bin_dir}/go"
}

# --- (a) default module-dir resolves to go/, unchanged from every existing
#     call site (test.yml go-race, bench.yml, ifa-determinism-gate.yml). ---
case_a="${tmp_root}/case-a"
install_fake_go "${case_a}/bin"
pwd_log="${case_a}/pwd.log"
count_file="${case_a}/count"
rc=0
PATH="${case_a}/bin:${PATH}" \
	FAKE_GO_PWD_LOG="${pwd_log}" \
	FAKE_GO_COUNT_FILE="${count_file}" \
	ESHU_GO_DOWNLOAD_RETRY_DELAY=0 \
	"${target}" >"${case_a}/out.log" 2>&1 || rc=$?
check "default module-dir exits 0" "$([ "${rc}" -eq 0 ] && echo 0 || echo 1)"
if [ -f "${pwd_log}" ] && [ "$(cat "${pwd_log}")" = "${repo_root}/go" ]; then
	check "default module-dir warms go/ (no argument, back-compat)" 0
else
	check "default module-dir warms go/ (no argument, back-compat)" 1
fi

# --- (b) an explicit module-dir warms a DIFFERENT module's cache, proving
#     the sdk/go/collector, sdk/go/factschema, and scorecard example gaps
#     this fix closes. ---
case_b="${tmp_root}/case-b"
install_fake_go "${case_b}/bin"
pwd_log_b="${case_b}/pwd.log"
count_file_b="${case_b}/count"
rc=0
PATH="${case_b}/bin:${PATH}" \
	FAKE_GO_PWD_LOG="${pwd_log_b}" \
	FAKE_GO_COUNT_FILE="${count_file_b}" \
	ESHU_GO_DOWNLOAD_RETRY_DELAY=0 \
	"${target}" "sdk/go/collector" >"${case_b}/out.log" 2>&1 || rc=$?
check "explicit module-dir exits 0" "$([ "${rc}" -eq 0 ] && echo 0 || echo 1)"
if [ -f "${pwd_log_b}" ] && [ "$(cat "${pwd_log_b}")" = "${repo_root}/sdk/go/collector" ]; then
	check "explicit module-dir warms sdk/go/collector, not go/" 0
else
	check "explicit module-dir warms sdk/go/collector, not go/" 1
fi

# --- (c) a module-dir with no go.mod fails fast and clearly, without ever
#     invoking go -- the same "fail loud, not silently" bar the attempts/
#     delay validation already holds itself to. ---
case_c="${tmp_root}/case-c"
install_fake_go "${case_c}/bin"
count_file_c="${case_c}/count"
rc=0
PATH="${case_c}/bin:${PATH}" \
	FAKE_GO_PWD_LOG="${case_c}/pwd.log" \
	FAKE_GO_COUNT_FILE="${count_file_c}" \
	ESHU_GO_DOWNLOAD_RETRY_DELAY=0 \
	"${target}" "does/not/exist" >"${case_c}/out.log" 2>&1 || rc=$?
check "missing go.mod at module-dir exits nonzero" "$([ "${rc}" -ne 0 ] && echo 0 || echo 1)"
if rg -q 'no go.mod at' "${case_c}/out.log"; then
	check "missing go.mod at module-dir names the path" 0
else
	check "missing go.mod at module-dir names the path" 1
fi
if [ ! -f "${count_file_c}" ]; then
	check "missing go.mod at module-dir never invokes go" 0
else
	check "missing go.mod at module-dir never invokes go" 1
fi

# --- (d) retries: two simulated proxy drops then a success still exits 0. ---
case_d="${tmp_root}/case-d"
install_fake_go "${case_d}/bin"
count_file_d="${case_d}/count"
rc=0
PATH="${case_d}/bin:${PATH}" \
	FAKE_GO_PWD_LOG="${case_d}/pwd.log" \
	FAKE_GO_COUNT_FILE="${count_file_d}" \
	FAKE_GO_FAIL_UNTIL=2 \
	ESHU_GO_DOWNLOAD_ATTEMPTS=3 \
	ESHU_GO_DOWNLOAD_RETRY_DELAY=0 \
	"${target}" >"${case_d}/out.log" 2>&1 || rc=$?
check "two transient failures then success still exits 0" "$([ "${rc}" -eq 0 ] && echo 0 || echo 1)"
if rg -q 'succeeded on attempt 3/3' "${case_d}/out.log"; then
	check "reports which attempt succeeded" 0
else
	check "reports which attempt succeeded" 1
fi

# --- (e) exhausting every attempt fails loud, naming the module. ---
case_e="${tmp_root}/case-e"
install_fake_go "${case_e}/bin"
count_file_e="${case_e}/count"
rc=0
PATH="${case_e}/bin:${PATH}" \
	FAKE_GO_PWD_LOG="${case_e}/pwd.log" \
	FAKE_GO_COUNT_FILE="${count_file_e}" \
	FAKE_GO_FAIL_UNTIL=99 \
	ESHU_GO_DOWNLOAD_ATTEMPTS=2 \
	ESHU_GO_DOWNLOAD_RETRY_DELAY=0 \
	"${target}" "sdk/go/factschema" >"${case_e}/out.log" 2>&1 || rc=$?
check "a module that never resolves fails, not silently" "$([ "${rc}" -ne 0 ] && echo 0 || echo 1)"
if rg -q 'still failing after 2 attempt' "${case_e}/out.log" && rg -q 'sdk/go/factschema' "${case_e}/out.log"; then
	check "exhausted-retry message names attempts and module" 0
else
	check "exhausted-retry message names attempts and module" 1
fi

echo
echo "test-ci-go-mod-download-retry: ${pass} passed, ${fail} failed"
[ "${fail}" -eq 0 ]
