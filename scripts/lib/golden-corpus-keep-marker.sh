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
	local marker="$1.keep" raw pid project where running current
	[[ -e "${marker}" || -L "${marker}" ]] || return 1
	if ! raw="$(cat "${marker}" 2>/dev/null)"; then
		printf 'could not read the --keep marker at %s, so its retained stack cannot be checked' "${marker}"
		return 0
	fi
	if [[ -z "${raw}" ]]; then
		printf 'an empty --keep marker at %s records no retained stack identity' "${marker}"
		return 0
	fi
	case "${raw}" in
		*:*) ;;
		*)
			printf 'a legacy path-only --keep marker (%s) records no compose project' "${raw}"
			return 0
			;;
	esac
	if [[ "${raw}" != *:*:*:* ]]; then
		printf 'a malformed --keep marker (%s) does not contain pid, start-id, compose project, and worktree fields' "${raw}"
		return 0
	fi

	pid="${raw%%:*}"
	project="${raw#*:*:}"
	project="${project%%:*}"
	where="${raw#*:*:*:}"
	if [[ -z "${project}" ]]; then
		printf 'holder pid %s at %s recorded an empty compose project, so its stack cannot be checked' \
			"${pid:-unknown}" "${where:-unknown}"
		return 0
	fi
	if ! command -v docker >/dev/null 2>&1; then
		printf 'holder pid %s at %s retained compose project %s; docker is unavailable, so this run cannot confirm the stack is down' \
			"${pid:-unknown}" "${where:-unknown}" "${project}"
		return 0
	fi
	if ! running="$(docker compose -p "${project}" ps -q 2>/dev/null)"; then
		printf 'holder pid %s at %s retained compose project %s; querying docker failed, so this run cannot confirm the stack is down' \
			"${pid:-unknown}" "${where:-unknown}" "${project}"
		return 0
	fi
	if [[ -n "${running}" ]]; then
		printf 'holder pid %s at %s still has compose project %s running on the fixed host ports' \
			"${pid:-unknown}" "${where:-unknown}" "${project}"
		return 0
	fi

	# No compliant holder or reclaimer can replace the marker while the caller
	# owns the applicable lock. Re-read so deletion targets the exact value whose
	# project was queried, and fail closed if deletion does not complete.
	if ! current="$(cat "${marker}" 2>/dev/null)" || [[ "${current}" != "${raw}" ]]; then
		printf 'the --keep marker at %s changed while compose project %s was checked; refusing to remove a replacement marker' \
			"${marker}" "${project}"
		return 0
	fi
	if ! rm -f "${marker}" 2>/dev/null || [[ -e "${marker}" || -L "${marker}" ]]; then
		printf 'could not remove the --keep marker at %s after compose project %s reported no running containers' \
			"${marker}" "${project}"
		return 0
	fi
	printf 'live-gate-lock: reclaimed --keep marker after compose project %s reported no running containers (pid %s, worktree %s).\n' \
		"${project}" "${pid:-unknown}" "${where:-unknown}" >&2
	return 1
}
