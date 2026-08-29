#!/usr/bin/env bash
# Cross-run mutex cases for test-verify-golden-corpus-gate.sh.
#
# Sourced, never executed: it runs in the caller's shell and uses the caller's
# ${repo_root}, ${script}, fail(), require() and require_lock(). Extracted so the
# mirror test stays under the 500-line cap - these cases are the bulk of it,
# because a mutex can only be proven behaviourally.
#
# The `golden-corpus-*` name is deliberate: all three trigger lists already glob
# scripts/lib/golden-corpus-*.sh, so this file is gated without registry work.

# ---------------------------------------------------------------------------
# Cross-run mutex.
#
# The gate binds fixed host ports, so two runs from different worktrees do not
# fail cleanly - they starve each other, and the loser surfaces as a drain that
# never reaches terminal (`residual=1 (dead_letter=1)`), which reads like a real
# reducer defect.
#
# The lock lives in scripts/lib/live-gate-lock.sh so the cases below can source
# and exercise the REAL implementation; a test that re-implements or
# text-extracts the lock proves nothing about the lock. Every case runs against
# a throwaway ESHU_LIVE_GATE_LOCK_DIR, so running this test never touches the
# repo's own lock under .git and cannot disturb a live gate run.
# ---------------------------------------------------------------------------
lock_lib="${repo_root}/scripts/lib/live-gate-lock.sh"
[[ -f "${lock_lib}" ]] || fail "missing live gate lock lib: ${lock_lib}"
bash -n "${lock_lib}" || fail "live-gate-lock.sh has a syntax error"

require "live gate mutex" 'acquire_live_gate_lock'
# cleanup() (and its release/retain calls) is extracted to
# scripts/lib/golden-corpus-cleanup.sh to keep the orchestrator under the
# 500-line cap, so these are asserted against that lib chunk, not the gate
# script itself.
cleanup_lib="${cleanup_lib:-${repo_root}/scripts/lib/golden-corpus-cleanup.sh}"
[[ -f "${cleanup_lib}" ]] || fail "missing cleanup lib: ${cleanup_lib}"
rg --fixed-strings --quiet -- 'release_live_gate_lock' "${cleanup_lib}" ||
	fail "missing lock released on exit in ${cleanup_lib}: release_live_gate_lock"
# --keep leaves the stack up on the fixed ports, so the cleanup trap must retain
# the mutex on that path rather than release it. Asserted against the cleanup
# lib, not the lock lib itself: the lock lib merely defines the function.
rg --pcre2 --multiline --quiet \
	'if \[\[ "\$\{keep\}" -eq 1 \]\]; then\n\t\tretain_live_gate_lock\n\telse\n\t\trelease_live_gate_lock' \
	"${cleanup_lib}" ||
	fail "the --keep cleanup branch must RETAIN the mutex and the else branch release it"

require_lock() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${lock_lib}" \
		|| fail "missing ${label} in live-gate-lock.sh: ${needle}"
}
require_lock "atomic symlink claim" 'ln -s "${payload}" "${candidate}"'
require_lock "exclusive stale-reclaim guard" 'claim_lock_link "$$:$(date +%s)" "${guard}"'
require_lock "reclaim re-validates the holder" \
	'if ! { [[ "${current_pid}" =~ ^[0-9]+$ ]] && process_is_alive "${current_pid}" &&'
# PID-reuse defense (#5826 review): a live pid alone is not enough evidence the
# recorded holder is still the SAME process, since the kernel can hand a dead
# holder's pid to an unrelated long-lived process. The payload and every
# liveness check must also carry/validate a start-id fingerprint.
require_lock "start-id fingerprint helper" 'start_id_for_pid() {'
require_lock "payload carries a start-id fingerprint" \
	'local payload="$$:$(start_id_for_pid "$$"):$(pwd -P)"'
# #5826 review, P1: `lstart` renders in the CALLER's ambient locale/TZ, so an
# unnormalized fingerprint mismatches for the SAME live pid across TZs, and a
# mismatch reads as "stale" - reclaiming a LIVE lock. Pin the exact
# normalization prefix, not just that `ps -o lstart=` appears, so a silent
# drop of `LC_ALL=C TZ=UTC ` (e.g. "simplifying" the helper back to a bare
# `ps` call) fails this mirror instead of only failing under a TZ the CI
# runner does not happen to use.
require_lock "start-id fingerprint is locale/TZ-normalized" \
	'LC_ALL=C TZ=UTC ps -o lstart= -p "$1" 2>/dev/null | tr -cd '\''0-9'\'''
# Both liveness checks treat an empty start-id as "cannot confirm" on BOTH
# sides of the comparison -- the payload's and the live re-read's. Comparing a
# non-empty payload fingerprint against an empty live read would reclaim a live
# lock, which is the fail-open class this whole file exists to prevent.
require_lock "holder liveness re-validates the start-id" \
	'[[ -z "${holder_startid}" || -z "${holder_live_startid}" ]] ||'
require_lock "holder liveness compares the captured live start-id" \
	'[[ "${holder_live_startid}" == "${holder_startid}" ]]'
require_lock "reclaim re-validates the start-id" \
	'[[ -z "${current_startid}" || -z "${current_live_startid}" ]] ||'
require_lock "reclaim compares the captured live start-id" \
	'[[ "${current_live_startid}" == "${current_startid}" ]]'
require_lock "CI bypass" 'ESHU_SKIP_LIVE_GATE_LOCK'
require_lock "test lock relocation" 'ESHU_LIVE_GATE_LOCK_DIR'
# `ln -s` into a DIRECTORY links inside it and reports success, so the claim
# must be verified rather than trusted, or a stale mkdir-era lock dir silently
# stops the mutex excluding anything.
require_lock "verified claim" 'claim_lock_link'
# `kill -0` fails with EPERM on another user's live process, which would read as
# "dead" and let a second run reclaim a live lock.
require_lock "ownership-independent liveness" 'ps -p "$1"'
if rg -v '^[[:space:]]*#' "${lock_lib}" | rg --fixed-strings -- 'kill -0' >/dev/null; then
	fail "liveness reverted to kill -0 (misreads another user's live process as dead)"
fi
# The orphan-guard reap must be age-gated; an unconditional reap can delete a
# guard a racer created microseconds ago. Age comes from the guard's own
# embedded birth epoch (pid:epoch payload), never a filesystem mtime: `find`
# is a banned discovery primitive repo-wide, and a directory's mtime resets on
# every claim_lock_link probe anyway (see the guard_status==2 branch).
# Pin the POLARITY, not the flag: inverting this to reap guards YOUNGER than
# the budget reinstates the bug while keeping the "> 60" substring.
require_lock "age-gate polarity" '(( $(date +%s) - guard_born > 60 ))'
if rg -v '^[[:space:]]*#' "${lock_lib}" | rg -- 'find ' >/dev/null; then
	fail "live-gate-lock.sh reverted to using find - it is a banned discovery primitive repo-wide"
fi
# The reclaim guard excludes other RECLAIMERS, not an ordinary claimer - and an
# ordinary claimer competes precisely when the name is FREE. So the replace must
# be conditional on the value validated under the guard; removing "nothing"
# deletes a live lock published microseconds earlier and both runs proceed.
require_lock "replace only what was validated" \
	'if [[ -n "${current}" || "${current_is_debris}" -eq 1 ]]; then'
# ...and re-validated immediately before the destroy, with no fork in between.
require_lock "re-read before destroying" \
	'if [[ "$(readlink "${candidate}" 2>/dev/null || true)" == "${current}" ]]; then'
# On a FREE name the destroy must be skipped outright. The re-read narrows that
# window to one fork; it does not close it, so this flag is what keeps it at
# zero. Flipping the initializer to 1 is a one-character, literal-preserving
# change that reintroduces the violation, and the consumer pin above cannot see
# it because it pins the reader, not the writer.
require_lock "a free name is never destroyed" 'current_is_debris=0'
require_lock "non-symlink guard is reapable" '[[ "${guard_status}" -eq 2 ]]'
# The guard-side arity check is the only invariant this file introduces with no
# behavioural case: reverting it alone leaves the whole suite green, because the
# holder-side check catches a bare-pid payload first and acquire fails CLOSED.
# The reclaim only happens when BOTH sites are gone, so this pin is what stops a
# future edit deleting half the fix silently.
require_lock "guard parse rejects a colon-less payload" 'case "${current}" in'
# NOT a bare 'retain_live_gate_lock' grep: that matches the definition itself and
# can never fail. Pin the part that makes it do something, and its fail-loud.
require_lock "keep marker write" 'lock_path}.keep"'
require_lock "keep marker failure is not swallowed" 'could not write the --keep marker'

lock_home="$(mktemp -d)"
lock_file="${lock_home}/eshu-live-gate.lock"
drop_lock_home() { rm -rf "${lock_home}" "${race_dir:-}" "${debris_dir:-}"; }
trap drop_lock_home EXIT

# Acquire against the throwaway lock dir, then hold it for `hold` seconds so a
# racing acquirer observes a genuinely LIVE holder rather than a pid that has
# already exited.
#
# An optional third arg is a shared critical-section token path. When given,
# entry is claimed with `set -o noclobber` (create-or-fail) immediately after
# acquiring the lock: a second racer that is ALSO inside the critical section
# at the same instant reports OVERLAP instead of ACQUIRED. This is the mutual-
# exclusion invariant a mutex actually promises. "exactly N winners across the
# whole run" is NOT that invariant when `hold` is short relative to the retry
# budget: the winner holds for `hold` seconds, then exits and its pid goes
# dead, so a racer still spinning in its own retry loop legitimately reclaims
# the now-free lock and becomes a SECOND winner later in the same run. #5826
# review reproduced this: 8 racers, hold=2s, "exactly one winner" failed with
# "got 8" under scheduler load - not a mutex bug, an test invariant that
# was already provably violated by the code as designed. Overlap (never two
# holders AT ONCE) is what must be asserted; sequential winners are correct.
try_acquire() {
	local hold="${1:-0}" token="${2:-}"
	ESHU_LIVE_GATE_LOCK_DIR="${lock_home}" bash -c '
		set -euo pipefail
		. "$1"
		acquire_live_gate_lock
		token="$3"
		if [[ -n "${token}" ]] && ! ( set -o noclobber; : >"${token}" ) 2>/dev/null; then
			printf "OVERLAP token=%s\n" "${token}"
		else
			printf "ACQUIRED=%s\n" "${lock_path}"
		fi
		[[ "$2" == "0" ]] || sleep "$2"
		[[ -z "${token}" ]] || rm -f "${token}"
	' _ "${lock_lib}" "${hold}" "${token}" 2>&1
}

# A free lock is acquired.
acquired_out="$(try_acquire)" \
	|| fail "acquire on a free lock must succeed; got: ${acquired_out}"
rg --quiet 'ACQUIRED=.*eshu-live-gate.lock' < <(printf '%s\n' "${acquired_out}") \
	|| fail "acquire must end holding the lock; got: ${acquired_out}"

# #P1-1 regression: the lock must be a symlink whose payload ALREADY carries the
# holder pid. With `mkdir` plus a separate pid write there is a window where the
# lock exists with no pid, and a second run reads that as "stale" and reclaims a
# LIVE lock - after which both runs proceed and the first deletes the second's.
[[ -L "${lock_file}" ]] \
	|| fail "lock must be a symlink so the holder identity is published atomically"
lock_payload_observed="$(readlink "${lock_file}")"
rg --quiet '^[0-9]+:' < <(printf '%s\n' "${lock_payload_observed}") \
	|| fail "lock payload must lead with the holder pid; got: ${lock_payload_observed}"

# A LIVE holder must block. Uses this test's own pid: guaranteed alive, so the
# assertion cannot pass by accident via a stale-lock reclaim.
rm -f "${lock_file}"
ln -s "$$:/nonexistent/sibling-worktree" "${lock_file}"
set +e
blocked_out="$(try_acquire)"
blocked_status=$?
set -e
[[ "${blocked_status}" -ne 0 ]] \
	|| fail "a second concurrent gate run must fail fast, got exit 0"
rg --quiet 'another live gate is already running' < <(printf '%s\n' "${blocked_out}") \
	|| fail "blocked run must say WHY it refused; got: ${blocked_out}"
rg --quiet 'serialized' < <(printf '%s\n' "${blocked_out}") \
	|| fail "blocked run must explain the serialization requirement"

# PID-reuse defense (#5826 review, P2): a live pid is not enough on its own -
# the kernel can hand a dead holder's pid to an unrelated long-lived process,
# which would otherwise read as a live holder forever and refuse every later
# run. A recorded start-id that does NOT match this pid's actual start-id
# must be treated as unconfirmed and reclaimed, not trusted as live.
rm -f "${lock_file}"
ln -s "$$:0:/nonexistent/reused-pid-worktree" "${lock_file}"
reuse_out="$(try_acquire)" \
	|| fail "a live pid with a mismatched start-id must be reclaimed as stale, not treated as live: ${reuse_out}"
rg --quiet 'reclaimed stale lock' < <(printf '%s\n' "${reuse_out}") \
	|| fail "pid-reuse reclaim must be announced, not silent; got: ${reuse_out}"

# ...and the inverse: a live pid whose start-id genuinely MATCHES must still
# block. Otherwise the fingerprint would defeat live exclusion entirely rather
# than narrow it. Computed the same way production does (LC_ALL=C TZ=UTC), so
# this case does not itself depend on the runner's ambient locale/TZ.
this_startid="$(LC_ALL=C TZ=UTC ps -o lstart= -p "$$" | tr -cd '0-9')"
rm -f "${lock_file}"
ln -s "$$:${this_startid}:/nonexistent/same-worktree" "${lock_file}"
set +e
samestart_out="$(try_acquire)"
samestart_status=$?
set -e
[[ "${samestart_status}" -ne 0 ]] \
	|| fail "a live pid with a MATCHING start-id must still block, got exit 0: ${samestart_out}"
rg --quiet 'another live gate is already running' < <(printf '%s\n' "${samestart_out}") \
	|| fail "matching start-id holder must be reported as running; got: ${samestart_out}"

# Payload-parsing edge cases (TZ/locale-dependent fingerprints, malformed
# colon-less payloads) are extracted to golden-corpus-lock-parse-cases.sh to
# keep this chunk under the 500-line cap.
. "${repo_root}/scripts/lib/golden-corpus-lock-parse-cases.sh"
[[ "${lock_parse_cases_completed:-0}" -eq 1 ]] ||
	fail "golden-corpus-lock-parse-cases.sh did not run to completion (gutted, or returned early)"

# A STALE holder (dead pid) must be reclaimed, or one crashed run wedges the
# gate for every later run.
# ps -p, not kill -0: kill -0 reports EPERM as "dead", which is the exact
# misread the lib documents. And do not silently skip - this hatch used to drop
# five behavioural cases while still printing "pass". Retry for a genuinely dead
# pid and fail if none can be obtained.
dead_pid=""
for _dead_attempt in 1 2 3 4 5 6 7 8 9 10; do
	_candidate_pid="$(bash -c 'echo $$')"
	if ! ps -p "${_candidate_pid}" >/dev/null 2>&1; then
		dead_pid="${_candidate_pid}"
		break
	fi
done
if [[ -z "${dead_pid}" ]]; then
	fail "could not obtain a dead pid in 10 attempts - the stale-reclaim cases would be silently skipped"
else
	rm -f "${lock_file}"
	ln -s "${dead_pid}:/nonexistent/dead-worktree" "${lock_file}"
	reclaim_out="$(try_acquire)" \
		|| fail "stale lock must be reclaimed, not fatal: ${reclaim_out}"
	rg --quiet 'reclaimed stale lock' < <(printf '%s\n' "${reclaim_out}") \
		|| fail "stale reclaim must be announced, not silent; got: ${reclaim_out}"

	# #P1-2 regression: N racers against ONE stale lock must never observe TWO
	# holders inside the critical section AT ONCE. A remove-then-create reclaim
	# lets several racers each believe they hold it simultaneously; that is what
	# must never happen. "exactly one winner across the whole run" is NOT the
	# invariant - see the try_acquire comment for why that assertion itself was
	# wrong (#5826 review: reproduced "got 8" under load with no mutex bug).
	rm -f "${lock_file}"
	ln -s "${dead_pid}:/nonexistent/dead-worktree" "${lock_file}"
	race_dir="$(mktemp -d)"
	race_token="${race_dir}/critical-section.token"
	for racer in 1 2 3 4 5 6 7 8; do
		try_acquire 2 "${race_token}" >"${race_dir}/out.${racer}" 2>&1 &
	done
	wait
	winners=0
	overlaps=0
	for out_file in "${race_dir}"/out.*; do
		if rg --quiet 'ACQUIRED=' "${out_file}"; then
			winners=$(( winners + 1 ))
		fi
		if rg --quiet 'OVERLAP' "${out_file}"; then
			overlaps=$(( overlaps + 1 ))
		fi
	done
	rm -rf "${race_dir}"
	[[ "${overlaps}" -eq 0 ]] \
		|| fail "mutual exclusion violated: ${overlaps} racer(s) found the critical section already occupied"
	[[ "${winners}" -ge 1 ]] \
		|| fail "at least one racer must reclaim the stale lock, got ${winners}"

	# A guard left behind by a dead reclaimer must NOT be reaped while it is
	# fresh - an unconditional reap deletes a guard a racer created microseconds
	# ago, which is the race the guard exists to prevent, one level up. Age
	# comes from the guard's own embedded birth epoch (pid:epoch), never a
	# filesystem mtime - see live-gate-lock.sh for why `find -mmin` was dropped.
	rm -f "${lock_file}"
	ln -s "${dead_pid}:/nonexistent/dead-worktree" "${lock_file}"
	ln -s "${dead_pid}:$(date +%s)" "${lock_file}.reclaim"
	set +e
	fresh_guard_out="$(try_acquire)"
	fresh_guard_status=$?
	set -e
	[[ "${fresh_guard_status}" -ne 0 ]] \
		|| fail "a fresh orphan guard must not be reaped; got exit 0: ${fresh_guard_out}"
	[[ -L "${lock_file}.reclaim" ]] \
		|| fail "a fresh orphan guard must be kept, not reaped"

	# Aged past the retry budget it must be reclaimable, or a reclaimer killed
	# mid-guard would wedge every later run permanently. Backdate the guard's
	# OWN embedded birth epoch (not a filesystem mtime, which the age gate no
	# longer reads at all).
	ln -sfn "${dead_pid}:$(( $(date +%s) - 120 ))" "${lock_file}.reclaim"
	aged_out="$(try_acquire)" \
		|| fail "an aged orphan guard must be reclaimable: ${aged_out}"
	rg --quiet 'ACQUIRED=' < <(printf '%s\n' "${aged_out}") \
		|| fail "aged-guard reclaim must end holding the lock; got: ${aged_out}"
	rm -f "${lock_file}.reclaim"

	# The pre-loop marker check alone is not enough: a run already inside the
	# retry loop when a --keep holder retains would reclaim the now-dead-pid lock
	# and destroy the retained stack. Force that interleaving deterministically by
	# parking the acquirer on a guard held by a LIVE pid, then publishing the
	# marker and freeing the guard.
	rm -f "${lock_file}" "${lock_file}.keep" "${lock_file}.reclaim"
	ln -s "${dead_pid}:/nonexistent/dead-worktree" "${lock_file}"
	ln -s "$$" "${lock_file}.reclaim"
	(
		sleep 1
		: >"${lock_file}.keep"
		rm -f "${lock_file}.reclaim"
	) &
	inloop_helper=$!
	set +e
	inloop_out="$(try_acquire)"
	inloop_status=$?
	set -e
	wait "${inloop_helper}" 2>/dev/null || true
	[[ "${inloop_status}" -ne 0 ]] ||
		fail "a --keep marker published while a racer was mid-loop must still block; got exit 0: ${inloop_out}"
	# Not just non-zero: exhausting the retry budget also exits non-zero, so the
	# case would go green for the wrong reason with the in-loop check removed.
	rg --quiet 'keep run retained the compose stack' < <(printf '%s\n' "${inloop_out}") ||
		fail "the mid-loop refusal must name the retained stack; got: ${inloop_out}"
	rm -f "${lock_file}" "${lock_file}.keep" "${lock_file}.reclaim"
fi

# Release must be conditional on still being the recorded holder: a run whose
# lock was reclaimed as stale must not delete the live holder's lock. Asserted
# behaviourally - a grep passes on `release_live_gate_lock() { :; }`.
rm -f "${lock_file}"
release_out="$(
	ESHU_LIVE_GATE_LOCK_DIR="${lock_home}" bash -c '
		set -euo pipefail
		. "$1"
		acquire_live_gate_lock
		ln -sfn "999999:/other/worktree" "${lock_path}"
		release_live_gate_lock
		printf "SURVIVED=%s\n" "$(readlink "${lock_path}" 2>/dev/null || echo GONE)"
	' _ "${lock_lib}" 2>&1
)" || fail "release probe failed: ${release_out}"
rg --quiet 'SURVIVED=999999:/other/worktree' < <(printf '%s\n' "${release_out}") \
	|| fail "release deleted a foreign holder's lock; got: ${release_out}"

# The happy path must actually release, or one clean run wedges the next.
rm -f "${lock_file}"
ESHU_LIVE_GATE_LOCK_DIR="${lock_home}" bash -c '
	set -euo pipefail
	. "$1"
	acquire_live_gate_lock
	release_live_gate_lock
' _ "${lock_lib}" >/dev/null 2>&1 || fail "acquire+release round trip failed"
[[ ! -e "${lock_file}" && ! -L "${lock_file}" ]] \
	|| fail "release left the lock behind: $(readlink "${lock_file}" 2>/dev/null)"

# The CI bypass must leave the globals safe under `set -u`, since cleanup still
# calls release on that path.
rm -f "${lock_file}"
skip_out="$(
	ESHU_SKIP_LIVE_GATE_LOCK=1 ESHU_LIVE_GATE_LOCK_DIR="${lock_home}" bash -c '
		set -euo pipefail
		. "$1"
		acquire_live_gate_lock
		release_live_gate_lock
		printf "SKIPPED lock_path=[%s]\n" "${lock_path}"
	' _ "${lock_lib}" 2>&1
)" || fail "ESHU_SKIP_LIVE_GATE_LOCK path failed under set -u: ${skip_out}"
rg --quiet 'SKIPPED lock_path=\[\]' < <(printf '%s\n' "${skip_out}") \
	|| fail "skip path must leave lock_path empty; got: ${skip_out}"

# A DIRECTORY at the lock path (what the superseded mkdir lock left behind) must
# not let every caller "acquire": `ln -s` links INTO a directory and reports
# success. No two racers may hold the critical section AT ONCE - same
# mutual-exclusion invariant as the stale-lock race above, and the same
# "exactly one winner across the whole run" assertion would be equally wrong
# here (hold=2s vs a 5s retry budget legitimately allows sequential winners).
rm -rf "${lock_file}"
mkdir -p "${lock_file}"
debris_dir="$(mktemp -d)"
debris_token="${debris_dir}/critical-section.token"
for racer in 1 2 3 4; do
	try_acquire 2 "${debris_token}" >"${debris_dir}/out.${racer}" 2>&1 &
done
wait
debris_winners=0
debris_overlaps=0
for out_file in "${debris_dir}"/out.*; do
	if rg --quiet 'ACQUIRED=' "${out_file}"; then
		debris_winners=$(( debris_winners + 1 ))
	fi
	if rg --quiet 'OVERLAP' "${out_file}"; then
		debris_overlaps=$(( debris_overlaps + 1 ))
	fi
done
rm -rf "${debris_dir}"
[[ "${debris_overlaps}" -eq 0 ]] \
	|| fail "mutual exclusion violated: ${debris_overlaps} racer(s) found the critical section already occupied via directory debris"
[[ "${debris_winners}" -ge 1 ]] \
	|| fail "at least one racer must reclaim the lock past the directory debris, got ${debris_winners}"
[[ -L "${lock_file}" ]] \
	|| fail "debris reclaim must leave a real symlink at the lock path"

# A live holder owned by ANOTHER user must still block. pid 1 is root-owned and
# always alive; `kill -0` fails on it with EPERM, which a naive check misreads
# as dead.
rm -f "${lock_file}"
ln -s "1:/root/other-user-worktree" "${lock_file}"
set +e
cross_user_out="$(try_acquire)"
cross_user_status=$?
set -e
[[ "${cross_user_status}" -ne 0 ]] \
	|| fail "a live holder owned by another user must block, got exit 0: ${cross_user_out}"
rg --quiet 'another live gate is already running' < <(printf '%s\n' "${cross_user_out}") \
	|| fail "cross-user holder must be reported as running; got: ${cross_user_out}"

# The full production retain lifecycle is extracted to keep this file under
# the 500-line cap. Its later acquire uses a running-Docker stub and proves the
# marker remains authoritative after the recorded holder exits.
. "${repo_root}/scripts/lib/golden-corpus-retain-lifecycle-cases.sh"
[[ "${retain_lifecycle_cases_completed:-0}" -eq 1 ]] ||
	fail "golden-corpus-retain-lifecycle-cases.sh did not run to completion"

# The PRE-LOOP marker check is load-bearing in exactly the state its own refusal
# message creates: the operator removes the lock but not the marker. This is the
# only case that isolates it - with a lock present the in-loop check also fires,
# which is why a neutered pre-loop die previously survived the suite.
rm -f "${lock_file}" "${lock_file}.keep"
: >"${lock_file}.keep"
set +e
keeponly_out="$(try_acquire)"
keeponly_status=$?
set -e
[[ "${keeponly_status}" -ne 0 ]] \
	|| fail "a .keep marker without a lock must still block; got exit 0: ${keeponly_out}"
rg --quiet 'keep run retained the compose stack' < <(printf '%s\n' "${keeponly_out}") \
	|| fail ".keep-without-lock refusal must name the retained stack; got: ${keeponly_out}"
rm -f "${lock_file}.keep"

. "${repo_root}/scripts/lib/golden-corpus-lock-race-cases.sh"

# Completion sentinel: `bash -n` only catches syntax errors. A chunk gutted to
# comments, or one that returns early, sources cleanly and would otherwise skip
# every case in it while the run still reported pass.
lock_cases_completed=1
