#!/usr/bin/env bash
#
# rootfs-resolve.sh - resolve a path inside an exported image filesystem
# without ever resolving against the host (#7762, #8878).
#
# Usage (bash 3.2 compatible):
#   source "<repo>/scripts/lib/rootfs-resolve.sh"
#   rootfs="<absolute rootfs path>"
#   MAX_LINK_HOPS=16
#   resolved="$(resolve_in_rootfs "usr/bin/<tool>")"
#
# Sourced by scripts/verify-runtime-image-tools.sh (every tool the runtime
# image ships) and scripts/verify-scanner-images.sh (the one component
# binary each destination scanner-<name> image ships). One copy keeps the
# jail behavior identical for both verifiers; both self-tests prove it.
# shellcheck disable=SC2034,SC2154  # globals are shared with the sourcing verifier

# resolve_in_rootfs REL: print the rootfs-relative path REL finally points at,
# resolving it one component at a time as if the rootfs were /. Every symlink
# on the way, a parent directory as well as the tool itself, is read and
# followed inside the rootfs, so the kernel never resolves a link against the
# host. As in the kernel, a component that has a "/" after it must be a
# directory, and an empty link target does not resolve. A ".." above the
# rootfs top fails closed. Fails past MAX_LINK_HOPS so a link loop terminates.
#
# Reads the caller's $rootfs (absolute path of the exported filesystem) and
# $MAX_LINK_HOPS globals.
resolve_in_rootfs() {
	local todo="$1" walked="" part target hops=0 more
	while [[ -n "${todo}" ]]; do
		part="${todo%%/*}"
		# more is 1 when a "/" follows part, even a trailing one.
		if [[ "${todo}" == */* ]]; then
			todo="${todo#*/}"
			more=1
		else
			todo=""
			more=0
		fi
		case "${part}" in
		"" | .)
			continue
			;;
		..)
			[[ -n "${walked}" ]] || return 1
			if [[ "${walked}" == */* ]]; then
				walked="${walked%/*}"
			else
				walked=""
			fi
			continue
			;;
		esac
		# walked holds no symlink, so this test reads only the link at part.
		if [[ -L "${rootfs}/${walked:+${walked}/}${part}" ]]; then
			hops=$((hops + 1))
			((hops <= MAX_LINK_HOPS)) || return 1
			target="$(readlink "${rootfs}/${walked:+${walked}/}${part}")"
			[[ -n "${target}" ]] || return 1
			# An absolute target restarts at the rootfs top. A relative one
			# continues from the directory that holds the link. Keep the "/"
			# that followed the link, so the target must be a directory too.
			if [[ "${target}" == /* ]]; then
				walked=""
			fi
			if ((more)); then
				todo="${target}/${todo}"
			else
				todo="${target}"
			fi
		else
			walked="${walked:+${walked}/}${part}"
			# walked holds no symlink, so -d follows none.
			if ((more)) && [[ ! -d "${rootfs}/${walked}" ]]; then
				return 1
			fi
		fi
	done
	printf '%s\n' "${walked}"
}
