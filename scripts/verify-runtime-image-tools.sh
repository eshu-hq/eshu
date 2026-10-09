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
#
# A tool passes when <rootfs>/{usr/bin,bin,usr/local/bin}/<tool> resolves,
# inside the rootfs, to an executable regular file. Symlinks are followed
# relative to the rootfs, never the host, so an absolute link target cannot
# false-green against a host binary. Resolution is textual on the tool's own
# link chain: a parent directory that is itself an absolute symlink is not
# rewritten into the rootfs. Alpine's /usr/bin, /bin, and /usr/local/bin are
# real directories, so the runtime image never hits that case.
#
# Exit codes: 0 every tool present; 1 one or more tools missing; 2 the input
# is unusable (missing argument, not a directory, or not an image rootfs).
set -euo pipefail

REQUIRED_TOOLS=(git ssh curl)
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

# resolve_in_rootfs REL: print the rootfs-relative path REL finally points at
# after following symlinks inside the rootfs. Fails past MAX_LINK_HOPS so a
# link loop terminates.
resolve_in_rootfs() {
	local rel="$1" hops=0 target
	while [[ -L "${rootfs}/${rel}" ]]; do
		hops=$((hops + 1))
		((hops <= MAX_LINK_HOPS)) || return 1
		target="$(readlink "${rootfs}/${rel}")"
		if [[ "${target}" == /* ]]; then
			rel="${target#/}"
		else
			rel="$(dirname "${rel}")/${target}"
		fi
	done
	printf '%s\n' "${rel}"
}

missing=()
for tool in "${REQUIRED_TOOLS[@]}"; do
	found=""
	for dir in "${SEARCH_DIRS[@]}"; do
		[[ -e "${rootfs}/${dir}/${tool}" || -L "${rootfs}/${dir}/${tool}" ]] || continue
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
