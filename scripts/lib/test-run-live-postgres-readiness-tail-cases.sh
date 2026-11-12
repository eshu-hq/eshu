#!/usr/bin/env bash
# Failure-tail cases for the PostgreSQL readiness runner mirror: the main log
# must carry the failing assertion text, not only the .readiness-debug event
# stream. Sourced by scripts/test-run-live-postgres-readiness-tests.sh, which
# defines fail, run_runner, out, events_of, first_pkg and first_proof.
# shellcheck disable=SC2154  # the sourcing test defines these variables.

# A failing test prints the TAIL of its output stream: a long test's early
# progress lines must not crowd out the assertion (#7814).
write_events
sed -i.bak "/\"Test\":\"${first_proof}\"/d" "$(events_of "${first_pkg}")"
{
  printf '{"Action":"run","Test":"%s"}\n' "${first_proof}"
  for i in $(seq 0 19); do
    printf '{"Action":"output","Test":"%s","Output":"early line %d\\n"}\n' "${first_proof}" "${i}"
  done
  printf '{"Action":"output","Test":"%s","Output":"--- FAIL: %s (40.38s)\\n"}\n' "${first_proof}" "${first_proof}"
  printf '{"Action":"output","Test":"%s","Output":"    seeded_test.go:99: SEEDED-FAILURE-MARKER\\n"}\n' "${first_proof}"
  printf '{"Action":"fail","Test":"%s","Elapsed":40.38}\n' "${first_proof}"
} >>"$(events_of "${first_pkg}")"
out="$(run_runner)" && fail "tailed failure passed"
[[ "${out}" == *"SEEDED-FAILURE-MARKER"* ]] || fail "failure tail not printed: ${out}"
[[ "${out}" != *"early line 0"* ]] || fail "early noise crowded out the failure: ${out}"

# A verify failure buries its assertion mid-stream: the assertion must still
# reach the main log, not only the .readiness-debug event stream (#10586).
# The verifier prints only the last 6 lines of its 12-line ring, so the seed
# parks the marker 11 lines above the stream end: inside the runner's bounded
# tail, outside the verifier's printed window.
write_events
sed -i.bak "/\"Test\":\"${first_proof}\"/d" "$(events_of "${first_pkg}")"
{
  printf '{"Action":"run","Test":"%s"}\n' "${first_proof}"
  printf '{"Action":"output","Test":"%s","Output":"    seeded_test.go:77: BURIED-ASSERTION-MARKER\\n"}\n' "${first_proof}"
  for i in $(seq 0 9); do
    printf '{"Action":"output","Test":"%s","Output":"trailing filler %d\\n"}\n' "${first_proof}" "${i}"
  done
  printf '{"Action":"fail","Test":"%s","Elapsed":40.38}\n' "${first_proof}"
} >>"$(events_of "${first_pkg}")"
out="$(run_runner)" && fail "buried assertion passed"
[[ "${out}" == *"BURIED-ASSERTION-MARKER"* ]] || fail "buried assertion not printed: ${out}"

tail_cases_completed=1
