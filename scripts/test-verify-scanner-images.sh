#!/usr/bin/env bash
#
# test-verify-scanner-images.sh - hermetic tests for
# scripts/verify-scanner-images.sh (#8878). No Docker and no network:
# each case builds a synthetic destination-image root filesystem under mktemp.
#
# Contract under test: the destination scanner-<name> image for one analyzer
# carries exactly its own stdio component binary. For component C in
# {image, sbom, osruntime} with binary eshu-scanner-component-C:
#   GREEN the binary resolves, inside the rootfs, to an executable regular
#         file, and neither sibling component binary has an entry under the
#         searched bin directories.
#   RED   the own binary is missing (exit 1 naming it), or a sibling binary
#         is present under any name the image would exec (exit 1 naming the
#         sibling).
# False-green guards (mirroring test-verify-runtime-image-tools.sh): an
# absolute symlink whose target exists only on the host, a relative symlink
# or a symlinked parent directory whose ".." climbs above the rootfs top, a
# non-executable file, a directory named like the binary, and a symlink loop
# must all count as missing. Unusable input (wrong arity, unknown component,
# not a directory, a directory with no bin tree) must exit 2, never 0.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-scanner-images.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

# bin_for COMPONENT: print the component binary name.
bin_for() {
	case "$1" in
		image) printf 'eshu-scanner-component-image\n' ;;
		sbom) printf 'eshu-scanner-component-sbom\n' ;;
		osruntime) printf 'eshu-scanner-component-osruntime\n' ;;
		*) return 1 ;;
	esac
}

# make_rootfs NAME BIN...: create <tmp>/NAME/usr/bin with an executable
# stub for each BIN and print the rootfs path.
make_rootfs() {
	local root="${tmp_root}/$1" bin
	shift
	mkdir -p "${root}/usr/bin" "${root}/bin"
	for bin in "$@"; do
		printf '#!/bin/sh\nexit 0\n' >"${root}/usr/bin/${bin}"
		chmod 0755 "${root}/usr/bin/${bin}"
	done
	printf '%s\n' "${root}"
}

# expect NAME WANT_EXIT [STDERR_SUBSTRING] -- ARGS...: run the verifier and
# check its exit code and, when given, that stderr names the substring.
expect() {
	local name="$1" want="$2" needle="" got=0 err
	shift 2
	if [[ "$1" != "--" ]]; then
		needle="$1"
		shift
	fi
	shift
	err="${tmp_root}/stderr.$$"
	bash "${verifier}" "$@" >/dev/null 2>"${err}" || got=$?
	if [[ "${got}" -ne "${want}" ]]; then
		record_fail "${name}: exit ${got}, want ${want} ($(tr '\n' ' ' <"${err}"))"
		return
	fi
	if [[ -n "${needle}" ]] && ! grep -q -- "${needle}" "${err}"; then
		record_fail "${name}: stderr does not mention '${needle}'"
		return
	fi
	record_pass "${name}"
}

if [[ ! -f "${verifier}" ]]; then
	record_fail "verifier exists at scripts/verify-scanner-images.sh"
	printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
	exit 1
fi

# RED per binary: a destination rootfs without its own component binary
# fails naming the binary.
for component in image sbom osruntime; do
	expect "RED scanner-${component} rootfs without own binary fails" 1 "$(bin_for "${component}")" -- "${component}" "$(make_rootfs "noown-${component}")"
done

# GREEN per binary: adding only the own binary passes.
for component in image sbom osruntime; do
	expect "GREEN scanner-${component} rootfs with own binary passes" 0 -- "${component}" "$(make_rootfs "own-${component}" "$(bin_for "${component}")")"
done

# RED sibling contamination: a destination image carrying another analyzer's
# binary fails naming the sibling. Each direction is enforced, not only one.
expect "RED scanner-image rootfs with sbom sibling fails" 1 "eshu-scanner-component-sbom" -- image "$(make_rootfs sib-image-sbom eshu-scanner-component-image eshu-scanner-component-sbom)"
expect "RED scanner-sbom rootfs with osruntime sibling fails" 1 "eshu-scanner-component-osruntime" -- sbom "$(make_rootfs sib-sbom-osruntime eshu-scanner-component-sbom eshu-scanner-component-osruntime)"
expect "RED scanner-osruntime rootfs with image sibling fails" 1 "eshu-scanner-component-image" -- osruntime "$(make_rootfs sib-osruntime-image eshu-scanner-component-osruntime eshu-scanner-component-image)"
expect "RED scanner-image rootfs with both siblings fails" 1 "eshu-scanner-component-" -- image "$(make_rootfs sib-image-both eshu-scanner-component-image eshu-scanner-component-sbom eshu-scanner-component-osruntime)"

# A sibling outside /usr/bin is still contamination.
sibalt="$(make_rootfs sibalt eshu-scanner-component-osruntime)"
mkdir -p "${sibalt}/usr/local/bin"
printf '#!/bin/sh\n' >"${sibalt}/usr/local/bin/eshu-scanner-component-sbom" && chmod 0755 "${sibalt}/usr/local/bin/eshu-scanner-component-sbom"
expect "RED sibling in /usr/local/bin fails" 1 "eshu-scanner-component-sbom" -- osruntime "${sibalt}"

# The own binary under /bin and /usr/local/bin counts.
alt="$(make_rootfs alt)"
mkdir -p "${alt}/usr/local/bin"
printf '#!/bin/sh\n' >"${alt}/bin/eshu-scanner-component-sbom" && chmod 0755 "${alt}/bin/eshu-scanner-component-sbom"
expect "sbom binary in /bin passes" 0 -- sbom "${alt}"
rm "${alt}/bin/eshu-scanner-component-sbom"
printf '#!/bin/sh\n' >"${alt}/usr/local/bin/eshu-scanner-component-osruntime" && chmod 0755 "${alt}/usr/local/bin/eshu-scanner-component-osruntime"
expect "osruntime binary in /usr/local/bin passes" 0 -- osruntime "${alt}"

# Symlinks resolve inside the rootfs: absolute and relative targets.
linked="$(make_rootfs linked eshu-scanner-component-image)"
mkdir -p "${linked}/usr/libexec"
mv "${linked}/usr/bin/eshu-scanner-component-image" "${linked}/usr/libexec/component-real"
ln -s /usr/libexec/component-real "${linked}/usr/bin/eshu-scanner-component-image"
expect "absolute symlink resolved inside rootfs passes" 0 -- image "${linked}"
rm "${linked}/usr/bin/eshu-scanner-component-image"
ln -s ../libexec/component-real "${linked}/usr/bin/eshu-scanner-component-image"
expect "relative symlink resolved inside rootfs passes" 0 -- image "${linked}"

# False green: an absolute link whose target exists only on the host. /bin/sh
# exists on every test host, so a host-relative resolver would pass this.
hostlink="$(make_rootfs hostlink)"
ln -s /bin/sh "${hostlink}/usr/bin/eshu-scanner-component-image"
expect "symlink to a host-only target fails" 1 "eshu-scanner-component-image" -- image "${hostlink}"

# Enough ".." to climb from any test rootfs to the host's /.
up="$(printf '../%.0s' {1..32})"

# False green: a relative link whose ".." segments climb above the rootfs top
# and land on the host /bin/sh.
dotdot="$(make_rootfs dotdot)"
ln -s "${up}bin/sh" "${dotdot}/usr/bin/eshu-scanner-component-sbom"
expect "relative symlink climbing out of the rootfs fails" 1 "eshu-scanner-component-sbom" -- sbom "${dotdot}"

# False green: present but not executable.
noexec="$(make_rootfs noexec)"
printf '#!/bin/sh\n' >"${noexec}/usr/bin/eshu-scanner-component-osruntime" && chmod 0644 "${noexec}/usr/bin/eshu-scanner-component-osruntime"
expect "non-executable osruntime binary fails" 1 "eshu-scanner-component-osruntime" -- osruntime "${noexec}"

# False green: a directory is executable (searchable) but not a binary.
dirtool="$(make_rootfs dirtool)"
mkdir -p "${dirtool}/usr/bin/eshu-scanner-component-image"
expect "directory named like the binary fails" 1 "eshu-scanner-component-image" -- image "${dirtool}"

# False green: a dangling symlink under a sibling name is still contamination.
dangling="$(make_rootfs dangling eshu-scanner-component-sbom)"
ln -s /nonexistent-sibling-target "${dangling}/usr/bin/eshu-scanner-component-image"
expect "dangling symlink under a sibling name fails" 1 "eshu-scanner-component-image" -- sbom "${dangling}"

# A symlink loop must terminate and fail, not hang.
loop="$(make_rootfs loop)"
ln -s component-b "${loop}/usr/bin/eshu-scanner-component-image"
ln -s eshu-scanner-component-image "${loop}/usr/bin/component-b"
expect "symlink loop fails" 1 "eshu-scanner-component-image" -- image "${loop}"

# Unusable input fails closed with exit 2.
expect "no argument exits 2" 2 -- image
expect "unknown component exits 2" 2 -- bogus "$(make_rootfs badcomp eshu-scanner-component-image)"
expect "nonexistent path exits 2" 2 -- image "${tmp_root}/does-not-exist"
printf 'x' >"${tmp_root}/plain-file"
expect "regular file exits 2" 2 -- sbom "${tmp_root}/plain-file"
mkdir -p "${tmp_root}/empty"
expect "directory with no bin tree exits 2" 2 -- osruntime "${tmp_root}/empty"

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" -eq 0 ]]
