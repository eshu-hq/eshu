#!/usr/bin/env bash
# The `--keep` marker's meaning for a second run, split out of
# live-gate-lock.sh to keep both chunks under the 500-line cap.
#
# Sourced, never executed: live-gate-lock.sh sources it and calls
# keep_marker_blocks() from both of its refusal sites.

# Decide what a `--keep` marker at ${1}.keep means for THIS run (#5987).
#
# Before this, any marker blocked unconditionally and recorded only a worktree
# path, so a marker whose session crashed or exited without cleanup was
# indistinguishable from one guarding a live retained stack. A second worktree
# could only wait forever or delete someone else's marker and destroy their
# state. Four consecutive pre-pr runs lost their live lane over ~40 minutes to
# exactly that.
#
# The pid cannot be the discriminator: retain_live_gate_lock runs in the
# cleanup trap immediately before `exit`, so a healthy --keep holder's pid is
# ALWAYS dead. The marker exists to protect a compose stack still bound to the
# fixed host ports, so the stack is what decides:
#
#   running, or cannot be determined -> block (fail closed)
#   positively not running           -> debris, reclaim it
#
# Fail-closed is deliberate. A wrong "debris" verdict hands the ports to a run
# that ends in `docker compose down -v`, destroying the retained stack the
# marker was protecting; a wrong "blocked" verdict costs a wait and prints who
# to ask. Those are not symmetric.
#
# Echoes a human description of the holder on stdout. Returns 0 to block,
# 1 when the marker is debris.
keep_marker_blocks() {
	local marker="$1.keep" raw pid startid project where running
	[[ -e "${marker}" ]] || return 1
	raw="$(cat "${marker}" 2>/dev/null)" || raw=""
	pid="${raw%%:*}"
	startid="${raw#*:}"
	startid="${startid%%:*}"
	project="${raw#*:*:}"
	project="${project%%:*}"
	where="${raw#*:*:*:}"
	# A legacy path-only marker (no colons) names no project, so this run
	# cannot confirm anything about its stack. Block, and say why.
	if [[ "${raw}" != *:*:*:* ]]; then
		printf 'a --keep marker in the pre-#5987 path-only format (%s); this run cannot determine whether its stack is still up' \
			"${raw:-empty}"
		return 0
	fi
	if [[ -z "${project}" ]]; then
		printf 'holder pid %s at %s recorded no compose project, so its stack cannot be checked' \
			"${pid:-unknown}" "${where:-unknown}"
		return 0
	fi
	if ! command -v docker >/dev/null 2>&1; then
		printf 'holder pid %s at %s retained compose project %s; docker is unavailable here, so this run cannot confirm the stack is down' \
			"${pid:-unknown}" "${where:-unknown}" "${project}"
		return 0
	fi
	running="$(docker compose -p "${project}" ps -q 2>/dev/null)" || {
		printf 'holder pid %s at %s retained compose project %s; querying docker failed, so this run cannot confirm the stack is down' \
			"${pid:-unknown}" "${where:-unknown}" "${project}"
		return 0
	}
	if [[ -n "${running}" ]]; then
		printf 'holder pid %s at %s still has compose project %s up on the fixed host ports' \
			"${pid:-unknown}" "${where:-unknown}" "${project}"
		return 0
	fi
	printf 'live-gate-lock: reclaiming a --keep marker whose stack is gone (pid %s, project %s, worktree %s).\n' \
		"${pid:-unknown}" "${project}" "${where:-unknown}" >&2
	rm -f "${marker}" 2>/dev/null || true
	return 1
}
