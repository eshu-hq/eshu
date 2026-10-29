#!/usr/bin/env bash
#
# verify-scanner-images.sh - assert one destination scanner-<name> image
# resolves its own stdio component binary and carries no sibling-component
# entry under the three binary directories (#8878).
#
# Usage: verify-scanner-images.sh <image|sbom|osruntime> <rootfs-dir>
#
# <rootfs-dir> is an exported destination-image filesystem, for example the
# `docker buildx build --target scanner-<name> --output type=local,dest=<dir>`
# tree that docker-publish.yml's verify-reproducibility job produces.
#
# The destination images are minimal one-binary stages off the Dockerfile
# builder (each COPYs only its eshu-scanner-component-<name> binary): the
# extension host launches one process per call, and the per-analyzer
# ComponentPackage manifest pins exactly one artifact image. The verifier
# asserts, for the named component:
#   - its eshu-scanner-component-<name> binary resolves, inside the rootfs,
#     to an executable regular file under usr/bin, bin, or usr/local/bin;
#   - neither sibling component binary has an entry (file, link, or
#     directory) under those directories: any entry under a sibling name
#     is contamination, even a dangling link.
#
# Path resolution reuses scripts/lib/rootfs-resolve.sh, the same jail the
# runtime image verifier uses, so no link target can false-green against a
# host binary. The sibling check is lexical on purpose: the question is
# whether the image ships the name at all, not what it points at.
#
# Exit codes: 0 the own binary resolves and no sibling entry is present
# under usr/bin, bin, or usr/local/bin; 1 the own binary is missing or a
# sibling entry is present; 2 the input is unusable (wrong arity, unknown
# component, not a directory, or not an image rootfs).
# MAX_LINK_HOPS below is consumed by the sourced rootfs-resolve library,
# which shellcheck cannot see when this file is linted alone.
# shellcheck disable=SC2034
set -euo pipefail

SEARCH_DIRS=(usr/bin bin usr/local/bin)
MAX_LINK_HOPS=16

usage_error() {
	printf 'verify-scanner-images: %s\n' "$1" >&2
	exit 2
}

[[ $# -eq 2 ]] || usage_error "usage: verify-scanner-images.sh <image|sbom|osruntime> <rootfs-dir>"
component="$1"
rootfs="$2"
case "${component}" in
image | sbom | osruntime) ;;
*) usage_error "unknown scanner component ${component}: want image, sbom, or osruntime" ;;
esac
[[ -d "${rootfs}" ]] || usage_error "rootfs ${rootfs} is not a directory"
rootfs="$(cd "${rootfs}" && pwd -P)"
# An empty or unrelated directory must not pass as "nothing required".
[[ -d "${rootfs}/usr/bin" || -d "${rootfs}/bin" ]] ||
	usage_error "rootfs ${rootfs} has neither usr/bin nor bin; not an image root filesystem"

# shellcheck source=lib/rootfs-resolve.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)/lib/rootfs-resolve.sh"

own="eshu-scanner-component-${component}"
siblings=()
for name in image sbom osruntime; do
	if [[ "${name}" != "${component}" ]]; then
		siblings+=("eshu-scanner-component-${name}")
	fi
done

bad=()

found=""
for dir in "${SEARCH_DIRS[@]}"; do
	if resolved="$(resolve_in_rootfs "${dir}/${own}")" &&
		[[ -f "${rootfs}/${resolved}" && -x "${rootfs}/${resolved}" ]]; then
		found="${dir}/${own}"
		break
	fi
done
if [[ -n "${found}" ]]; then
	printf 'ok - %s at /%s\n' "${own}" "${found}"
else
	printf 'MISSING - %s: no executable in /%s\n' "${own}" "${SEARCH_DIRS[*]}" >&2
	bad+=("${own}")
fi

for sibling in "${siblings[@]}"; do
	for dir in "${SEARCH_DIRS[@]}"; do
		if [[ -e "${rootfs}/${dir}/${sibling}" || -L "${rootfs}/${dir}/${sibling}" ]]; then
			printf 'UNEXPECTED - %s: sibling binary present at /%s/%s\n' "${sibling}" "${dir}" "${sibling}" >&2
			bad+=("${sibling}")
			break
		fi
	done
done

if ((${#bad[@]} > 0)); then
	printf 'verify-scanner-images: scanner-%s image violates the one-binary contract: %s\n' "${component}" "${bad[*]}" >&2
	exit 1
fi
printf 'verify-scanner-images: scanner-%s image carries exactly %s\n' "${component}" "${own}"
