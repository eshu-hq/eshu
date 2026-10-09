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
# False-green guards: an absolute symlink whose target exists only on the
# host, a non-executable file, a directory named like the tool, and a symlink
# loop must all count as missing. Unusable input (no argument, not a
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
