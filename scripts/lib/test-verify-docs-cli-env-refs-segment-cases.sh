#!/usr/bin/env bash
#
# Piped, chained, and diagnostic-scope cases for
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
    'eshu docs verify $(echo --not-a-real-flag-in-plain-subst )' \
    '(eshu docs verify --json ; eshu docs verify --not-a-real-flag-in-subshell)' \
    '| eshu docs verify --not-a-real-flag-after-leading-pipe' \
    'eshu docs verify --not-a-real-flag-before-trailing-pipe |' \
    '```'
  : >"${baseline}"
  if ESHU_TEST_PINNED_SKIPPED=9 run_verifier "${root}" "${baseline}" "${out}"; then
    record_pass "unsupported shell forms stay outside the gate's scope"
  else
    record_fail "unsupported shell forms stay outside the gate's scope"
    sed -n '1,160p' "${out}" >&2
  fi
  # A substitution with no list operator on the line is the shape the earlier
  # fixtures missed: it reached the single-command path, and --fake-flag was
  # resolved against the command `docs verify $(echo`.
  assert_absent "--not-a-real-flag-in-plain-subst" "${out}" \
    "a substitution with no list operator stays out of scope"
  # The positive half of the same claim. Exiting 0 proves only that nothing was
  # reported; the count proves the nine lines were SEEN and deliberately
  # declined, not that the scanner stopped reading the fence.
  assert_output_line \
    '^docs-cli-env-refs: 0 Eshu command segment\(s\) attributed, 9 Eshu command line\(s\) skipped as unsupported shell forms$' \
    "${out}" "every unsupported line is counted, not silently dropped"
}

# test_escaped_quote_does_not_hide_a_later_flag is the PR #6239 review case, at
# the gate rather than in a unit test. A `\"` does not close a double-quoted
# word, so the pipe behind it is not a segment boundary. A scanner that closes
# the quote there splits the line, the tail no longer starts with `eshu`, and
# every flag on it is never checked -- the gate exits 0 on a stale flag.
test_escaped_quote_does_not_hide_a_later_flag() {
  local root="${tmp_root}/escaped-quote/docs/public"
  local baseline="${tmp_root}/escaped-quote/baseline.txt"
  local out="${tmp_root}/escaped-quote.out"
  write_doc "${root}" "escaped-quote.md" \
    '```bash' \
    'eshu docs verify "a\"|b" --not-a-real-flag-behind-escaped-quote' \
    'eshu docs verify "a\\" | eshu graph status --not-a-real-flag-after-real-pipe' \
    '```'
  : >"${baseline}"
  if run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "a flag behind an escaped quote is still checked"
  else
    record_pass "a flag behind an escaped quote is still checked"
  fi
  assert_output_line \
    '^docs-cli-env-refs: escaped-quote\.md cites unknown flag --not-a-real-flag-behind-escaped-quote on command .eshu docs verify. \(not in .*\)$' \
    "${out}" "an escaped quote keeps the pipe inside one command segment"
  # The escaped backslash consumes only itself, so THIS closing quote really
  # closes and the pipe after it really is a boundary.
  assert_output_line \
    '^docs-cli-env-refs: escaped-quote\.md cites unknown flag --not-a-real-flag-after-real-pipe on command .eshu graph status. \(not in .*\)$' \
    "${out}" "an escaped backslash still lets the closing quote close"
  assert_output_line \
    '^docs-cli-env-refs: 3 Eshu command segment\(s\) attributed, 0 Eshu command line\(s\) skipped as unsupported shell forms$' \
    "${out}" "both escaped-quote lines stay inside the attributed population"
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

# test_root_flag_and_env_diagnostics_name_their_scope pins the two branches of
# the #6108 command-scope suffix that no other case reaches: a root-level flag
# names the bare binary, and an environment reference carries no command scope
# at all. Both assertions are whole-line -- a substring assertion on the flag or
# the variable passes no matter what scope is appended after it, which is how a
# wrong suffix could ship through this suite.
test_root_flag_and_env_diagnostics_name_their_scope() {
  local root="${tmp_root}/root-scope/docs/public"
  local baseline="${tmp_root}/root-scope/baseline.txt"
  local out="${tmp_root}/root-scope.out"
  write_doc "${root}" "root-scope.md" \
    'Use `ESHU_NOT_REGISTERED_ROOT`.' \
    '```bash' \
    'eshu --not-a-real-root-flag' \
    '```'
  : >"${baseline}"
  if run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "an unknown root-level flag fails the gate"
  else
    record_pass "an unknown root-level flag fails the gate"
  fi
  assert_output_line \
    '^docs-cli-env-refs: root-scope\.md cites unknown flag --not-a-real-root-flag on command .eshu. \(not in .*\)$' \
    "${out}" "a root-level flag is scoped to the bare eshu command"
  assert_output_line \
    '^docs-cli-env-refs: root-scope\.md cites unknown env ESHU_NOT_REGISTERED_ROOT \(not in .*\)$' \
    "${out}" "an environment reference carries no command scope"
}

# test_env_and_sudo_prefixed_commands_are_attributed covers #6230. A segment
# that reaches `eshu` only after a `NAME=value` assignment or a `sudo` used to
# fall out of BOTH populations: `fields[0] == "eshu"` was false so nothing was
# attributed, and the line carried no list operator so it never reached the
# skipped count either. Every flag on it was unchecked AND invisible in the
# summary, which is the exact failure the two counters exist to prevent.
#
# Every assertion here is whole-line: the prefix is stripped for attribution
# only, so the diagnostic must still name the eshu subcommand that owns the
# flag rather than `eshu sudo` or a command built from the assignment.
test_env_and_sudo_prefixed_commands_are_attributed() {
  local root="${tmp_root}/prefixed/docs/public"
  local baseline="${tmp_root}/prefixed/baseline.txt"
  local out="${tmp_root}/prefixed.out"
  write_doc "${root}" "prefixed.md" \
    '```bash' \
    'ESHU_PPROF_ADDR=127.0.0.1:0 eshu docs verify --totally-not-a-real-flag' \
    'sudo eshu docs verify --also-not-a-real-flag' \
    'sudo ESHU_PPROF_ADDR=127.0.0.1:0 eshu docs verify --sudo-then-env-invalid' \
    'ESHU_PPROF_ADDR=127.0.0.1:0 sudo eshu docs verify --env-then-sudo-invalid' \
    'eshu docs verify --json | sudo eshu graph status --after-sudo-in-pipe-invalid' \
    '```'
  # Precision guard: `sudo` is stripped, not treated as a synonym for `eshu`.
  # A line whose real command is something else must stay out of scope even
  # when the word `eshu` appears later on it.
  write_doc "${root}" "not-eshu.md" \
    '```bash' \
    'sudo docker compose logs eshu --not-an-eshu-flag' \
    'ESHU_PPROF_ADDR=127.0.0.1:0 docker compose up --not-an-eshu-flag-either' \
    '```'
  : >"${baseline}"
  if run_verifier "${root}" "${baseline}" "${out}"; then
    record_fail "env- and sudo-prefixed eshu commands are checked"
  else
    record_pass "env- and sudo-prefixed eshu commands are checked"
  fi
  for prefixed_case in totally-not-a-real-flag also-not-a-real-flag \
    sudo-then-env-invalid env-then-sudo-invalid; do
    assert_output_line \
      "^docs-cli-env-refs: prefixed\\.md cites unknown flag --${prefixed_case} on command .eshu docs verify. \\(not in .*\\)$" \
      "${out}" "prefixed segment is attributed to eshu docs verify (--${prefixed_case})"
  done
  assert_output_line \
    '^docs-cli-env-refs: prefixed\.md cites unknown flag --after-sudo-in-pipe-invalid on command .eshu graph status. \(not in .*\)$' \
    "${out}" "a sudo prefix inside a pipeline keeps its own segment's command"
  assert_absent "not-eshu.md" "${out}" \
    "a prefix in front of a non-Eshu command stays out of scope"
  # Six segments: four prefixed single commands plus both halves of the
  # pipeline. Zero skipped, because the whole point is that these lines are now
  # inside the grammar rather than sitting in the blind spot between the two
  # populations.
  assert_output_line \
    '^docs-cli-env-refs: 6 Eshu command segment\(s\) attributed, 0 Eshu command line\(s\) skipped as unsupported shell forms$' \
    "${out}" "prefixed segments join the attributed population"
}
