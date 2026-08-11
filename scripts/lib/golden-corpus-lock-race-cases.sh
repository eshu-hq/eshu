#!/usr/bin/env bash
# Race and mutual-exclusion cases for the live-gate mutex, split out of
# golden-corpus-lock-cases.sh to keep both chunks under the 500-line cap.
#
# Sourced, never executed: runs in the caller's shell and uses its ${lock_lib},
# ${lock_file}, ${lock_home}, try_acquire(), fail() and require_lock().
#
# These are behavioural on purpose. A mutex cannot be proven by assertions on its
# source: every defect found in this lock so far survived at least one text pin.

# MUTUAL EXCLUSION. Counting winners is not an exclusion proof - it cannot see
# two runs occupying the critical section at once, and this suite was green on a
# lock that admitted exactly that. Racers RETRY on refusal so they keep polling
# and can hit the free-name window; occupancy is asserted with `set -o
# noclobber` create-or-fail. The no-lock control proves the harness has power,
# so a clean run means "excluded", not "stopped looking".
# #5826 review, F-3: each racer retries at most 6 times and, if every attempt
# is refused, exits the subshell silently - no ACQUIRED, no VIOLATION, nothing
# in the log. Under heavy exclusion (the mutex working AS INTENDED) that can
# legitimately happen for every racer in every round, and the old return value
# was violations alone: `real_violations -eq 0` then passes having sampled
# NOTHING, which is indistinguishable from a clean 180-acquisition run. Emit
# an ACQUIRED marker for every successful `acquire_live_gate_lock` (independent
# of whether that attempt also won or lost the noclobber occupancy race) and
# return the acquired count alongside violations, so the caller can assert a
# floor and fail loudly on "never sampled" instead of reading it as "excluded".
occupancy_probe() {
	local probe_lib="$1" racers="$2" rounds="$3" home token start round i
	home="$(mktemp -d)"
	local violations=0 acquired=0
	for round in $(seq 1 "${rounds}"); do
		rm -rf "${home}"; mkdir -p "${home}"
		token="${home}/OCCUPIED"; start="${home}/START"
		for i in $(seq 1 "${racers}"); do
			(
				while [[ ! -e "${start}" ]]; do :; done
				for _ in 1 2 3 4 5 6; do
					ESHU_LIVE_GATE_LOCK_DIR="${home}" bash -c '
						set -uo pipefail
						. "$1"
						acquire_live_gate_lock 2>/dev/null || exit 7
						printf "ACQUIRED\n"
						( set -o noclobber; : > "$2" ) 2>/dev/null || printf "VIOLATION\n"
						sleep 0.02
						rm -f "$2"
						release_live_gate_lock
					' _ "${probe_lib}" "${token}" 2>/dev/null && break
					sleep 0.05
				done
			) >>"${home}/log" 2>&1 &
		done
		: >"${start}"
		wait
		violations=$(( violations + $(rg --count 'VIOLATION' "${home}/log" 2>/dev/null || echo 0) ))
		acquired=$(( acquired + $(rg --count 'ACQUIRED' "${home}/log" 2>/dev/null || echo 0) ))
		rm -f "${home}/log"
	done
	rm -rf "${home}"
	printf '%s %s\n' "${violations}" "${acquired}"
}

no_lock_stub="$(mktemp)"
cat >"${no_lock_stub}" <<'STUB'
lock_path=""; lock_payload=""
acquire_live_gate_lock() { lock_path="${ESHU_LIVE_GATE_LOCK_DIR}/eshu-live-gate.lock"; lock_payload="$$"; return 0; }
release_live_gate_lock() { return 0; }
STUB
read -r control_violations control_acquired <<<"$(occupancy_probe "${no_lock_stub}" 8 4)"
rm -f "${no_lock_stub}"
[[ "${control_violations}" -gt 0 ]] \
	|| fail "occupancy harness has no power: the no-lock control reported 0 violations"
# Mirrors the two sibling races' `-ge 1` winner floor, scaled to this probe's
# racer count: at least one acquisition per racer across the whole run, or the
# harness sampled too little to say anything about exclusion.
[[ "${control_acquired}" -ge 8 ]] \
	|| fail "occupancy control harness never sampled: only ${control_acquired} acquisition(s) across 8 racers - its 0-violation floor above would be meaningless"

read -r real_violations real_acquired <<<"$(occupancy_probe "${lock_lib}" 12 15)"
[[ "${real_acquired}" -ge 12 ]] \
	|| fail "occupancy probe never sampled the real mutex: only ${real_acquired} acquisition(s) across 12 racers over 15 rounds - 'excluded' is indistinguishable from 'never sampled' below this floor"
[[ "${real_violations}" -eq 0 ]] \
	|| fail "the mutex admitted ${real_violations} overlapping occupancies (control detected ${control_violations})"

# The read->act window. ${current} is captured BEFORE the liveness fork, so a
# holder alive at read time can release and exit during that fork while an
# ordinary claimer - which the reclaim guard does NOT exclude - takes the freed
# name. Constructed deterministically: the probabilistic harness above cannot
# reach this window, and a text pin cannot see a literal-preserving mutant.
window_home="$(mktemp -d)"
window_lock="${window_home}/eshu-live-gate.lock"
window_dead="$(bash -c 'echo $$')"
sleep 60 & window_y=$!
sleep 60 & window_c=$!
ln -s "${window_dead}:/dead-worktree" "${window_lock}"
(
	ESHU_LIVE_GATE_LOCK_DIR="${window_home}" bash -c '
		set -uo pipefail
		. "$1"
		# Widen the read->act window without touching production code: the lib
		# resolves process_is_alive at call time.
		process_is_alive() { sleep 1.5; ps -p "$1" >/dev/null 2>&1; }
		acquire_live_gate_lock 2>/dev/null && printf "R_ACQUIRED\n"
	' _ "${lock_lib}"
) >"${window_home}/r.out" 2>&1 &
window_r=$!
sleep 0.5
rm -f "${window_lock}"
ln -s "${window_y}:/y-worktree" "${window_lock}"      # alive at the guard-side read
sleep 1.3
kill "${window_y}" 2>/dev/null || true
wait "${window_y}" 2>/dev/null || true
rm -f "${window_lock}"
ln -s "${window_c}:/claimer-worktree" "${window_lock}" \
	|| fail "window case could not publish the live claimer's lock"
wait "${window_r}" 2>/dev/null || true
window_now="$(readlink "${window_lock}" 2>/dev/null || echo GONE)"
kill "${window_c}" 2>/dev/null || true
wait "${window_c}" 2>/dev/null || true
rm -rf "${window_home}"
[[ "${window_now}" == "${window_c}:/claimer-worktree" ]] \
	|| fail "the reclaim destroyed a LIVE claimer's lock during the liveness fork (lock became: ${window_now})"

# A non-symlink at the GUARD path used to wedge acquire permanently: readlink
# yields nothing so the dead-pid arm can never fire, and an age gate cannot help
# either, because probing a directory with claim_lock_link creates and removes an
# entry inside it and so resets its mtime on every attempt.
rm -rf "${lock_file}" "${lock_file}.reclaim" "${lock_file}.keep"
ln -s "${window_dead}:/dead-worktree" "${lock_file}"
mkdir -p "${lock_file}.reclaim"
guard_debris_out="$(try_acquire)" \
	|| fail "a non-symlink guard must self-heal, not wedge: ${guard_debris_out}"
rg --quiet 'ACQUIRED=' <<<"${guard_debris_out}" \
	|| fail "guard-debris reclaim must end holding the lock; got: ${guard_debris_out}"

# ...but a guard held by a LIVE reclaimer must still be respected.
rm -rf "${lock_file}" "${lock_file}.reclaim"
sleep 30 & guard_live=$!
ln -s "${window_dead}:/dead-worktree" "${lock_file}"
ln -s "${guard_live}" "${lock_file}.reclaim"
set +e
guard_live_out="$(try_acquire)"
guard_live_status=$?
set -e
kill "${guard_live}" 2>/dev/null || true
wait "${guard_live}" 2>/dev/null || true
[[ "${guard_live_status}" -ne 0 ]] \
	|| fail "a guard held by a LIVE reclaimer must not be reaped; got exit 0: ${guard_live_out}"
rm -rf "${lock_file}" "${lock_file}.reclaim"

# On a FREE name the destroy must never run at all. The re-read narrows that
# window to one fork; only skipping the destroy keeps it at zero. Asserted by
# counting invocations rather than racing the fork: shim `rm` after sourcing (the
# lib resolves it at call time) and require zero destroys of the lock path. A
# text pin cannot carry this - appending an unconditional `current_is_debris=1`
# after the block preserves every pinned literal and still reintroduces it.
freename_home="$(mktemp -d)"
freename_dead="$(bash -c 'echo $$')"
ln -s "${freename_dead}:/dead-worktree" "${freename_home}/eshu-live-gate.lock"
freename_out="$(
	ESHU_LIVE_GATE_LOCK_DIR="${freename_home}" bash -c '
		set -uo pipefail
		. "$1"
		lockpath="$2/eshu-live-gate.lock"
		# Free the name the instant the outer liveness check runs, so the
		# guard-side read sees an empty name.
		# command rm: this teardown must not be counted by the rm shim below
		process_is_alive() { command rm -f "${lockpath}"; return 1; }
		rm() {
			for a in "$@"; do
				# fd 3 keeps the marker out of the discarded stdout/stderr
				[[ "${a}" == "${lockpath}" ]] && printf "DESTROY\n" >&3
			done
			command rm "$@"
		}
		acquire_live_gate_lock >/dev/null 2>&1 || true
	' _ "${lock_lib}" "${freename_home}" 3>&1 >/dev/null 2>&1
)"
freename_destroys="$(printf '%s\n' "${freename_out}" | rg --count 'DESTROY' || true)"
rm -rf "${freename_home}"
[[ "${freename_destroys:-0}" -eq 0 ]] \
	|| fail "the reclaim destroyed a FREE lock name ${freename_destroys} time(s); an ordinary claimer, which the guard does not exclude, can hold it"

# The reclaim guard must be exclusive, or the lock destroy above loses its only
# guarantee: a non-exclusive guard lets two reclaimers validate the same dead
# holder and both claim. Both reap arms therefore re-validate before deleting.
guardpin_live="$(mktemp -d)"
sleep 30 & guardpin_pid=$!
guardpin_payload="${guardpin_pid}:$(( $(date +%s) - 120 ))"
ln -s "${freename_dead:-1}:/dead-worktree" "${guardpin_live}/eshu-live-gate.lock"
# Backdated birth epoch (120s old, past the 60s budget) so this case actually
# exercises the age-eligible path; liveness alone must still refuse the reap.
ln -s "${guardpin_payload}" "${guardpin_live}/eshu-live-gate.lock.reclaim"
ESHU_LIVE_GATE_LOCK_DIR="${guardpin_live}" bash -c '
	set -uo pipefail
	. "$1"
	acquire_live_gate_lock >/dev/null 2>&1 || true
' _ "${lock_lib}" || true
guardpin_now="$(readlink "${guardpin_live}/eshu-live-gate.lock.reclaim" 2>/dev/null || echo GONE)"
kill "${guardpin_pid}" 2>/dev/null || true
wait "${guardpin_pid}" 2>/dev/null || true
rm -rf "${guardpin_live}"
[[ "${guardpin_now}" == "${guardpin_payload}" ]] \
	|| fail "an aged guard held by a LIVE reclaimer was reaped (now: ${guardpin_now})"
require_lock "guard reap re-validates" \
	'if [[ "$(readlink "${guard}" 2>/dev/null || true)" == "${guard_payload}" ]]; then'
# The OTHER reap arm. Deleting this leaves an unconditional `rm -rf "${guard}"`,
# preserves every other pinned literal, and steals a live guard 5/5.
require_lock "debris guard reap re-validates" '[[ ! -L "${guard}" && -e "${guard}" ]]'
# Both guard-reap arms were pin-only, and a pin cannot see an APPENDED destroy:
# adding `rm -rf "${guard}"` after the debris arm steals a live reclaimer's guard
# (measured 50 thefts), and `rm -f "${guard}"` after the aged arm does the same.
# The guard is the invariant the lock destroy rests on, and "just make sure the
# guard is gone" is the most likely future hardening edit, so count them.
guard_hard_destroys="$(rg -v '^[[:space:]]*#' "${lock_lib}" | rg --count --fixed-strings -- 'rm -rf "${guard}"' || true)"
[[ "${guard_hard_destroys:-0}" -eq 1 ]] ||
	fail "expected exactly 1 unconditional-shape 'rm -rf \"\${guard}\"', found ${guard_hard_destroys:-0}: an appended destroy steals a live reclaimer's guard"
guard_soft_destroys="$(rg -v '^[[:space:]]*#' "${lock_lib}" | rg --count --fixed-strings -- 'rm -f "${guard}"' || true)"
[[ "${guard_soft_destroys:-0}" -eq 5 ]] ||
	fail "expected exactly 5 'rm -f \"\${guard}\"', found ${guard_soft_destroys:-0}: an appended guard release can reap a live reclaimer's guard"
# The free-name probe above counts invocations of a shell function named `rm`.
# Changing the destroy to `command rm` or `mv` is a plausible hardening edit that
# would silently blind it, so the destroy statement itself is pinned.
require_lock "destroy stays observable to the rm shim" 'rm -rf "${candidate}"'
# ...and the bypasses must not appear at all: `command rm -rf "${candidate}"`
# CONTAINS the pinned string, so the pin above cannot see that substitution, and
# a rename would evade the shim entirely. Neither has any legitimate use here.
for destroy_bypass in 'command rm' 'mv '; do
	if rg -v '^[[:space:]]*#' "${lock_lib}" | rg --fixed-strings --quiet -- "${destroy_bypass}"; then
		fail "live-gate-lock.sh uses '${destroy_bypass}': it bypasses the rm shim the free-name probe depends on"
	fi
done

# Counting destroys catches an APPENDED one but not a RELOCATED one: moving
# `rm -f "${guard}"` out of its re-validation and leaving the conditional as a
# no-op keeps both counts and both pins, and steals a live reclaimer's guard
# 20/20. Assert the OUTCOME instead - that kills the whole class (appended,
# relocated, verb-substituted, decoy-wrapped) rather than one member of it.
reval_home="$(mktemp -d)"
reval_lock="${reval_home}/eshu-live-gate.lock"
reval_guard="${reval_lock}.reclaim"
reval_dead_lock="$(bash -c 'echo $$')"
reval_dead_guard="$(bash -c 'echo $$')"
if [[ "${reval_dead_lock}" == "${reval_dead_guard}" ]] || ps -p "${reval_dead_guard}" >/dev/null 2>&1; then
	fail "need two distinct dead pids for the guard re-validation case"
fi
sleep 30 &
reval_live=$!
ln -s "${reval_dead_lock}:/dead-holder" "${reval_lock}"
# Backdated birth epoch (120s old, past the 60s budget) so the age gate is
# actually eligible to fire - age comes from this embedded epoch, never a
# filesystem mtime.
ln -s "${reval_dead_guard}:$(( $(date +%s) - 120 ))" "${reval_guard}"
ESHU_LIVE_GATE_LOCK_DIR="${reval_home}" DG="${reval_dead_guard}" \
	LIVE="${reval_live}" G="${reval_guard}" bash -c '
	set -uo pipefail
	. "$1"
	# Republish a LIVE guard at the instant the liveness decision is taken, so
	# the value the reap decided on is no longer the value at the path.
	process_is_alive() {
		if [[ "$1" == "${DG}" ]]; then
			ln -sfn "${LIVE}" "${G}"
			return 1
		fi
		ps -p "$1" >/dev/null 2>&1
	}
	acquire_live_gate_lock >/dev/null 2>&1 || true
' _ "${lock_lib}" >/dev/null 2>&1 || true
reval_now="$(readlink "${reval_guard}" 2>/dev/null || echo GONE)"
kill "${reval_live}" 2>/dev/null || true
wait "${reval_live}" 2>/dev/null || true
rm -rf "${reval_home}"
[[ "${reval_now}" == "${reval_live}" ]] \
	|| fail "the aged-guard reap destroyed a guard a LIVE reclaimer republished during the liveness decision (guard became: ${reval_now})"

# THE --keep MARKER'S OWN LIVENESS (#5987). The ordinary lock is reclaimed when
# its recorded pid is gone; the `.keep` marker recorded only a worktree path, so
# a marker whose session crashed, was killed, or exited without cleanup was
# indistinguishable from one still holding a retained stack -- and the block is
# unconditional, so it refused every other worktree forever. Observed: four
# consecutive pre-pr runs from an unrelated worktree lost their live lane over
# ~40 minutes with no safe way to tell the two cases apart.
#
# Behavioural on purpose, like every other case here: a text pin on the marker
# format cannot see a marker that parses correctly and is still treated as an
# unconditional block.
keep_home="$(mktemp -d)"
keep_lock="${keep_home}/eshu-live-gate.lock"
keep_dead="$(bash -c 'echo $$')"
ps -p "${keep_dead}" >/dev/null 2>&1 \
	&& fail "need a genuinely dead pid for the --keep marker case"

# The discriminator is the STACK, not the pid. retain_live_gate_lock runs in
# the cleanup trap immediately before `exit`, so a healthy --keep holder's pid
# is always dead; reclaiming on a dead pid would discard every legitimate
# marker at the moment it starts mattering, hand the fixed ports to the next
# run, and let its `docker compose down -v` destroy the retained stack.
#
# `docker` is stubbed on PATH so these cases assert the decision logic without
# needing a real daemon, and stay hermetic in CI.
mkdir -p "${keep_home}/empty-bin"
keep_bin="${keep_home}/bin"
mkdir -p "${keep_bin}"

# Stack GONE (`ps -q` prints nothing) -> the marker is debris and must not block.
printf '#!/usr/bin/env bash\nexit 0\n' >"${keep_bin}/docker"
chmod +x "${keep_bin}/docker"
printf '%s:%s:%s:%s\n' "${keep_dead}" "0" "eshu-gate-dead" "/dead-keep-worktree" >"${keep_lock}.keep"
keep_dead_out="$(
	PATH="${keep_bin}:${PATH}" ESHU_LIVE_GATE_LOCK_DIR="${keep_home}" bash -c '
		set -uo pipefail
		. "$1"
		acquire_live_gate_lock >/dev/null && printf "KEEP_RECLAIMED\n"
	' _ "${lock_lib}" 2>&1 || true
)"
rm -f "${keep_lock}"
[[ "${keep_dead_out}" == *KEEP_RECLAIMED* ]] \
	|| fail "a --keep marker whose compose stack is GONE blocked the run; it is debris and must be reclaimable (got: ${keep_dead_out})"
[[ ! -e "${keep_lock}.keep" ]] \
	|| fail "reclaiming a debris --keep marker left the marker on disk, so the next run blocks on it again"

# Stack STILL UP (`ps -q` prints a container id) -> must keep blocking, even
# though the recorded pid is long dead. This is the case that makes --keep
# worth having.
printf '#!/usr/bin/env bash\necho deadbeefcafe\n' >"${keep_bin}/docker"
printf '%s:%s:%s:%s\n' "${keep_dead}" "0" "eshu-gate-live" "/live-keep-worktree" >"${keep_lock}.keep"
keep_up_out="$(
	PATH="${keep_bin}:${PATH}" ESHU_LIVE_GATE_LOCK_DIR="${keep_home}" bash -c '
		set -uo pipefail
		. "$1"
		acquire_live_gate_lock >/dev/null && printf "KEEP_RECLAIMED\n"
	' _ "${lock_lib}" 2>&1 || true
)"
rm -f "${keep_lock}" "${keep_lock}.keep"
[[ "${keep_up_out}" != *KEEP_RECLAIMED* ]] \
	|| fail "a --keep marker whose compose stack is STILL UP was reclaimed; the next run would tear that stack down on exit"
[[ "${keep_up_out}" == *eshu-gate-live* ]] \
	|| fail "the refusal did not name the retained compose project, so the operator still cannot tell which case they are in (got: ${keep_up_out})"

# Cannot determine (docker missing) -> fail closed, and say so rather than
# guessing. A wrong "debris" verdict destroys someone's stack; a wrong "blocked"
# verdict costs a wait.
printf '%s:%s:%s:%s\n' "${keep_dead}" "0" "eshu-gate-unknown" "/unknown-keep-worktree" >"${keep_lock}.keep"
keep_nodocker_out="$(
	PATH="${keep_home}/empty-bin" ESHU_LIVE_GATE_LOCK_DIR="${keep_home}" bash -c '
		set -uo pipefail
		. "$1"
		acquire_live_gate_lock >/dev/null && printf "KEEP_RECLAIMED\n"
	' _ "${lock_lib}" 2>&1 || true
)"
rm -f "${keep_lock}" "${keep_lock}.keep"
[[ "${keep_nodocker_out}" != *KEEP_RECLAIMED* ]] \
	|| fail "a --keep marker was reclaimed while docker was unavailable; the stack could not be checked, so this must fail closed"

# A legacy path-only marker names no project, so its stack cannot be checked:
# it must keep blocking rather than be reclaimed on a format it predates.
printf '%s\n' "/legacy-keep-worktree" >"${keep_lock}.keep"
keep_legacy_out="$(
	PATH="${keep_bin}:${PATH}" ESHU_LIVE_GATE_LOCK_DIR="${keep_home}" bash -c '
		set -uo pipefail
		. "$1"
		acquire_live_gate_lock >/dev/null && printf "KEEP_RECLAIMED\n"
	' _ "${lock_lib}" 2>&1 || true
)"
rm -rf "${keep_home}"
[[ "${keep_legacy_out}" != *KEEP_RECLAIMED* ]] \
	|| fail "a legacy path-only --keep marker was reclaimed; that format records no compose project, so its stack cannot be confirmed down"

drop_lock_home
trap - EXIT

# Completion sentinel: `bash -n` only catches syntax errors. A chunk gutted to
# comments, or one that returns early, sources cleanly and would otherwise skip
# every case in it while the run still reported pass.
lock_race_cases_completed=1
