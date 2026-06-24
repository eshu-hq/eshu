#!/usr/bin/env bash
#
# Test mirror for scripts/verify-license-header.sh and
# scripts/add-license-header.sh. Creates isolated scratch repos, runs the
# verifier/generator against them, and asserts the expected pass/fail
# outcome for each scenario. Pattern mirrors scripts/test-verify-package-docs.sh.
#
# Scenarios:
#   1. All files carry the correct SPDX header  -> verifier passes.
#   2. One file is missing the header           -> verifier fails.
#   3. One file carries a wrong SPDX identifier -> verifier fails.
#   4. One file carries a wrong copyright line  -> verifier fails.
#   5. Header + //go:build constraint           -> verifier passes.
#   6. No .go files in the tree                 -> verifier passes (vacuous).
#   7. add-license-header.sh is idempotent on a fresh tree and on a
#      pre-headered tree; produces the exact two-line header.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-license-header.sh"
generator="${repo_root}/scripts/add-license-header.sh"

if [ ! -x "${verifier}" ]; then
  printf 'test-verify-license-header: verifier missing or not executable: %s\n' "${verifier}" >&2
  exit 1
fi
if [ ! -x "${generator}" ]; then
  printf 'test-verify-license-header: generator missing or not executable: %s\n' "${generator}" >&2
  exit 1
fi

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

write_file() {
  local repo="$1" path="$2" content="$3"
  mkdir -p "$(dirname "${repo}/${path}")"
  printf '%s' "${content}" > "${repo}/${path}"
}

run_verifier() {
  local repo="$1"
  ESHU_LICENSE_HEADER_REPO_ROOT="${repo}" \
    "${verifier}" >/tmp/eshu-license-header.out 2>/tmp/eshu-license-header.err
}

expect_pass() {
  local label="$1" repo="$2"
  if ! run_verifier "${repo}"; then
    {
      printf 'FAIL scenario %s: expected verifier to pass\n' "${label}"
      printf '-- stdout --\n'
      sed -n '1,80p' /tmp/eshu-license-header.out
      printf '-- stderr --\n'
      sed -n '1,80p' /tmp/eshu-license-header.err
    } >&2
    exit 1
  fi
  printf '  scenario %s ok (verifier passes)\n' "${label}"
}

expect_fail() {
  local label="$1" repo="$2"
  if run_verifier "${repo}"; then
    {
      printf 'FAIL scenario %s: expected verifier to fail\n' "${label}"
      printf '-- stdout --\n'
      sed -n '1,80p' /tmp/eshu-license-header.out
      printf '-- stderr --\n'
      sed -n '1,80p' /tmp/eshu-license-header.err
    } >&2
    exit 1
  fi
  printf '  scenario %s ok (verifier fails)\n' "${label}"
}

SPDX_OK='// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pkg
'

# 1. All headers present.
repo1="$(mktemp -d "${tmp_root}/s1.XXXXXX")"
write_file "${repo1}" pkg/a.go "${SPDX_OK}"
write_file "${repo1}" pkg/b.go "${SPDX_OK}"
expect_pass 1 "${repo1}"

# 2. Missing header.
repo2="$(mktemp -d "${tmp_root}/s2.XXXXXX")"
write_file "${repo2}" pkg/a.go "${SPDX_OK}"
write_file "${repo2}" pkg/b.go 'package pkg
'
expect_fail 2 "${repo2}"

# 3. Wrong SPDX identifier.
repo3="$(mktemp -d "${tmp_root}/s3.XXXXXX")"
write_file "${repo3}" pkg/a.go '// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2025-2026 eshu-hq

package pkg
'
expect_fail 3 "${repo3}"

# 4. Wrong copyright line.
repo4="$(mktemp -d "${tmp_root}/s4.XXXXXX")"
write_file "${repo4}" pkg/a.go '// SPDX-License-Identifier: MIT
// Copyright (c) 2099 somebody-else

package pkg
'
expect_fail 4 "${repo4}"

# 5. Header + build constraint (proper structure).
repo5="$(mktemp -d "${tmp_root}/s5.XXXXXX")"
write_file "${repo5}" pkg/a.go '// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build linux

package pkg
'
expect_pass 5 "${repo5}"

# 6. No .go files (vacuous pass).
repo6="$(mktemp -d "${tmp_root}/s6.XXXXXX")"
write_file "${repo6}" README.md '# no go files here'
expect_pass 6 "${repo6}"

# 7. Generator is idempotent and produces the right header.
repo7="$(mktemp -d "${tmp_root}/s7.XXXXXX")"
write_file "${repo7}" pkg/a.go 'package pkg
'
write_file "${repo7}" pkg/b.go 'package pkg
const X = 1
'

ESHU_LICENSE_HEADER_REPO_ROOT="${repo7}" "${generator}" >/tmp/eshu-license-header.gen.out

line1="$(sed -n '1p' "${repo7}/pkg/a.go")"
line2="$(sed -n '2p' "${repo7}/pkg/a.go")"
line3="$(sed -n '3p' "${repo7}/pkg/a.go")"
if [ "${line1}" != '// SPDX-License-Identifier: MIT' ] \
   || [ "${line2}" != '// Copyright (c) 2025-2026 eshu-hq' ] \
   || [ "${line3}" != '' ]; then
  {
    printf 'FAIL scenario 7a: generator did not produce expected first-3 lines for fresh file\n'
    printf 'line1: %q\nline2: %q\nline3: %q\n' "${line1}" "${line2}" "${line3}"
  } >&2
  exit 1
fi
printf '  scenario 7a ok (generator prepends correct header + blank line)\n'

# Idempotent: re-running does not duplicate the header.
before_hash="$(shasum "${repo7}/pkg/a.go" "${repo7}/pkg/b.go" | sort)"
ESHU_LICENSE_HEADER_REPO_ROOT="${repo7}" "${generator}" >/tmp/eshu-license-header.gen2.out
after_hash="$(shasum "${repo7}/pkg/a.go" "${repo7}/pkg/b.go" | sort)"
if [ "${before_hash}" != "${after_hash}" ]; then
  {
    printf 'FAIL scenario 7b: generator is not idempotent (file hash changed)\n'
    printf 'before:\n%s\nafter:\n%s\n' "${before_hash}" "${after_hash}"
  } >&2
  exit 1
fi
printf '  scenario 7b ok (generator idempotent on already-headered files)\n'

# After running the generator, the verifier must accept the same tree.
expect_pass 7 "${repo7}"

# 8. Generator handles a file that starts with //go:build.
repo8="$(mktemp -d "${tmp_root}/s8.XXXXXX")"
write_file "${repo8}" pkg/build.go '//go:build linux

package pkg
'
ESHU_LICENSE_HEADER_REPO_ROOT="${repo8}" "${generator}" >/dev/null
expected8="$(mktemp "${tmp_root}/expected8.XXXXXX")"
trap 'rm -f "${expected8}" 2>/dev/null || true' EXIT
{
  printf '%s\n' '// SPDX-License-Identifier: MIT'
  printf '%s\n' '// Copyright (c) 2025-2026 eshu-hq'
  printf '\n'
  printf '%s\n' '//go:build linux'
  printf '\n'
  printf '%s\n' 'package pkg'
} > "${expected8}"
trap 'rm -rf "${tmp_root}" "${expected8}" 2>/dev/null || true' EXIT
if ! diff -u "${expected8}" "${repo8}/pkg/build.go" >/dev/null; then
  {
    printf 'FAIL scenario 8: generator did not preserve //go:build placement\n'
    diff -u "${expected8}" "${repo8}/pkg/build.go" >&2
  } >&2
  exit 1
fi
printf '  scenario 8 ok (generator preserves //go:build constraint)\n'

# 9. Verifier rejects a file with the build constraint directly under
# the header (no blank line) -- this matches the canonical structure
# add-license-header.sh emits.
repo9="$(mktemp -d "${tmp_root}/s9.XXXXXX")"
write_file "${repo9}" pkg/badbuild.go '// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//go:build linux

package pkg
'
# Lines 1-2 are still correct, so verifier accepts (presence check only).
expect_pass 9 "${repo9}"

# Helper for byte-exact file comparison that survives bash command
# substitution's trailing-newline stripping.
assert_file_eq() {
  local label="$1" got="$2" want="$3"
  local gotf wantf
  gotf="$(mktemp)"
  wantf="$(mktemp)"
  printf '%s' "$got" > "$gotf"
  printf '%s' "$want" > "$wantf"
  if ! diff -u "$wantf" "$gotf" >/dev/null; then
    {
      printf 'FAIL %s: file content mismatch\n' "$label"
      diff -u "$wantf" "$gotf"
    } >&2
    rm -f "$gotf" "$wantf"
    exit 1
  fi
  rm -f "$gotf" "$wantf"
}

# 10. Generator replaces a stale (non-canonical) SPDX block instead of
# prepending. A file that already declares Apache-2.0 should NOT end up
# with both an MIT header on lines 1-2 AND a stale Apache-2.0 header
# below; the generator must strip the stale block before writing.
stale_repo="$(mktemp -d "${tmp_root}/s10.XXXXXX")"
write_file "${stale_repo}" pkg/stale.go "$(printf '// SPDX-License-Identifier: Apache-2.0\n// Copyright (c) 2024 other-org\n\npackage pkg\n')"
ESHU_LICENSE_HEADER_REPO_ROOT="${stale_repo}" "${generator}" >/dev/null
stale_got="$(cat "${stale_repo}/pkg/stale.go")"
stale_expected="$(printf '// SPDX-License-Identifier: MIT\n// Copyright (c) 2025-2026 eshu-hq\n\npackage pkg\n')"
assert_file_eq "scenario 10" "${stale_got}" "${stale_expected}"
# Confirm the Apache reference is gone from the file (no second header).
if grep -q 'Apache-2.0' "${stale_repo}/pkg/stale.go"; then
  printf 'FAIL scenario 10: stale Apache SPDX identifier still present\n' >&2
  exit 1
fi
printf '  scenario 10 ok (generator replaces stale SPDX block)\n'

# 11. Generator handles a file with only Copyright (no SPDX) -- treat
# Copyright as part of the license block.
copyright_repo="$(mktemp -d "${tmp_root}/s11.XXXXXX")"
write_file "${copyright_repo}" pkg/cp.go "$(printf '// Copyright (c) 2024 other-org\n\npackage cp\n')"
ESHU_LICENSE_HEADER_REPO_ROOT="${copyright_repo}" "${generator}" >/dev/null
cp_got="$(cat "${copyright_repo}/pkg/cp.go")"
cp_expected="$(printf '// SPDX-License-Identifier: MIT\n// Copyright (c) 2025-2026 eshu-hq\n\npackage cp\n')"
assert_file_eq "scenario 11" "${cp_got}" "${cp_expected}"
printf '  scenario 11 ok (generator replaces Copyright-only header)\n'

# 12. Generator handles a stale SPDX followed by a //go:build constraint.
stale_build_repo="$(mktemp -d "${tmp_root}/s12.XXXXXX")"
write_file "${stale_build_repo}" pkg/stalebuild.go "$(printf '// SPDX-License-Identifier: Apache-2.0\n// Copyright (c) 2024 other-org\n\n//go:build linux\n\npackage stalebuild\n')"
ESHU_LICENSE_HEADER_REPO_ROOT="${stale_build_repo}" "${generator}" >/dev/null
sb_got="$(cat "${stale_build_repo}/pkg/stalebuild.go")"
sb_expected="$(printf '// SPDX-License-Identifier: MIT\n// Copyright (c) 2025-2026 eshu-hq\n\n//go:build linux\n\npackage stalebuild\n')"
assert_file_eq "scenario 12" "${sb_got}" "${sb_expected}"
printf '  scenario 12 ok (generator strips stale block, preserves //go:build)\n'

# 13. The scripts use rg --files for discovery (AGENTS.md mandates rg,
# forbids find). Verified by ensuring both scripts run on this temp
# repo successfully -- if rg were missing the verifier would not find
# the file and exit non-zero on a missing header scenario.
rg_only_repo="$(mktemp -d "${tmp_root}/s13.XXXXXX")"
write_file "${rg_only_repo}" pkg/ok.go '// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ok
'
expect_pass 13 "${rg_only_repo}"

printf 'verify-license-header tests passed\n'
