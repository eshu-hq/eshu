#!/usr/bin/env bash
# test-verify-doc-import-pins.sh - hermetic tests for
# scripts/verify-doc-import-pins.sh (#10205 P2: the arm64 pin file gets no
# build proof on PRs, so a static well-formedness guard must fail fast on a
# corrupted pin file). No network, no Docker: fixtures are tmp copies of the
# real pin files with planted corruption.
#
# Seeded RED/GREEN pair:
#   GREEN the real specs/doc-import-pins/ directory: exit 0.
#   RED   a one-character hex typo, a dropped line, or a garbage line in
#         either arch file: exit 1 naming the file.
# Fail-closed: a missing arch file, an empty pin directory, or an unreadable
# file exits 2, never 0.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-doc-import-pins.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT
export GIT_TRACE2_EVENT=0 GIT_TRACE2_PERF=0

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

# GREEN: the real pin directory passes.
if "${verifier}" >/dev/null 2>&1; then
  record_pass 'real pin directory passes'
else
  record_fail 'real pin directory passes'
fi

# Fixture: a scratch copy of the real pins to corrupt.
setup_fixture() {
  local dir="$1"
  mkdir -p "${dir}"
  cp "${repo_root}"/specs/doc-import-pins/*.sha256 "${dir}/"
}

# RED: a one-character hex typo in the arm64 file fails naming the file.
arm_dir="${tmp_root}/typo"
setup_fixture "${arm_dir}"
sed -i 's/^38b441bd41257037a03d6a15d9f849d8bd83da4c24dab1df428a7a4ad265332f/38b441bd41257037a03d6a15d9f849d8bd83da4c24dab1df428a7a4ad26533XX/' \
  "${arm_dir}/tesseract-5.5.0.arm64.sha256" 2>/dev/null || true
# Portable fallback: corrupt the first hex char of line 1 instead.
if head -1 "${arm_dir}/tesseract-5.5.0.arm64.sha256" | grep -q '^X'; then :; else
  sed -i '1s/^./X/' "${arm_dir}/tesseract-5.5.0.arm64.sha256"
fi
if "${verifier}" --pins-dir "${arm_dir}" 2>"${tmp_root}/typo.err"; then
  record_fail 'hex typo in arm64 file fails'
elif grep -q 'tesseract-5.5.0.arm64.sha256' "${tmp_root}/typo.err"; then
  record_pass 'hex typo in arm64 file fails'
else
  record_fail 'hex typo in arm64 file fails'
fi

# RED: a dropped line fails.
drop_dir="${tmp_root}/drop"
setup_fixture "${drop_dir}"
sed -i '17d' "${drop_dir}/tesseract-5.5.0.arm64.sha256"
if "${verifier}" --pins-dir "${drop_dir}" >/dev/null 2>&1; then
  record_fail 'dropped line fails'
else
  record_pass 'dropped line fails'
fi

# RED: a garbage line fails.
garbage_dir="${tmp_root}/garbage"
setup_fixture "${garbage_dir}"
printf 'not-a-digest\n' >> "${garbage_dir}/tesseract-5.5.0.amd64.sha256"
if "${verifier}" --pins-dir "${garbage_dir}" >/dev/null 2>&1; then
  record_fail 'garbage line fails'
else
  record_pass 'garbage line fails'
fi

# Fail-closed: a missing arm64 file exits 2.
missing_dir="${tmp_root}/missing"
setup_fixture "${missing_dir}"
rm "${missing_dir}/tesseract-5.5.0.arm64.sha256"
if "${verifier}" --pins-dir "${missing_dir}" >/dev/null 2>&1; then
  record_fail 'missing arm64 file fails closed'
else
  code=$?
  if [[ ${code} -eq 2 ]]; then
    record_pass 'missing arm64 file fails closed'
  else
    record_fail 'missing arm64 file fails closed'
  fi
fi

# Fail-closed: an empty pin directory exits 2.
empty_dir="${tmp_root}/empty"
mkdir -p "${empty_dir}"
if "${verifier}" --pins-dir "${empty_dir}" >/dev/null 2>&1; then
  record_fail 'empty pin directory fails closed'
else
  code=$?
  if [[ ${code} -eq 2 ]]; then
    record_pass 'empty pin directory fails closed'
  else
    record_fail 'empty pin directory fails closed'
  fi
fi

printf '%d passed, %d failed\n' "${PASS}" "${FAIL}"
[[ ${FAIL} -eq 0 ]]
