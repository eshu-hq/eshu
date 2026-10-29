#!/usr/bin/env bash
#
# verify-runtime-image-tools.sh - assert the built runtime image root
# filesystem ships every external tool the Go runtimes exec (#7762).
#
# Usage: verify-runtime-image-tools.sh <rootfs-dir>
#
# <rootfs-dir> is an exported image filesystem, for example the
# `docker buildx build --output type=local,dest=<dir>` tree that
# docker-publish.yml's verify-reproducibility job already produces.
#
# Required tools and the runtime path that execs each one:
#   git   repository collector clone, fetch, and committed-tree reads
#   ssh   git transport for ESHU_GIT_AUTH_METHOD=ssh (GIT_SSH_COMMAND)
#   curl  the image HEALTHCHECK
#   eshu-scanner-component-image/sbom/osruntime (#8878) per-analyzer scanner
#         stdio components exec'd by the extension host over the
#         collector-sdk/v1alpha3 paged contract
#
# A tool passes when <rootfs>/{usr/bin,bin,usr/local/bin}/<tool> resolves,
# inside the rootfs, to an executable regular file. The path is resolved one
# component at a time with the rootfs as /, and every symlink on it (a parent
# directory as well as the tool) is followed inside the rootfs, never the
# host, so no link target can false-green against a host binary. A ".." that
# would climb above the rootfs top fails closed: the kernel would clamp it at
# /, but no real image needs that, and refusing it is the safe reading.
#
# Exit codes: 0 every tool present; 1 one or more tools missing; 2 the input
# is unusable (missing argument, not a directory, or not an image rootfs).
set -euo pipefail

REQUIRED_TOOLS=(git ssh curl eshu-scanner-component-image eshu-scanner-component-sbom eshu-scanner-component-osruntime)
SEARCH_DIRS=(usr/bin bin usr/local/bin)
MAX_LINK_HOPS=16

usage_error() {
	printf 'verify-runtime-image-tools: %s\n' "$1" >&2
	exit 2
}

[[ $# -eq 1 ]] || usage_error "usage: verify-runtime-image-tools.sh <rootfs-dir>"
rootfs="$1"
[[ -d "${rootfs}" ]] || usage_error "rootfs ${rootfs} is not a directory"
rootfs="$(cd "${rootfs}" && pwd -P)"
# An empty or unrelated directory must not pass as "nothing required".
[[ -d "${rootfs}/usr/bin" || -d "${rootfs}/bin" ]] ||
	usage_error "rootfs ${rootfs} has neither usr/bin nor bin; not an image root filesystem"

# resolve_in_rootfs REL: print the rootfs-relative path REL finally points at,
# resolving it one component at a time as if the rootfs were /. Every symlink
# on the way, a parent directory as well as the tool itself, is read and
# followed inside the rootfs, so the kernel never resolves a link against the
# host. As in the kernel, a component that has a "/" after it must be a
# directory, and an empty link target does not resolve. A ".." above the
# rootfs top fails closed. Fails past MAX_LINK_HOPS so a link loop terminates.
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

missing=()
for tool in "${REQUIRED_TOOLS[@]}"; do
	found=""
	for dir in "${SEARCH_DIRS[@]}"; do
		if resolved="$(resolve_in_rootfs "${dir}/${tool}")" &&
			[[ -f "${rootfs}/${resolved}" && -x "${rootfs}/${resolved}" ]]; then
			found="${dir}/${tool}"
			break
		fi
	done
	if [[ -n "${found}" ]]; then
		printf 'ok - %s at /%s\n' "${tool}" "${found}"
	else
		printf 'MISSING - %s: no executable in /%s\n' "${tool}" "${SEARCH_DIRS[*]}" >&2
		missing+=("${tool}")
	fi
done

if ((${#missing[@]} > 0)); then
	printf 'verify-runtime-image-tools: runtime image is missing: %s\n' "${missing[*]}" >&2
	exit 1
fi
printf 'verify-runtime-image-tools: all %d required tools present\n' "${#REQUIRED_TOOLS[@]}"
