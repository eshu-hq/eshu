#!/usr/bin/env bash
# Hostile cases for raw Go file-and-line citations (#6383). Sourced by
# scripts/test-verify-doc-citations.sh after its shared helpers are defined.

write_plain_go() {
  local root="$1" rel="$2"
  local file="${root}/${rel}"
  mkdir -p "$(dirname "${file}")"
  printf 'package scratch\n\nconst Evidence = true\n' >"${file}"
}

test_in_range_line_citation_fails() {
  local root="${tmp_root}/line-in-range" out="${tmp_root}/line-in-range.out"
  write_plain_go "${root}" "go/internal/example.go"
  write_doc "${root}" "line.md" \
    '# Line citation' '' \
    'This in-range pointer is still unstable: `go/internal/example.go:2`.'
  if run_verifier "${root}" "${out}"; then
    record_fail "line case: an unbaselined in-range pointer fails (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "line case: an unbaselined in-range pointer fails"
  fi
  assert_contains "unstable line citation" "${out}" \
    "line case: failure explains that range validity is insufficient"
}

test_underscore_digit_line_citation_fails() {
  local root="${tmp_root}/line-path-shape" out="${tmp_root}/line-path-shape.out"
  write_plain_go "${root}" "go/internal/example_2.go"
  write_doc "${root}" "line.md" \
    '# Line citation' '' \
    'This filename escaped the issue census: `go/internal/example_2.go:3`.'
  if run_verifier "${root}" "${out}"; then
    record_fail "line case: filenames containing _ and digits are scanned (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "line case: filenames containing _ and digits are scanned"
  fi
  assert_contains "go/internal/example_2.go:3" "${out}" \
    "line case: failure names the full path shape"
}

test_stale_line_baseline_fails() {
  local root="${tmp_root}/line-stale" out="${tmp_root}/line-stale.out"
  write_doc "${root}" "line.md" '# No raw line citations remain.'
  mkdir -p "${root}/scripts"
  printf 'LINE docs/public/languages/line.md go/internal/gone_2.go:9\n' \
    >"${root}/scripts/docs-citations-baseline.txt"
  if run_verifier "${root}" "${out}"; then
    record_fail "line case: stale LINE debt records fail (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "line case: stale LINE debt records fail"
  fi
  assert_contains "stale LINE baseline" "${out}" \
    "line case: failure names the stale debt record"
}

test_duplicate_line_citation_in_same_source_fails() {
  local root="${tmp_root}/line-duplicate" out="${tmp_root}/line-duplicate.out"
  local doc="${root}/docs/public/languages/line.md"
  write_plain_go "${root}" "go/internal/example.go"
  write_doc "${root}" "line.md" \
    '# Line citation' '' \
    'Existing debt: `go/internal/example.go:3`.'
  mkdir -p "${root}/scripts"
  printf 'LINE docs/public/languages/line.md go/internal/example.go:3\n' \
    >"${root}/scripts/docs-citations-baseline.txt"
  printf 'Repeated debt: `go/internal/example.go:3`.\n' >>"${doc}"
  if run_verifier "${root}" "${out}"; then
    record_fail "line case: duplicate occurrences in one source fail (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "line case: duplicate occurrences in one source fail"
  fi
  assert_contains "line citation multiplicity changed" "${out}" \
    "line case: failure names the source/target count change"
}

test_update_refuses_new_line_debt() {
  local root="${tmp_root}/line-update-growth" out="${tmp_root}/line-update-growth.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt"
  local before="${tmp_root}/line-update-growth.before"
  local doc="${root}/docs/public/languages/line.md"
  write_plain_go "${root}" "go/internal/example.go"
  write_doc "${root}" "line.md" \
    '# Line citation' '' \
    'Approved debt: `go/internal/example.go:3`.'
  mkdir -p "${root}/scripts"
  printf 'LINE docs/public/languages/line.md go/internal/example.go:3\n' \
    >"${baseline}"
  cp "${baseline}" "${before}"
  printf 'New debt: `go/internal/example.go:3`.\n' >>"${doc}"

  if run_verifier "${root}" "${out}" -update; then
    record_fail "line case: -update refuses increased LINE debt (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "line case: -update refuses increased LINE debt"
  fi
  assert_contains "refusing -update: LINE debt may only decrease" "${out}" \
    "line case: rejection comes from the monotonic LINE guard"
  if cmp -s "${before}" "${baseline}"; then
    record_pass "line case: rejected -update leaves the baseline byte-identical"
  else
    record_fail "line case: rejected -update leaves the baseline byte-identical"
  fi
}

test_update_allows_line_debt_reduction() {
  local root="${tmp_root}/line-update-reduction" out="${tmp_root}/line-update-reduction.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt"
  local after="${tmp_root}/line-update-reduction.after"
  write_plain_go "${root}" "go/internal/example.go"
  write_doc "${root}" "line.md" \
    '# Line citation' '' \
    'Remaining debt: `go/internal/example.go:3`.'
  mkdir -p "${root}/scripts"
  printf '%s\n' \
    'LINE docs/public/languages/line.md go/internal/example.go:3' \
    'LINE docs/public/languages/line.md go/internal/example.go:3' \
    >"${baseline}"

  if run_verifier "${root}" "${out}" -update; then
    record_pass "line case: -update allows LINE debt reduction"
  else
    record_fail "line case: -update allows LINE debt reduction"
    cat "${out}" >&2
  fi
  if [[ "$(rg -c '^LINE ' "${baseline}")" -eq 1 ]]; then
    record_pass "line case: reduction rewrites the exact remaining multiplicity"
  else
    record_fail "line case: reduction rewrites the exact remaining multiplicity"
  fi
  cp "${baseline}" "${after}"
  run_verifier "${root}" "${out}" -update
  if cmp -s "${after}" "${baseline}"; then
    record_pass "line case: reduced baseline rewrite is deterministic"
  else
    record_fail "line case: reduced baseline rewrite is deterministic"
  fi
}

test_full_sha_permalink_passes() {
  local root="${tmp_root}/line-permalink" out="${tmp_root}/line-permalink.out"
  write_doc "${root}" "line.md" \
    '# Historical evidence' '' \
    'Immutable: https://github.com/eshu-hq/eshu/blob/0123456789abcdef0123456789abcdef01234567/go/internal/example.go#L3'
  if run_verifier "${root}" "${out}"; then
    record_pass "line case: a full-SHA #L permalink is allowed"
  else
    record_fail "line case: a full-SHA #L permalink is allowed"
    cat "${out}" >&2
  fi
}

test_mutable_or_short_permalink_fails() {
  local root="${tmp_root}/line-mutable-permalink"
  local mutable_out="${tmp_root}/line-mutable-permalink.out"
  local short_out="${tmp_root}/line-short-permalink.out"
  write_doc "${root}" "mutable.md" \
    '# Mutable evidence' '' \
    'Mutable: https://github.com/eshu-hq/eshu/blob/main/go/internal/example.go#L3'
  if run_verifier "${root}" "${mutable_out}"; then
    record_fail "line case: a mutable-ref #L permalink fails (verifier exited zero)"
    cat "${mutable_out}" >&2
  else
    record_pass "line case: a mutable-ref #L permalink fails"
  fi
  assert_contains "permalink must use a full 40-hex commit SHA" "${mutable_out}" \
    "line case: mutable-ref failure explains the immutable-ref contract"

  write_doc "${root}" "short.md" \
    '# Ambiguous evidence' '' \
    'Short ref: https://github.com/eshu-hq/eshu/blob/deadbee/go/internal/example.go#L3'
  if run_verifier "${root}" "${short_out}"; then
    record_fail "line case: a short-ref #L permalink fails (verifier exited zero)"
    cat "${short_out}" >&2
  else
    record_pass "line case: a short-ref #L permalink fails"
  fi
  assert_contains "permalink must use a full 40-hex commit SHA" "${short_out}" \
    "line case: short-ref failure explains the immutable-ref contract"
}

test_branch_cannot_self_authorize_line_debt() {
  local root="${tmp_root}/line-immutable-base"
  local check_out="${tmp_root}/line-immutable-base-check.out"
  local update_out="${tmp_root}/line-immutable-base-update.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt"
  local before="${tmp_root}/line-immutable-base.before" base

  write_plain_go "${root}" "go/internal/example.go"
  write_doc "${root}" "line.md" \
    '# Existing debt' '' \
    'Existing: `go/internal/example.go:3`.'
  mkdir -p "${root}/scripts"
  printf 'LINE docs/public/languages/line.md go/internal/example.go:3\n' >"${baseline}"
  git -C "${root}" init -q
  git -C "${root}" config user.name 'Doc Citation Test'
  git -C "${root}" config user.email 'doc-citation-test@example.invalid'
  git -C "${root}" add .
  git -C "${root}" commit -qm 'base'
  base="$(git -C "${root}" rev-parse HEAD)"

  printf 'Branch-added: `go/internal/example.go:3`.\n' \
    >>"${root}/docs/public/languages/line.md"
  printf 'LINE docs/public/languages/line.md go/internal/example.go:3\n' >>"${baseline}"
  git -C "${root}" add .
  git -C "${root}" commit -qm 'self-authorized line debt'

  if ESHU_DOC_CITATIONS_BASE="${base}" run_verifier "${root}" "${check_out}"; then
    record_fail "line case: a branch cannot self-authorize LINE debt in check mode (verifier exited zero)"
    cat "${check_out}" >&2
  else
    record_pass "line case: a branch cannot self-authorize LINE debt in check mode"
  fi
  assert_contains "branch-added LINE debt" "${check_out}" \
    "line case: check rejection is bound to immutable base authority"

  cp "${baseline}" "${before}"
  if ESHU_DOC_CITATIONS_BASE="${base}" run_verifier "${root}" "${update_out}" -update; then
    record_fail "line case: -update cannot self-authorize branch-added LINE debt (verifier exited zero)"
    cat "${update_out}" >&2
  else
    record_pass "line case: -update cannot self-authorize branch-added LINE debt"
  fi
  assert_contains "branch-added LINE debt" "${update_out}" \
    "line case: -update rejection is bound to immutable base authority"
  if cmp -s "${before}" "${baseline}"; then
    record_pass "line case: rejected branch -update leaves the baseline byte-identical"
  else
    record_fail "line case: rejected branch -update leaves the baseline byte-identical"
  fi
}

test_update_reconciles_debt_already_present_at_base() {
  local root="${tmp_root}/line-base-reconcile"
  local check_before_out="${tmp_root}/line-base-reconcile-before.out"
  local update_out="${tmp_root}/line-base-reconcile-update.out"
  local check_after_out="${tmp_root}/line-base-reconcile-after.out"
  local baseline="${root}/scripts/docs-citations-baseline.txt" base

  write_plain_go "${root}" "go/internal/example.go"
  write_doc "${root}" "line.md" \
    '# Base citation moved before the ledger caught up' '' \
    'Base debt: `go/internal/example.go:3`.'
  mkdir -p "${root}/scripts"
  printf 'LINE docs/public/languages/line.md go/internal/example.go:2\n' >"${baseline}"
  git -C "${root}" init -q
  git -C "${root}" config user.name 'Doc Citation Test'
  git -C "${root}" config user.email 'doc-citation-test@example.invalid'
  git -C "${root}" add .
  git -C "${root}" commit -qm 'base with legacy ledger'
  base="$(git -C "${root}" rev-parse HEAD)"

  if ESHU_DOC_CITATIONS_BASE="${base}" run_verifier "${root}" "${check_before_out}"; then
    record_fail "line case: check reports a legacy ledger that disagrees with base-tree debt (verifier exited zero)"
    cat "${check_before_out}" >&2
  else
    record_pass "line case: check reports a legacy ledger that disagrees with base-tree debt"
  fi
  assert_contains "go/internal/example.go:3" "${check_before_out}" \
    "line case: pre-update check names debt already present at base"
  assert_contains "stale LINE baseline" "${check_before_out}" \
    "line case: pre-update check names the legacy ledger row"

  if ESHU_DOC_CITATIONS_BASE="${base}" run_verifier "${root}" "${update_out}" -update; then
    record_pass "line case: -update reconciles LINE debt already present at base"
  else
    record_fail "line case: -update reconciles LINE debt already present at base"
    cat "${update_out}" >&2
  fi
  if rg -q --fixed-strings \
    'LINE docs/public/languages/line.md go/internal/example.go:3' "${baseline}" &&
    ! rg -q --fixed-strings \
      'LINE docs/public/languages/line.md go/internal/example.go:2' "${baseline}"; then
    record_pass "line case: reconciliation replaces legacy A with base-authorized B"
  else
    record_fail "line case: reconciliation replaces legacy A with base-authorized B"
  fi

  if ESHU_DOC_CITATIONS_BASE="${base}" run_verifier "${root}" "${check_after_out}"; then
    record_pass "line case: reconciled base-authorized ledger passes check"
  else
    record_fail "line case: reconciled base-authorized ledger passes check"
    cat "${check_after_out}" >&2
  fi
}

test_tracked_hidden_sources_are_scanned() {
  local root="${tmp_root}/line-hidden-tracked"
  local hidden_out="${tmp_root}/line-hidden-tracked.out"
  local metadata_root="${tmp_root}/line-git-metadata"
  local metadata_out="${tmp_root}/line-git-metadata.out"

  write_plain_go "${root}" "go/internal/example.go"
  write_doc "${root}" ".hidden.md" \
    '# Hidden but tracked' '' \
    'Tracked debt: `go/internal/example.go:3`.'
  git -C "${root}" init -q
  git -C "${root}" add .
  if run_verifier "${root}" "${hidden_out}"; then
    record_fail "line case: tracked hidden docs are scanned (verifier exited zero)"
    cat "${hidden_out}" >&2
  else
    record_pass "line case: tracked hidden docs are scanned"
  fi
  assert_contains "docs/public/languages/.hidden.md" "${hidden_out}" \
    "line case: hidden-file failure names the tracked source"

  write_doc "${metadata_root}" "visible.md" '# No raw citation'
  git -C "${metadata_root}" init -q
  printf 'metadata only: go/internal/metadata.go:7\n' \
    >"${metadata_root}/.git/description"
  git -C "${metadata_root}" add .
  if run_verifier "${metadata_root}" "${metadata_out}"; then
    record_pass "line case: repository metadata is excluded from the tracked-file scan"
  else
    record_fail "line case: repository metadata is excluded from the tracked-file scan"
    cat "${metadata_out}" >&2
  fi
}

test_empty_line_scan_fails_closed() {
  local root="${tmp_root}/line-empty" out="${tmp_root}/line-empty.out"
  mkdir -p "${root}/docs/internal" "${root}/docs/public" "${root}/go/internal" "${root}/specs"
  printf 'gates: []\n' >"${root}/specs/ci-gates.v1.yaml"
  if run_verifier "${root}" "${out}"; then
    record_fail "line case: a real-tree-shaped empty scan fails closed (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "line case: a real-tree-shaped empty scan fails closed"
  fi
  assert_contains "found zero raw Go line citations" "${out}" \
    "line case: empty-scan failure is explicit"
}

test_line_scanner_error_fails_closed() {
  local root="${tmp_root}/line-rg-error" out="${tmp_root}/line-rg-error.out"
  local scanner="${root}/rg-fail"
  write_doc "${root}" "line.md" '# Scanner failure'
  mkdir -p "${root}/go/internal"
  printf '#!/bin/sh\nexit 2\n' >"${scanner}"
  chmod +x "${scanner}"
  if ESHU_DOC_CITATIONS_RG="${scanner}" run_verifier "${root}" "${out}"; then
    record_fail "line case: scanner errors fail closed (verifier exited zero)"
    cat "${out}" >&2
  else
    record_pass "line case: scanner errors fail closed"
  fi
  assert_contains "rg-fail exited 2" "${out}" \
    "line case: injected nonzero scanner failure is explicit"
}

line_trigger_contract_holds() {
  local registry="$1" workflow="$2"
  local registry_triggers="${tmp_root}/line-registry-triggers.txt"
  local workflow_triggers="${tmp_root}/line-workflow-triggers.txt"
  local failed=0 required
  awk '
    /^  - id: doc-citations$/ { in_gate = 1 }
    in_gate && /^    triggers:$/ { in_triggers = 1; next }
    in_triggers && /^    local:$/ { exit }
    in_triggers && /^      - / {
      value = $0
      sub(/^      - "/, "", value)
      sub(/"$/, "", value)
      print value
    }
  ' "${registry}" | LC_ALL=C sort >"${registry_triggers}"
  awk '
    /^            doccitations:$/ { in_gate = 1; next }
    in_gate && /^            [[:alnum:]_-]+:$/ { exit }
    in_gate && /^              - / {
      value = $0
      sub(/^              - /, "", value)
      gsub(/\047/, "", value)
      print value
    }
  ' "${workflow}" | LC_ALL=C sort >"${workflow_triggers}"
  if ! cmp -s "${registry_triggers}" "${workflow_triggers}"; then
    failed=1
  fi
  for required in 'docs/**' 'go/**' 'scripts/lib/doc-citation-lines.sh' \
    'scripts/lib/test-verify-doc-citations-line-cases.sh' \
    'scripts/lib/test-verify-doc-citations-review-cases.sh' \
    'scripts/lib/test-verify-doc-citations-binary-cases.sh' \
    'scripts/lib/test-verify-doc-citations-preparation-cases.sh' \
    'scripts/lib/gate-diff-base.sh'; do
    if ! rg -q -x --fixed-strings "${required}" "${registry_triggers}" ||
      ! rg -q -x --fixed-strings "${required}" "${workflow_triggers}"; then
      failed=1
    fi
  done
  [[ "${failed}" -eq 0 ]]
}

remove_registry_trigger() {
  local source="$1" target="$2" destination="$3"
  awk -v target="${target}" '
    /^  - id: doc-citations$/ { in_block = 1 }
    in_block && /^    local:/ { in_block = 0 }
    in_block && index($0, target) { next }
    { print }
  ' "${source}" >"${destination}"
}

remove_workflow_trigger() {
  local source="$1" target="$2" destination="$3"
  awk -v target="${target}" '
    /^            doccitations:$/ { in_block = 1; print; next }
    in_block && /^            [[:alnum:]_-]+:$/ { in_block = 0 }
    in_block && index($0, target) { next }
    { print }
  ' "${source}" >"${destination}"
}

test_line_trigger_lockstep() {
  local registry="${repo_root}/specs/ci-gates.v1.yaml"
  local workflow="${repo_root}/.github/workflows/static-contract-gates.yml"
  if line_trigger_contract_holds "${registry}" "${workflow}"; then
    record_pass "line case: registry/workflow line-citation triggers are in lockstep"
  else
    record_fail "line case: registry/workflow line-citation triggers are in lockstep"
  fi
}

test_line_trigger_contract_is_block_scoped() {
  local registry="${repo_root}/specs/ci-gates.v1.yaml"
  local workflow="${repo_root}/.github/workflows/static-contract-gates.yml"
  local target mutated_registry mutated_workflow
  for target in 'docs/**' 'go/**' \
    'scripts/lib/test-verify-doc-citations-preparation-cases.sh'; do
    mutated_registry="${tmp_root}/registry-without-${target%%/*}.yaml"
    mutated_workflow="${tmp_root}/workflow-without-${target%%/*}.yml"
    remove_registry_trigger "${registry}" "${target}" "${mutated_registry}"
    if line_trigger_contract_holds "${mutated_registry}" "${workflow}"; then
      record_fail "line case: registry doc-citations block must watch ${target}"
    else
      record_pass "line case: registry doc-citations block must watch ${target}"
    fi
    remove_workflow_trigger "${workflow}" "${target}" "${mutated_workflow}"
    if line_trigger_contract_holds "${registry}" "${mutated_workflow}"; then
      record_fail "line case: workflow doccitations block must watch ${target}"
    else
      record_pass "line case: workflow doccitations block must watch ${target}"
    fi
  done
}

test_line_contract_help_and_registry_comment() {
  local help_out="${tmp_root}/line-help.out"
  local registry="${repo_root}/specs/ci-gates.v1.yaml"
  local registry_block="${tmp_root}/line-registry-block.txt"
  "${BASH:-bash}" "${verifier}" --help >"${help_out}" 2>&1
  assert_contains "raw Go line citations" "${help_out}" \
    "line case: usage names raw Go line citations"
  assert_contains "cannot add branch-authored" "${help_out}" \
    "line case: usage documents immutable-base reconciliation"
  awk '
    /^  - id: doc-citations$/ { in_block = 1 }
    in_block { print }
    in_block && /^  - id: / && $0 !~ /doc-citations/ { exit }
  ' "${registry}" >"${registry_block}"
  if rg -q --fixed-strings 'go/internal/**/*_test.go' "${registry_block}"; then
    record_fail "line case: registry comment describes the current broad Go/docs trigger contract"
  else
    record_pass "line case: registry comment describes the current broad Go/docs trigger contract"
  fi
}

run_line_citation_cases() {
  test_in_range_line_citation_fails
  test_underscore_digit_line_citation_fails
  test_stale_line_baseline_fails
  test_duplicate_line_citation_in_same_source_fails
  test_update_refuses_new_line_debt
  test_update_allows_line_debt_reduction
  test_full_sha_permalink_passes
  test_mutable_or_short_permalink_fails
  test_branch_cannot_self_authorize_line_debt
  test_update_reconciles_debt_already_present_at_base
  test_tracked_hidden_sources_are_scanned
  test_empty_line_scan_fails_closed
  test_line_scanner_error_fails_closed
  test_line_trigger_lockstep
  test_line_trigger_contract_is_block_scoped
  test_line_contract_help_and_registry_comment
}
