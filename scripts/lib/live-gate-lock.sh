#!/usr/bin/env bash
# Cross-run mutex for the live gates.
#
# Sourced, never executed. Provides acquire_live_gate_lock and
# release_live_gate_lock plus the lock_path/lock_payload globals the caller's
# cleanup trap reads. It lives in its own chunk so the real implementation is
# sourceable by scripts/test-verify-golden-corpus-gate.sh -- a test that
# re-implements or text-extracts the lock proves nothing about the lock.
#
# The caller is expected to define die(); this fallback keeps the lib usable
# standalone (and therefore testable) without one.
if ! declare -F die >/dev/null 2>&1; then
	die() { printf 'live-gate-lock: %s\n' "$*" >&2; exit 1; }
fi

# ----------------------------------------------------------------------------
# Cross-run mutex.
#
# This gate binds FIXED host ports (Postgres ${ESHU_POSTGRES_PORT}, api
# ${GATE_API_PORT}, mcp ${GATE_MCP_PORT}) and a compose project derived from the
# worktree name. Two runs started from different worktrees do not fail cleanly on
# a port bind - they contend for CPU and Docker I/O, and the loser shows up as a
# drain that never reaches terminal: `fact_work_items_residual: residual=1
# (dead_letter=1)` after the drain timeout. That failure reads exactly like a
# real reducer defect, so it costs a long, wrong investigation before anyone
# thinks to look at what else is running.
#
# Scope, stated precisely because the loose version invites the exact mistake
# this exists to prevent: it serializes THIS script against itself, within one
# clone. Port disjointness is NOT safety - the failure above is CPU and Docker
# I/O contention, so another Docker-heavy gate starves this one even on
# different ports. And the lock lives under the git common dir, so a separate
# clone of eshu on the same machine has a separate lock and escapes it entirely.
#
# Why a symlink rather than `mkdir` plus a pid file: the holder's identity has to
# become visible in the SAME atomic step that claims the name. With `mkdir`
# followed by a separate write, a freshly created lock is briefly pid-less, and a
# second run reading it cannot distinguish "live holder, mid-publish" from
# "crashed holder, stale" - so it reclaims a live lock and both runs proceed.
# `ln -s` publishes the payload and claims the name in one operation, so that
# window does not exist: `readlink` returns either nothing or a complete payload.
#
# Reclaiming a stale lock IS remove-then-create - there is no `mv` here. That
# shape is unsafe on its own (two racers both "win", the second destroying the
# first's fresh lock), so three things make it safe together: the reclaim is
# serialized behind an exclusive `.reclaim` guard, the destroy re-reads the value
# it validated immediately before acting, and the re-claim is create-or-fail. Do
# not "simplify" this to a rename: an atomic rename is still not conditional on
# the value being the one that was read, which is the property that matters.
#
# ESHU_SKIP_LIVE_GATE_LOCK=1 bypasses the mutex - for CI, where each job has an
# isolated runner and there is no sibling worktree to collide with.
# ESHU_LIVE_GATE_LOCK_DIR relocates it, so tests can exercise the lock without
# touching the real one under .git.
# ----------------------------------------------------------------------------
lock_path=""
lock_payload=""

# `kill -0` reports FAILURE on EPERM, so a live process owned by another user
# reads as dead - and a second run would reclaim a live lock. `ps -p` is
# ownership-independent and portable to macOS and Linux.
process_is_alive() {
	ps -p "$1" >/dev/null 2>&1
}

# A pid alone is unique only among CURRENTLY-alive processes. If a gate is
# killed without cleanup and the kernel later hands its pid to an unrelated
# long-lived process, a pid-only liveness check reads the stale lock as live
# forever and refuses every later run. `ps -o lstart=` gives that pid's actual
# start time; reduced to digits (tr -cd) it is a colon-free fingerprint safe to
# embed in the payload. Returns empty when the pid is not currently running,
# which callers treat as "cannot confirm" rather than "stale."
#
# LC_ALL=C TZ=UTC is mandatory here, not cosmetic (#5826 review, P1): `lstart`
# renders in the CALLER's locale/TZ, not a fixed one, so the SAME live pid
# produces a DIFFERENT fingerprint depending on what TZ/LC_ALL happened to be
# ambient when each side ran `ps`. A holder recorded under TZ=UTC and read
# back under TZ=Asia/Tokyo then mismatches even though the process never
# changed, which reads as "stale" and gets reclaimed - destroying a LIVE
# holder's lock. That fails the mutex OPEN, which is worse than the pid-reuse
# bug this fingerprint exists to close: two runs proceed at once instead of
# one being wrongly refused. Pinning both LC_ALL and TZ inside the helper
# makes the fingerprint a function of the pid alone, never of the caller's
# environment.
# The `|| true` is load-bearing under `set -e`: `ps -p` exits non-zero for a
# dead, empty, or unreadable pid, and callers that capture this into a variable
# BEFORE the liveness check would otherwise abort the whole script instead of
# receiving the documented empty "cannot confirm" result.
start_id_for_pid() {
	LC_ALL=C TZ=UTC ps -o lstart= -p "$1" 2>/dev/null | tr -cd '0-9' || true
}

# The --keep marker decision lives in its own chunk (see that file's header for
# why the compose stack, not the holder pid, is the discriminator). Resolved
# relative to THIS file so it is found however the caller was invoked.
. "${BASH_SOURCE[0]%/*}/golden-corpus-keep-marker.sh"

# Translate the marker helper's explicit status contract into the boolean
# refusal contract used below. Only status 10 means clear. Status 0 carries the
# helper's refusal reason; every other status blocks as an internal decision
# failure, so a future arithmetic/command regression cannot fail open.
keep_marker_refusal() {
	local candidate="$1" reason status=0
	reason="$(keep_marker_blocks "${candidate}")" || status=$?
	case "${status}" in
		0)
			printf '%s' "${reason}"
			return 0
			;;
		10)
			return 1
			;;
		*)
			printf 'the --keep marker decision failed internally with status %s; ownership and age are unknown' "${status}"
			return 0
			;;
	esac
}

# `ln -s` is create-or-fail only when the name does not exist. If a DIRECTORY
# sits at the lock path - e.g. left behind by an older mkdir-based lock - `ln`
# links INTO it and reports success, so every caller would "acquire" and the
# mutex would silently stop excluding anything. Verify the link actually landed
# at the lock path carrying our payload before trusting the claim.
# Returns: 0 claimed, 1 name already taken, 2 a non-symlink is in the way.
# Orphan-guard reap age, in seconds. Production keeps 60: a guard younger than
# that belongs to a reclaimer that may still be working, and reaping it would
# let two runs into the same reclaim. It is overridable ONLY so the lock's own
# tests can assert "a fresh guard is not reaped" without that assertion
# depending on how long the test itself takes to run. The check compares wall
# clock against the guard's birth epoch, so a try_acquire that is slow enough --
# 50 attempts with sleeps, on a loaded machine -- can push a guard stamped
# "now" past a 60s threshold DURING the test, reap it, and fail an assertion
# about freshness that the code under test never actually violated.
: "${ESHU_LIVE_GATE_REAP_AGE_SECONDS:=60}"

claim_lock_link() {
	local payload="$1" candidate="$2"
	ln -s "${payload}" "${candidate}" 2>/dev/null || return 1
	if [[ -L "${candidate}" && "$(readlink "${candidate}" 2>/dev/null)" == "${payload}" ]]; then
		return 0
	fi
	# The link was created INSIDE a directory at ${candidate}; take our stray
	# entry back out so the reclaim path sees the directory unchanged.
	rm -f "${candidate}/${payload##*/}" 2>/dev/null || true
	return 2
}

acquire_live_gate_lock() {
	if [[ "${ESHU_SKIP_LIVE_GATE_LOCK:-0}" == "1" ]]; then
		return 0
	fi
	local lock_home
	if [[ -n "${ESHU_LIVE_GATE_LOCK_DIR:-}" ]]; then
		lock_home="${ESHU_LIVE_GATE_LOCK_DIR}"
	else
		# Absolute: from the main checkout this prints a RELATIVE ".git", and a
		# relative lock_path would make the release a silent no-op if the cwd
		# ever moved between acquire and cleanup.
		lock_home="$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)" ||
			lock_home="$(git rev-parse --git-common-dir 2>/dev/null)" || {
				printf 'live-gate-lock: not a git repo; running WITHOUT the cross-run mutex\n' >&2
				return 0
			}
	fi
	if [[ ! -d "${lock_home}" ]]; then
		printf 'live-gate-lock: %s is not a directory; running WITHOUT the cross-run mutex\n' \
			"${lock_home}" >&2
		return 0
	fi
	local candidate="${lock_home}/eshu-live-gate.lock"
	# pid first, then the start-id fingerprint, then the worktree - split on the
	# first TWO colons: a path may contain a colon, a pid and a start-id
	# fingerprint never do. The start-id defends against PID reuse (see
	# start_id_for_pid); an empty fingerprint (this pid is somehow already gone
	# by the time we read it back) degrades gracefully at the read side, not here.
	local payload="$$:$(start_id_for_pid "$$"):$(pwd -P)"
	local max_attempts=50
	local attempt holder holder_pid holder_startid holder_rest holder_where claim_status
	local guard guard_payload guard_pid guard_born guard_status
	local current current_pid current_startid current_rest current_is_debris

	for (( attempt = 1; attempt <= max_attempts; attempt++ )); do
		# Must not be a bare call: under the caller's `set -e` a non-zero return
		# kills the shell before ${claim_status} is ever read, and the gate exits
		# silently instead of reporting who holds the lock.
		claim_status=0
		claim_lock_link "${payload}" "${candidate}" || claim_status=$?
		if [[ "${claim_status}" -eq 0 ]]; then
			lock_path="${candidate}"
			lock_payload="${payload}"
			# Decide marker-only state only after publishing our main lock. A
			# second contender now observes this live owner instead of querying
			# and deleting the same marker while we replace it on --keep.
			local keep_why
			if keep_why="$(keep_marker_refusal "${candidate}")"; then
				release_live_gate_lock
				die "a --keep run retained the compose stack on the fixed host ports.
  ${keep_why}.
  Releasing the lock would hand those ports to this run, which would then tear
  that stack down on exit. Stop the retained stack, then remove:
    ${candidate}.keep
    ${candidate}
  Set ESHU_SKIP_LIVE_GATE_LOCK=1 only where runners are isolated (CI)."
			fi
			return 0
		fi

		if [[ "${claim_status}" -eq 2 ]]; then
			# A directory or plain file is squatting the lock path. It carries no
			# holder identity, so clear it through the same guarded reclaim.
			holder_pid=""
			holder_startid=""
			holder_where="non-symlink debris at ${candidate}"
		else
			holder="$(readlink "${candidate}" 2>/dev/null || true)"
			if [[ -n "${holder}" ]]; then
				# Arity check (#5826 review, P3): a payload with NO colon at all (a
				# bare pid, no start-id, no worktree) is malformed, not merely a
				# shorter valid format. Without this case split, `${holder#*:}` and
				# `${holder%%:*}` both fall through UNCHANGED when there is no colon
				# to match on - so holder_rest, then holder_startid, silently become
				# the pid string itself. That is numeric, so it passes the
				# digit-only check below as if it were a genuine start-id, and gets
				# compared against the pid's ACTUAL start-id fingerprint - which it
				# will essentially never equal. A genuinely live pid then reads as
				# an unconfirmed mismatch.
				#
				# Reverting THIS check alone fails closed, not open: the guard-side
				# arity check below catches the same payload and acquire wedges
				# ("could not acquire live gate lock after N attempts"). The
				# reclaim of a live lock needs BOTH sites gone, which is why the
				# guard side carries its own require_lock pin -- deleting half the
				# fix is the reachable mistake, and it is the half with no
				# behavioural case.
				case "${holder}" in
					*:*)
						holder_pid="${holder%%:*}"
						holder_rest="${holder#*:}"
						holder_startid="${holder_rest%%:*}"
						# Not a digit string: either an old-format two-field payload
						# (the whole worktree path landed here) or corruption. Degrade
						# to pid-only rather than trust a non-numeric value as a
						# start-id fingerprint.
						[[ "${holder_startid}" =~ ^[0-9]+$ ]] || holder_startid=""
						holder_where="${holder_rest#*:}"
						;;
					*)
						holder_pid="${holder}"
						holder_startid=""
						holder_where="malformed payload (missing ':' field separator) at ${candidate}"
						;;
				esac
			elif [[ -e "${candidate}" ]]; then
				holder_pid=""
				holder_startid=""
				holder_where="non-symlink debris at ${candidate}"
			else
				# Released between our claim and our readlink; claim it again.
				sleep 0.1
				continue
			fi
		fi

		# A live pid alone is not enough: if the kernel reused it after the
		# original holder crashed, this would read a stale lock as live forever.
		# An empty holder_startid (payload predates the fingerprint, or the
		# original process could not be read at claim time) degrades to the
		# pid-only check rather than being treated as stale. An empty LIVE
		# re-read degrades the same way: start_id_for_pid documents empty as
		# "cannot confirm", and comparing "" against a real fingerprint would
		# turn an unreadable-but-alive holder into a reclaim -- fail-open, the
		# exact class this lock exists to prevent. Not currently reachable on
		# macOS or Linux, where ps -p fails first, but a missing ps binary
		# reaches it.
		holder_live_startid="$(start_id_for_pid "${holder_pid}")"
		if [[ "${holder_pid}" =~ ^[0-9]+$ ]] && process_is_alive "${holder_pid}" &&
			{ [[ -z "${holder_startid}" || -z "${holder_live_startid}" ]] ||
				[[ "${holder_live_startid}" == "${holder_startid}" ]]; }; then
			die "another live gate is already running (pid ${holder_pid}, ${holder_where}).
  This gate binds fixed host ports and must be serialized: a second concurrent run
  does not fail cleanly, it starves the first into a spurious drain failure.
  Wait for that run to finish, or stop it, then retry.
  Lock: ${candidate}
  Set ESHU_SKIP_LIVE_GATE_LOCK=1 only where runners are isolated (CI)."
		fi

		# Stale holder. Reclaiming is a read-then-replace, and the read can go
		# stale: a racer that observed the dead holder a moment ago would happily
		# replace the FRESH lock a winner has since published, and then both runs
		# proceed. An atomic rename does not fix that on its own - the rename is
		# atomic, but it is not conditional on the value still being the one we
		# read. So serialize the reclaim behind its own exclusive guard and
		# re-validate the holder underneath it.
		guard="${candidate}.reclaim"
		# Verified like the lock claim itself: a bare `ln -s` reports success
		# when a DIRECTORY occupies the path (it links inside), which would make
		# every reclaimer believe it holds the guard.
		# Payload carries a birth epoch (pid:epoch) so the orphan-reap age check
		# below never has to stat a filesystem mtime - see that check for why.
		guard_status=0
		claim_lock_link "$$:$(date +%s)" "${guard}" || guard_status=$?
		if [[ "${guard_status}" -eq 0 ]]; then
			current="$(readlink "${candidate}" 2>/dev/null || true)"
			# Same arity check as the holder parse above (#5826 review, P3): a bare
			# pid with no colon must not fall through and become its own start-id.
			case "${current}" in
				*:*)
					current_pid="${current%%:*}"
					current_rest="${current#*:}"
					current_startid="${current_rest%%:*}"
					[[ "${current_startid}" =~ ^[0-9]+$ ]] || current_startid=""
					;;
				*)
					current_pid="${current}"
					current_startid=""
					;;
			esac
			# Distinguish "a dead holder's link" from "nothing is here": they are
			# both reclaimable, but only one of them is safe to remove. These are
			# two separate stats, and that is race-free ONLY because a live lock is
			# a DANGLING symlink: the payload "pid:/abs/path" never resolves
			# relative to the lock dir, so -e is false for a live lock. If the
			# payload format ever became a resolvable path, a claimer publishing
			# between the readlink and the -e would be misread as debris.
			current_is_debris=0
			if [[ -z "${current}" && -e "${candidate}" ]]; then
				current_is_debris=1
			fi
			current_live_startid="$(start_id_for_pid "${current_pid}")"
			if ! { [[ "${current_pid}" =~ ^[0-9]+$ ]] && process_is_alive "${current_pid}" &&
				{ [[ -z "${current_startid}" || -z "${current_live_startid}" ]] ||
					[[ "${current_live_startid}" == "${current_startid}" ]]; }; }; then
				# Re-check under the guard: a --keep holder may have retained the
				# stack after our pre-loop check, and its pid is dead by design. The
				# marker is written BEFORE that holder exits, so at the instant the
				# pid reads dead the marker is guaranteed to be on disk.
				local keep_why_guard
				if keep_why_guard="$(keep_marker_refusal "${candidate}")"; then
					rm -f "${guard}"
					die "a --keep run retained the compose stack on the fixed host ports.
  ${keep_why_guard}.
  Reclaiming this lock would hand those ports to this run, which would then tear
  that stack down on exit. Stop the retained stack, then remove:
    ${candidate}.keep
    ${candidate}"
				fi
				# Replace ONLY the exact thing that was validated under the
				# guard. A dead holder's link cannot change underneath us - its
				# owner is gone and other reclaimers are excluded - so removing
				# it is safe. A FREE name is a different case entirely: the
				# guard excludes other RECLAIMERS, not an ordinary claimer in
				# the main retry loop, and an ordinary claimer competes
				# precisely when the name is free. Removing "nothing" therefore
				# deletes a live lock published microseconds earlier, and both
				# runs proceed. claim_lock_link is create-or-fail, so simply
				# losing that race is the correct outcome.
				# rm -rf, not rm -f: a squatter may be a directory.
				if [[ -n "${current}" || "${current_is_debris}" -eq 1 ]]; then
					# Re-read immediately before destroying, with no fork in
					# between. ${current} was captured BEFORE process_is_alive
					# forked `ps`, so "its owner is gone" is only true as of the
					# ps, not as of the read: a holder alive at read time can
					# release and exit during that fork, and an ordinary claimer
					# - which this guard does NOT exclude - can take the freed
					# name. Destroy only the exact value that was validated.
					if [[ "$(readlink "${candidate}" 2>/dev/null || true)" == "${current}" ]]; then
						rm -rf "${candidate}"
					else
						rm -f "${guard}"
						sleep 0.1
						continue
					fi
				fi
				if claim_lock_link "${payload}" "${candidate}"; then
					rm -f "${guard}"
					lock_path="${candidate}"
					lock_payload="${payload}"
					printf 'live-gate-lock: reclaimed stale lock from %s (%s)\n' \
						"${holder_pid:-no live holder}" "${holder_where}" >&2
					return 0
				fi
			fi
			rm -f "${guard}"
		else
			# Another process is reclaiming. Reap the guard only if its pid is
			# dead AND it has outlived the entire retry budget. An unconditional
			# reap would delete a guard a racer created microseconds ago - the
			# same unconditional-mutation flaw this lock avoids one level down.
			# Cost of the conservative choice: a reclaimer killed mid-guard makes
			# the next runs fail for up to a minute, then self-heals. It never
			# wedges permanently. Exclusivity comes from the re-validation
			# below, not from the age gate, which cannot see a recreated guard.
			if [[ "${guard_status}" -eq 2 ]]; then
				# A non-symlink at the guard path is never a legitimate holder -
				# nothing publishes a directory or a plain file here. Reap it at
				# once, deliberately WITHOUT the age gate: probing a directory with
				# claim_lock_link creates and removes an entry inside it, which
				# updates the directory's mtime and resets the age clock on every
				# attempt - so an aged-out reap can never fire and acquire wedges
				# forever. Reaping immediately is safe: two racers that both reap
				# then both call claim_lock_link, which is create-or-fail, so only
				# one comes out holding the guard. Re-validate first, for the
				# same reason the lock destroy does: this decision was taken
				# before the check below, and a racer may have published a real
				# guard in between. Two stats, not one atomic check: what makes
				# it safe is that -L short-circuits a legitimate guard, and a
				# legitimate guard is a DANGLING symlink (its payload is a bare
				# pid), so -e is false for one even if the path flipped between
				# the two tests.
				if [[ ! -L "${guard}" && -e "${guard}" ]]; then
					rm -rf "${guard}"
				fi
			else
				# The guard payload is pid:epoch (see the claim above): age comes from
				# that embedded birth epoch, never a filesystem mtime. Reading the
				# claimed birth time instead of stat/-mmin also drops the last `find`
				# call from this file - `find` is a banned discovery primitive repo-wide.
				guard_payload="$(readlink "${guard}" 2>/dev/null || true)"
				guard_pid="${guard_payload%%:*}"
				guard_born="${guard_payload#*:}"
				if [[ "${guard_pid}" =~ ^[0-9]+$ ]] && ! process_is_alive "${guard_pid}" &&
					[[ "${guard_born}" =~ ^[0-9]+$ ]] &&
					(( $(date +%s) - guard_born > ESHU_LIVE_GATE_REAP_AGE_SECONDS )); then
					# The age and liveness decisions were both taken before this
					# point, and neither can see a guard recreated since. Destroy
					# only the value that was actually judged - the guard must be
					# exclusive or the lock destroy at the top of this block loses
					# its only guarantee.
					if [[ "$(readlink "${guard}" 2>/dev/null || true)" == "${guard_payload}" ]]; then
						rm -f "${guard}"
					fi
				fi
			fi
		fi
		sleep 0.1
	done
	die "could not acquire live gate lock at ${candidate} after ${max_attempts} attempts.
  If this persists, inspect BOTH the lock and its reclaim guard - a non-symlink at
  either path blocks acquisition:
    ${candidate}
    ${candidate}.reclaim"
}

# A `--keep` run deliberately leaves the compose stack up on the fixed ports.
# Releasing the mutex there would hand those ports to the next run, which would
# then tear the retained stack down with `docker compose down -v` on its own
# exit - destroying the very thing --keep was for. The marker has to outlive the
# holder pid (the pid is gone; the ports are not). Later acquisition may clear
# it only while serialized and only after Compose positively reports that the
# recorded project has no running containers.
retain_live_gate_lock() {
	[[ -n "${lock_path}" ]] || return 0
	# Version, pid, start-id fingerprint, retention epoch, compose project, then
	# the worktree. The v2 prefix distinguishes this layout from the four-field
	# marker briefly emitted by the open #5987 branch. Splitting on the first
	# FIVE colons preserves any later colon in the worktree path.
	#
	# The pid and fingerprint are NOT a liveness test for this marker (#5987):
	# this function runs in the cleanup trap immediately before `exit`, so the
	# recorded pid is dead within milliseconds in the normal --keep case.
	# Reclaiming on a dead pid would therefore discard every legitimate marker
	# at exactly the moment it starts mattering. They are recorded so a blocked
	# run can NAME the owner instead of printing a bare path. What actually
	# decides reclaimability is the compose project: the marker protects a
	# running stack, so its validity is keyed on that stack still existing.
	local retained_at
	retained_at="$(date +%s 2>/dev/null)" || retained_at=""
	printf 'v2:%s:%s:%s:%s:%s\n' "$$" "$(start_id_for_pid "$$")" \
		"${retained_at}" "${GATE_COMPOSE_PROJECT:-}" "$(pwd -P)" >"${lock_path}.keep" 2>/dev/null ||
		die "could not write the --keep marker at ${lock_path}.keep - the retained
  stack is NOT protected, and the next run will tear it down. Stop the stack now."
	[[ "${retained_at}" =~ ^[0-9]{1,10}$ && "${retained_at}" != "0" ]] ||
		die "could not record a valid retention timestamp for ${lock_path}.keep. The
  marker remains fail-closed with unknown age; stop the retained stack manually."
	printf 'live-gate-lock: --keep retained the stack, so the lock is retained too.\n  Clear it with: rm -f %s %s\n' \
		"${lock_path}.keep" "${lock_path}" >&2
}

# Release the mutex, but only while this process is still the recorded holder,
# so a run whose lock was reclaimed as stale cannot delete the live holder's
# lock on its way out. This cannot race a concurrent reclaim: a reclaimer only
# acts on a pid that is already dead, and this process is alive while it runs.
release_live_gate_lock() {
	[[ -n "${lock_path}" ]] || return 0
	if [[ "$(readlink "${lock_path}" 2>/dev/null || true)" == "${lock_payload}" ]]; then
		rm -f "${lock_path}"
	fi
}
