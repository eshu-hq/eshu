#!/usr/bin/env bash
#
# test-verify-runtime-image-tools.sh - hermetic tests for
# scripts/verify-runtime-image-tools.sh (#7762). No Docker and no network:
# each case builds a synthetic image root filesystem under mktemp.
#
# The seeded RED/GREEN pair:
#   RED   a rootfs shaped like the pre-#7762 runtime stage (git and curl, no
#         ssh): the verifier must exit 1 and name ssh.
#   GREEN the same rootfs plus usr/bin/ssh: the verifier must exit 0.
# The #8878 pair (removal follow-up): scanner stdio components are no longer
# required, so a rootfs with only the shell tools must exit 0.
#   GREEN a rootfs with git, curl, ssh and no scanner binaries: exit 0.
# False-green guards: an absolute symlink whose target exists only on the
# host, a relative symlink or a symlinked parent directory whose ".." climbs
# above the rootfs top, a link path the kernel rejects (through a missing
# directory or a regular file, a trailing "/" after a file, an empty target),
# a non-executable file, a directory named like the tool, and a symlink loop
# must all count as missing. Parent directories that are symlinks, relative
# or absolute, resolve inside the rootfs. Unusable input (no argument, not a
# directory, a directory with no bin tree) must exit 2, never 0.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
verifier="${repo_root}/scripts/verify-runtime-image-tools.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

# Scanner stdio components were removed from the runtime image by #8878
# (execution lives in the satellite), so no passing rootfs carries them.

# make_rootfs NAME TOOL...: create <tmp>/NAME/usr/bin with an executable
# stub for each TOOL and print the rootfs path.
make_rootfs() {
	local root="${tmp_root}/$1" tool
	shift
	mkdir -p "${root}/usr/bin" "${root}/bin"
	for tool in "$@"; do
		printf '#!/bin/sh\nexit 0\n' >"${root}/usr/bin/${tool}"
		chmod 0755 "${root}/usr/bin/${tool}"
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
	record_fail "verifier exists at scripts/verify-runtime-image-tools.sh"
	printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
	exit 1
fi

# RED: the pre-#7762 runtime stage installed git and curl but not ssh.
red="$(make_rootfs red git curl)"
expect "RED pre-#7762 rootfs without ssh fails" 1 "ssh" -- "${red}"

# GREEN: adding ssh is the only change needed.
green="$(make_rootfs green git curl ssh)"
expect "GREEN rootfs with git, ssh, curl passes" 0 -- "${green}"

# GREEN (#8878 removal follow-up): scanner stdio components are gone from the
# runtime image, so a rootfs with only the shell tools must pass.
noscan="$(make_rootfs noscan git curl ssh)"
expect "GREEN rootfs without scanner components passes" 0 -- "${noscan}"

# Every required tool is enforced, not only ssh.
expect "rootfs without git fails" 1 "git" -- "$(make_rootfs nogit ssh curl)"
expect "rootfs without curl fails" 1 "curl" -- "$(make_rootfs nocurl git ssh)"

# Tools found under /bin and /usr/local/bin count.
alt="$(make_rootfs alt git)"
mkdir -p "${alt}/usr/local/bin"
printf '#!/bin/sh\n' >"${alt}/bin/ssh" && chmod 0755 "${alt}/bin/ssh"
printf '#!/bin/sh\n' >"${alt}/usr/local/bin/curl" && chmod 0755 "${alt}/usr/local/bin/curl"
expect "tools in /bin and /usr/local/bin pass" 0 -- "${alt}"

# Symlinks resolve inside the rootfs: absolute and relative targets.
linked="$(make_rootfs linked git curl)"
mkdir -p "${linked}/usr/libexec"
printf '#!/bin/sh\n' >"${linked}/usr/libexec/ssh-real" && chmod 0755 "${linked}/usr/libexec/ssh-real"
ln -s /usr/libexec/ssh-real "${linked}/usr/bin/ssh"
expect "absolute symlink resolved inside rootfs passes" 0 -- "${linked}"
rm "${linked}/usr/bin/ssh"
ln -s ../libexec/ssh-real "${linked}/usr/bin/ssh"
expect "relative symlink resolved inside rootfs passes" 0 -- "${linked}"

# False green: an absolute link whose target exists only on the host. /bin/sh
# exists on every test host, so a host-relative resolver would pass this.
hostlink="$(make_rootfs hostlink git curl)"
ln -s /bin/sh "${hostlink}/usr/bin/ssh"
expect "symlink to a host-only target fails" 1 "ssh" -- "${hostlink}"

# Enough ".." to climb from any test rootfs to the host's /.
up="$(printf '../%.0s' {1..32})"

# False green: a relative link whose ".." segments climb above the rootfs top
# and land on the host /bin/sh.
dotdot="$(make_rootfs dotdot git curl)"
ln -s "${up}bin/sh" "${dotdot}/usr/bin/ssh"
expect "relative symlink climbing out of the rootfs fails" 1 "ssh" -- "${dotdot}"

# False green: the tool link stays inside the rootfs, but a directory on its
# path is a relative symlink that climbs out to the host /bin.
dirclimb="$(make_rootfs dirclimb git curl)"
mkdir -p "${dirclimb}/usr/lib"
ln -s "${up}bin" "${dirclimb}/usr/lib/hostbin"
ln -s ../lib/hostbin/sh "${dirclimb}/usr/bin/ssh"
expect "directory symlink climbing out of the rootfs fails" 1 "ssh" -- "${dirclimb}"

# A ".." above the rootfs top fails closed rather than clamping at / the way
# the kernel would, even when the clamped path exists inside the rootfs.
clamp="$(make_rootfs clamp git curl)"
mkdir -p "${clamp}/usr/libexec"
printf '#!/bin/sh\n' >"${clamp}/usr/libexec/ssh-real" && chmod 0755 "${clamp}/usr/libexec/ssh-real"
ln -s "${up}usr/libexec/ssh-real" "${clamp}/usr/bin/ssh"
expect "relative symlink above the rootfs top fails closed" 1 "ssh" -- "${clamp}"

# False green: a path the kernel rejects must not pass because ".." or a
# trailing "/" was normalized away. Each link names a real executable after
# lexical normalization, but exec through it fails with ENOENT or ENOTDIR.
nondir="$(make_rootfs nondir git curl)"
mkdir -p "${nondir}/usr/libexec"
printf '#!/bin/sh\n' >"${nondir}/usr/libexec/ssh-real" && chmod 0755 "${nondir}/usr/libexec/ssh-real"
ln -s ../libexec/missing/../ssh-real "${nondir}/usr/bin/ssh"
expect "symlink through a missing directory fails" 1 "ssh" -- "${nondir}"
rm "${nondir}/usr/bin/ssh"
ln -s git/../../libexec/ssh-real "${nondir}/usr/bin/ssh"
expect "symlink through a regular file fails" 1 "ssh" -- "${nondir}"
rm "${nondir}/usr/bin/ssh"
ln -s ../libexec/ssh-real/ "${nondir}/usr/bin/ssh"
expect "symlink with a trailing slash to a file fails" 1 "ssh" -- "${nondir}"
rm "${nondir}/usr/bin/ssh"
ln -s ssh-real "${nondir}/usr/libexec/ssh-link"
ln -s ../libexec/ssh-link/ "${nondir}/usr/bin/ssh"
expect "trailing slash after a symlink to a file fails" 1 "ssh" -- "${nondir}"

# An empty link target fails closed. Only some systems (macOS) can create
# one, so the case runs only where ln accepts it.
emptylink="$(make_rootfs emptylink git curl)"
printf '#!/bin/sh\n' >"${emptylink}/usr/ssh-real" && chmod 0755 "${emptylink}/usr/ssh-real"
if ln -s "" "${emptylink}/usr/empty" 2>/dev/null; then
	ln -s ../empty/ssh-real "${emptylink}/usr/bin/ssh"
	expect "directory symlink with an empty target fails" 1 "ssh" -- "${emptylink}"
else
	printf 'skip - directory symlink with an empty target: ln cannot create one here\n'
fi

# Directory symlinks on the path resolve inside the rootfs: a relative one,
# and an absolute one whose target exists only inside the rootfs.
dirlinked="$(make_rootfs dirlinked git curl)"
mkdir -p "${dirlinked}/opt/eshu-7762-ssh/bin"
printf '#!/bin/sh\n' >"${dirlinked}/opt/eshu-7762-ssh/bin/ssh"
chmod 0755 "${dirlinked}/opt/eshu-7762-ssh/bin/ssh"
ln -s ../opt/eshu-7762-ssh "${dirlinked}/usr/sshdir"
ln -s ../sshdir/bin/ssh "${dirlinked}/usr/bin/ssh"
expect "relative directory symlink resolved inside rootfs passes" 0 -- "${dirlinked}"
rm "${dirlinked}/usr/sshdir"
ln -s /opt/eshu-7762-ssh "${dirlinked}/usr/sshdir"
expect "absolute directory symlink resolved inside rootfs passes" 0 -- "${dirlinked}"

# False green: present but not executable.
noexec="$(make_rootfs noexec git curl)"
printf '#!/bin/sh\n' >"${noexec}/usr/bin/ssh" && chmod 0644 "${noexec}/usr/bin/ssh"
expect "non-executable ssh fails" 1 "ssh" -- "${noexec}"

# False green: a directory is executable (searchable) but not a binary.
dirtool="$(make_rootfs dirtool git curl)"
mkdir -p "${dirtool}/usr/bin/ssh"
expect "directory named ssh fails" 1 "ssh" -- "${dirtool}"

# A symlink loop must terminate and fail, not hang.
loop="$(make_rootfs loop git curl)"
ln -s ssh-b "${loop}/usr/bin/ssh"
ln -s ssh "${loop}/usr/bin/ssh-b"
expect "symlink loop fails" 1 "ssh" -- "${loop}"

# Unusable input fails closed with exit 2.
expect "no argument exits 2" 2 --
expect "nonexistent path exits 2" 2 -- "${tmp_root}/does-not-exist"
printf 'x' >"${tmp_root}/plain-file"
expect "regular file exits 2" 2 -- "${tmp_root}/plain-file"
mkdir -p "${tmp_root}/empty"
expect "directory with no bin tree exits 2" 2 -- "${tmp_root}/empty"

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" -eq 0 ]]
