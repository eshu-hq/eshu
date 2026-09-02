#!/usr/bin/env bash
# Adversarial cases for shared citation-file preparation.
# Sourced by test-verify-doc-citations.sh after shared helpers exist.

test_file_preparation_is_shared_across_scan_modes() {
  local root="${tmp_root}/shared-file-preparation"
  local out="${tmp_root}/shared-file-preparation.out"
  local bin="${tmp_root}/shared-file-preparation-bin"
  local enum_count_file="${tmp_root}/shared-file-preparation-enum.count"
  local nul_count_file="${tmp_root}/shared-file-preparation-nul.count"
  local scan_count_file="${tmp_root}/shared-file-preparation-scan.count"
  local real_rg index enum_count nul_count scan_count

  for index in $(seq 1 260); do
    write_doc "${root}" "$(printf 'file-%03d.md' "${index}")" \
      '# Ordinary prose' 'No citations here.'
  done
  mkdir -p "${root}/scripts" "${bin}"
  printf '# empty ledger\n' >"${root}/scripts/docs-citations-baseline.txt"
  real_rg="$(command -v rg)"
  {
    printf '%s\n' '#!/usr/bin/env bash'
    printf '%s\n' 'for arg in "$@"; do'
    printf '%s\n' '  [[ "${arg}" == "--files" ]] && printf x >>"${ENUM_COUNT_FILE:?}"'
    printf '%s\n' '  [[ "${arg}" == "--files-with-matches" ]] && printf x >>"${NUL_COUNT_FILE:?}"'
    printf '%s\n' '  [[ "${arg}" == "--with-filename" ]] && printf x >>"${SCAN_COUNT_FILE:?}"'
    printf '%s\n' 'done'
    printf '%s\n' 'exec "${REAL_RG:?}" "$@"'
  } >"${bin}/rg"
  chmod +x "${bin}/rg"

  if ! PATH="${bin}:${PATH}" REAL_RG="${real_rg}" \
    ENUM_COUNT_FILE="${enum_count_file}" NUL_COUNT_FILE="${nul_count_file}" \
    SCAN_COUNT_FILE="${scan_count_file}" \
    ESHU_DOC_CITATIONS_RG="${bin}/rg" run_verifier "${root}" "${out}"; then
    record_fail "preparation case: instrumented verifier accepts the citation-free tree"
    cat "${out}" >&2
    return
  fi
  assert_contains 'verify-doc-citations: OK' "${out}" \
    "preparation case: instrumented verifier accepts the citation-free tree"
  enum_count="$(wc -c <"${enum_count_file}" | tr -d ' ')"
  nul_count="$(wc -c <"${nul_count_file}" | tr -d ' ')"
  scan_count="$(wc -c <"${scan_count_file}" | tr -d ' ')"
  if [[ "${enum_count}" -eq 1 && "${nul_count}" -eq 3 && "${scan_count}" -eq 3 ]]; then
    record_pass "preparation case: raw and permalink scans share enumeration, NUL preflight, and content scan"
  else
    record_fail "preparation case: scans are shared (enumerations=${enum_count}, NUL batches=${nul_count}, content batches=${scan_count})"
  fi
}

test_shared_scan_preserves_mixed_same_line_matches() {
  local root="${tmp_root}/shared-content-scan" out="${tmp_root}/shared-content-scan.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt"
  local main_link='https://github.com/eshu-hq/eshu/blob/main/go/internal/example.go#L3'
  local short_link='https://github.com/eshu-hq/eshu/blob/abc123/go/internal/example.go#L4'
  write_plain_go "${root}" 'go/internal/example.go'
  write_doc "${root}" 'mixed.md' '# Mixed matches' \
    "Raw go/internal/example.go:3 plus ${main_link} and ${short_link}."
  mkdir -p "$(dirname "${baseline}")"
  printf 'LINE docs/public/languages/mixed.md go/internal/example.go:3\n' >"${baseline}"
  git -C "${root}" init -q
  if run_verifier "${root}" "${out}"; then
    record_fail "preparation case: mixed same-line mutable permalinks fail"
  else
    record_pass "preparation case: mixed same-line mutable permalinks fail"
  fi
  assert_contains "${main_link}" "${out}" \
    "preparation case: shared scan preserves the first same-line permalink"
  assert_contains "${short_link}" "${out}" \
    "preparation case: shared scan preserves the second same-line permalink"
}

write_mixed_raw_scan_fixture() {
  local root="$1" baseline="${1}/scripts/docs-citations-baseline.txt"
  local full_sha='0123456789abcdef0123456789abcdef01234567'
  local full_link="https://github.com/eshu-hq/eshu/blob/${full_sha}/go/internal/example.go?plain=1#L3"
  write_plain_go "${root}" 'go/internal/example.go'
  write_doc "${root}" 'mixed-raw.md' '# Mixed raw matches' \
    "First go/internal/example.go:3, then ${full_link}, then go/internal/example.go:3."
  mkdir -p "$(dirname "${baseline}")"
  printf '%s\n%s\n' \
    'LINE docs/public/languages/mixed-raw.md go/internal/example.go:3' \
    'LINE docs/public/languages/mixed-raw.md go/internal/example.go:3' >"${baseline}"
  git -C "${root}" init -q
}

test_shared_scan_preserves_mixed_raw_multiplicity() {
  local root="${tmp_root}/shared-mixed-raw" out="${tmp_root}/shared-mixed-raw.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt" count
  write_mixed_raw_scan_fixture "${root}"
  if run_verifier "${root}" "${out}" && run_verifier "${root}" "${out}" -update; then
    record_pass "preparation case: mixed raw/full-SHA line passes check and update"
  else
    record_fail "preparation case: mixed raw/full-SHA line passes check and update"
    cat "${out}" >&2
  fi
  count="$(awk '$0 == "LINE docs/public/languages/mixed-raw.md go/internal/example.go:3" { count++ } END { print count + 0 }' "${baseline}")"
  if [[ "${count}" -eq 2 ]]; then
    record_pass "preparation case: shared scan preserves mixed raw multiplicity"
  else
    record_fail "preparation case: shared scan preserves mixed raw multiplicity (got ${count}, want 2)"
  fi
}

test_shared_scan_rejects_mixed_raw_drop_mutation() {
  local root="${tmp_root}/shared-mixed-raw-mutation"
  local out="${tmp_root}/shared-mixed-raw-mutation.out"
  local scanner="${root}/mutated-rg" real_rg
  write_mixed_raw_scan_fixture "${root}"
  real_rg="$(command -v rg)"
  {
    printf '%s\n' '#!/usr/bin/env bash' 'set -o pipefail'
    printf '%s\n' '"${REAL_RG:?}" "$@" | sed '\''/\/blob\// s#go/internal/example.go:3##g'\'''
  } >"${scanner}"
  chmod +x "${scanner}"
  if REAL_RG="${real_rg}" ESHU_DOC_CITATIONS_RG="${scanner}" \
    run_verifier "${root}" "${out}"; then
    record_fail "preparation case: dropping mixed raw matches must fail"
  else
    record_pass "preparation case: dropping mixed raw matches must fail"
  fi
  assert_contains 'stale LINE baseline' "${out}" \
    "preparation case: mixed raw drop mutation is caught by recurrence proof"
}

assert_colon_number_path_fails_closed() {
  local case_name="$1" rel="$2"
  local root="${tmp_root}/colon-number-${case_name}"
  local check_out="${tmp_root}/colon-number-${case_name}-check.out"
  local update_out="${tmp_root}/colon-number-${case_name}-update.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt"
  local before="${tmp_root}/colon-number-${case_name}.before"
  write_plain_go "${root}" 'go/internal/example.go'
  write_doc "${root}" "${rel}" '# Ambiguous path' \
    'Stable wording: go/internal/example.go:3.'
  mkdir -p "$(dirname "${baseline}")"
  printf '# empty ledger\n' >"${baseline}"
  git -C "${root}" init -q
  git -C "${root}" add docs go scripts/docs-citations-baseline.txt
  cp "${baseline}" "${before}"
  if run_verifier "${root}" "${check_out}"; then
    record_fail "preparation case: ${case_name} colon-number path fails check mode"
  else
    record_pass "preparation case: ${case_name} colon-number path fails check mode"
  fi
  assert_contains 'file path contains an ambiguous :number: segment' "${check_out}" \
    "preparation case: ${case_name} colon-number check names the fail-closed reason"
  if run_verifier "${root}" "${update_out}" -update; then
    record_fail "preparation case: ${case_name} colon-number path fails update mode"
  else
    record_pass "preparation case: ${case_name} colon-number path fails update mode"
  fi
  assert_contains 'file path contains an ambiguous :number: segment' "${update_out}" \
    "preparation case: ${case_name} colon-number update names the fail-closed reason"
  if cmp -s "${before}" "${baseline}"; then
    record_pass "preparation case: rejected ${case_name} path leaves ledger byte-identical"
  else
    record_fail "preparation case: rejected ${case_name} path leaves ledger byte-identical"
  fi
}

test_colon_number_paths_fail_closed_independently() {
  assert_colon_number_path_fails_closed file-multi 'ambiguous:27:name.md'
  assert_colon_number_path_fails_closed directory-multi 'segment:345:part/example.md'
  assert_colon_number_path_fails_closed single-digit 'ambiguous:7:name.md'
  assert_colon_number_path_fails_closed zero 'ambiguous:0:name.md'
  assert_colon_number_path_fails_closed leading-zero 'segment:01:part/example.md'
}

test_legal_colon_paths_pass() {
  local root="${tmp_root}/legal-colon-paths"
  local check_out="${tmp_root}/legal-colon-paths-check.out"
  local update_out="${tmp_root}/legal-colon-paths-update.out"
  write_doc "${root}" 'name:part.md' '# Legal colon' 'Ordinary prose.'
  write_doc "${root}" 'name:1part.md' '# Legal colon digit' 'Ordinary prose.'
  write_doc "${root}" 'name:1.md' '# Legal trailing digit' 'Ordinary prose.'
  mkdir -p "${root}/scripts"
  printf '# empty ledger\n' >"${root}/scripts/docs-citations-baseline.txt"
  git -C "${root}" init -q
  git -C "${root}" add docs scripts/docs-citations-baseline.txt
  if run_verifier "${root}" "${check_out}" && run_verifier "${root}" "${update_out}" -update; then
    record_pass "preparation case: non-framing colon paths pass check and update"
  else
    record_fail "preparation case: non-framing colon paths pass check and update"
    cat "${check_out}" "${update_out}" >&2
  fi
  if rg -q '^LINE ' "${root}/scripts/docs-citations-baseline.txt"; then
    record_fail "preparation case: legal colon paths do not create LINE debt"
  else
    record_pass "preparation case: legal colon paths do not create LINE debt"
  fi
}

test_colon_number_binary_path_remains_excluded() {
  local root="${tmp_root}/colon-number-binary"
  local check_out="${tmp_root}/colon-number-binary-check.out"
  local update_out="${tmp_root}/colon-number-binary-update.out"
  local binary_png="${root}/docs/public/languages/chart:2026:v1.PNG"
  local binary_gz="${root}/docs/public/languages/archive:2026:v2.GZ"
  write_plain_go "${root}" 'go/internal/example.go'
  mkdir -p "$(dirname "${binary_png}")" "${root}/scripts"
  printf 'binary\0go/internal/example.go:3\n' >"${binary_png}"
  printf 'archive\0go/internal/example.go:3\n' >"${binary_gz}"
  printf '# empty ledger\n' >"${root}/scripts/docs-citations-baseline.txt"
  git -C "${root}" init -q
  git -C "${root}" add docs go scripts/docs-citations-baseline.txt
  if run_verifier "${root}" "${check_out}"; then
    record_pass "preparation case: excluded binaries may contain colon-number paths in check mode"
  else
    record_fail "preparation case: excluded binaries may contain colon-number paths in check mode"
    cat "${check_out}" >&2
  fi
  if run_verifier "${root}" "${update_out}" -update; then
    record_pass "preparation case: excluded binaries may contain colon-number paths in update mode"
  else
    record_fail "preparation case: excluded binaries may contain colon-number paths in update mode"
    cat "${update_out}" >&2
  fi
  if rg -q '^LINE ' "${root}/scripts/docs-citations-baseline.txt"; then
    record_fail "preparation case: excluded colon-number binaries never enter the LINE ledger"
  else
    record_pass "preparation case: excluded colon-number binaries never enter the LINE ledger"
  fi
}

run_line_citation_preparation_cases() {
  test_file_preparation_is_shared_across_scan_modes
  test_shared_scan_preserves_mixed_same_line_matches
  test_shared_scan_preserves_mixed_raw_multiplicity
  test_shared_scan_rejects_mixed_raw_drop_mutation
  test_colon_number_paths_fail_closed_independently
  test_legal_colon_paths_pass
  test_colon_number_binary_path_remains_excluded
}
