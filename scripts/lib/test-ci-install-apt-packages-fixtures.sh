#!/usr/bin/env bash
#
# test-ci-install-apt-packages-fixtures.sh -- fixture and assertion helpers
# for scripts/test-ci-install-apt-packages.sh. Split out to keep that mirror
# under the repo's 500-line file cap (the same split shape as
# scripts/lib/golden-corpus-fixtures.sh and
# scripts/lib/telemetry-coverage-row-check.sh).
#
# Sourced by scripts/test-ci-install-apt-packages.sh; not meant to run
# standalone. Expects `set -euo pipefail` to already be active and provides:
# tmp_root (plus its EXIT trap), the PASS/FAIL counters, record_pass,
# record_fail, sha256_of, build_ripgrep_fixture, eight generic assertion
# wrappers (assert_exit_zero, assert_exit_nonzero, assert_path_absent,
# assert_path_present, assert_output_has, assert_output_lacks,
# assert_output_has_all, assert_output_lacks_any) that collapse the case
# functions' most repeated if/record_pass/else/record_fail shapes to one
# call, and two source-line value parsers (curl_flag_value,
# script_var_default) the transfer-bound case uses to harden its curl-flag
# assertion past mere presence. Each assertion wrapper takes the
# already-computed condition input (an exit status already captured via
# `cmd || rc=$?`, a path, or an output file) plus the caller's exact pass and
# fail message text -- it changes no assertion, only how many lines it takes
# to state one. All consumed by the case functions the caller defines after
# sourcing this file.
#
# Registered as a trigger of the ci-install-apt-packages gate in
# specs/ci-gates.v1.yaml alongside the mirror itself, so a change confined to
# this file still re-runs the mirror that exercises it.

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0

record_pass() {
	PASS=$((PASS + 1))
	printf 'ok - %s\n' "$1"
}

record_fail() {
	FAIL=$((FAIL + 1))
	printf 'not ok - %s\n' "$1" >&2
}

# sha256_of prints the sha256 of $1 as a bare lowercase hex digest, using
# whichever of sha256sum/shasum is on PATH -- the same detection order as
# scripts/run-two-team-governance-proof.sh.
sha256_of() {
	local file="$1"
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "${file}" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "${file}" | awk '{print $1}'
	else
		echo "test-ci-install-apt-packages: neither sha256sum nor shasum is available" >&2
		exit 1
	fi
}

# build_ripgrep_fixture writes a fake ripgrep release tarball at
# "${1}/ripgrep.tar.gz" containing a single "<versioned-dir>/rg" member with
# placeholder bytes (never executed -- the pinned binary targets
# linux-x86_64-musl and these tests run on the macOS/arm64 dev machine, so the
# extracted rg is only ever verified by presence and content, not run). Prints
# the fixture's sha256 to stdout.
build_ripgrep_fixture() {
	local dir="$1"
	local stage="${dir}/stage/ripgrep-14.1.1-x86_64-unknown-linux-musl"
	mkdir -p "${stage}"
	printf 'scratch fixture rg binary -- not a real executable\n' >"${stage}/rg"
	local tarball="${dir}/ripgrep.tar.gz"
	tar -czf "${tarball}" -C "${dir}/stage" "ripgrep-14.1.1-x86_64-unknown-linux-musl"
	sha256_of "${tarball}"
}

# assert_exit_zero records pass/fail for an exit status already captured in
# $1 (via `cmd || rc=$?`) -- pass iff it is exactly 0. $3 is used verbatim as
# the fail message, so callers append any $(cat "${out}") themselves.
assert_exit_zero() {
	local rc="$1" pass_msg="$2" fail_msg="$3"
	if [ "${rc}" -eq 0 ]; then
		record_pass "${pass_msg}"
	else
		record_fail "${fail_msg}"
	fi
}

# assert_exit_nonzero is assert_exit_zero's mirror for cases that must fail.
assert_exit_nonzero() {
	local rc="$1" pass_msg="$2" fail_msg="$3"
	if [ "${rc}" -ne 0 ]; then
		record_pass "${pass_msg}"
	else
		record_fail "${fail_msg}"
	fi
}

# assert_path_absent records pass/fail on whether path $1 does NOT exist.
assert_path_absent() {
	local path="$1" pass_msg="$2" fail_msg="$3"
	if [ ! -e "${path}" ]; then
		record_pass "${pass_msg}"
	else
		record_fail "${fail_msg}"
	fi
}

# assert_path_present is assert_path_absent's mirror for cases that must
# create the path.
assert_path_present() {
	local path="$1" pass_msg="$2" fail_msg="$3"
	if [ -e "${path}" ]; then
		record_pass "${pass_msg}"
	else
		record_fail "${fail_msg}"
	fi
}

# assert_output_has records pass/fail on whether the fixed string $2 is
# present in file $1.
assert_output_has() {
	local out="$1" needle="$2" pass_msg="$3" fail_msg="$4"
	if rg -q --fixed-strings "${needle}" "${out}"; then
		record_pass "${pass_msg}"
	else
		record_fail "${fail_msg}"
	fi
}

# assert_output_lacks is assert_output_has's mirror for cases that must NOT
# print the fixed string $2.
assert_output_lacks() {
	local out="$1" needle="$2" pass_msg="$3" fail_msg="$4"
	if rg -q --fixed-strings "${needle}" "${out}"; then
		record_fail "${fail_msg}"
	else
		record_pass "${pass_msg}"
	fi
}

# assert_output_has_all records pass/fail on whether BOTH fixed strings $2
# and $3 are present in file $1.
assert_output_has_all() {
	local out="$1" needle1="$2" needle2="$3" pass_msg="$4" fail_msg="$5"
	if rg -q --fixed-strings "${needle1}" "${out}" &&
		rg -q --fixed-strings "${needle2}" "${out}"; then
		record_pass "${pass_msg}"
	else
		record_fail "${fail_msg}"
	fi
}

# assert_output_lacks_any records pass/fail on whether NEITHER fixed string
# $2 nor $3 is present in file $1.
assert_output_lacks_any() {
	local out="$1" needle1="$2" needle2="$3" pass_msg="$4" fail_msg="$5"
	if rg -q --fixed-strings "${needle1}" "${out}" ||
		rg -q --fixed-strings "${needle2}" "${out}"; then
		record_fail "${fail_msg}"
	else
		record_pass "${pass_msg}"
	fi
}

# curl_flag_value prints curl flag $2's bare integer argument found on source
# line $1 (e.g. "--speed-limit 1024"), or 0 if the flag is absent or its
# argument is not a literal integer (a variable reference like "${max_time}"
# does not match, by design -- see script_var_default for that case).
curl_flag_value() {
	local line="$1" flag="$2"
	if [[ "${line}" =~ ${flag}[[:space:]]+([0-9]+) ]]; then
		printf '%s\n' "${BASH_REMATCH[1]}"
	else
		printf '0\n'
	fi
}

# script_var_default prints the fallback integer in a "${VAR:-N}" default
# expansion on the first line of file $1 matching fixed string $2, or 0 if no
# line matches or it carries no such default. Used to resolve a curl flag
# whose source-level value is a variable reference, not a literal.
script_var_default() {
	local file="$1" needle="$2" line
	line="$(rg -m1 -- "${needle}" "${file}")"
	if [[ "${line}" =~ :-([0-9]+)\} ]]; then
		printf '%s\n' "${BASH_REMATCH[1]}"
	else
		printf '0\n'
	fi
}
