#!/usr/bin/env bash
#
# Executable regression guard for the gosec arms of scripts/dev/precommit-go.sh
# (gosec, gosec-all, gosec-changed; implemented in scripts/lib/precommit-go-gosec.sh).
#
# WHY THIS EXISTS (#8326, #8335)
#
# #8326: `make pre-push` did not run gosec, so a G301 surfaced only when the git
# pre-push hook ran, after the floor and the review receipt were done. The row
# `gosec-changed` now sits in the floor and runs the `gosec-changed` mode.
#
# #8335: every gosec arm used to fail OPEN. It sent gosec's output to /dev/null,
# ran with -no-fail, and read the SARIF with `jq ... || echo 0`, so gosec exiting
# 0 with no file, an empty file, or non-JSON counted as "0 findings". With the
# pinned gosec a package with a type error also exits 0, writes 0 results and
# logs "skipping SSA analysis", which read as clean. A gate that cannot say "I did
# not scan" must not report clean. R4-R9 are the regression tests for that.
#
# CASES (R = must fail, G = must pass)
#   R1 planted os.MkdirAll(dir, 0o755) in a changed package -> exit 1, names G301
#      and the file (real gosec)
#   G1 clean changed package -> exit 0 (real gosec)
#   G2 docs-only change -> exit 0, "no changed Go packages", gosec never invoked
#   R2 unresolvable base -> exit 2, message names the base, gosec never invoked
#   R3 each whole-module trigger file changed -> argv has ./..., message names it
#   R4 gosec killed by SIGTERM -> exit 143 and "gosec exited 143"
#   R5 gosec exits 0 and writes no file      -> exit 1, "no parsable result"
#   R6 gosec exits 0 and writes an empty file -> exit 1
#   R7 gosec exits 0 and writes `not json`   -> exit 1
#   R8 real gosec on a package with a type error -> exit 1, errors printed
#   R9 valid JSON with Stats.files 0 for a requested package -> exit 1
#
# Mechanics cases use a stub gosec that logs its argv, so a pass cannot be
# vacuous. Findings cases use the REAL pinned gosec from the shared tool cache
# (installed on demand). Everything runs in a throwaway git repo under $TMPDIR:
# precommit-go.sh derives every path from `git rev-parse --show-toplevel`, so the
# repo's own tool cache and worktree state are never touched.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/dev/precommit-go.sh"
[ -f "${script}" ] || { printf 'test-precommit-go-gosec: missing %s\n' "${script}" >&2; exit 1; }

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT
fx="${tmp_root}/fx"
stub_log="${tmp_root}/stub.log"
out_file="${tmp_root}/out.txt"
failures=0
gosec_version="v2.27.1"

fail() { printf 'test-precommit-go-gosec: FAIL: %s\n' "$*" >&2; failures=$((failures + 1)); }
pass() { printf 'test-precommit-go-gosec: ok: %s\n' "$*"; }

# real_gosec prints the path of the pinned gosec binary, installing it into the
# shared tool cache when absent (the same location precommit-go.sh uses).
real_gosec() {
	local common cache bin
	common="$(git -C "${repo_root}" rev-parse --git-common-dir)"
	case "${common}" in /*) ;; *) common="${repo_root}/${common}" ;; esac
	cache="${common}/eshu-precommit"
	bin="${cache}/gosec-${gosec_version}"
	if [ ! -x "${bin}" ]; then
		mkdir -p "${cache}"
		GOBIN="${cache}" GOFLAGS=-mod=mod env -u GOROOT go install \
			"github.com/securego/gosec/v2/cmd/gosec@${gosec_version}" >&2 \
			&& mv "${cache}/gosec" "${bin}"
	fi
	printf '%s' "${bin}"
}

write_stub() {
	local dest="$1"
	cat >"${dest}" <<'STUB_A'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${STUB_LOG}"
out=""; prev=""
for a in "$@"; do [ "${prev}" = -out ] && out="${a}"; prev="${a}"; done
j='{"Issues":[],"Stats":{"files":3},"Golang errors":{}}'
STUB_A
	cat >>"${dest}" <<'STUB_B'
case "${STUB_MODE:-ok}" in
	ok) printf '%s\n' "${j}" >"${out}" ;;
	empty) : >"${out}" ;;
	notjson) printf 'not json\n' >"${out}" ;;
	zero) printf '{"Issues":[],"Stats":{"files":0},"Golang errors":{}}\n' >"${out}" ;;
	term) kill -TERM $$ ;;
esac
exit 0
STUB_B
	chmod +x "${dest}"
}

# build_fixture creates the base repo: a clean Go module, the files the
# whole-module fallback watches, and refs/remotes/origin/main at that commit.
build_fixture() {
	local p
	mkdir -p "${fx}/go/internal/ok" "${fx}/.github/workflows" "${fx}/scripts/dev" \
		"${fx}/scripts/lib" "${fx}/scripts/ci" "${fx}/docs"
	git init -q -b main "${fx}"
	git -C "${fx}" config user.email t@example.invalid
	git -C "${fx}" config user.name t
	printf 'module fx\n\ngo 1.22\n' >"${fx}/go/go.mod"
	: >"${fx}/go/go.sum"
	printf 'package ok\n\n// Add adds two ints.\nfunc Add(a, b int) int { return a + b }\n' \
		>"${fx}/go/internal/ok/ok.go"
	printf 'run: scripts/ci/go-install-retry.sh github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2\n' \
		>"${fx}/.github/workflows/test.yml"
	printf 'run: scripts/ci/go-install-retry.sh github.com/securego/gosec/v2/cmd/gosec@%s\n' "${gosec_version}" \
		>"${fx}/.github/workflows/security-scan.yml"
	for p in scripts/dev/precommit-go.sh scripts/lib/precommit-go-gosec.sh \
		scripts/ci/go-install-retry.sh scripts/ci/go-mod-download-retry.sh; do
		printf '# fixture\n' >"${fx}/${p}"
	done
	printf '# docs\n' >"${fx}/docs/x.md"
	git -C "${fx}" add -A
	git -C "${fx}" commit -q -m base
	git -C "${fx}" update-ref refs/remotes/origin/main HEAD
	mkdir -p "${fx}/.git/eshu-precommit"
}

# topic starts a fresh branch off the fake origin/main with a clean tree.
topic() {
	git -C "${fx}" checkout -q -f -B topic refs/remotes/origin/main
	git -C "${fx}" clean -fdq
}

commit_all() {
	git -C "${fx}" add -A
	git -C "${fx}" commit -q -m change
}

use_stub() { write_stub "${fx}/.git/eshu-precommit/gosec-${gosec_version}"; }
use_real() { cp "$(real_gosec)" "${fx}/.git/eshu-precommit/gosec-${gosec_version}"; }

plant_g301() {
	mkdir -p "${fx}/go/internal/bad"
	printf 'package bad\n\nimport "os"\n\n// Mk makes dir.\nfunc Mk(d string) error { return os.MkdirAll(d, 0o755) }\n' \
		>"${fx}/go/internal/bad/bad.go"
}

plant_type_error() {
	mkdir -p "${fx}/go/internal/te"
	printf 'package te\n\n// F has a type error.\nfunc F() int { return "x" }\n' >"${fx}/go/internal/te/te.go"
}

touch_ok() { printf '\n// touched\n' >>"${fx}/go/internal/ok/ok.go"; }

# run_arm runs one precommit-go.sh subcommand inside the fixture and records the
# exit code in $rc and the combined output in $out_file. Extra env comes first.
rc=0
run_arm() {
	: >"${stub_log}"
	set +e
	( cd "${fx}" && env STUB_LOG="${stub_log}" "$@" ) >"${out_file}" 2>&1
	rc=$?
	set -e
}


# check <label> <want-rc> [fixed-string-in-output ...]
check() {
	local label="$1" want="$2" s
	shift 2
	if [ "${rc}" -ne "${want}" ]; then
		fail "${label}: exit ${rc}, want ${want} (output: $(tail -n 4 "${out_file}" | tr '\n' ' '))"
		return 0
	fi
	for s in "$@"; do
		if ! rg --quiet --fixed-strings -- "${s}" "${out_file}"; then
			fail "${label}: output lacks '${s}' (output: $(tail -n 4 "${out_file}" | tr '\n' ' '))"
			return 0
		fi
	done
	pass "${label}: exit ${rc}"
}

stub_calls() { awk 'END { print NR }' "${stub_log}"; }

expect_no_scan() {
	if [ "$(stub_calls)" -ne 0 ]; then
		fail "$1: gosec was invoked ($(stub_calls) call(s)) but nothing should be scanned"
	fi
}

# arm_input <arm> prints the extra args that arm needs for a changed ok.go.
arm_input() {
	case "$1" in gosec) printf 'go/internal/ok/ok.go' ;; *) ;; esac
}

build_fixture

# --- Case 0: harness self-check. The stub must be invoked with JSON output and
# the changed package, otherwise every later stub case could pass vacuously.
topic; touch_ok; commit_all; use_stub
run_arm STUB_MODE=ok bash "${script}" gosec-changed
check "self-check gosec-changed (stub ok)" 0
if ! rg --quiet --fixed-strings -- '-fmt=json' "${stub_log}" \
	|| ! rg --quiet --fixed-strings -- './internal/ok/...' "${stub_log}"; then
	fail "self-check: stub argv lacks -fmt=json or ./internal/ok/... ($(tr '\n' ';' <"${stub_log}"))"
fi

# --- R1 / G1: real gosec, all three arms.
use_real
for arm in gosec gosec-all gosec-changed; do
	topic; plant_g301; commit_all
	run_arm bash "${script}" "${arm}" $(if [ "${arm}" = gosec ]; then printf 'go/internal/bad/bad.go'; fi)
	check "R1 ${arm}: planted G301" 1 G301 bad.go
	topic; touch_ok; commit_all
	run_arm bash "${script}" "${arm}" $(arm_input "${arm}")
	check "G1 ${arm}: clean package" 0
done

# --- R8: real gosec, package with a type error, all three arms.
for arm in gosec gosec-all gosec-changed; do
	topic; plant_type_error; commit_all
	run_arm bash "${script}" "${arm}" $(if [ "${arm}" = gosec ]; then printf 'go/internal/te/te.go'; fi)
	check "R8 ${arm}: type error" 1 'cannot use "x"'
done

# --- G2: docs-only change scans nothing.
use_stub
topic; printf 'more\n' >>"${fx}/docs/x.md"; commit_all
run_arm STUB_MODE=ok bash "${script}" gosec-changed
check "G2 gosec-changed: docs only" 0 'no changed Go packages'
expect_no_scan "G2 gosec-changed"
run_arm STUB_MODE=ok bash "${script}" gosec
check "G2 gosec: no files" 0 'no changed Go packages'
expect_no_scan "G2 gosec"

# --- R2: unresolvable base.
topic; touch_ok; commit_all
run_arm STUB_MODE=ok ESHU_PRE_PUSH_BASE=refs/heads/nope bash "${script}" gosec-changed
check "R2 gosec-changed: bad base" 2 'refs/heads/nope'
expect_no_scan "R2 gosec-changed"

# --- R3: each whole-module trigger file widens the scan to ./...
for p in go/go.mod go/go.sum scripts/dev/precommit-go.sh scripts/lib/precommit-go-gosec.sh \
	scripts/ci/go-install-retry.sh scripts/ci/go-mod-download-retry.sh .github/workflows/security-scan.yml; do
	topic; printf '# changed\n' >>"${fx}/${p}"; commit_all
	run_arm STUB_MODE=ok bash "${script}" gosec-changed
	check "R3 gosec-changed: ${p}" 0 "whole-module scan, ${p} changed"
	if ! rg --quiet --fixed-strings -- './...' "${stub_log}"; then
		fail "R3 ${p}: gosec argv lacks ./... ($(tr '\n' ';' <"${stub_log}"))"
	fi
done

# --- Uncommitted work is scanned too (staged and unstaged).
topic; touch_ok
run_arm STUB_MODE=ok bash "${script}" gosec-changed
check "gosec-changed: unstaged change" 0
rg --quiet --fixed-strings -- './internal/ok/...' "${stub_log}" || fail "unstaged change not scanned"
git -C "${fx}" add -A
run_arm STUB_MODE=ok bash "${script}" gosec-changed
check "gosec-changed: staged change" 0
rg --quiet --fixed-strings -- './internal/ok/...' "${stub_log}" || fail "staged change not scanned"

# --- R4-R7, R9: broken gosec behaviour must fail closed on every arm.
for arm in gosec gosec-all gosec-changed; do
	topic; touch_ok; commit_all
	in="$(arm_input "${arm}")"
	run_arm STUB_MODE=term bash "${script}" "${arm}" ${in}
	check "R4 ${arm}: SIGTERM" 143 'gosec exited 143'
	run_arm STUB_MODE=nofile bash "${script}" "${arm}" ${in}
	check "R5 ${arm}: no result file" 1 'no parsable result'
	run_arm STUB_MODE=empty bash "${script}" "${arm}" ${in}
	check "R6 ${arm}: empty result file" 1 'no parsable result'
	run_arm STUB_MODE=notjson bash "${script}" "${arm}" ${in}
	check "R7 ${arm}: non-JSON result" 1 'no parsable result'
	run_arm STUB_MODE=zero bash "${script}" "${arm}" ${in}
	check "R9 ${arm}: zero files scanned" 1 'scanned 0 files'
done

if [ "${failures}" -ne 0 ]; then
	printf 'test-precommit-go-gosec: %d failure(s)\n' "${failures}" >&2
	exit 1
fi
printf 'test-precommit-go-gosec: all cases passed\n'
