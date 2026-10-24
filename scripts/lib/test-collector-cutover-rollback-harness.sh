#!/usr/bin/env bash
# Shared harness for scripts/test-collector-cutover-rollback.sh (#8895). Sourced
# by the entry script, which then sources the case files.
#
# eshu, kubectl, psql, and helm are PATH stubs (scripts/lib/test-collector-
# cutover-rollback-fake-*.sh) backed by a fake component home (activations and
# grants files) and a fake claim table. Every stub appends its argv to
# calls.log, which is how the cases prove step order, the refusal gates, and
# that `plan` touches nothing.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="${repo_root}/scripts/collector-cutover-rollback.sh"
truth_header="# eshu-rollback-truth format=1"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}" 2>/dev/null || true' EXIT

bin_dir="${tmp_root}/bin"
mkdir -p "${bin_dir}"
for tool in eshu kubectl psql helm; do
	cp "${repo_root}/scripts/lib/test-collector-cutover-rollback-fake-${tool}.sh" "${bin_dir}/${tool}"
	chmod +x "${bin_dir}/${tool}"
done

failures=0
pass() { printf 'PASS %s\n' "$1"; }
fail() {
	printf 'FAIL %s\n' "$1"
	failures=$((failures + 1))
	# The mutant runner needs only the first failing check to call a mutant dead.
	[[ -z "${ROLLBACK_TEST_FAIL_FAST:-}" ]] || exit 1
}

# new_state builds a fresh fake world and exports FAKE_STATE: the external
# instance enabled and holding a claim that the reaper clears after one poll, the
# worker running, grants live.
new_state() {
	FAKE_STATE="${tmp_root}/state-$1"
	export FAKE_STATE
	rm -rf "${FAKE_STATE}"
	mkdir -p "${FAKE_STATE}"
	printf 'ext-1\n' >"${FAKE_STATE}/activations"
	{
		printf 'dev.example.jira|1.2.3|work_item.issue|jira|false\n'
		printf 'dev.example.jira|1.2.3|work_item.comment|jira|false\n'
	} >"${FAKE_STATE}/grants"
	{
		printf 'jira|ext-1|claimed|active\n'
		printf 'jira|tree-1|pending|\n'
	} >"${FAKE_STATE}/claims"
	printf '1\n' >"${FAKE_STATE}/drain_polls_left"
	printf '1\n' >"${FAKE_STATE}/worker_pods"
	printf '1\n' >"${FAKE_STATE}/worker_replicas"
	printf '0\n' >"${FAKE_STATE}/intree_replicas"
	printf '0|0|2|0|0\n' >"${FAKE_STATE}/replay"
	printf 'work_item.comment|10|keysA|payloadA\nwork_item.issue|4|keysB|payloadB\n' >"${FAKE_STATE}/truth"
	{
		printf '%s\n' "${truth_header}"
		cat "${FAKE_STATE}/truth"
	} >"${FAKE_STATE}/baseline"
	printf 'work_item.comment\tp1\tC-1\nwork_item.issue\tp2\tI-1\nwork_item.issue\tp3\tI-2\n' >"${FAKE_STATE}/keys"
	LC_ALL=C sort "${FAKE_STATE}/keys" >"${FAKE_STATE}/baseline.keys"
}

# new_clear_state is the world after steps 1 to 3 have completed: external
# instance disabled, worker gone, no claim held by a non-in-tree instance,
# grants revoked, in-tree Deployment rolled out. Cases break one fact at a time.
new_clear_state() {
	new_state "$1"
	: >"${FAKE_STATE}/activations"
	printf '0\n' >"${FAKE_STATE}/worker_pods"
	printf '0\n' >"${FAKE_STATE}/worker_replicas"
	printf '1\n' >"${FAKE_STATE}/intree_replicas"
	printf 'jira|tree-1|pending|\n' >"${FAKE_STATE}/claims"
	rm -f "${FAKE_STATE}/drain_polls_left"
	{
		printf 'dev.example.jira|1.2.3|work_item.issue|jira|true\n'
		printf 'dev.example.jira|1.2.3|work_item.comment|jira|true\n'
	} >"${FAKE_STATE}/grants"
}

base_args() {
	ARGS=(
		--collector-kind jira --external-instance ext-1 --in-tree-instance tree-1
		--component-id dev.example.jira --component-version 1.2.3 --component-home /data/components
		--grant-kind work_item.issue --grant-kind work_item.comment
		--namespace eso --worker-deployment eshu-worker --coordinator-deployment eshu-coordinator
		--in-tree-deployment jira-collector
		--helm-release eshu --helm-chart ./chart --restore-values /tmp/rollback-values.yaml
	)
}

rc=0
out=""
run_script() {
	rc=0
	out="$(PATH="${bin_dir}:${PATH}" ROLLBACK_POLL_SECONDS=0.05 "${script}" "$@" 2>&1)" || rc=$?
}

calls() { cat "${FAKE_STATE}/calls.log" 2>/dev/null || true; }
# first_line prints the first calls.log line number containing the fixed string.
first_line() { calls | awk -v want="$1" 'index($0, want) { print NR; exit }'; }

expect_rc() { # label want
	if [[ "${rc}" == "$2" ]]; then pass "$1"; else
		fail "$1 (exit ${rc}, want $2)"
		printf '%s\n' "${out}" | sed 's/^/    /'
	fi
}
expect_out() { # label fixed-string
	if [[ "${out}" == *"$2"* ]]; then pass "$1"; else
		fail "$1 (output lacks: $2)"
		printf '%s\n' "${out}" | sed 's/^/    /'
	fi
}
expect_no_out() { # label fixed-string
	if [[ "${out}" != *"$2"* ]]; then pass "$1"; else fail "$1 (output has: $2)"; fi
}
expect_no_call() { # label fixed-string
	if [[ -z "$(first_line "$2")" ]]; then pass "$1"; else fail "$1 (unexpected call: $2)"; fi
}
expect_call() { # label fixed-string
	if [[ -n "$(first_line "$2")" ]]; then pass "$1"; else fail "$1 (missing call: $2)"; fi
}
# expect_order label pattern... asserts the first occurrence of each pattern is
# strictly increasing in calls.log.
expect_order() {
	local label="$1" prev=0 line pattern
	shift
	for pattern in "$@"; do
		line="$(first_line "${pattern}")"
		if [[ -z "${line}" || "${line}" -le "${prev}" ]]; then
			fail "${label} (pattern '${pattern}' at ${line:-missing}, after ${prev})"
			return
		fi
		prev="${line}"
	done
	pass "${label}"
}

# expect_refused runs `run` from a step with the world as built and asserts the
# refusal exit code and that none of the named later-step calls happened.
#   expect_refused label want-rc from-step extra-run-args... -- forbidden-call...
expect_refused() {
	local label="$1" want="$2" from="$3" extra=() forbidden=()
	shift 3
	while [[ $# -gt 0 && "$1" != "--" ]]; do
		extra+=("$1")
		shift
	done
	shift
	forbidden=("$@")
	run_script run --yes --from-step "${from}" --step-timeout 0 --drain-timeout 0 \
		--baseline "${FAKE_STATE}/baseline" ${extra[@]+"${extra[@]}"} "${ARGS[@]}"
	expect_rc "${label}" "${want}"
	local call
	for call in ${forbidden[@]+"${forbidden[@]}"}; do
		expect_no_call "${label}: no ${call}" "${call}"
	done
}
