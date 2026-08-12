#!/usr/bin/env bash
# Retained-stack marker decisions for the live gate (#5987).
#
# Sourced by live-gate-lock.sh. The golden-corpus-*.sh name makes the existing
# workflow, registry, pre-pr, and private-data globs cover this helper.

# Decide what ${1}.keep means while the caller owns either the main live-gate
# lock or its exclusive stale-reclaim guard.
#
# Returns 0 and describes why the marker blocks on stdout. Returns 1 when the
# marker is absent or was positively proven stale and removed. The holder pid
# is diagnostic only: a retained marker normally outlives that process. The
# recorded Compose project is the durable resource authority.
keep_marker_blocks() {
	local marker="$1.keep" raw rest pid retained_at project where now elapsed age running current
	[[ -e "${marker}" || -L "${marker}" ]] || return 1
	if ! raw="$(cat "${marker}" 2>/dev/null)"; then
		printf 'could not read the --keep marker at %s, so its retained stack and age are unknown' "${marker}"
		return 0
	fi
	if [[ -z "${raw}" ]]; then
		printf 'an empty --keep marker at %s records no retained stack identity; retention age is unknown' "${marker}"
		return 0
	fi
	case "${raw}" in
		*:*) ;;
		*)
			printf 'a legacy path-only --keep marker (%s) records no compose project; retention age is unknown' "${raw}"
			return 0
			;;
	esac

	# The open #5987 branch briefly emitted pid:start-id:project:worktree.
	# Version the timestamped format so a colon in either old or new worktree
	# paths cannot make the two layouts ambiguous.
	if [[ "${raw}" != v2:* ]]; then
		if [[ "${raw}" == *:*:*:* ]]; then
			pid="${raw%%:*}"
			rest="${raw#*:}"
			rest="${rest#*:}"
			project="${rest%%:*}"
			where="${rest#*:}"
			printf 'holder pid %s at %s has a pre-age four-field --keep marker for compose project %s; retention age is unknown' \
				"${pid:-unknown}" "${where:-unknown}" "${project:-unknown}"
			return 0
		fi
		printf 'a malformed --keep marker (%s) has no recognized version or complete legacy fields; retention age is unknown' "${raw}"
		return 0
	fi
	if [[ "${raw}" != v2:*:*:*:*:* ]]; then
		printf 'a malformed v2 --keep marker (%s) does not contain pid, start-id, retained-at, compose project, and worktree fields; age is unknown' "${raw}"
		return 0
	fi

	rest="${raw#v2:}"
	pid="${rest%%:*}"
	rest="${rest#*:}"       # start-id:retained-at:project:worktree
	rest="${rest#*:}"       # retained-at:project:worktree
	retained_at="${rest%%:*}"
	rest="${rest#*:}"       # project:worktree
	project="${rest%%:*}"
	where="${rest#*:}"      # preserve every colon in the worktree path

	if [[ -z "${retained_at}" ]]; then
		printf 'holder pid %s at %s has a missing retention timestamp; age is unknown and the marker remains protected' \
			"${pid:-unknown}" "${where:-unknown}"
		return 0
	fi
	if [[ ! "${retained_at}" =~ ^[0-9]{1,10}$ || "${retained_at}" == "0" ]]; then
		printf 'holder pid %s at %s has an invalid retention timestamp (%s); age is unknown and the marker remains protected' \
			"${pid:-unknown}" "${where:-unknown}" "${retained_at}"
		return 0
	fi
	if ! now="$(date +%s 2>/dev/null)" || [[ ! "${now}" =~ ^[0-9]{1,10}$ || "${now}" == "0" ]]; then
		printf 'holder pid %s at %s retained compose project %s, but the current clock is unavailable; age is unknown and the marker remains protected' \
			"${pid:-unknown}" "${where:-unknown}" "${project:-unknown}"
		return 0
	fi
	if (( retained_at > now )); then
		printf 'holder pid %s at %s has a future retention timestamp (%s > %s); age is unknown and the marker remains protected' \
			"${pid:-unknown}" "${where:-unknown}" "${retained_at}" "${now}"
		return 0
	fi
	elapsed=$(( now - retained_at ))
	age="retained for ${elapsed} seconds"

	if [[ -z "${project}" ]]; then
		printf 'holder pid %s at %s (%s) recorded an empty compose project, so its stack cannot be checked' \
			"${pid:-unknown}" "${where:-unknown}" "${age}"
		return 0
	fi
	if ! command -v docker >/dev/null 2>&1; then
		printf 'holder pid %s at %s (%s) retained compose project %s; docker is unavailable, so this run cannot confirm the stack is down' \
			"${pid:-unknown}" "${where:-unknown}" "${age}" "${project}"
		return 0
	fi
	if ! running="$(docker compose -p "${project}" ps -q 2>/dev/null)"; then
		printf 'holder pid %s at %s (%s) retained compose project %s; querying docker failed, so this run cannot confirm the stack is down' \
			"${pid:-unknown}" "${where:-unknown}" "${age}" "${project}"
		return 0
	fi
	if [[ -n "${running}" ]]; then
		printf 'holder pid %s at %s (%s) still has compose project %s running on the fixed host ports' \
			"${pid:-unknown}" "${where:-unknown}" "${age}" "${project}"
		return 0
	fi

	# No compliant holder or reclaimer can replace the marker while the caller
	# owns the applicable lock. Re-read so deletion targets the exact value whose
	# project was queried, and fail closed if deletion does not complete.
	if ! current="$(cat "${marker}" 2>/dev/null)" || [[ "${current}" != "${raw}" ]]; then
		printf 'holder pid %s at %s (%s) changed the --keep marker while compose project %s was checked; refusing to remove the replacement' \
			"${pid:-unknown}" "${where:-unknown}" "${age}" "${project}"
		return 0
	fi
	if ! rm -f "${marker}" 2>/dev/null || [[ -e "${marker}" || -L "${marker}" ]]; then
		printf 'holder pid %s at %s (%s): could not remove the --keep marker at %s after compose project %s reported no running containers' \
			"${pid:-unknown}" "${where:-unknown}" "${age}" "${marker}" "${project}"
		return 0
	fi
	printf 'live-gate-lock: reclaimed --keep marker after compose project %s reported no running containers (pid %s, worktree %s, %s).\n' \
		"${project}" "${pid:-unknown}" "${where:-unknown}" "${age}" >&2
	return 1
}
