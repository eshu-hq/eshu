#!/usr/bin/env bash
# Run one lane of the value-flow cloud sink conformance cases and check that
# they ran and passed (#6192, #6690).
#
# History. The value-flow cloud sink loader used to run one statement that
# returned zero rows on NornicDB. Its conformance pair sat behind an opt-in, and
# this gate ran it with the expectation inverted: the NornicDB lane had to fail
# naming the case, the Neo4j lane had to pass. #6690 replaced that statement with
# two statements and a Go-side single-workload check. Both statements return the
# exact expected rows on the pinned NornicDB v1.3.3 image and on Neo4j, and both
# now sit in the default conformance corpus with exact-row assertions. So both
# lanes are now expected to PASS.
#
# The gate still exists because required-gates-complete awaits it for its
# triggers through the default branch's registry; retiring it takes a
# registry-only change first and the workflow deletion after. Until then it is a
# focused positive check: both backends must pass the live corpus, and the run
# must show that the two value-flow cases actually ran.
#
# It checks the run, not only the exit code. A green run that never executed the
# value-flow cases would prove nothing about them, so the lane only counts when
# TestLiveBackendConformance logs "read case passed: <name>" for both cases.
#
# Usage:
#   scripts/verify-value-flow-conformance-expectation.sh nornicdb
#   scripts/verify-value-flow-conformance-expectation.sh neo4j
#
# Both lanes talk to bolt://localhost:7687 by default, so they cannot share a
# machine at the same time. Run one, tear its stack down, then run the other.
#
# The optional second argument overrides the live-conformance driver. It exists
# for the test mirror, scripts/test-verify-value-flow-conformance-expectation.sh,
# which drives this script with stub lanes to prove every verdict is reachable
# without a Bolt endpoint. Callers with a live backend should never pass it.
#
# Exit codes:
#   0 — the lane passed and ran both value-flow cases.
#   1 — it did not, and the message says which way.
#   2 — usage error.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# The markers TestLiveBackendConformance logs for the two value-flow read cases
# (go/internal/backendconformance/corpus_value_flow.go). Matched as fixed
# strings: a pattern that quietly stopped matching would read as "never ran".
required_markers=(
	'read case passed: value-flow cloud action workload rows'
	'read case passed: value-flow cloud sink targets by pair'
)

fail() {
	printf 'verify-value-flow-conformance-expectation: %s\n' "$*" >&2
	exit 1
}

usage() {
	printf 'usage: %s <nornicdb|neo4j> [driver]\n' "${BASH_SOURCE[0]}" >&2
	exit 2
}

lane="${1:-}"
driver="${2:-${repo_root}/scripts/verify_backend_conformance_live.sh}"

case "${lane}" in
	nornicdb | neo4j) ;;
	*) usage ;;
esac

[[ -x "${driver}" ]] || fail "driver is not executable: ${driver}"

log_file="$(mktemp "${TMPDIR:-/tmp}/value-flow-expectation.XXXXXX")"
trap 'rm -f "${log_file}"' EXIT

printf '== value-flow conformance: %s lane ==\n' "${lane}"

# Capture the driver's own status directly. Reading $? after a pipe reports the
# pipe's last stage instead, which is how a failing gate comes to read as
# exit 0.
set +e
ESHU_GRAPH_BACKEND="${lane}" "${driver}" > "${log_file}" 2>&1
observed_exit=$?
set -e

cat "${log_file}"
printf '\n%s lane: observed exit code %d\n' "${lane}" "${observed_exit}"

if [[ "${observed_exit}" -ne 0 ]]; then
	fail "${lane} lane FAILED (exit ${observed_exit}).
  Since #6690 the value-flow cloud sink statements return their exact rows on
  both the pinned NornicDB image and Neo4j, so a red lane is a regression, a
  broken fixture, or an environment failure. Read the run output above; a
  read case that returned the wrong rows prints both the returned and the
  expected rows."
fi

for marker in "${required_markers[@]}"; do
	rg --fixed-strings --quiet -- "${marker}" "${log_file}" ||
		fail "${lane} lane passed WITHOUT running a value-flow case: no \"${marker}\" line.
  A green run that never executed the case proves nothing about it. Check that
  the case is still in DefaultReadCorpus and that TestLiveBackendConformance
  still logs each passing read case."
done

printf '%s lane: passed, and both value-flow cases ran.\n' "${lane}"
