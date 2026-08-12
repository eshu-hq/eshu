#!/usr/bin/env bash
# Behavioural cases for the live gate's retained-stack marker (#5987).
#
# Sourced by golden-corpus-lock-race-cases.sh. This file deliberately follows
# the golden-corpus-*.sh convention so edits to the tests exercise the same
# workflow, registry, and pre-pr trigger glob as the production marker helper.

keep_home="$(mktemp -d)"
keep_lock="${keep_home}/eshu-live-gate.lock"
keep_bin="${keep_home}/bin"
bash_bin="$(command -v bash)"
keep_retained_at="$(( $(date +%s) - 42 ))"
mkdir -p "${keep_bin}"

# A helper-only change must select all three gate mirrors. The helper name is
# intentionally inside the one wildcard already shared by the workflow, both
# registry rows, pre-pr, and the private-data scan.
keep_helper_path="scripts/lib/golden-corpus-keep-marker.sh"
[[ "${keep_helper_path}" == scripts/lib/golden-corpus-*.sh ]] \
	|| fail "#5987 helper escaped the shared golden-corpus trigger glob"
require_workflow_path "#5987 keep-marker helper" "scripts/lib/golden-corpus-*.sh"
for keep_gate_id in golden-corpus-mirror golden-corpus-gate; do
	require_in_region "#5987 helper registry trigger ${keep_gate_id}" "${ci_gates}" \
		"/^  - id: ${keep_gate_id}\$/,/^    local:/" '- "scripts/lib/golden-corpus-*.sh"'
done
require_matches "#5987 helper pre-pr live trigger" "${prepr}" \
	"^(?!\\s*#)[^\\n]*run_or_defer golden-corpus \\\\\\n[^\\n]*scripts/lib/\\(golden-corpus-\\.\\+"

keep_marker() {
	printf 'v2:%s:%s:%s:%s:%s\n' "$$" "0" "${keep_retained_at}" "$1" "$2" >"${keep_lock}.keep"
}

require_holder_age() {
	local label="$1" output="$2" where="$3"
	[[ "${output}" == *"holder pid $$"* && "${output}" == *"at ${where}"* ]] \
		|| fail "${label} must identify the retained holder and full worktree (got: ${output})"
	rg --quiet -- 'retained for [0-9]+ seconds' <<<"${output}" \
		|| fail "${label} must report elapsed retention age in stable units (got: ${output})"
}

keep_acquire() {
	local path_prefix="${1:-${keep_bin}:${PATH}}"
	PATH="${path_prefix}" ESHU_LIVE_GATE_LOCK_DIR="${keep_home}" \
		"${bash_bin}" -c '
			set -uo pipefail
			. "$1"
			acquire_live_gate_lock >/dev/null && printf "KEEP_RECLAIMED\n"
		' _ "${lock_lib}" 2>&1
}

# Use this test runner's known-live pid. Marker reclaim must not depend on pid
# death or a guessed unused pid: the Compose project is the durable authority.
ps -p "$$" >/dev/null 2>&1 || fail "keep-marker fixture pid $$ must be live"

# Stack gone: exit 0 plus no container ids is positive evidence that the
# retained project no longer owns running containers.
printf '#!/usr/bin/env bash\nexit 0\n' >"${keep_bin}/docker"
chmod +x "${keep_bin}/docker"
keep_marker "eshu-gate-gone" "/gone-worktree"
keep_gone_out="$(keep_acquire || true)"
rm -f "${keep_lock}"
[[ "${keep_gone_out}" == *KEEP_RECLAIMED* ]] \
	|| fail "a marker whose Compose project is gone must be reclaimed (got: ${keep_gone_out})"
[[ ! -e "${keep_lock}.keep" && ! -L "${keep_lock}.keep" ]] \
	|| fail "positive stack-gone evidence must remove the retained marker"

# Running project: an id from Compose keeps the marker authoritative even
# though process identity is irrelevant to its lifetime.
printf '#!/usr/bin/env bash\nprintf "deadbeefcafe\\n"\n' >"${keep_bin}/docker"
chmod +x "${keep_bin}/docker"
keep_marker "eshu-gate-running" "/running:worktree"
keep_running_out="$(keep_acquire || true)"
rm -f "${keep_lock}" "${keep_lock}.keep"
[[ "${keep_running_out}" != *KEEP_RECLAIMED* && "${keep_running_out}" == *"compose project eshu-gate-running"* ]] \
	|| fail "a running retained project must block and name the project (got: ${keep_running_out})"
require_holder_age "running-project refusal" "${keep_running_out}" "/running:worktree"

# Docker unavailable must be a truthful child execution: resolve bash before
# overriding command -v for Docker only. Emptying PATH before invoking `bash`
# makes the child fail with 127 and can turn this assertion into a false green.
keep_marker "eshu-gate-no-docker" "/no-docker-worktree"
keep_nodocker_out="$(
	ESHU_LIVE_GATE_LOCK_DIR="${keep_home}" "${bash_bin}" -c '
		set -uo pipefail
		command() {
			if [[ "${1:-}" == "-v" && "${2:-}" == "docker" ]]; then return 1; fi
			builtin command "$@"
		}
		. "$1"
		acquire_live_gate_lock >/dev/null && printf "KEEP_RECLAIMED\n"
	' _ "${lock_lib}" 2>&1 || true
)"
rm -f "${keep_lock}" "${keep_lock}.keep"
[[ "${keep_nodocker_out}" != *KEEP_RECLAIMED* && "${keep_nodocker_out}" == *"docker is unavailable"* ]] \
	|| fail "Docker-unavailable marker decision must fail closed with its real reason (got: ${keep_nodocker_out})"
require_holder_age "Docker-unavailable refusal" "${keep_nodocker_out}" "/no-docker-worktree"

# Query failure is not an empty project. Compose must positively report no
# running containers before the marker can be removed.
printf '#!/usr/bin/env bash\nexit 37\n' >"${keep_bin}/docker"
chmod +x "${keep_bin}/docker"
keep_marker "eshu-gate-query-failed" "/query-failed-worktree"
keep_query_out="$(keep_acquire || true)"
rm -f "${keep_lock}" "${keep_lock}.keep"
[[ "${keep_query_out}" != *KEEP_RECLAIMED* && "${keep_query_out}" == *"querying docker failed"* ]] \
	|| fail "Compose query failure must block with a distinct diagnostic (got: ${keep_query_out})"
require_holder_age "Compose-query refusal" "${keep_query_out}" "/query-failed-worktree"

# Legacy, malformed, empty, unreadable, and empty-project markers are separate
# operator states. All fail closed, but diagnostics must not conflate them.
printf '%s\n' "/legacy-worktree" >"${keep_lock}.keep"
keep_legacy_out="$(keep_acquire || true)"
rm -f "${keep_lock}" "${keep_lock}.keep"
[[ "${keep_legacy_out}" == *"legacy path-only --keep marker"* ]] \
	|| fail "legacy marker diagnostic is not distinct (got: ${keep_legacy_out})"

printf '%s\n' "123:456:truncated" >"${keep_lock}.keep"
keep_malformed_out="$(keep_acquire || true)"
rm -f "${keep_lock}" "${keep_lock}.keep"
[[ "${keep_malformed_out}" == *"malformed --keep marker"* ]] \
	|| fail "truncated marker must be diagnosed as malformed, not legacy (got: ${keep_malformed_out})"

: >"${keep_lock}.keep"
keep_empty_out="$(keep_acquire || true)"
rm -f "${keep_lock}" "${keep_lock}.keep"
[[ "${keep_empty_out}" == *"empty --keep marker"* ]] \
	|| fail "empty marker diagnostic is not distinct (got: ${keep_empty_out})"

mkdir "${keep_lock}.keep"
keep_unreadable_out="$(keep_acquire || true)"
rm -f "${keep_lock}"
rm -rf "${keep_lock}.keep"
[[ "${keep_unreadable_out}" == *"could not read the --keep marker"* ]] \
	|| fail "unreadable marker must fail closed with a distinct diagnostic (got: ${keep_unreadable_out})"

keep_marker "" "/empty-project-worktree"
keep_noproject_out="$(keep_acquire || true)"
rm -f "${keep_lock}" "${keep_lock}.keep"
[[ "${keep_noproject_out}" == *"recorded an empty compose project"* ]] \
	|| fail "empty Compose project diagnostic is not distinct (got: ${keep_noproject_out})"

# The open PR briefly emitted four-field markers without a retained-at epoch.
# Do not reinterpret the project as a timestamp or reclaim it: fail closed and
# tell the operator that its age is unavailable.
printf '%s:%s:%s:%s\n' "$$" "0" "eshu-gate-pre-age" "/pre-age-worktree" >"${keep_lock}.keep"
keep_pre_age_out="$(keep_acquire || true)"
rm -f "${keep_lock}" "${keep_lock}.keep"
[[ "${keep_pre_age_out}" != *KEEP_RECLAIMED* && "${keep_pre_age_out}" == *"pre-age four-field --keep marker"* && "${keep_pre_age_out}" == *"retention age is unknown"* ]] \
	|| fail "four-field marker must fail closed with unknown age (got: ${keep_pre_age_out})"

# Missing, invalid, and future timestamps cannot be used to calculate truthful
# age. None may reach the stack-reclaim decision even when Docker would report
# no running containers.
for age_case in \
	"missing::missing retention timestamp" \
	"invalid:not-a-time:invalid retention timestamp" \
	"future:$(( $(date +%s) + 3600 )):future retention timestamp"; do
	IFS=: read -r age_label age_value age_message <<<"${age_case}"
	printf 'v2:%s:%s:%s:%s:%s\n' "$$" "0" "${age_value}" "eshu-gate-${age_label}" "/${age_label}-age-worktree" >"${keep_lock}.keep"
	age_out="$(keep_acquire || true)"
	rm -f "${keep_lock}" "${keep_lock}.keep"
	[[ "${age_out}" != *KEEP_RECLAIMED* && "${age_out}" == *"${age_message}"* && "${age_out}" == *"age is unknown"* ]] \
		|| fail "${age_label} timestamp must fail closed truthfully (got: ${age_out})"
done

# A valid recorded timestamp is still unusable when the current clock cannot
# be read. Stub date only after the marker is written so the fixture stays
# deterministic, and assert that no reclaim occurs.
printf '#!/usr/bin/env bash\nexit 74\n' >"${keep_bin}/date"
chmod +x "${keep_bin}/date"
keep_marker "eshu-gate-no-clock" "/no-clock-worktree"
keep_clock_out="$(keep_acquire || true)"
rm -f "${keep_lock}" "${keep_lock}.keep" "${keep_bin}/date"
[[ "${keep_clock_out}" != *KEEP_RECLAIMED* && "${keep_clock_out}" == *"current clock is unavailable"* && "${keep_clock_out}" == *"age is unknown"* ]] \
	|| fail "unavailable current clock must fail closed truthfully (got: ${keep_clock_out})"

# Clock failure while publishing --keep must still leave a fail-closed marker.
# Otherwise the ordinary lock becomes stale when this process exits and a later
# run can reclaim it while the retained stack is still up.
printf '#!/usr/bin/env bash\nexit 74\n' >"${keep_bin}/date"
chmod +x "${keep_bin}/date"
keep_publish_out="$(
	PATH="${keep_bin}:${PATH}" ESHU_LIVE_GATE_LOCK_DIR="${keep_home}" \
		GATE_COMPOSE_PROJECT="eshu-gate-publish-no-clock" "${bash_bin}" -c '
			. "$1"
			acquire_live_gate_lock
			retain_live_gate_lock
		' _ "${lock_lib}" 2>&1 || true
)"
rm -f "${keep_lock}" "${keep_bin}/date"
[[ "${keep_publish_out}" == *"could not record a valid retention timestamp"* ]] \
	|| fail "retention-time clock failure must be reported (got: ${keep_publish_out})"
[[ -e "${keep_lock}.keep" ]] \
	|| fail "retention-time clock failure must leave a fail-closed marker"
rg --quiet -- '^v2:[0-9]+:[0-9]+::eshu-gate-publish-no-clock:' "${keep_lock}.keep" \
	|| fail "clock-failure marker must preserve holder and project with an empty timestamp"
rm -f "${keep_lock}.keep"

# A dangling marker symlink is invisible to `-e` but still occupies the name.
# It must fail closed as unreadable, not be treated as marker absence.
ln -s "${keep_home}/missing-marker-target" "${keep_lock}.keep"
keep_dangling_out="$(keep_acquire || true)"
rm -f "${keep_lock}" "${keep_lock}.keep"
[[ "${keep_dangling_out}" == *"could not read the --keep marker"* ]] \
	|| fail "dangling marker symlink must not bypass retention (got: ${keep_dangling_out})"

# A positive no-running result is not enough if marker deletion fails. Stub rm
# only for the marker, then ensure acquisition blocks and the marker survives.
printf '#!/usr/bin/env bash\nexit 0\n' >"${keep_bin}/docker"
printf '#!/usr/bin/env bash\ncase "${*: -1}" in *.keep) exit 73;; esac\nexec /bin/rm "$@"\n' >"${keep_bin}/rm"
chmod +x "${keep_bin}/docker" "${keep_bin}/rm"
keep_marker "eshu-gate-remove-failed" "/remove-failed-worktree"
keep_rm_out="$(keep_acquire || true)"
/bin/rm -f "${keep_lock}"
[[ "${keep_rm_out}" != *KEEP_RECLAIMED* && "${keep_rm_out}" == *"could not remove the --keep marker"* ]] \
	|| fail "failed marker deletion must block with a distinct diagnostic (got: ${keep_rm_out})"
[[ -e "${keep_lock}.keep" ]] || fail "failed marker deletion must leave the marker in place"
/bin/rm -f "${keep_lock}.keep" "${keep_bin}/rm"

# Marker inspection/deletion must happen only after owning the main lock. A
# stale marker makes contender A pause inside the Docker query. Correct code has
# already published A's main lock, so B refuses on that live holder and never
# reaches Docker. The old pre-loop check lets both contenders inspect and race.
probe_dir="${keep_home}/probe"
mkdir -p "${probe_dir}"
printf '%s\n' '#!/usr/bin/env bash' \
	'probe="${KEEP_PROBE_DIR:?}"' \
	': >"${probe}/${KEEP_CONTENDER:?}.query"' \
	'while [[ ! -e "${probe}/release" ]]; do sleep 0.01; done' \
	'exit 0' >"${keep_bin}/docker"
chmod +x "${keep_bin}/docker"
keep_marker "eshu-gate-stale" "/stale-worktree"
(
	PATH="${keep_bin}:${PATH}" KEEP_PROBE_DIR="${probe_dir}" KEEP_CONTENDER="A" \
		ESHU_LIVE_GATE_LOCK_DIR="${keep_home}" GATE_COMPOSE_PROJECT="eshu-gate-replacement" \
		"${bash_bin}" -c '. "$1"; acquire_live_gate_lock; retain_live_gate_lock' _ "${lock_lib}"
) >"${probe_dir}/A.out" 2>&1 &
keep_a_pid=$!
for _ in $(seq 1 200); do [[ -e "${probe_dir}/A.query" ]] && break; sleep 0.01; done
[[ -e "${probe_dir}/A.query" ]] || fail "contender A never reached the coordinated marker query"
(
	PATH="${keep_bin}:${PATH}" KEEP_PROBE_DIR="${probe_dir}" KEEP_CONTENDER="B" \
		ESHU_LIVE_GATE_LOCK_DIR="${keep_home}" "${bash_bin}" -c '. "$1"; acquire_live_gate_lock' _ "${lock_lib}"
) >"${probe_dir}/B.out" 2>&1 &
keep_b_pid=$!
for _ in $(seq 1 40); do
	[[ -e "${probe_dir}/B.query" ]] && break
	! ps -p "${keep_b_pid}" >/dev/null 2>&1 && break
	sleep 0.01
done
: >"${probe_dir}/release"
wait "${keep_a_pid}" || fail "contender A failed: $(<"${probe_dir}/A.out")"
wait "${keep_b_pid}" 2>/dev/null || true
[[ ! -e "${probe_dir}/B.query" ]] \
	|| fail "contender B inspected the marker without owning the main lock"
[[ -e "${keep_lock}.keep" ]] || fail "contender race deleted A's replacement marker"
rg --fixed-strings --quiet -- ":eshu-gate-replacement:" "${keep_lock}.keep" \
	|| fail "contender race did not preserve A's replacement marker"
rg --quiet -- '^v2:[0-9]+:[0-9]+:[0-9]{1,10}:eshu-gate-replacement:' "${keep_lock}.keep" \
	|| fail "retain_live_gate_lock did not publish the versioned retained-at marker format"
rm -f "${keep_lock}" "${keep_lock}.keep"

# The dead-lock path makes the same decision while holding `.reclaim`. Use a
# syntactically invalid pid, not a guessed dead numeric pid that the OS can reuse.
printf '#!/usr/bin/env bash\nexit 0\n' >"${keep_bin}/docker"
chmod +x "${keep_bin}/docker"
ln -s "not-a-pid:0:/dead-lock-worktree" "${keep_lock}"
keep_marker "eshu-gate-dead-lock" "/dead-lock-worktree"
keep_guard_out="$(keep_acquire || true)"
rm -f "${keep_lock}"
[[ "${keep_guard_out}" == *KEEP_RECLAIMED* && ! -e "${keep_lock}.keep" ]] \
	|| fail "dead-lock marker must be decided and reclaimed under the reclaim guard (got: ${keep_guard_out})"

rm -rf "${keep_home}"
keep_marker_cases_completed=1
