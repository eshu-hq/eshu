#!/usr/bin/env bash
#
# Piped and chained command-segment cases for
# scripts/test-verify-docs-cli-env-refs.sh (#6108). Sourced by that suite, which
# owns tmp_root, run_verifier, write_doc, record_pass/record_fail, and the
# assert_* helpers these cases call.

# test_pipeline_and_chain_segments_are_checked_per_command covers #6108. Each
# assertion pins the FULL diagnostic line so a splitter that attributes a flag to
# the wrong segment fails here instead of passing on the document name alone.
test_pipeline_and_chain_segments_are_checked_per_command() {
  local root="${tmp_root}/segments/docs/public"
  local baseline="${tmp_root}/segments/baseline.txt"
  local out="${tmp_root}/segments.out"
  write_doc "${root}" "segments.md" \
    '```bash' \
    'eshu docs verify --json | eshu definitely-not-a-command --not-a-real-flag' \
    'eshu docs verify --json && eshu graph status --unknown-after-and' \
    'eshu docs verify --json ; eshu graph status --unknown-after-semicolon' \
    'cat input.json | eshu service-report --not-a-real-pipe-flag' \
    '```'
  # --report-out is a real `eshu first-run` flag and is NOT an
  # `eshu first-run-benchmark` flag. A splitter that folds later flags into the
  # first command resolves it against first-run and this fixture passes.
  write_doc "${root}" "hostile-collision.md" \
    '```bash' \
    'eshu first-run --json | eshu first-run-benchmark --report-out /tmp/first-run.md' \
    '```'
  # The example named in #6108: --json belongs to first-run and --path belongs to
  # first-run-benchmark, so folding the segments together reports a false failure.
  write_doc "${root}" "valid-pipeline.md" \
    '```bash' \
    'eshu first-run --json | eshu first-run-benchmark --path local_binary' \
    '```'
  : >"${baseline}"
  if run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "piped and chained segments are checked per command"
  else
    record_pass "piped and chained segments are checked per command"
  fi
  assert_output_line \
    '^docs-cli-env-refs: segments\.md cites unknown flag --not-a-real-flag on command .eshu definitely-not-a-command. \(not in .*\)$' \
    "${out}" "pipeline segment keeps its own unknown command"
  assert_output_line \
    '^docs-cli-env-refs: segments\.md cites unknown flag --unknown-after-and on command .eshu graph status. \(not in .*\)$' \
    "${out}" "AND-list segment keeps its own command"
  assert_output_line \
    '^docs-cli-env-refs: segments\.md cites unknown flag --unknown-after-semicolon on command .eshu graph status. \(not in .*\)$' \
    "${out}" "semicolon-list segment keeps its own command"
  assert_output_line \
    '^docs-cli-env-refs: segments\.md cites unknown flag --not-a-real-pipe-flag on command .eshu service-report. \(not in .*\)$' \
    "${out}" "Eshu segment after a non-Eshu pipeline stage is checked"
  assert_output_line \
    '^docs-cli-env-refs: hostile-collision\.md cites unknown flag --report-out on command .eshu first-run-benchmark. \(not in .*\)$' \
    "${out}" "hostile collision blames the segment that owns the flag"
  assert_absent 'on command `eshu first-run`' "${out}" \
    "hostile collision does not misattribute the later flag to the earlier command"
  assert_absent "valid-pipeline.md" "${out}" \
    "a pipeline whose segments each own their flags stays green"
  # Every line in this fixture is inside the supported grammar, so NONE may be
  # counted as skipped. A scanner that quietly stopped parsing fences would also
  # emit no diagnostics; only the zero here tells the two apart.
  # Both numbers on one line: eleven segments attributed across the three
  # fixtures and nothing skipped. The skip count alone cannot tell a clean run
  # from a scanner that read no fences; the attributed count is its denominator.
  assert_output_line \
    '^docs-cli-env-refs: 11 Eshu command segment\(s\) attributed, 0 Eshu command line\(s\) skipped as unsupported shell forms$' \
    "${out}" "supported segments are attributed and none is silently skipped"
}

# test_unsupported_shell_forms_stay_skipped pins the deliberate
# under-approximation: forms outside the documented simple-list grammar keep the
# pre-#6108 skip instead of being guessed at.
test_unsupported_shell_forms_stay_skipped() {
  local root="${tmp_root}/unsupported/docs/public"
  local baseline="${tmp_root}/unsupported/baseline.txt"
  local out="${tmp_root}/unsupported.out"
  write_doc "${root}" "unsupported.md" \
    '```bash' \
    'eshu docs verify --json || eshu docs verify --not-a-real-flag-after-or' \
    'eshu docs verify --json & eshu docs verify --not-a-real-flag-after-background' \
    'eshu docs verify --json ;; eshu docs verify --not-a-real-flag-after-case' \
    'eshu docs verify --json 2>&1 ; eshu docs verify --not-a-real-flag-after-redirect' \
    'eshu docs verify --json $(echo a | eshu docs verify --not-a-real-flag-in-subst)' \
    '(eshu docs verify --json ; eshu docs verify --not-a-real-flag-in-subshell)' \
    '| eshu docs verify --not-a-real-flag-after-leading-pipe' \
    'eshu docs verify --not-a-real-flag-before-trailing-pipe |' \
    '```'
  : >"${baseline}"
  if ESHU_TEST_PINNED_SKIPPED=8 run_verifier "${root}" "${baseline}" "${out}"; then
    record_pass "unsupported shell forms stay outside the gate's scope"
  else
    record_fail "unsupported shell forms stay outside the gate's scope"
    sed -n '1,160p' "${out}" >&2
  fi
  # The positive half of the same claim. Exiting 0 proves only that nothing was
  # reported; the count proves the eight lines were SEEN and deliberately
  # declined, not that the scanner stopped reading the fence.
  assert_output_line \
    '^docs-cli-env-refs: 0 Eshu command segment\(s\) attributed, 8 Eshu command line\(s\) skipped as unsupported shell forms$' \
    "${out}" "every unsupported line is counted, not silently dropped"
}

# test_scan_coverage_pins_are_enforced proves the coverage numbers are asserted,
# not printed. Each case moves one number and expects the gate to fail, so a
# summary line nobody checks cannot pass for a gate.
test_scan_coverage_pins_are_enforced() {
  local root="${tmp_root}/coverage/docs/public"
  local baseline="${tmp_root}/coverage/baseline.txt"
  local out="${tmp_root}/coverage.out"
  # Every flag here is real, so the ONLY thing that can fail is the coverage
  # assertion itself.
  write_doc "${root}" "supported.md" \
    '```bash' \
    'eshu docs verify --json | eshu graph status --workspace-root /repo' \
    '```'
  write_doc "${root}" "unsupported.md" \
    '```bash' \
    'eshu docs verify --json || eshu docs verify --json' \
    '```'
  : >"${baseline}"

  if ESHU_TEST_PINNED_SKIPPED=1 ESHU_TEST_MIN_ATTRIBUTED=2 \
    run_verifier "${root}" "${baseline}" "${out}"; then
    record_pass "the pinned coverage shape passes"
  else
    record_fail "the pinned coverage shape passes"
    sed -n '1,160p' "${out}" >&2
  fi
  assert_output_line \
    '^docs-cli-env-refs: 2 Eshu command segment\(s\) attributed, 1 Eshu command line\(s\) skipped as unsupported shell forms$' \
    "${out}" "coverage summary reports both populations"

  if ESHU_TEST_PINNED_SKIPPED=0 run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "a grown skip population fails the gate"
  else
    record_pass "a grown skip population fails the gate"
  fi
  assert_output_line \
    '^docs-cli-env-refs: skipped Eshu command lines \(unsupported shell forms\) grew from its pinned count of 0 to 1: .*$' \
    "${out}" "skip growth names the pinned count and what to do"

  if ESHU_TEST_PINNED_SKIPPED=2 run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "an unre-pinned skip shrink fails the gate"
  else
    record_pass "an unre-pinned skip shrink fails the gate"
  fi
  assert_output_line \
    '^docs-cli-env-refs: skipped Eshu command lines \(unsupported shell forms\) shrank from its pinned count of 2 to 1: re-pin .*$' \
    "${out}" "skip shrink demands a re-pin instead of passing quietly"

  if ESHU_TEST_PINNED_SKIPPED=1 ESHU_TEST_MIN_ATTRIBUTED=999 \
    run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "collapsed attribution coverage fails the gate"
  else
    record_pass "collapsed attribution coverage fails the gate"
  fi
  assert_output_line \
    '^docs-cli-env-refs: scanner attributed only 2 Eshu command segment\(s\), below the code-owned floor of 999: .*$' \
    "${out}" "coverage collapse is named as a collapse, not a clean run"
}
