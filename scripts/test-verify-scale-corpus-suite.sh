#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-scale-corpus-suite.sh"
spec="${repo_root}/specs/scale-lab-corpus.v1.yaml"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

expect_pass() {
	local label="$1"
	shift
	if ! "$@" >"${tmp_root}/${label}.out" 2>"${tmp_root}/${label}.err"; then
		printf 'expected %s to pass\n' "${label}" >&2
		sed -n '1,120p' "${tmp_root}/${label}.err" >&2
		exit 1
	fi
}

expect_fail_with() {
	local label="$1"
	local expected="$2"
	shift 2
	if "$@" >"${tmp_root}/${label}.out" 2>"${tmp_root}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	if ! rg --fixed-strings --quiet -- "${expected}" "${tmp_root}/${label}.err"; then
		printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2
		sed -n '1,120p' "${tmp_root}/${label}.err" >&2
		exit 1
	fi
}

expect_pass published_contract "${verifier}" --spec "${spec}"

missing_pathological="${tmp_root}/missing-pathological.yaml"
# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes the
# entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-scale-corpus-suite-missing-pathological.yaml" >"${missing_pathological}"
expect_fail_with missing_pathological \
	"missing required corpus slot: pathological/fanout_correlation" \
	"${verifier}" --spec "${missing_pathological}"

private_value="${tmp_root}/private-value.yaml"
cp "${spec}" "${private_value}"
printf '\nprivate_example: "%s://private.example.invalid/resource"\n' "https" >>"${private_value}"
expect_fail_with private_value "spec looks like private data" \
	"${verifier}" --spec "${private_value}"

printf 'verify-scale-corpus-suite tests passed\n'
