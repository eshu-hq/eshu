#!/usr/bin/env bash
# Workflow-artifact citation cases (#9315) for test-verify-doc-citations.sh.
# Sourced by it, which owns scratch cleanup and the run_verifier/write_doc
# helpers. These cases run in the fixtures mode via run_basic_fixture_cases.

# A well-formed run-id/name pointer and a full run URL satisfy the gate with
# no committed file and no baseline row.
test_artifact_citation_passes() {
  local root="${tmp_root}/artifact-pass" out="${tmp_root}/artifact-pass.out"
  write_doc "${root}" "example.md" \
    '# Example' '' \
    'Scale proof: `artifact:39421484741/freshness-scale`.' \
    'Matrix proof: `artifact:https://github.com/eshu-hq/eshu/actions/runs/39421484741`.'
  if run_verifier "${root}" "${out}"; then
    record_pass "artifact: well-formed run pointers pass"
  else
    record_fail "artifact: well-formed run pointers pass (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}

# A run id with no artifact name is a malformed pointer and must fail,
# naming the expected shape. Never baseline-able, like subtest citations.
test_malformed_artifact_citation_fails() {
  local root="${tmp_root}/artifact-malformed" out="${tmp_root}/artifact-malformed.out"
  write_doc "${root}" "example.md" \
    '# Example' '' \
    'Scale proof: `artifact:39421484741`.'
  if run_verifier "${root}" "${out}"; then
    record_fail "artifact: bare run id fails (verifier exited zero)"
  else
    record_pass "artifact: bare run id fails"
  fi
  assert_contains "malformed workflow artifact" "${out}" "artifact: names the malformed pointer"
}

# `artifact:<GoIdentifier>` prose (the established anchor-doc convention)
# is not a citation attempt and must keep passing.
test_artifact_identifier_prose_ignored() {
  local root="${tmp_root}/artifact-prose" out="${tmp_root}/artifact-prose.out"
  write_doc "${root}" "example.md" \
    '# Example' '' \
    'The anchor carries artifact:EvidenceArtifact for the deployment edge.'
  if run_verifier "${root}" "${out}"; then
    record_pass "artifact: identifier prose is ignored"
  else
    record_fail "artifact: identifier prose is ignored (verifier exited non-zero)"
    cat "${out}" >&2
  fi
}
