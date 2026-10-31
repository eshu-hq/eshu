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
# The per-analyzer scanner stdio components
# (eshu-scanner-component-image/sbom/osruntime) were removed here by #8878:
# execution lives in the eshu-scanner-worker satellite, so the in-tree
# runtime image no longer ships those binaries and the extension host no
# longer execs them.
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
# MAX_LINK_HOPS below is consumed by the sourced rootfs-resolve library,
# which shellcheck cannot see when this file is linted alone.
# shellcheck disable=SC2034
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

# Path resolution lives in scripts/lib/rootfs-resolve.sh so the destination
# scanner-image verifier shares the identical rootfs jail. resolve_in_rootfs
# reads the $rootfs and $MAX_LINK_HOPS globals set above.
# shellcheck source=lib/rootfs-resolve.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)/lib/rootfs-resolve.sh"

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
