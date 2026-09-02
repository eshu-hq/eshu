#!/usr/bin/env bash
# Binary-suffix and unknown-suffix cases for the doc-citation scanner.
# Sourced by test-verify-doc-citations.sh after shared helpers exist.

test_known_binary_suffixes_are_excluded() {
  local root="${tmp_root}/line-known-binary"
  local check_out="${tmp_root}/line-known-binary-check.out"
  local update_out="${tmp_root}/line-known-binary-update.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt"
  local png="${root}/docs/public/languages/evidence.PNG"
  local archive="${root}/docs/public/languages/evidence.GZ"

  mkdir -p "$(dirname "${png}")" "${root}/scripts"
  printf 'image bytes\0Citation-like bytes: go/internal/example.go:3\n' >"${png}"
  printf 'archive bytes\0Citation-like bytes: go/internal/example.go:3\n' >"${archive}"
  printf '# stable empty ledger\n' >"${baseline}"
  git -C "${root}" init -q
  git -C "${root}" add docs scripts/docs-citations-baseline.txt

  if run_verifier "${root}" "${check_out}"; then
    record_pass "binary case: known binary suffixes are excluded case-insensitively in check mode"
  else
    record_fail "binary case: known binary suffixes are excluded case-insensitively in check mode"
    cat "${check_out}" >&2
  fi
  if run_verifier "${root}" "${update_out}" -update; then
    record_pass "binary case: known binary suffixes are excluded case-insensitively in update mode"
  else
    record_fail "binary case: known binary suffixes are excluded case-insensitively in update mode"
    cat "${update_out}" >&2
  fi
  if rg -q '^LINE .*evidence\.(PNG|GZ) ' "${baseline}"; then
    record_fail "binary case: excluded media/archive bytes never enter the LINE ledger"
  else
    record_pass "binary case: excluded media/archive bytes never enter the LINE ledger"
  fi
}

test_nul_unknown_suffix_fails_closed() {
  local root="${tmp_root}/line-nul-unknown"
  local check_out="${tmp_root}/line-nul-unknown-check.out"
  local update_out="${tmp_root}/line-nul-unknown-update.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt"
  local before="${tmp_root}/line-nul-unknown.before"
  local unknown="${root}/docs/public/languages/evidence.unknown"

  mkdir -p "$(dirname "${unknown}")" "${root}/scripts"
  printf 'unknown bytes\0Later raw citation: go/internal/example.go:3\n' >"${unknown}"
  printf '# stable empty ledger\n' >"${baseline}"
  git -C "${root}" init -q
  git -C "${root}" add docs scripts/docs-citations-baseline.txt
  cp "${baseline}" "${before}"

  if run_verifier "${root}" "${check_out}"; then
    record_fail "binary case: a NUL-bearing unknown suffix fails check mode (verifier exited zero)"
  else
    record_pass "binary case: a NUL-bearing unknown suffix fails check mode"
  fi
  assert_contains "NUL byte in eligible citation file" "${check_out}" \
    "binary case: unknown-suffix NUL check names the fail-closed reason"
  if run_verifier "${root}" "${update_out}" -update; then
    record_fail "binary case: -update rejects a NUL-bearing unknown suffix (verifier exited zero)"
  else
    record_pass "binary case: -update rejects a NUL-bearing unknown suffix"
  fi
  assert_contains "NUL byte in eligible citation file" "${update_out}" \
    "binary case: unknown-suffix NUL update names the fail-closed reason"
  if cmp -s "${before}" "${baseline}"; then
    record_pass "binary case: rejected unknown-suffix NUL leaves the ledger byte-identical"
  else
    record_fail "binary case: rejected unknown-suffix NUL leaves the ledger byte-identical"
  fi
}

test_non_nul_unknown_suffix_is_scanned() {
  local root="${tmp_root}/line-text-unknown"
  local out="${tmp_root}/line-text-unknown.out"
  local unknown="${root}/docs/public/languages/evidence.unknown"
  mkdir -p "$(dirname "${unknown}")"
  printf 'Unknown suffix remains text-eligible: go/internal/example.go:3\n' >"${unknown}"
  git -C "${root}" init -q
  git -C "${root}" add docs

  if run_verifier "${root}" "${out}"; then
    record_fail "binary case: a non-NUL unknown suffix is scanned (verifier exited zero)"
  else
    record_pass "binary case: a non-NUL unknown suffix is scanned"
  fi
  assert_contains "docs/public/languages/evidence.unknown" "${out}" \
    "binary case: unknown-suffix citation failure names its source"
}

run_line_citation_binary_cases() {
  test_known_binary_suffixes_are_excluded
  test_nul_unknown_suffix_fails_closed
  test_non_nul_unknown_suffix_is_scanned
}
