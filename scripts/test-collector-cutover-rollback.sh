#!/usr/bin/env bash
# Focused tests for scripts/collector-cutover-rollback.sh (#8895).
#
# eshu, kubectl, psql, and helm are PATH stubs (scripts/lib/test-collector-
# cutover-rollback-fake-*.sh) backed by a fake component home (activations and
# grants files) and a fake claim table. Every stub appends its argv to
# calls.log, which is how these tests prove step order, the refusal cases, and
# that `plan` touches nothing.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/collector-cutover-rollback.sh"

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
}

# new_state builds a fresh fake world and exports FAKE_STATE.
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
	printf '0\n' >"${FAKE_STATE}/intree_replicas"
	printf '0|0|2|0|0\n' >"${FAKE_STATE}/replay"
	printf 'work_item.comment|10|keysA|payloadA\nwork_item.issue|4|keysB|payloadB\n' >"${FAKE_STATE}/truth"
	cp "${FAKE_STATE}/truth" "${FAKE_STATE}/baseline"
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
	out="$(PATH="${bin_dir}:${PATH}" ESHU_ROLLBACK_POLL_SECONDS=0.05 "${script}" "$@" 2>&1)" || rc=$?
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

# --- plan -------------------------------------------------------------------

new_state plan
base_args
run_script plan "${ARGS[@]}"
expect_rc "plan exits 0" 0
for n in 1 2 3 4 5 6; do expect_out "plan prints STEP ${n}" "STEP ${n} "; done
expect_out "plan prints the disable command" "eshu component disable dev.example.jira --instance ext-1 --component-home /data/components"
expect_out "plan prints the revoke command" "eshu component revoke-grant dev.example.jira --version 1.2.3 --kind work_item.issue --scope jira --component-home /data/components"
expect_out "plan prints the helm restore" "helm upgrade eshu ./chart --namespace eso --reuse-values -f /tmp/rollback-values.yaml"
if [[ ! -e "${FAKE_STATE}/calls.log" ]]; then pass "plan invokes no tool"; else fail "plan invoked a tool: $(calls)"; fi
plan_out="${out}"

# --- run: order, evidence, and plan/run agreement -----------------------------

new_state order
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "run succeeds" 0
expect_order "run executes the six steps in order" \
	"eshu component disable" \
	"kubectl -n eso scale deployment/eshu-worker --replicas=0" \
	"kubectl -n eso rollout restart deployment/eshu-coordinator" \
	"psql drain" \
	"eshu component revoke-grant" \
	"helm upgrade" \
	"kubectl -n eso scale deployment/jira-collector --replicas=1" \
	"psql replay" \
	"psql truth"
revoke_line="$(first_line 'eshu component revoke-grant')"
helm_line="$(first_line 'helm upgrade')"
drain_between="$(calls | awk -v a="${revoke_line}" -v b="${helm_line}" 'NR > a && NR < b && index($0, "psql drain") { n++ } END { print n + 0 }')"
if [[ "${drain_between}" -ge 1 ]]; then pass "drain is re-checked between revoke and restore"; else fail "no drain re-check before restore"; fi
for n in 1 2 3 4 5 6; do expect_out "run reports STEP ${n}" "STEP ${n} "; done
expect_out "run prints evidence lines" "EVIDENCE[step 2]"
expect_out "run reports the final verdict" "ROLLBACK COMPLETE"
missing=0
while IFS= read -r line; do
	cmd="${line#  \$ }"
	[[ "${cmd}" == *psql* ]] && continue
	if [[ -z "$(first_line "${cmd}")" ]]; then
		missing=1
		printf '    planned but never run: %s\n' "${cmd}"
	fi
done < <(printf '%s\n' "${plan_out}" | awk '/^  \$ /')
if [[ "${missing}" == 0 ]]; then pass "every planned command is executed"; else fail "plan and run disagree"; fi

# --- run guards ---------------------------------------------------------------

new_state noyes
run_script run --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "run without --yes is a usage error" 2
expect_no_call "run without --yes calls nothing" "eshu"

new_state nobaseline
run_script run --yes "${ARGS[@]}"
expect_rc "run without a baseline is refused before any change" 2
if [[ ! -e "${FAKE_STATE}/calls.log" ]]; then pass "missing baseline calls nothing"; else fail "missing baseline still called a tool"; fi

new_state unsafe
run_script plan "${ARGS[@]}" --collector-kind 'jira;drop'
expect_rc "unsafe identifier is rejected" 2

# --- refusals -------------------------------------------------------------------

# Claims still held: step 4 must refuse to restore the in-tree producer.
new_state refuse4
printf '0\n' >"${FAKE_STATE}/worker_pods"
: >"${FAKE_STATE}/activations"
printf 'dev.example.jira|1.2.3|work_item.issue|jira|true\ndev.example.jira|1.2.3|work_item.comment|jira|true\n' >"${FAKE_STATE}/grants"
rm -f "${FAKE_STATE}/drain_polls_left"
run_script run --yes --from-step 4 --restore-applied-by-gitops --baseline "${FAKE_STATE}/baseline" \
	"${ARGS[@]:0:${#ARGS[@]}-6}"
expect_rc "restore refuses while claims are held" 4
expect_out "refusal names the held claims" "REFUSED"
expect_no_call "no in-tree scale after refusal" "scale deployment/jira-collector"
expect_no_call "no helm after refusal" "helm"

# Step 1 unconfirmed (worker pod survives): step 2 must not start waiting.
new_state refuse2
touch "${FAKE_STATE}/worker_stuck"
run_script run --yes --from-step 2 --step-timeout 1 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "drain wait refuses before step 1 is confirmed" 4
expect_no_call "no drain query before step 1 is confirmed" "psql drain"

# Step 2 not drained: step 3 must not revoke.
new_state refuse3
printf '0\n' >"${FAKE_STATE}/worker_pods"
rm -f "${FAKE_STATE}/drain_polls_left"
run_script run --yes --from-step 3 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "revoke refuses while claims are held" 4
expect_no_call "no revoke before drain" "revoke-grant"

# Step 3 not done: step 4 must not restore.
new_state refuse4b
printf '0\n' >"${FAKE_STATE}/worker_pods"
: >"${FAKE_STATE}/claims"
run_script run --yes --from-step 4 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "restore refuses while a grant is still live" 4
expect_no_call "no helm while a grant is live" "helm"

# --- timeouts ---------------------------------------------------------------------

new_state drain-timeout
rm -f "${FAKE_STATE}/drain_polls_left"
run_script run --yes --drain-timeout 1 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a drain that never finishes times out" 5
expect_no_call "timeout stops before revoke" "revoke-grant"
expect_no_call "timeout stops before restore" "helm"

new_state replay-timeout
printf '1|0|1|0|0\n' >"${FAKE_STATE}/replay"
run_script run --yes --replay-timeout 1 --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "a replay that never finishes times out" 5
expect_no_call "replay timeout stops before truth capture" "psql truth"

new_state replay-failed
printf '0|0|1|0|3\n' >"${FAKE_STATE}/replay"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "terminal replay failures abort" 3
expect_no_call "failed replay stops before truth capture" "psql truth"

# --- idempotent re-run ------------------------------------------------------------

new_state idem
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "first run succeeds" 0
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "second run is idempotent" 0
expect_out "second run reports the instance already disabled" "already disabled"

# --- a failing step aborts the run ---------------------------------------------------

new_state fail-disable
touch "${FAKE_STATE}/fail_disable"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "failed disable aborts" 3
expect_no_call "no scale after a failed disable" "scale"
expect_no_call "no revoke after a failed disable" "revoke-grant"

new_state fail-revoke
: >"${FAKE_STATE}/grants"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "failed revoke aborts" 3
expect_no_call "no restore after a failed revoke" "helm"
expect_no_call "no in-tree scale after a failed revoke" "scale deployment/jira-collector"

new_state fail-helm
touch "${FAKE_STATE}/fail_helm"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "failed helm restore aborts" 3
expect_no_call "no in-tree scale after a failed helm restore" "scale deployment/jira-collector"
expect_no_call "no replay after a failed helm restore" "psql replay"

new_state fail-rollout
touch "${FAKE_STATE}/rollout_fail"
run_script run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}"
expect_rc "failed coordinator rollout aborts step 1" 3
expect_no_call "no drain after a failed rollout" "psql drain"

# --- verify -------------------------------------------------------------------------

verify_args=(--collector-kind jira --external-instance ext-1 --in-tree-instance tree-1)

new_state verify-ok
: >"${FAKE_STATE}/claims"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify passes when truth equals the baseline" 0

printf 'work_item.comment|10|keysA|payloadA\nwork_item.issue|4|keysB|payloadCHANGED\n' >"${FAKE_STATE}/truth"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify fails on a payload difference" 6
expect_out "verify names the differing fact kind" "work_item.issue"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline" --keys-only
expect_rc "verify --keys-only ignores payload digests" 0

printf 'work_item.comment|9|keysX|payloadA\nwork_item.issue|4|keysB|payloadB\n' >"${FAKE_STATE}/truth"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline" --keys-only
expect_rc "verify --keys-only still fails on a count difference" 6

cp "${FAKE_STATE}/baseline" "${FAKE_STATE}/truth"
printf '1|1|0\n' >"${FAKE_STATE}/dual"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify fails when both producers hold claims" 7
printf '0|0|2\n' >"${FAKE_STATE}/dual"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify fails on overlapping claim intervals" 7

new_state capture
run_script verify "${verify_args[@]}" --capture "${FAKE_STATE}/captured"
expect_rc "capture writes a baseline" 0
if cmp -s "${FAKE_STATE}/captured" "${FAKE_STATE}/truth"; then pass "captured baseline equals current truth"; else fail "captured baseline differs"; fi
: >"${FAKE_STATE}/truth"
run_script verify "${verify_args[@]}" --capture "${FAKE_STATE}/empty"
expect_rc "capture refuses an empty truth set" 3

# --- secrets never reach argv or output ------------------------------------------------

new_state secrets
rc=0
out="$(PATH="${bin_dir}:${PATH}" PGPASSWORD='s3cr3t-token-value' ESHU_ROLLBACK_POLL_SECONDS=0.05 \
	"${script}" run --yes --baseline "${FAKE_STATE}/baseline" "${ARGS[@]}" 2>&1)" || rc=$?
expect_rc "run with a PGPASSWORD set succeeds" 0
if [[ "${out}" != *s3cr3t-token-value* && "$(calls)" != *s3cr3t-token-value* ]]; then
	pass "the database password is never printed or passed on a command line"
else
	fail "the database password leaked"
fi

# --- injectable binaries -----------------------------------------------------------------

new_state inject
cp "${bin_dir}/eshu" "${tmp_root}/my-eshu"
rc=0
out="$(PATH="${bin_dir}:${PATH}" ESHU_BIN="${tmp_root}/my-eshu" "${script}" plan "${ARGS[@]}" 2>&1)" || rc=$?
expect_rc "plan honors ESHU_BIN" 0
expect_out "plan prints the injected binary" "${tmp_root}/my-eshu component disable"

if [[ "${failures}" -gt 0 ]]; then
	printf '\n%d check(s) failed\n' "${failures}"
	exit 1
fi
printf '\nAll collector cutover rollback checks passed\n'
