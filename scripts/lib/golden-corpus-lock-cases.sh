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
require "lock released on exit" 'release_live_gate_lock'
# --keep leaves the stack up on the fixed ports, so the ORCHESTRATOR must retain
# the mutex on that path rather than release it. Asserted against the gate, not
# the lib: the lib merely defines the function.
rg --pcre2 --multiline --quiet \
	'if \[\[ "\$\{keep\}" -eq 1 \]\]; then\n\t\tretain_live_gate_lock\n\telse\n\t\trelease_live_gate_lock' \
	"${script}" ||
	fail "the --keep cleanup branch must RETAIN the mutex and the else branch release it"

require_lock() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${lock_lib}" \
		|| fail "missing ${label} in live-gate-lock.sh: ${needle}"
}
require_lock "atomic symlink claim" 'ln -s "${payload}" "${candidate}"'
require_lock "exclusive stale-reclaim guard" 'claim_lock_link "$$" "${guard}"'
require_lock "reclaim re-validates the holder" \
	'if ! { [[ "${current_pid}" =~ ^[0-9]+$ ]] && process_is_alive "${current_pid}"; }; then'
require_lock "CI bypass" 'ESHU_SKIP_LIVE_GATE_LOCK'
require_lock "test lock relocation" 'ESHU_LIVE_GATE_LOCK_DIR'
# `ln -s` into a DIRECTORY links inside it and reports success, so the claim
# must be verified rather than trusted, or a stale mkdir-era lock dir silently
# stops the mutex excluding anything.
require_lock "verified claim" 'claim_lock_link'
# `kill -0` fails with EPERM on another user's live process, which would read as
# "dead" and let a second run reclaim a live lock.
require_lock "ownership-independent liveness" 'ps -p "$1"'
if rg -v '^[[:space:]]*#' "${lock_lib}" | rg --fixed-strings --quiet -- 'kill -0'; then
	fail "liveness reverted to kill -0 (misreads another user's live process as dead)"
fi
# The orphan-guard reap must be age-gated; an unconditional reap can delete a
# guard a racer created microseconds ago.
# Pin the POLARITY, not the flag: inverting this to reap guards YOUNGER than the
# budget reinstates the bug while keeping the "-mmin +1" substring.
require_lock "age-gate polarity" '[[ -n "$(find "${guard}" -prune -mmin +1 2>/dev/null)" ]]'
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
try_acquire() {
	local hold="${1:-0}"
	ESHU_LIVE_GATE_LOCK_DIR="${lock_home}" bash -c '
		set -euo pipefail
		. "$1"
		acquire_live_gate_lock
		printf "ACQUIRED=%s\n" "${lock_path}"
		[[ "$2" == "0" ]] || sleep "$2"
	' _ "${lock_lib}" "${hold}" 2>&1
}

# A free lock is acquired.
acquired_out="$(try_acquire)" \
	|| fail "acquire on a free lock must succeed; got: ${acquired_out}"
rg --quiet 'ACQUIRED=.*eshu-live-gate.lock' <<<"${acquired_out}" \
	|| fail "acquire must end holding the lock; got: ${acquired_out}"

# #P1-1 regression: the lock must be a symlink whose payload ALREADY carries the
# holder pid. With `mkdir` plus a separate pid write there is a window where the
# lock exists with no pid, and a second run reads that as "stale" and reclaims a
# LIVE lock - after which both runs proceed and the first deletes the second's.
[[ -L "${lock_file}" ]] \
	|| fail "lock must be a symlink so the holder identity is published atomically"
lock_payload_observed="$(readlink "${lock_file}")"
rg --quiet '^[0-9]+:' <<<"${lock_payload_observed}" \
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
rg --quiet 'another live gate is already running' <<<"${blocked_out}" \
	|| fail "blocked run must say WHY it refused; got: ${blocked_out}"
rg --quiet 'serialized' <<<"${blocked_out}" \
	|| fail "blocked run must explain the serialization requirement"

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
	rg --quiet 'reclaimed stale lock' <<<"${reclaim_out}" \
		|| fail "stale reclaim must be announced, not silent; got: ${reclaim_out}"

	# #P1-2 regression: N racers against ONE stale lock must produce exactly one
	# winner. The winner holds the lock while the losers evaluate it, so a
	# correct implementation refuses every loser. A remove-then-create reclaim
	# lets several racers each believe they won.
	rm -f "${lock_file}"
	ln -s "${dead_pid}:/nonexistent/dead-worktree" "${lock_file}"
	race_dir="$(mktemp -d)"
	for racer in 1 2 3 4 5 6 7 8; do
		try_acquire 2 >"${race_dir}/out.${racer}" 2>&1 &
	done
	wait
	winners=0
	for out_file in "${race_dir}"/out.*; do
		if rg --quiet 'ACQUIRED=' "${out_file}"; then
			winners=$(( winners + 1 ))
		fi
	done
	rm -rf "${race_dir}"
	[[ "${winners}" -eq 1 ]] \
		|| fail "exactly one racer may reclaim a stale lock, got ${winners}"

	# A guard left behind by a dead reclaimer must NOT be reaped while it is
	# fresh - an unconditional reap deletes a guard a racer created microseconds
	# ago, which is the race the guard exists to prevent, one level up.
	rm -f "${lock_file}"
	ln -s "${dead_pid}:/nonexistent/dead-worktree" "${lock_file}"
	ln -s "${dead_pid}" "${lock_file}.reclaim"
	set +e
	fresh_guard_out="$(try_acquire)"
	fresh_guard_status=$?
	set -e
	[[ "${fresh_guard_status}" -ne 0 ]] \
		|| fail "a fresh orphan guard must not be reaped; got exit 0: ${fresh_guard_out}"
	[[ -L "${lock_file}.reclaim" ]] \
		|| fail "a fresh orphan guard must be kept, not reaped"

	# Aged past the retry budget it must be reclaimable, or a reclaimer killed
	# mid-guard would wedge every later run permanently.
	touch -h -t 202001010000 "${lock_file}.reclaim"
	aged_out="$(try_acquire)" \
		|| fail "an aged orphan guard must be reclaimable: ${aged_out}"
	rg --quiet 'ACQUIRED=' <<<"${aged_out}" \
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
	rg --quiet 'keep run retained the compose stack' <<<"${inloop_out}" ||
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
rg --quiet 'SURVIVED=999999:/other/worktree' <<<"${release_out}" \
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
rg --quiet 'SKIPPED lock_path=\[\]' <<<"${skip_out}" \
	|| fail "skip path must leave lock_path empty; got: ${skip_out}"

# A DIRECTORY at the lock path (what the superseded mkdir lock left behind) must
# not let every caller "acquire": `ln -s` links INTO a directory and reports
# success. Exactly one racer may come out of this holding the lock.
rm -rf "${lock_file}"
mkdir -p "${lock_file}"
debris_dir="$(mktemp -d)"
for racer in 1 2 3 4; do
	try_acquire 2 >"${debris_dir}/out.${racer}" 2>&1 &
done
wait
debris_winners=0
for out_file in "${debris_dir}"/out.*; do
	if rg --quiet 'ACQUIRED=' "${out_file}"; then
		debris_winners=$(( debris_winners + 1 ))
	fi
done
rm -rf "${debris_dir}"
[[ "${debris_winners}" -eq 1 ]] \
	|| fail "a directory at the lock path must not admit ${debris_winners} holders (want 1)"
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
rg --quiet 'another live gate is already running' <<<"${cross_user_out}" \
	|| fail "cross-user holder must be reported as running; got: ${cross_user_out}"

# A --keep run leaves the compose stack up on the fixed ports, so it retains the
# mutex too. Releasing it would hand those ports to the next run, which would
# then tear the retained stack down with `docker compose down -v` on its exit.
# The marker must outlive the holder pid, so pid liveness must not override it.
rm -f "${lock_file}" "${lock_file}.keep"
retain_out="$(
	ESHU_LIVE_GATE_LOCK_DIR="${lock_home}" bash -c '
		set -euo pipefail
		. "$1"
		acquire_live_gate_lock
		retain_live_gate_lock
		printf "MARKER=%s LOCK=%s\n" \
			"$([[ -e "${lock_path}.keep" ]] && echo yes || echo no)" \
			"$([[ -L "${lock_path}" ]] && echo yes || echo no)"
	' _ "${lock_lib}" 2>&1
)" || fail "retain probe failed: ${retain_out}"
rg --quiet 'MARKER=yes LOCK=yes' <<<"${retain_out}" \
	|| fail "retain must write the marker and leave the lock in place; got: ${retain_out}"

# And the retained lock must block the next run even though the holder pid is
# dead - that is the whole point of the marker outliving the pid.
set +e
keep_out="$(try_acquire)"
keep_status=$?
set -e
[[ "${keep_status}" -ne 0 ]] \
	|| fail "a --keep-retained lock must block a later run, got exit 0: ${keep_out}"
rg --quiet 'keep run retained the compose stack' <<<"${keep_out}" \
	|| fail "--keep refusal must explain the retained stack; got: ${keep_out}"
rm -f "${lock_file}.keep"

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
rg --quiet 'keep run retained the compose stack' <<<"${keeponly_out}" \
	|| fail ".keep-without-lock refusal must name the retained stack; got: ${keeponly_out}"
rm -f "${lock_file}.keep"

. "${repo_root}/scripts/lib/golden-corpus-lock-race-cases.sh"

# Completion sentinel: `bash -n` only catches syntax errors. A chunk gutted to
# comments, or one that returns early, sources cleanly and would otherwise skip
# every case in it while the run still reported pass.
lock_cases_completed=1
