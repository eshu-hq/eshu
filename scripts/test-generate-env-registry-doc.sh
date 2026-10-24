#!/usr/bin/env bash
#
# test-generate-env-registry-doc.sh - hermetic checks for the generated
# environment variable reference gate. The reference is an index page plus one
# page per subsystem family (docs/public/reference/env-registry.md and
# env-registry/*.md), every one under the Markdown line cap.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
generator="${repo_root}/scripts/generate-env-registry-doc.sh"
verifier="${repo_root}/scripts/verify-env-registry-doc.sh"
line_cap="${repo_root}/scripts/verify-markdown-line-cap.sh"
ref_dir="${repo_root}/docs/public/reference"
index_path="${ref_dir}/env-registry.md"
pages_dir="${ref_dir}/env-registry"

tmp_root="$(mktemp -d)"
backup="${tmp_root}/backup"
mkdir -p "${backup}/env-registry"

restore() {
  cp "${backup}/env-registry.md" "${index_path}" 2>/dev/null || true
  rm -rf "${pages_dir}"
  mkdir -p "${pages_dir}"
  cp "${backup}/env-registry/"*.md "${pages_dir}/" 2>/dev/null || true
}
trap 'restore; rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0

record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

assert_contains() {
  local needle="$1"
  local file="$2"
  if ! rg -q --fixed-strings "${needle}" "${file}"; then
    printf 'expected %s to contain: %s\n' "${file}" "${needle}" >&2
    cat "${file}" >&2
    exit 1
  fi
}

# same_tree is true when the live reference tree is byte-identical to the backup.
same_tree() {
  cmp -s "${index_path}" "${backup}/env-registry.md" \
    && diff -r -q "${pages_dir}" "${backup}/env-registry" >/dev/null
}

cp "${index_path}" "${backup}/env-registry.md"
cp "${pages_dir}/"*.md "${backup}/env-registry/"

pages=("${index_path}" "${pages_dir}/"*.md)

if [ "${#pages[@]}" -ge 3 ]; then
  record_pass "reference is split into an index plus several pages (${#pages[@]} files)"
else
  record_fail "reference has only ${#pages[@]} file(s); expected an index plus several pages"
fi

out="${tmp_root}/generator.out"
if "${generator}" >"${out}" 2>&1 && same_tree; then
  record_pass "generator is idempotent on a clean committed reference"
else
  record_fail "generator changed the committed reference on a clean run"
fi

marker_missing=0
for page in "${pages[@]}"; do
  rg -q --fixed-strings "Generated from go/internal/envregistry. Do not edit by hand" "${page}" || marker_missing=1
done
if [ "${marker_missing}" -eq 0 ]; then
  record_pass "every generated page carries the generated marker"
else
  record_fail "a generated page is missing the generated marker"
fi

cap_out="${tmp_root}/cap.out"
if "${line_cap}" --files "${pages[@]#"${repo_root}/"}" >"${cap_out}" 2>&1; then
  record_pass "line-cap gate accepts every generated page"
else
  cat "${cap_out}" >&2
  record_fail "line-cap gate rejected a generated page"
fi

verify_out="${tmp_root}/verify.out"
if "${verifier}" >"${verify_out}" 2>&1; then
  record_pass "verifier accepts the committed generated reference"
else
  record_fail "verifier rejected the committed generated reference"
fi

# A stale edit to a family page (not just the index) must be caught.
page_one="$(ls "${pages_dir}/"*.md | head -1)"
printf '\n<!-- stale edit -->\n' >>"${page_one}"
stale_out="${tmp_root}/stale.out"
if "${verifier}" >"${stale_out}" 2>&1; then
  record_fail "verifier accepted a stale family page"
else
  assert_contains "is out of date" "${stale_out}"
  record_pass "verifier rejects a stale family page"
fi
restore

# A stale edit to the index must be caught.
printf '\n<!-- stale edit -->\n' >>"${index_path}"
stale_out="${tmp_root}/stale-index.out"
if "${verifier}" >"${stale_out}" 2>&1; then
  record_fail "verifier accepted a stale index"
else
  assert_contains "env-registry.md is out of date" "${stale_out}"
  record_pass "verifier rejects a stale index"
fi
restore

# A leftover page the generator no longer produces must be caught, and the
# generator must remove it.
printf '# leftover\n' >"${pages_dir}/leftover-page.md"
orphan_out="${tmp_root}/orphan.out"
if "${verifier}" >"${orphan_out}" 2>&1; then
  record_fail "verifier accepted an orphaned page"
else
  assert_contains "leftover-page.md is not produced by the generator" "${orphan_out}"
  record_pass "verifier rejects an orphaned page"
fi
if "${generator}" >"${out}" 2>&1 && [ ! -e "${pages_dir}/leftover-page.md" ] && same_tree; then
  record_pass "generator removes an orphaned page"
else
  record_fail "generator left an orphaned page behind"
fi
restore

# A directory inside the pages directory is not a generated page: the verifier
# must not call it an orphan, and the generator must neither fail on a
# non-empty one nor delete an empty one.
mkdir -p "${pages_dir}/notes" "${pages_dir}/scratch"
printf 'keep\n' >"${pages_dir}/notes/keep.md"
printf '# leftover\n' >"${pages_dir}/leftover-page.md"
subdir_out="${tmp_root}/subdir.out"
if "${generator}" >"${subdir_out}" 2>&1 \
  && [ -f "${pages_dir}/notes/keep.md" ] && [ -d "${pages_dir}/scratch" ] \
  && [ ! -e "${pages_dir}/leftover-page.md" ]; then
  record_pass "generator leaves subdirectories alone and still removes a stray page file"
else
  cat "${subdir_out}" >&2
  record_fail "generator failed on, or deleted, a subdirectory of the pages directory"
fi
verify_subdir_out="${tmp_root}/verify-subdir.out"
if "${verifier}" >"${verify_subdir_out}" 2>&1; then
  record_pass "verifier ignores subdirectories of the pages directory"
else
  cat "${verify_subdir_out}" >&2
  record_fail "verifier flagged a subdirectory of the pages directory"
fi
restore

# A missing family page must be caught.
rm -f "${page_one}"
missing_out="${tmp_root}/missing.out"
if "${verifier}" >"${missing_out}" 2>&1; then
  record_fail "verifier accepted a missing family page"
else
  record_pass "verifier rejects a missing family page"
fi
restore

if [ "${FAIL}" -ne 0 ]; then
  printf 'generate-env-registry-doc tests FAILED: %d/%d\n' "${FAIL}" "$((PASS + FAIL))" >&2
  exit 1
fi

printf 'generate-env-registry-doc tests passed: %d/%d\n' "${PASS}" "$((PASS + FAIL))"
