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
#   R3 each whole-module trigger file changed -> message names it, no ./... in argv
#   R4 gosec killed by SIGTERM -> exit 143 and "gosec exited 143"
#   R5 gosec exits 0 and writes no file      -> exit 1, "no parsable result"
#   R6 gosec exits 0 and writes an empty file -> exit 1
#   R7 gosec exits 0 and writes `not json`   -> exit 1
#   R8 real gosec on a package with a type error -> exit 1, errors printed
#   R9 valid JSON with Stats.files 0 for a requested package -> exit 1
#   A-control the pinned gosec loads a 301-file package by directory: 0 Go errors
#      and Stats.files equal to the `go list` count (#8339, #11241)
#   A1-A5 a package whose per-file pattern list once exceeded the go/packages
#      16,383-byte limit (#8339), real gosec on the directory-loading pin
#      (#11241): A1 a planted G115 in the last file is reported with the REAL
#      path; A2 clean -> exit 0, all files scanned, no alias; A3 a small package
#      -> clean; A4 a package over the limit EVEN via the retired alias: clean ->
#      exit 0 with every file scanned, and a G115 planted in its last file is
#      reported at its real path (the seeded case); A5 whole-module mode never
#      passes ./... to gosec
#   E1-E2 the gosec version extractor in precommit-go.sh reads a pseudo-version
#      pin (v2.30.1-0.<date>-<sha>) whole, and a plain tag as before
#   C1 no go.mod -> "must run from the module root"; C2 Stats.files below the
#      expected count -> exit 1; C3 an absolute pattern -> exit 1
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
# #8673: keep fixture repos out of the host git-ai daemon's Trace2 tracking.
# The daemon keys on its Trace2 socket env; any git process that emits to it
# teaches the daemon the fixture path (fixtures and their gate runs inherit
# this env). GIT_TRACE2_EVENT=0 disables the event stream (same convention as
# test-verify-moved-file-refs.sh); PERF likewise. Teardown retries stay as
# belt-and-braces.
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0
trap 'rm -rf "${tmp_root}"' EXIT
fx="${tmp_root}/fx"
stub_log="${tmp_root}/stub.log"
out_file="${tmp_root}/out.txt"
failures=0
gosec_version="v2.30.1-0.20261026162949-ff25931a2edd"

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
n=0
for a in "$@"; do case "${a}" in ./*) c=$(ls "${a}"*.go 2>/dev/null | wc -l); n=$((n + c)) ;; esac; done
j="{\"Issues\":[],\"Stats\":{\"files\":${n}},\"Golang errors\":{}}"
STUB_A
	cat >>"${dest}" <<'STUB_B'
case "${STUB_MODE:-ok}" in
	ok) printf '%s\n' "${j}" >"${out}" ;;
	empty) : >"${out}" ;;
	notjson) printf 'not json\n' >"${out}" ;;
	zero) printf '{"Issues":[],"Stats":{"files":0},"Golang errors":{}}\n' >"${out}" ;;
	term) kill -TERM $$ ;;
	file) cp "${STUB_JSON}" "${out}" ;;
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
	printf 'package ok\n\n// Sub subtracts two ints.\nfunc Sub(a, b int) int { return a - b }\n' \
		>"${fx}/go/internal/ok/ok2.go"
	printf 'run: scripts/ci/go-install-retry.sh github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2\n' \
		>"${fx}/.github/workflows/test.yml"
	printf 'run: scripts/ci/go-install-retry.sh github.com/securego/gosec/v2/cmd/gosec@%s\n' "${gosec_version}" \
		>"${fx}/.github/workflows/security-scan.yml"
	for p in scripts/dev/precommit-go.sh scripts/lib/precommit-go-gosec.sh \
		scripts/lib/gosec-json-to-sarif.py \
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

check_absent() { # <label> <fixed string that must NOT be in the output>
	if rg --quiet --fixed-strings -- "$2" "${out_file}"; then
		fail "$1: output unexpectedly contains '$2'"
	fi
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
	|| ! rg --quiet --fixed-strings -- './internal/ok/' "${stub_log}"; then
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

# --- R3: each whole-module trigger file widens the scan to every package, as explicit dirs
for p in go/go.mod go/go.sum scripts/dev/precommit-go.sh scripts/lib/precommit-go-gosec.sh \
	scripts/lib/gosec-json-to-sarif.py \
	scripts/ci/go-install-retry.sh scripts/ci/go-mod-download-retry.sh .github/workflows/security-scan.yml; do
	topic; if [ "${p}" = go/go.mod ]; then printf '// changed\n' >>"${fx}/${p}"; else printf '# changed\n' >>"${fx}/${p}"; fi; commit_all
	run_arm STUB_MODE=ok bash "${script}" gosec-changed
	check "R3 gosec-changed: ${p}" 0 "whole-module scan, ${p} changed"
	if rg --quiet --fixed-strings -- './...' "${stub_log}" || ! rg --quiet --fixed-strings -- './internal/ok/' "${stub_log}"; then
		fail "R3 ${p}: gosec argv must list explicit package dirs, never ./... ($(tr '\n' ';' <"${stub_log}"))"
	fi
done

# --- Uncommitted work is scanned too (staged and unstaged).
topic; touch_ok
run_arm STUB_MODE=ok bash "${script}" gosec-changed
check "gosec-changed: unstaged change" 0
rg --quiet --fixed-strings -- './internal/ok/' "${stub_log}" || fail "unstaged change not scanned"
git -C "${fx}" add -A
run_arm STUB_MODE=ok bash "${script}" gosec-changed
check "gosec-changed: staged change" 0
rg --quiet --fixed-strings -- './internal/ok/' "${stub_log}" || fail "staged change not scanned"

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
	check "R9 ${arm}: zero files scanned" 1 'scanned 0 of'
done

# --- A-control, A1-A5: a package whose per-file pattern list exceeds the
# go/packages limit (#8339). gosec v2.27.1 passed one pattern per file, so
# x/tools split the list at 16383 bytes into several `go list` runs and kept only
# the first run's imports: gosec type-checked the package with missing imports and
# skipped its SSA analyzers (G115, G407, G602...). The pinned gosec loads module
# packages by directory (securego/gosec#1803, #1806), so there is no per-file
# budget (#11241). The last file imports a package no other file does and plants
# a G115 conversion, so a loader that drops imports cannot see it.
big_pkg() { # <dir> <count> <name-length>
	local dir="$1" n="$2" len="$3" i name pad
	mkdir -p "${fx}/go/internal/${dir}"
	pad="$(printf '%*s' "${len}" '' | tr ' ' 'x')"
	i=0
	while [ "${i}" -lt "${n}" ]; do
		name="$(printf 'f%04d_%s' "${i}" "${pad}")"
		printf 'package %s\n\n// F%d returns %d.\nfunc F%d() int { return %d }\n' "${dir}" "${i}" "${i}" "${i}" "${i}" \
			>"${fx}/go/internal/${dir}/${name}.go"
		i=$((i + 1))
	done
	printf 'package %s\n\nimport "text/tabwriter"\n\n// Align is a value only this file needs.\nvar Align = tabwriter.AlignRight\n' "${dir}" \
		>"${fx}/go/internal/${dir}/zzlast.go"
}
plant_g115() { # <dir>
	printf '\n// Narrow converts an int to int8.\nfunc Narrow(v int) int8 { return int8(v) }\n' >>"${fx}/go/internal/$1/zzlast.go"
}
aliases_left() { ls -d "${fx}"/go/_g* 2>/dev/null | wc -l | tr -d ' '; }

use_real
topic; big_pkg bigpackage 300 30; commit_all
# Control: the pinned gosec called directly scans every file of this package with
# no Go errors, so A1/A2 prove the gate and not a fixture the loader cannot read.
( cd "${fx}/go" && "${fx}/.git/eshu-precommit/gosec-${gosec_version}" -no-fail -fmt=json -out "${tmp_root}/ctl.json" ./internal/bigpackage/ >/dev/null 2>&1 )
ctl_errors="$(jq -r '."Golang errors" | length' "${tmp_root}/ctl.json")"
ctl_files="$(jq -r '.Stats.files' "${tmp_root}/ctl.json")"
ctl_listed="$(cd "${fx}/go" && go list -f '{{len .GoFiles}}' ./internal/bigpackage/)"
if [ "${ctl_errors}" -eq 0 ] && [ "${ctl_files}" -eq "${ctl_listed}" ] && [ "${ctl_files}" -eq 301 ]; then
	pass "A-control: direct gosec loads the 301-file package by directory (0 Go errors, Stats.files ${ctl_files} = go list ${ctl_listed})"
else
	fail "A-control: direct gosec reported ${ctl_errors} Go error(s) and ${ctl_files} file(s); go list reports ${ctl_listed} (want 0 errors, 301 files)"
fi
for arm in gosec gosec-changed; do
	topic; big_pkg bigpackage 300 30; plant_g115 bigpackage; commit_all
	run_arm bash "${script}" "${arm}" $(if [ "${arm}" = gosec ]; then printf 'go/internal/bigpackage/zzlast.go'; fi)
	check "A1 ${arm}: G115 in the last file" 1 G115 'internal/bigpackage/zzlast.go'
	check_absent "A1 ${arm}" 'alias'
	check_absent "A1 ${arm}" '/_g'
	topic; big_pkg bigpackage 300 30; commit_all
	run_arm bash "${script}" "${arm}" $(if [ "${arm}" = gosec ]; then printf 'go/internal/bigpackage/zzlast.go'; fi)
	check "A2 ${arm}: clean oversized package" 0 '301 file(s) scanned'
	check_absent "A2 ${arm}" 'alias'
	[ "$(aliases_left)" -eq 0 ] || fail "A2 ${arm}: alias left behind: $(ls -d "${fx}"/go/_g*)"
done
topic; big_pkg small 20 10; commit_all
run_arm bash "${script}" gosec-changed
check "A3 gosec-changed: small package" 0 '21 file(s) scanned'
check_absent "A3" 'alias'
# A4: 401 files of 45-character names: its pattern list is over the limit even
# through the retired short alias (the case the old gate refused with "even via
# alias"). Clean -> scanned in full; seeded G115 in the last file -> reported.
topic; big_pkg hugepackage 400 45; commit_all
run_arm bash "${script}" gosec-changed
check "A4 gosec-changed: clean package over the limit even via alias" 0 '401 file(s) scanned'
check_absent "A4 clean" 'even via alias'
check_absent "A4 clean" 'alias'
[ "$(aliases_left)" -eq 0 ] || fail "A4: alias left behind: $(ls -d "${fx}"/go/_g*)"
topic; big_pkg hugepackage 400 45; plant_g115 hugepackage; commit_all
run_arm bash "${script}" gosec-changed
check "A4 gosec-changed: seeded G115 in the last file of a package over the limit" 1 G115 'internal/hugepackage/zzlast.go'
check_absent "A4 seeded" 'even via alias'
check_absent "A4 seeded" 'Go errors'
use_stub
topic; big_pkg bigpackage 300 30; commit_all
run_arm STUB_MODE=ok bash "${script}" gosec-all
check "A5 gosec-all: oversized package, stub" 0 '303 file(s) scanned'
if rg --quiet --fixed-strings -- './...' "${stub_log}"; then fail "A5: gosec-all passed ./... to gosec"; fi
rg --quiet --fixed-strings -- './internal/ok/' "${stub_log}" || fail "A5: gosec-all argv lacks ./internal/ok/"
rg --quiet --fixed-strings -- './internal/bigpackage/' "${stub_log}" || fail "A5: gosec-all argv lacks ./internal/bigpackage/"

# --- E1-E2: the version extractor in precommit-go.sh keeps a whole pseudo-version
# (it used to cut at the first hyphen: v2.30.1-0.<date>-<sha> read as v2.30.1, a
# tag that does not exist, so the gate tried to install the wrong binary).
topic; touch_ok; commit_all
rm -f "${fx}/.git/eshu-precommit/gosec-"*
write_stub "${fx}/.git/eshu-precommit/gosec-${gosec_version}"
run_arm STUB_MODE=ok bash "${script}" gosec-changed
check "E1 gosec-changed: pseudo-version pin read whole" 0 'file(s) scanned'
check_absent "E1" 'installing gosec'
rg --quiet --fixed-strings -- './internal/ok/' "${stub_log}" || fail "E1: the stub pinned by the pseudo-version was not invoked"
printf 'run: scripts/ci/go-install-retry.sh github.com/securego/gosec/v2/cmd/gosec@v2.27.1 # plain tag\n' \
	>"${fx}/.github/workflows/security-scan.yml"
rm -f "${fx}/.git/eshu-precommit/gosec-"*
write_stub "${fx}/.git/eshu-precommit/gosec-v2.27.1"
git -C "${fx}" add -A; git -C "${fx}" commit -q -m plain-pin
run_arm STUB_MODE=ok bash "${script}" gosec-changed
check "E2 gosec-changed: plain tag pin still read" 0 'file(s) scanned'
check_absent "E2" 'installing gosec'
rm -f "${fx}/.git/eshu-precommit/gosec-v2.27.1"
use_stub

json_file="${tmp_root}/stub.json"
# --- C1-C3: the run is anchored to the module root and checks what it scanned.
topic; touch_ok; commit_all
mv "${fx}/go/go.mod" "${fx}/go/go.mod.off"
run_arm STUB_MODE=ok bash "${script}" gosec-all
check "C1 gosec-all: no go.mod" 1 'must run from the module root'
mv "${fx}/go/go.mod.off" "${fx}/go/go.mod"
printf '{"Issues":[],"Stats":{"files":1},"Golang errors":{}}\n' >"${json_file}"
run_arm STUB_MODE=file STUB_JSON="${json_file}" bash "${script}" gosec-changed
check "C2 gosec-changed: fewer files than expected" 1 'scanned 1 of 2 expected files'
printf '{"Issues":[],"Stats":{"files":0},"Golang errors":{}}\n' >"${json_file}"
run_arm STUB_MODE=file STUB_JSON="${json_file}" bash "${script}" gosec-changed
check "C2b gosec-changed: zero files" 1 'scanned 0 of 2 expected files'
real_lib="${repo_root}/scripts/lib/precommit-go-gosec.sh"
c3_out="${tmp_root}/c3.out"
c3_rc=0
( repo_root="${fx}"; go_dir="${fx}/go"; worktree_cache_dir="${tmp_root}/wc"; mkdir -p "${worktree_cache_dir}"
  ensure_gosec() { printf '%s' "${fx}/.git/eshu-precommit/gosec-${gosec_version}"; }
  die() { printf '%s\n' "$*" >&2; exit 1; }
  # shellcheck source=lib/precommit-go-gosec.sh
  source "${real_lib}"
  run_gosec c3 "${fx}/go/internal/ok/" ) >"${c3_out}" 2>&1 || c3_rc=$?
if [ "${c3_rc}" -ne 1 ] || ! rg --quiet --fixed-strings -- 'absolute pattern' "${c3_out}"; then
	fail "C3: absolute pattern: exit ${c3_rc}, output: $(tail -n 3 "${c3_out}" | tr '\n' ' ')"
else
	pass "C3: absolute pattern refused, exit 1"
fi

# --- S1-S3: GOSEC_SARIF_OUT emits valid SARIF 2.1.0 on clean & finding scans,
# and writes nothing when gosec fails closed on short/invalid results.
topic; touch_ok; commit_all
sarif_clean="${tmp_root}/clean.sarif"
run_arm STUB_MODE=ok GOSEC_SARIF_OUT="${sarif_clean}" bash "${script}" gosec-all
check "S1 gosec-all with GOSEC_SARIF_OUT clean" 0 'file(s) scanned'
if [ -s "${sarif_clean}" ] && jq -e '.version == "2.1.0" and (.runs[0].results | length) == 0' "${sarif_clean}" >/dev/null 2>&1; then
	pass "S1: clean SARIF 2.1.0 written with 0 results"
else
	fail "S1: clean SARIF missing or invalid (${sarif_clean})"
fi
sarif_err="${tmp_root}/err.sarif"
printf '{"stale":true}\n' >"${sarif_err}"
run_arm STUB_MODE=zero GOSEC_SARIF_OUT="${sarif_err}" bash "${script}" gosec-all
check "S2 gosec-all with GOSEC_SARIF_OUT short scan fails closed" 1 'scanned 0 of'
if [ -e "${sarif_err}" ]; then fail "S2: stale SARIF survived failed short scan"; else pass "S2: stale SARIF removed on failed short scan"; fi
use_real
topic; plant_g301; commit_all
run_arm GOSEC_SARIF_OUT="go/gosec.sarif" bash "${script}" gosec-all
check "S3 gosec-all with relative GOSEC_SARIF_OUT planted G301 exits 1" 1 'G301'
if [ -s "${fx}/go/gosec.sarif" ] && jq -e '.runs[0].results | any(.ruleId == "G301" and .locations[0].physicalLocation.artifactLocation.uri == "go/internal/bad/bad.go")' "${fx}/go/gosec.sarif" >/dev/null 2>&1; then
	pass "S3: relative SARIF 2.1.0 written with repo-relative URI before exit 1"
else
	fail "S3: finding SARIF missing G301 or repo-relative URI (${fx}/go/gosec.sarif)"
fi
if python3 "${repo_root}/scripts/test-gosec-json-to-sarif.py" >/dev/null 2>&1; then
	pass "S4: gosec-json-to-sarif unit suite passed"
else
	fail "S4: gosec-json-to-sarif unit suite failed"
fi

if [ "${failures}" -ne 0 ]; then
	printf 'test-precommit-go-gosec: %d failure(s)\n' "${failures}" >&2
	exit 1
fi
printf 'test-precommit-go-gosec: all cases passed\n'
