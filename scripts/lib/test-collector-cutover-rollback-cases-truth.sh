#!/usr/bin/env bash
# Truth cases for scripts/collector-cutover-rollback.sh (#8895): verify, the
# baseline format, capture, the drift report, and the SQL contract pins.
# Sourced by scripts/test-collector-cutover-rollback.sh after the harness.

# The harness defines these variables and the entry script sources this file.
# shellcheck disable=SC2154,SC2034

verify_args=(--collector-kind jira --external-instance ext-1 --in-tree-instance tree-1)
current_truth_rows() { tail -n +2 "${FAKE_STATE}/baseline" >"${FAKE_STATE}/truth"; }

# --- verify -------------------------------------------------------------------

new_state verify-ok
: >"${FAKE_STATE}/claims"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify passes when truth equals the baseline" 0

printf 'work_item.comment|10|keysA|payloadA\nwork_item.issue|4|keysB|payloadCHANGED\n' >"${FAKE_STATE}/truth"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify fails on a payload difference" 6
expect_out "verify names the differing fact kind" "work_item.issue"
expect_no_out "verify prints no drift report unless asked" "DRIFT REPORT"
expect_no_call "verify reads no per-fact keys unless asked" "psql keys"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline" --keys-only
expect_rc "verify --keys-only ignores payload digests" 0

printf 'work_item.comment|9|keysX|payloadA\nwork_item.issue|4|keysB|payloadB\n' >"${FAKE_STATE}/truth"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline" --keys-only
expect_rc "verify --keys-only still fails on a count difference" 6

current_truth_rows
printf 'jira|ext-1|claimed|active\njira|tree-1|claimed|active\n' >"${FAKE_STATE}/claims"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify fails when both producers hold claims" 7
: >"${FAKE_STATE}/claims"
printf '2\n' >"${FAKE_STATE}/overlaps"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify fails on overlapping claim intervals" 7
rm -f "${FAKE_STATE}/overlaps"
printf 'jira|ext-1|claimed|active\n' >"${FAKE_STATE}/claims"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify refuses while only the external instance holds a claim" 4

# verify checks the cluster, registry, and grants when it is given their options.
full_verify_args=("${verify_args[@]}" --component-id dev.example.jira --component-version 1.2.3
	--component-home /data/components --grant-kind work_item.issue --grant-kind work_item.comment
	--namespace eso --worker-deployment eshu-worker)
new_clear_state verify-full
run_script verify "${full_verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify with cluster, registry, and grant options passes on a clear world" 0
expect_out "verify records the worker check" "no extension worker pod remains"
expect_out "verify records the registry check" "has no enabled activation"
expect_out "verify records the grant check" "external producer grants are revoked"
new_clear_state verify-worker
printf '1\n' >"${FAKE_STATE}/worker_replicas"
run_script verify "${full_verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify refuses while the worker Deployment still wants replicas" 4
new_clear_state verify-registry
printf 'ext-1\n' >"${FAKE_STATE}/activations"
run_script verify "${full_verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify refuses while the component has an enabled activation" 4
new_clear_state verify-grants
printf 'dev.example.jira|1.2.3|work_item.issue|jira|false\ndev.example.jira|1.2.3|work_item.comment|jira|true\n' >"${FAKE_STATE}/grants"
run_script verify "${full_verify_args[@]}" --baseline "${FAKE_STATE}/baseline"
expect_rc "verify refuses while a grant is live" 4

# --- baseline format ------------------------------------------------------------

new_state legacy-baseline
: >"${FAKE_STATE}/claims"
tail -n +2 "${FAKE_STATE}/baseline" >"${FAKE_STATE}/legacy"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/legacy"
expect_rc "verify refuses a baseline without the format header" 3
expect_out "the refusal names the expected format" "eshu-rollback-truth format=1"
new_state other-format-baseline
: >"${FAKE_STATE}/claims"
{
	printf '# eshu-rollback-truth format=2\n'
	tail -n +2 "${FAKE_STATE}/baseline"
} >"${FAKE_STATE}/other"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/other"
expect_rc "verify refuses a baseline written by another format version" 3
new_state legacy-run
tail -n +2 "${FAKE_STATE}/baseline" >"${FAKE_STATE}/legacy"
run_script run --yes --baseline "${FAKE_STATE}/legacy" "${ARGS[@]}"
expect_rc "run refuses a legacy baseline before any change" 2
if [[ ! -e "${FAKE_STATE}/calls.log" ]]; then pass "a legacy baseline calls nothing"; else fail "a legacy baseline still called a tool"; fi

# --- capture --------------------------------------------------------------------

new_state capture
run_script verify "${verify_args[@]}" --capture "${FAKE_STATE}/captured"
expect_rc "capture writes a baseline" 0
if [[ "$(head -n 1 "${FAKE_STATE}/captured")" == "${truth_header}" ]]; then pass "capture writes the format header first"; else fail "capture header missing"; fi
if cmp -s <(tail -n +2 "${FAKE_STATE}/captured") "${FAKE_STATE}/truth"; then pass "captured baseline rows equal current truth"; else fail "captured baseline differs"; fi
if [[ ! -e "${FAKE_STATE}/captured.keys" ]]; then pass "capture writes no key sidecar by default"; else fail "unexpected key sidecar"; fi
: >"${FAKE_STATE}/claims"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/captured" --collector-kind jira
expect_rc "a captured baseline compares equal to the truth it came from" 0
: >"${FAKE_STATE}/truth"
run_script verify "${verify_args[@]}" --capture "${FAKE_STATE}/empty"
expect_rc "capture refuses an empty truth set" 3

new_state capture-keys
run_script verify "${verify_args[@]}" --capture "${FAKE_STATE}/captured" --capture-keys
expect_rc "capture --capture-keys writes the sidecar" 0
if cmp -s "${FAKE_STATE}/captured.keys" "${FAKE_STATE}/baseline.keys"; then pass "the sidecar holds the sorted per-fact keys"; else fail "sidecar differs from the fact keys"; fi
touch "${FAKE_STATE}/psql_fail_keys"
run_script verify "${verify_args[@]}" --capture "${FAKE_STATE}/captured2" --capture-keys
expect_rc "a failing keys query fails the capture" 3
run_script verify "${verify_args[@]}" --capture-keys
expect_rc "--capture-keys without --capture is a usage error" 2

# A sidecar belongs to the capture that wrote it. Recapturing the same file without
# --capture-keys, or with a keys query that fails, must not leave the old sidecar
# behind to be read as the new baseline's keys (R2).
new_state capture-keys-stale
run_script verify "${verify_args[@]}" --capture "${FAKE_STATE}/captured" --capture-keys
expect_rc "the first capture writes a sidecar" 0
printf 'work_item.comment\tp1\tC-1\nwork_item.issue\tp2\tI-1\nwork_item.issue\tp3\tI-2\nwork_item.issue\tp9\tI-NEW\n' >"${FAKE_STATE}/keys"
run_script verify "${verify_args[@]}" --capture "${FAKE_STATE}/captured"
expect_rc "a recapture without --capture-keys succeeds" 0
if [[ ! -e "${FAKE_STATE}/captured.keys" ]]; then pass "a recapture without --capture-keys removes the stale sidecar"; else fail "stale sidecar survived the recapture"; fi
: >"${FAKE_STATE}/claims"
printf 'work_item.comment|10|keysA|payloadA\nwork_item.issue|4|keysCHANGED|payloadCHANGED\n' >"${FAKE_STATE}/truth"
printf 'work_item.comment\tp1\tC-1\nwork_item.issue\tp2\tI-1\nwork_item.issue\tp3\tI-2\nwork_item.issue\tp9\tI-NEW\nwork_item.issue\tp8\tI-NEWER\n' >"${FAKE_STATE}/keys"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/captured" --report-drift
expect_rc "the report after a recapture without a sidecar still exits 6" 6
expect_out "the report says no sidecar exists" "does not exist"
expect_no_out "the report names no key from a stale sidecar" "added: I-NEW"
new_state capture-keys-stale-fail
run_script verify "${verify_args[@]}" --capture "${FAKE_STATE}/captured" --capture-keys
expect_rc "a first capture with keys succeeds" 0
touch "${FAKE_STATE}/psql_fail_keys"
run_script verify "${verify_args[@]}" --capture "${FAKE_STATE}/captured" --capture-keys
expect_rc "a recapture whose keys query fails fails" 3
if [[ ! -e "${FAKE_STATE}/captured.keys" ]]; then pass "a failed keys capture leaves no stale sidecar"; else fail "stale sidecar survived a failed keys capture"; fi

# The baseline and its sidecar hold stable fact keys, so they are private to the
# operator (R6): no group or world bits, whatever the caller's umask.
# group_world_bits prints the group and world permission columns of a file. `ls -l`
# is the one form that reads the same on BSD and GNU systems; stat flags differ.
group_world_bits() { # shellcheck disable=SC2012
	ls -l "$1" | cut -c5-10
}
new_state capture-private
rc=0
out="$(umask 022 && PATH="${bin_dir}:${PATH}" "${script}" verify "${verify_args[@]}" --capture "${FAKE_STATE}/private" --capture-keys 2>&1)" || rc=$?
expect_rc "a capture under a permissive umask succeeds" 0
if [[ "$(group_world_bits "${FAKE_STATE}/private")" == "------" ]]; then pass "the baseline has no group or world access"; else fail "the baseline is readable by group or world"; fi
if [[ "$(group_world_bits "${FAKE_STATE}/private.keys")" == "------" ]]; then pass "the key sidecar has no group or world access"; else fail "the key sidecar is readable by group or world"; fi

new_state capture-private-overwrite
printf 'old\n' >"${FAKE_STATE}/existing"
chmod 644 "${FAKE_STATE}/existing"
run_script verify "${verify_args[@]}" --capture "${FAKE_STATE}/existing"
expect_rc "a recapture over an existing baseline succeeds" 0
if [[ "$(group_world_bits "${FAKE_STATE}/existing")" == "------" ]]; then pass "a recapture replaces a world-readable baseline with a private one"; else fail "an existing baseline kept its permissive mode"; fi

# A run killed mid key query leaves nothing behind in TMPDIR: the raw key list is
# removed on the way out.
new_state capture-killed
touch "${FAKE_STATE}/psql_hang_keys"
tmp_private="${tmp_root}/private-tmp"
mkdir -p "${tmp_private}"
PATH="${bin_dir}:${PATH}" TMPDIR="${tmp_private}" "${script}" verify "${verify_args[@]}" \
	--capture "${FAKE_STATE}/killed" --capture-keys >"${FAKE_STATE}/kill.out" 2>&1 &
script_pid=$!
for _ in $(seq 1 100); do
	[[ -e "${FAKE_STATE}/keys_started" ]] && break
	sleep 0.05
done
if [[ -n "$(ls -A "${tmp_private}")" ]]; then pass "the key query runs with a temp file in TMPDIR"; else fail "no temp file existed during the key query"; fi
kill -TERM "${script_pid}"
rc=0
wait "${script_pid}" || rc=$?
expect_rc "a killed capture exits 143" 143
if [[ -z "$(ls -A "${tmp_private}")" ]]; then pass "a killed capture leaves no temp file behind"; else fail "temp files survived a killed capture"; fi
kill "$(cat "${FAKE_STATE}/keys_started")" 2>/dev/null || true

# --- the drift report (F6) --------------------------------------------------------

# One key added (I-3), one removed (I-2), one changed (I-1 payload), and a count
# change on work_item.issue: exact equality fails, the report says why, exit stays 6.
new_state drift
: >"${FAKE_STATE}/claims"
printf 'work_item.comment|10|keysA|payloadA\nwork_item.issue|4|keysCHANGED|payloadCHANGED\n' >"${FAKE_STATE}/truth"
printf 'work_item.comment\tp1\tC-1\nwork_item.issue\tp2-new\tI-1\nwork_item.issue\tp4\tI-3\n' >"${FAKE_STATE}/keys"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline" --report-drift
expect_rc "a drifted truth still exits 6 with --report-drift" 6
expect_out "the report counts per kind and class" "work_item.issue  added=1 removed=1 changed=1"
expect_out "the report keeps unchanged kinds visible" "work_item.comment  added=0 removed=0 changed=0"
expect_out "the report names an added key" "work_item.issue  added: I-3"
expect_out "the report names a removed key" "work_item.issue  removed: I-2"
expect_out "the report names a changed key" "work_item.issue  changed: I-1"
expect_call "the report reads the per-fact keys" "psql keys"

# Bounded key list: two added keys, --drift-keys 1 prints one but counts both.
printf 'work_item.comment\tp1\tC-1\nwork_item.issue\tp2\tI-1\nwork_item.issue\tp3\tI-2\nwork_item.issue\tp4\tI-3\nwork_item.issue\tp5\tI-4\n' >"${FAKE_STATE}/keys"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline" --report-drift --drift-keys 1
expect_rc "a bounded drift report still exits 6" 6
expect_out "the bounded report counts every added key" "added=2 removed=0 changed=0"
expect_out "the bounded report prints the first added key" "added: I-3"
expect_no_out "the bounded report omits keys past the bound" "added: I-4"

# No sidecar: the report says so and exit stays 6.
new_state drift-no-sidecar
: >"${FAKE_STATE}/claims"
printf 'work_item.comment|10|keysA|payloadA\nwork_item.issue|4|keysCHANGED|payloadCHANGED\n' >"${FAKE_STATE}/truth"
rm -f "${FAKE_STATE}/baseline.keys"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline" --report-drift
expect_rc "a drift report without a sidecar still exits 6" 6
expect_out "the report says the sidecar is missing" "does not exist"
expect_no_call "no key query without a sidecar" "psql keys"

# An all-removed current set (an empty keys result) must not shift the report.
new_state drift-empty
: >"${FAKE_STATE}/claims"
printf 'work_item.comment|0|k|p\n' >"${FAKE_STATE}/truth"
: >"${FAKE_STATE}/keys"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline" --report-drift
expect_rc "an empty current key set is reported" 6
expect_out "every baseline key is reported removed" "work_item.issue  added=0 removed=2 changed=0"

# Equal truth never prints a report even with the flag.
new_state drift-equal
: >"${FAKE_STATE}/claims"
run_script verify "${verify_args[@]}" --baseline "${FAKE_STATE}/baseline" --report-drift
expect_rc "--report-drift on equal truth passes" 0
expect_no_out "equal truth prints no drift report" "DRIFT REPORT"

# --- SQL contract pins (F7) -----------------------------------------------------------
# The fake psql cannot see a column rename. These pins fail when a SQL file
# changes, so the change cannot merge without someone re-running the real
# Postgres check: scripts/test-collector-cutover-rollback-sql.sh. After it
# passes, update the digest here.

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{ print $1 }'; else shasum -a 256 "$1" | awk '{ print $1 }'; fi
}
pin_sql() { # name digest
	local got
	got="$(sha256_of "${repo_root}/scripts/lib/collector-cutover-rollback-$1.sql")"
	if [[ "${got}" == "$2" ]]; then pass "SQL pin: $1 is unchanged"; else
		fail "SQL pin: $1 changed (now ${got}); run scripts/test-collector-cutover-rollback-sql.sh against real Postgres, then update this pin"
	fi
}
pin_sql drain a6d4d544f7d65ba0cd1a5dc5591996a7e22d24117db79e89b3117e553bd55faa
pin_sql dual b054695006f681669f3412b71e50fb3119a72b6df72ce410922c81eb467c7e51
pin_sql now a8dc9a97eab35bd531fa7480308f498946ee2258ce86383acdfe11e0b9e11612
pin_sql replay a0705986ea5174764fda9ad155daf182a3b9efea48f02bdb2b03c4b16d196188
pin_sql truth fb26daf042d8218223399d6cdec24877f03ee7b080af014129820ed1d183d1a0
pin_sql keys 686ec5d8e33a537c5004c154eaa4d734c618ceabce0dce4d588aafdff5db60b3
