#!/usr/bin/env bash
# Run one lane of the value-flow cloud sink conformance pair and check it
# against the behaviour this repository has measured and written down (#6192).
#
# The pair reproduces four NornicDB divergences that empty the production query
# valueFlowCloudSinkTargetsCypher. It is opt-in behind
# ESHU_BACKEND_CONFORMANCE_VALUE_FLOW because it fails on NornicDB by design,
# and nothing set that variable anywhere, so nothing ran the pair on its own.
# This script is what runs it, with the expectation inverted so the normal
# result is green:
#
#   nornicdb lane — expected to FAIL, naming the value-flow read case.
#   neo4j lane    — expected to PASS.
#
# Green means "still broken upstream, exactly as documented". Red means
# something changed and somebody needs to look: either upstream landed a fix
# (see "When upstream lands" below) or the fixture broke.
#
# Three things keep this honest rather than decorative.
#
# It matches the MESSAGE, not the exit code. A non-zero exit can come from a
# broken fixture, a failed seed, or a connection error, and an expected-fail
# that passes for the wrong reason is a false green wearing the costume of a
# gate. The nornicdb lane only counts when the run names the value-flow read
# case and its row shortfall.
#
# It requires that message to be the ONLY failure. TestLiveBackendConformance
# calls t.Fatalf from two deferred closures — the corpus cleanup and the driver
# close — and a defer runs after the read-corpus t.Fatalf has already recorded
# the documented failure, so both messages land in the same run. Matching the
# documented message alone would let a cleanup or close regression ride in
# behind it and still report green.
#
# The neo4j lane is the positive control, and it belongs in the same CI job as
# the nornicdb lane. Without it, a broken fixture and the backend defect the
# pair exists to detect produce the same observation: a red nornicdb lane with
# every hermetic guard green. That ambiguity was real rather than theoretical.
# An earlier recorded measurement went stale when the read case's bound
# parameter changed from function_uid to function_uids, and a wrong parameter
# key empties the result on BOTH backends.
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
#   0 — the lane behaved as documented.
#   1 — it did not, and the message says which way.
#   2 — usage error.
#
# When upstream lands
# -------------------
# The nornicdb lane going green here is the signal, and the repair is one
# change: delete valueFlowCasesEnabled and its callers in
# go/internal/backendconformance/corpus_value_flow.go so the pair joins the
# default corpora, which puts it back under the blocking e2e live-conformance
# gate on both backends. This script, its test mirror, its workflow, and its
# registry gate have nothing left to assert at that point and come out with it.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# The exact text corpus.go emits when the read case comes back empty. Matched
# as a fixed string rather than a pattern: this one observation is what the
# whole gate rests on, so a regex that quietly stopped matching would look
# exactly like the defect having been fixed.
expected_nornicdb_failure='read case "value-flow cloud sink aggregation and subscript projection" returned 0 rows, want at least 1'

# The failures that can be recorded ALONGSIDE the one above, rather than instead
# of it. Both come from a deferred closure in live_test.go — cleanupLiveCorpus
# and driver.Close — and a defer runs after the read-corpus t.Fatalf has already
# recorded the documented failure, so the run carries two. Every other t.Fatalf
# in that test sits on the straight-line path, where the first one ends the test
# and the documented message never appears at all; those are already caught by
# the needle check. The test mirror pins live_test.go's failure set so this list
# cannot quietly go stale when someone adds another deferred failure.
cooccurring_failures=(
	'cleanup live corpus fixture:'
	'close Bolt driver:'
)

# The driver prints this before it runs anything. Without it a green neo4j lane
# proves nothing, because the pair is ABSENT from the corpus when the opt-in is
# unset rather than skipped — the run would pass having never touched the case.
included_banner='value-flow cloud sink pair: INCLUDED'

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

printf '== value-flow conformance expectation: %s lane ==\n' "${lane}"

# Capture the driver's own status directly. Reading $? after a pipe reports the
# pipe's last stage instead, which is how a failing gate comes to read as
# exit 0.
set +e
ESHU_BACKEND_CONFORMANCE_VALUE_FLOW=1 \
	ESHU_GRAPH_BACKEND="${lane}" \
	"${driver}" > "${log_file}" 2>&1
observed_exit=$?
set -e

cat "${log_file}"
printf '\n%s lane: observed exit code %d\n' "${lane}" "${observed_exit}"

rg --fixed-strings --quiet -- "${included_banner}" "${log_file}" ||
	fail "${lane} lane never included the value-flow pair (no \"${included_banner}\" line).
  With the opt-in unset the pair is absent from the corpus rather than skipped,
  so this run proved nothing about the value-flow cloud sink query."

case "${lane}" in
	nornicdb)
		if [[ "${observed_exit}" -eq 0 ]]; then
			fail "nornicdb lane PASSED, and it is documented as failing.
  Upstream has most likely landed a fix for orneryd/NornicDB#297, #298, #301
  or #302. Confirm that, then take the pair off its opt-in: delete
  valueFlowCasesEnabled and its callers in
  go/internal/backendconformance/corpus_value_flow.go so the pair runs in the
  default corpora, and remove this gate with it."
		fi
		rg --fixed-strings --quiet -- "${expected_nornicdb_failure}" "${log_file}" ||
			fail "nornicdb lane failed (exit ${observed_exit}) WITHOUT naming the value-flow read case.
  Expected to find: ${expected_nornicdb_failure}
  A different failure is a broken fixture, a failed seed, or a connection error,
  not the backend divergence this gate tracks. Read the run output above."

		# The documented shape has exactly ONE failure in it. Naming the read
		# case is not enough on its own: a second failure in the same run makes
		# the red mean something else as well, and the gate would report green
		# over it.
		for marker in "${cooccurring_failures[@]}"; do
			if rg --fixed-strings --quiet -- "${marker}" "${log_file}"; then
				fail "nornicdb lane named the value-flow read case, but also recorded a second failure: \"${marker}\"
  That one comes from a deferred closure in TestLiveBackendConformance, so it
  lands in the same run as the documented failure rather than replacing it.
  Only the read-case row shortfall is expected here. Read the run output above:
  a cleanup or driver-close regression is riding in behind an expected red."
			fi
		done

		# A second FAILING TEST is the case the message check cannot see, because
		# its failure text is its own. go test prints one "--- FAIL:" line per
		# test — however many messages that test recorded — so more than one of
		# them means more than one test failed. That also makes this check blind
		# to the co-occurring messages above, which is why both exist.
		failed_tests="$(rg --count-matches '^[[:space:]]*--- FAIL: ' "${log_file}" || true)"
		if [[ "${failed_tests:-0}" -gt 1 ]]; then
			fail "nornicdb lane named the value-flow read case, but more than one test failed (${failed_tests} \"--- FAIL:\" lines).
  Only TestLiveBackendConformance is documented as failing here, and the lane's
  driver runs three go test invocations. Read the run output above: something
  other than the backend divergence this gate tracks is red."
		fi

		printf '%s lane: failed as documented, naming the value-flow read case.\n' "${lane}"
		;;
	neo4j)
		if [[ "${observed_exit}" -ne 0 ]]; then
			fail "neo4j lane FAILED (exit ${observed_exit}), and it is the positive control.
  Neo4j serves this query, so a red here says the fixture, the seed, or the
  environment is broken — which also means the nornicdb lane's red proves
  nothing this run. Fix the control before reading the other lane."
		fi
		printf '%s lane: passed, so the fixture and the seed are sound.\n' "${lane}"
		;;
esac
