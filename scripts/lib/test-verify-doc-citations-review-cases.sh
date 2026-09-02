#!/usr/bin/env bash
# Adversarial review cases for immutable citation context and file discovery.
# Sourced by test-verify-doc-citations.sh after shared helpers exist.

test_reworded_same_pair_cannot_reuse_base_authority() {
  local root="${tmp_root}/line-context-replacement"
  local check_out="${tmp_root}/line-context-replacement-check.out"
  local update_out="${tmp_root}/line-context-replacement-update.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt"
  local before="${tmp_root}/line-context-replacement.before" base

  write_plain_go "${root}" "go/internal/example.go"
  write_doc "${root}" "line.md" \
    '# Existing claim' '' \
    'Old wording: `go/internal/example.go:3`.'
  mkdir -p "${root}/scripts"
  printf 'LINE docs/public/languages/line.md go/internal/example.go:3\n' >"${baseline}"
  git -C "${root}" init -q
  git -C "${root}" config user.name 'Doc Citation Test'
  git -C "${root}" config user.email 'doc-citation-test@example.invalid'
  git -C "${root}" add .
  git -C "${root}" commit -qm 'base claim'
  base="$(git -C "${root}" rev-parse HEAD)"

  write_doc "${root}" "line.md" \
    '# Replacement claim' '' \
    'Unrelated new wording: `go/internal/example.go:3`.'
  git -C "${root}" add .
  git -C "${root}" commit -qm 'replace claim but retain pair'

  if ESHU_DOC_CITATIONS_BASE="${base}" run_verifier "${root}" "${check_out}"; then
    record_fail "review case: rewording cannot reuse same-pair base authority in check mode (verifier exited zero)"
    cat "${check_out}" >&2
  else
    record_pass "review case: rewording cannot reuse same-pair base authority in check mode"
  fi
  assert_contains "branch-replaced LINE context" "${check_out}" \
    "review case: check explains the immutable containing-line mismatch"

  cp "${baseline}" "${before}"
  if ESHU_DOC_CITATIONS_BASE="${base}" run_verifier "${root}" "${update_out}" -update; then
    record_fail "review case: -update cannot authorize reworded same-pair debt (verifier exited zero)"
    cat "${update_out}" >&2
  else
    record_pass "review case: -update cannot authorize reworded same-pair debt"
  fi
  assert_contains "branch-replaced LINE context" "${update_out}" \
    "review case: -update explains the immutable containing-line mismatch"
  if cmp -s "${before}" "${baseline}"; then
    record_pass "review case: rejected rewording leaves the baseline byte-identical"
  else
    record_fail "review case: rejected rewording leaves the baseline byte-identical"
  fi
}

test_byte_identical_same_line_move_is_allowed() {
  local root="${tmp_root}/line-context-move"
  local out="${tmp_root}/line-context-move.out" base

  write_plain_go "${root}" "go/internal/example.go"
  write_doc "${root}" "line.md" \
    '# Evidence' \
    'Stable wording: `go/internal/example.go:3`.' \
    'Trailing prose.'
  mkdir -p "${root}/scripts"
  printf 'LINE docs/public/languages/line.md go/internal/example.go:3\n' \
    >"${root}/scripts/docs-citations-baseline.txt"
  git -C "${root}" init -q
  git -C "${root}" config user.name 'Doc Citation Test'
  git -C "${root}" config user.email 'doc-citation-test@example.invalid'
  git -C "${root}" add .
  git -C "${root}" commit -qm 'base exact wording'
  base="$(git -C "${root}" rev-parse HEAD)"

  write_doc "${root}" "line.md" \
    '# Evidence' \
    'Trailing prose moved first.' \
    'Stable wording: `go/internal/example.go:3`.'
  if ESHU_DOC_CITATIONS_BASE="${base}" run_verifier "${root}" "${out}"; then
    record_pass "review case: a byte-identical containing line may move within its source file"
  else
    record_fail "review case: a byte-identical containing line may move within its source file"
    cat "${out}" >&2
  fi
}

test_force_added_ignored_hidden_files_are_scanned() {
  local raw_root="${tmp_root}/line-force-added-raw"
  local raw_out="${tmp_root}/line-force-added-raw.out"
  local link_root="${tmp_root}/line-force-added-link"
  local link_out="${tmp_root}/line-force-added-link.out"
  local hidden='docs/public/languages/.ignored.md'

  write_plain_go "${raw_root}" "go/internal/example.go"
  write_doc "${raw_root}" ".ignored.md" \
    '# Force-added evidence' '' \
    'Tracked despite ignore: `go/internal/example.go:3`.'
  printf '%s\n' "${hidden}" >"${raw_root}/.gitignore"
  git -C "${raw_root}" init -q
  git -C "${raw_root}" add .gitignore go/internal/example.go
  git -C "${raw_root}" add -f "${hidden}"
  if run_verifier "${raw_root}" "${raw_out}"; then
    record_fail "review case: force-added ignored hidden docs raw citations are scanned (verifier exited zero)"
    cat "${raw_out}" >&2
  else
    record_pass "review case: force-added ignored hidden docs raw citations are scanned"
  fi
  assert_contains "${hidden}" "${raw_out}" \
    "review case: ignored hidden raw-citation failure names the tracked file"

  write_doc "${link_root}" ".ignored.md" \
    '# Force-added link' '' \
    'Mutable: https://github.com/eshu-hq/eshu/blob/main/go/internal/example.go#L3'
  printf '%s\n' "${hidden}" >"${link_root}/.gitignore"
  git -C "${link_root}" init -q
  git -C "${link_root}" add .gitignore
  git -C "${link_root}" add -f "${hidden}"
  if run_verifier "${link_root}" "${link_out}"; then
    record_fail "review case: force-added ignored hidden docs mutable permalinks are scanned (verifier exited zero)"
    cat "${link_out}" >&2
  else
    record_pass "review case: force-added ignored hidden docs mutable permalinks are scanned"
  fi
  assert_contains "permalink must use a full 40-hex commit SHA" "${link_out}" \
    "review case: ignored hidden permalink failure keeps the immutable-ref diagnostic"
}

test_query_string_line_permalinks_follow_ref_contract() {
  local mutable_root="${tmp_root}/line-query-mutable"
  local mutable_out="${tmp_root}/line-query-mutable.out"
  local short_root="${tmp_root}/line-query-short"
  local short_out="${tmp_root}/line-query-short.out"
  local full_root="${tmp_root}/line-query-full"
  local full_out="${tmp_root}/line-query-full.out"

  write_doc "${mutable_root}" "line.md" \
    '# Mutable query link' '' \
    'Mutable: https://github.com/eshu-hq/eshu/blob/main/go/internal/example.go?plain=1#L3'
  if run_verifier "${mutable_root}" "${mutable_out}"; then
    record_fail "review case: main-ref query-string #L permalink fails (verifier exited zero)"
    cat "${mutable_out}" >&2
  else
    record_pass "review case: main-ref query-string #L permalink fails"
  fi
  assert_contains "permalink must use a full 40-hex commit SHA" "${mutable_out}" \
    "review case: main-ref query failure explains the immutable-ref contract"

  write_doc "${short_root}" "line.md" \
    '# Short query link' '' \
    'Short: https://github.com/eshu-hq/eshu/blob/deadbee/go/internal/example.go?plain=1#L3'
  if run_verifier "${short_root}" "${short_out}"; then
    record_fail "review case: short-ref query-string #L permalink fails (verifier exited zero)"
    cat "${short_out}" >&2
  else
    record_pass "review case: short-ref query-string #L permalink fails"
  fi
  assert_contains "permalink must use a full 40-hex commit SHA" "${short_out}" \
    "review case: short-ref query failure explains the immutable-ref contract"

  write_doc "${full_root}" "line.md" \
    '# Immutable query link' '' \
    'Immutable: https://github.com/eshu-hq/eshu/blob/0123456789abcdef0123456789abcdef01234567/go/internal/example.go?plain=1#L3'
  if run_verifier "${full_root}" "${full_out}"; then
    record_pass "review case: full-SHA query-string #L permalink is allowed"
  else
    record_fail "review case: full-SHA query-string #L permalink is allowed"
    cat "${full_out}" >&2
  fi
}

test_public_gate_name_states_recurrence_scope() {
  local registry="${repo_root}/specs/ci-gates.v1.yaml"
  local generated="${repo_root}/docs/public/reference/ci-gates.md"
  local name='Doc test/fixture existence and raw-line recurrence guard'
  assert_contains "name: ${name}" "${registry}" \
    "review case: registry name states existence and recurrence scope"
  assert_contains "| \`doc-citations\` | ${name} |" "${generated}" \
    "review case: generated public name states existence and recurrence scope"
}

write_batch_scanner() {
  local path="$1" mode="$2"
  mkdir -p "$(dirname "${path}")"
  {
    printf '%s\n' \
    '#!/bin/sh' \
    "mode='${mode}'" \
    'if [ "${mode}" = reject-large ] && [ "$#" -gt 200 ]; then exit 2; fi' \
    'if [ "${mode}" = fail-late ] && [ "$#" -lt 200 ]; then' \
    '  for arg do case "${arg}" in *batch-260.md) exit 2 ;; esac; done' \
    'fi' \
    'exec rg "$@"'
  } >"${path}"
  chmod +x "${path}"
}

write_many_batch_docs() {
  local root="$1" index path
  for index in $(seq 1 260); do
    path="$(printf 'batch-%03d.md' "${index}")"
    write_doc "${root}" "${path}" '# Batch fixture' 'No citation here.'
  done
}

test_bounded_batches_scan_later_files() {
  local root="${tmp_root}/line-batched-late-match"
  local out="${tmp_root}/line-batched-late-match.out"
  local scanner="${root}/batch-rg"
  write_plain_go "${root}" "go/internal/example.go"
  write_many_batch_docs "${root}"
  printf 'Late citation: `go/internal/example.go:3`.\n' \
    >>"${root}/docs/public/languages/batch-260.md"
  git -C "${root}" init -q
  write_batch_scanner "${scanner}" reject-large
  if ESHU_DOC_CITATIONS_RG="${scanner}" run_verifier "${root}" "${out}"; then
    record_fail "review case: bounded batches scan a raw citation after the first batch (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "review case: bounded batches scan a raw citation after the first batch"
  fi
  assert_contains "batch-260.md" "${out}" \
    "review case: later-batch raw citation names its source"
}

test_later_batch_scanner_failure_fails_closed() {
  local root="${tmp_root}/line-batched-late-error"
  local out="${tmp_root}/line-batched-late-error.out"
  local scanner="${root}/batch-rg"
  write_plain_go "${root}" "go/internal/example.go"
  write_many_batch_docs "${root}"
  printf 'Tracked citation: `go/internal/example.go:3`.\n' \
    >>"${root}/docs/public/languages/batch-260.md"
  mkdir -p "${root}/scripts"
  printf 'LINE docs/public/languages/batch-260.md go/internal/example.go:3\n' \
    >"${root}/scripts/docs-citations-baseline.txt"
  git -C "${root}" init -q
  write_batch_scanner "${scanner}" fail-late
  if ESHU_DOC_CITATIONS_RG="${scanner}" run_verifier "${root}" "${out}"; then
    record_fail "review case: a later-batch scanner failure cannot false-green (verifier exited zero)"
  else
    record_pass "review case: a later-batch scanner failure cannot false-green"
  fi
  assert_contains "batch-rg exited 2" "${out}" \
    "review case: later-batch scanner failure keeps the injected status"
}

test_nul_bearing_raw_citation_file_fails_closed() {
  local root="${tmp_root}/line-nul-raw"
  local check_out="${tmp_root}/line-nul-raw-check.out"
  local update_out="${tmp_root}/line-nul-raw-update.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt"
  local before="${tmp_root}/line-nul-raw.before"
  local binary="${root}/docs/public/languages/nul-raw.md"

  write_plain_go "${root}" "go/internal/example.go"
  write_doc "${root}" "line.md" \
    '# Existing debt' 'Visible: `go/internal/example.go:3`.'
  mkdir -p "$(dirname "${binary}")" "${root}/scripts"
  printf 'binary prefix\0Later raw citation: `go/internal/example.go:3`.\n' \
    >"${binary}"
  printf 'LINE docs/public/languages/line.md go/internal/example.go:3\n' \
    >"${baseline}"
  git -C "${root}" init -q
  git -C "${root}" add docs go scripts/docs-citations-baseline.txt
  cp "${baseline}" "${before}"

  if run_verifier "${root}" "${check_out}"; then
    record_fail "review case: a NUL-bearing raw-citation file fails check mode (verifier exited zero)"
  else
    record_pass "review case: a NUL-bearing raw-citation file fails check mode"
  fi
  assert_contains "NUL byte in eligible citation file" "${check_out}" \
    "review case: NUL-bearing raw file has an explicit fail-closed diagnostic"
  if run_verifier "${root}" "${update_out}" -update; then
    record_fail "review case: -update rejects a NUL-bearing raw-citation file (verifier exited zero)"
  else
    record_pass "review case: -update rejects a NUL-bearing raw-citation file"
  fi
  assert_contains "NUL byte in eligible citation file" "${update_out}" \
    "review case: NUL-bearing raw-file update names the fail-closed reason"
  if cmp -s "${before}" "${baseline}"; then
    record_pass "review case: rejected NUL-bearing raw file leaves the ledger byte-identical"
  else
    record_fail "review case: rejected NUL-bearing raw file leaves the ledger byte-identical"
  fi
}

test_nul_bearing_permalink_file_fails_closed() {
  local root="${tmp_root}/line-nul-permalink"
  local check_out="${tmp_root}/line-nul-permalink-check.out"
  local update_out="${tmp_root}/line-nul-permalink-update.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt"
  local before="${tmp_root}/line-nul-permalink.before"
  local binary="${root}/docs/public/languages/nul-link.md"

  write_doc "${root}" "line.md" '# Ordinary prose' 'No citations here.'
  mkdir -p "$(dirname "${binary}")" "${root}/scripts"
  printf 'binary prefix\0Later mutable link: https://github.com/eshu-hq/eshu/blob/main/go/internal/example.go#L3\n' \
    >"${binary}"
  printf '# stable empty ledger\n' >"${baseline}"
  git -C "${root}" init -q
  git -C "${root}" add docs scripts/docs-citations-baseline.txt
  cp "${baseline}" "${before}"

  if run_verifier "${root}" "${check_out}"; then
    record_fail "review case: a NUL-bearing permalink file fails check mode (verifier exited zero)"
  else
    record_pass "review case: a NUL-bearing permalink file fails check mode"
  fi
  assert_contains "NUL byte in eligible citation file" "${check_out}" \
    "review case: NUL-bearing permalink file has an explicit fail-closed diagnostic"
  if run_verifier "${root}" "${update_out}" -update; then
    record_fail "review case: -update rejects a NUL-bearing permalink file (verifier exited zero)"
  else
    record_pass "review case: -update rejects a NUL-bearing permalink file"
  fi
  assert_contains "NUL byte in eligible citation file" "${update_out}" \
    "review case: NUL-bearing permalink update names the fail-closed reason"
  if cmp -s "${before}" "${baseline}"; then
    record_pass "review case: rejected NUL-bearing permalink file leaves the ledger byte-identical"
  else
    record_fail "review case: rejected NUL-bearing permalink file leaves the ledger byte-identical"
  fi
}

test_duplicate_targets_on_one_line_preserve_multiplicity() {
  local root="${tmp_root}/line-same-line-duplicates"
  local check_out="${tmp_root}/line-same-line-duplicates-check.out"
  local update_out="${tmp_root}/line-same-line-duplicates-update.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt" count
  write_plain_go "${root}" "go/internal/example.go"
  write_doc "${root}" "line.md" '# Duplicate evidence' \
    'Both `go/internal/example.go:3` and `go/internal/example.go:3` support this claim.'
  mkdir -p "${root}/scripts"
  printf '%s\n' \
    'LINE docs/public/languages/line.md go/internal/example.go:3' \
    'LINE docs/public/languages/line.md go/internal/example.go:3' >"${baseline}"

  if run_verifier "${root}" "${check_out}"; then
    record_pass "review case: check preserves duplicate targets on one physical line"
  else
    record_fail "review case: check preserves duplicate targets on one physical line"
    cat "${check_out}" >&2
  fi
  if run_verifier "${root}" "${update_out}" -update; then
    record_pass "review case: -update preserves duplicate targets on one physical line"
  else
    record_fail "review case: -update preserves duplicate targets on one physical line"
    cat "${update_out}" >&2
  fi
  count="$(rg -c '^LINE docs/public/languages/line.md go/internal/example.go:3$' "${baseline}")"
  if [[ "${count}" -eq 2 ]]; then
    record_pass "review case: same-line duplicate target emits two LINE multiset rows"
  else
    record_fail "review case: same-line duplicate target emits two LINE multiset rows (got ${count})"
  fi
}

test_newline_path_fails_closed_in_check_and_update() {
  local root="${tmp_root}/line-newline-path"
  local check_out="${tmp_root}/line-newline-path-check.out"
  local update_out="${tmp_root}/line-newline-path-update.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt"
  local before="${tmp_root}/line-newline-path.before"
  local rel=$'docs/public/languages/new\nline.md'
  local path="${root}/${rel}"

  write_plain_go "${root}" "go/internal/example.go"
  mkdir -p "$(dirname "${path}")" "${root}/scripts"
  printf 'Raw citation: `go/internal/example.go:3`.\n' >"${path}"
  printf '# stable empty ledger\n' >"${baseline}"
  git -C "${root}" init -q
  git -C "${root}" add go scripts/docs-citations-baseline.txt "${rel}"
  cp "${baseline}" "${before}"

  if run_verifier "${root}" "${check_out}"; then
    record_fail "review case: a tracked newline path fails check mode (verifier exited zero)"
  else
    record_pass "review case: a tracked newline path fails check mode"
  fi
  assert_contains "file path contains a newline" "${check_out}" \
    "review case: newline-path check names the fail-closed reason"
  if run_verifier "${root}" "${update_out}" -update; then
    record_fail "review case: -update rejects a tracked newline path (verifier exited zero)"
  else
    record_pass "review case: -update rejects a tracked newline path"
  fi
  assert_contains "file path contains a newline" "${update_out}" \
    "review case: newline-path update names the fail-closed reason"
  if cmp -s "${before}" "${baseline}"; then
    record_pass "review case: rejected newline path leaves the ledger byte-identical"
  else
    record_fail "review case: rejected newline path leaves the ledger byte-identical"
  fi
}

test_baseline_validation_has_bounded_awk_processes() {
  local bin="${tmp_root}/line-bounded-baseline-bin"
  local real_awk small_count='' large_count='' label rows
  local root out count_file baseline doc count index

  mkdir -p "${bin}"
  real_awk="$(command -v awk)"
  {
    printf '%s\n' '#!/usr/bin/env bash'
    printf '%s\n' 'printf x >>"${AWK_COUNT_FILE:?}"'
    printf '%s\n' 'exec "${REAL_AWK:?}" "$@"'
  } >"${bin}/awk"
  chmod +x "${bin}/awk"

  for label in small large; do
    [[ "${label}" == "small" ]] && rows=1 || rows=80
    root="${tmp_root}/line-bounded-baseline-${label}"
    out="${tmp_root}/line-bounded-baseline-${label}.out"
    count_file="${tmp_root}/line-bounded-baseline-${label}.count"
    baseline="${root}/scripts/docs-citations-baseline.txt"
    write_doc "${root}" "line.md" '# Baselined missing fixtures'
    doc="${root}/docs/public/languages/line.md"
    mkdir -p "${root}/scripts"
    for index in $(seq 1 "${rows}"); do
      printf 'Missing: `testdata/missing-%03d.json`.\n' "${index}" >>"${doc}"
      printf 'FIXTURE testdata/missing-%03d.json\n' "${index}" >>"${baseline}"
    done
    if ! PATH="${bin}:${PATH}" AWK_COUNT_FILE="${count_file}" REAL_AWK="${real_awk}" \
      run_verifier "${root}" "${out}"; then
      record_fail "review case: ${label} valid baseline passes under awk instrumentation"
      cat "${out}" >&2
      return
    fi
    assert_contains 'verify-doc-citations: OK' "${out}" \
      "review case: ${label} valid baseline passes under awk instrumentation"
    count="$(wc -c <"${count_file}" | tr -d ' ')"
    [[ "${label}" == "small" ]] && small_count="${count}" || large_count="${count}"
  done
  if [[ "${small_count}" -eq "${large_count}" && "${large_count}" -le 24 ]]; then
    record_pass "review case: baseline validation keeps awk process count row-independent (${small_count})"
  else
    record_fail "review case: baseline validation keeps awk process count row-independent (small=${small_count}, large=${large_count})"
  fi
}

test_baseline_malformed_diagnostics_are_exact() {
  local root="${tmp_root}/line-malformed-diagnostics"
  local out="${tmp_root}/line-malformed-diagnostics.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt" record expected label
  write_doc "${root}" "line.md" '# Malformed ledger' 'No citations here.'
  mkdir -p "${root}/scripts"
  while IFS='|' read -r label record expected; do
    printf '%s' "${record}" >"${baseline}"
    if run_verifier "${root}" "${out}"; then
      record_fail "review case: ${label} baseline row fails closed"
    else
      record_pass "review case: ${label} baseline row fails closed"
    fi
    assert_contains "verify-doc-citations: baseline malformed at line 1: ${expected}, got: ${record}" \
      "${out}" "review case: ${label} baseline diagnostic is exact"
  done <<'CASES'
TEST fields|TEST only-two-fields|expected "TEST <doc-relpath> <citation>"
FIXTURE fields|FIXTURE testdata/a extra|expected "FIXTURE <fixture-path>"
LINE fields|LINE docs/a.md|expected "LINE <source-relpath> <citation>"
unknown final line|UNKNOWN value|unknown record kind
CASES
}

run_line_citation_review_cases() {
  test_reworded_same_pair_cannot_reuse_base_authority
  test_byte_identical_same_line_move_is_allowed
  test_force_added_ignored_hidden_files_are_scanned
  test_query_string_line_permalinks_follow_ref_contract
  test_public_gate_name_states_recurrence_scope
  test_bounded_batches_scan_later_files
  test_later_batch_scanner_failure_fails_closed
  test_nul_bearing_raw_citation_file_fails_closed
  test_nul_bearing_permalink_file_fails_closed
  test_duplicate_targets_on_one_line_preserve_multiplicity
  test_newline_path_fails_closed_in_check_and_update
  test_baseline_validation_has_bounded_awk_processes
  test_baseline_malformed_diagnostics_are_exact
}
