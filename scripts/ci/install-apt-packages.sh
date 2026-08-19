#!/usr/bin/env bash
#
# install-apt-packages.sh - install CI tool dependencies: apt where apt is
# appropriate, and a pinned checksum-verified release binary for ripgrep
# because the apt path intermittently hangs until the job timeout (do not
# "simplify" ripgrep back onto apt-get -- that reintroduces the hang).
#
# Env overrides (all optional; defaults are the verified pin):
#   ESHU_CI_APT_RIPGREP_URL                  download source (file:// works,
#                                             for hermetic tests)
#   ESHU_CI_APT_RIPGREP_SHA256                expected sha256 of that download
#   ESHU_CI_APT_BIN_DIR                       install destination (default
#                                             /usr/local/bin), honors
#                                             ESHU_CI_APT_NO_SUDO
#   ESHU_CI_APT_RIPGREP_DOWNLOAD_ATTEMPTS      bounded retry count (default 3)
#   ESHU_CI_APT_RIPGREP_DOWNLOAD_RETRY_DELAY   seconds, doubles each retry
#                                             (default 2)
#   ESHU_CI_APT_RIPGREP_DOWNLOAD_MAX_TIME      seconds, hard ceiling on the
#                                             whole transfer, not just
#                                             connect (default 120)
# Existing knobs (unchanged): ESHU_CI_APT_NO_SUDO, ESHU_CI_APT_FORCE_INSTALL,
# ESHU_CI_APT_SKIP_INSTALL, ESHU_CI_APT_SOURCES_DIR.
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: $0 <apt-package>..." >&2
  exit 64
fi

# Verified against ripgrep's own published .sha256 for this release; do not
# change without re-verifying upstream.
readonly ESHU_CI_APT_PINNED_RIPGREP_URL="https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-x86_64-unknown-linux-musl.tar.gz"
readonly ESHU_CI_APT_PINNED_RIPGREP_SHA256="4cf9f2741e6c465ffdb7c26f38056a59e2a2544b51f7cc128ef28337eeae4d8e"

# RIPGREP_WORK_DIR is set only once install_ripgrep_pinned_binary actually
# runs; the EXIT trap below is a no-op otherwise.
RIPGREP_WORK_DIR=""
cleanup_ripgrep_work_dir() {
  if [ -n "${RIPGREP_WORK_DIR}" ]; then
    rm -rf "${RIPGREP_WORK_DIR}"
  fi
}
trap cleanup_ripgrep_work_dir EXIT

command_for_package() {
  case "$1" in
    ripgrep)
      printf 'rg'
      ;;
    *)
      printf '%s' "$1"
      ;;
  esac
}

run_privileged() {
  if [ "${ESHU_CI_APT_NO_SUDO:-0}" = "1" ]; then
    "$@"
    return
  fi
  sudo "$@"
}

disable_unstable_runner_sources() {
  local source_dir="${ESHU_CI_APT_SOURCES_DIR:-/etc/apt/sources.list.d}"
  if [ ! -d "${source_dir}" ]; then
    return
  fi

  local source
  shopt -s nullglob
  for source in \
    "${source_dir}"/*microsoft* \
    "${source_dir}"/*azure-cli* \
    "${source_dir}"/*packages-microsoft*; do
    if [ ! -e "${source}" ] || [[ "${source}" == *.eshu-disabled ]]; then
      continue
    fi
    echo "Disabling unstable runner apt source: ${source}" >&2
    run_privileged mv "${source}" "${source}.eshu-disabled"
  done
  shopt -u nullglob
}

# sha256_command prints the sha256 tool invocation to use, preferring
# sha256sum (the ubuntu-latest runners) and falling back to `shasum -a 256`
# (macOS dev machines) -- same detection order as
# scripts/run-two-team-governance-proof.sh.
sha256_command() {
  if command -v sha256sum >/dev/null 2>&1; then
    printf 'sha256sum'
  elif command -v shasum >/dev/null 2>&1; then
    printf 'shasum -a 256'
  else
    echo "install-apt-packages: neither sha256sum nor shasum is available to verify ripgrep" >&2
    exit 1
  fi
}

# download_ripgrep_archive downloads $1 to $2 with bounded retry and
# doubling backoff -- the same shape as scripts/ci/go-mod-download-retry.sh.
# Only transient download failures are retried; a checksum mismatch after a
# successful download is never retried (that would just spend attempts
# re-fetching a file whose bytes already do not match).
#
# --connect-timeout alone is NOT enough: it bounds connection setup only, so
# a host that accepts the connection and then stalls mid-transfer -- the same
# failure mode as the apt hang this function exists to remove, just on a
# different host -- would hang until the job timeout with the retry loop
# never getting a turn. --max-time bounds the whole transfer as a hard
# ceiling; --speed-limit/--speed-time aborts a stall faster, without
# punishing a slow-but-progressing link. Both are load-bearing: dropping
# either re-opens the hang. See test_download_bounds_transfer_not_just_connect
# in scripts/test-ci-install-apt-packages.sh, which asserts this curl
# invocation still carries both.
download_ripgrep_archive() {
  local url="$1" dest="$2"
  local attempts="${ESHU_CI_APT_RIPGREP_DOWNLOAD_ATTEMPTS:-3}"
  local delay="${ESHU_CI_APT_RIPGREP_DOWNLOAD_RETRY_DELAY:-2}"
  local max_time="${ESHU_CI_APT_RIPGREP_DOWNLOAD_MAX_TIME:-120}"

  if ! [[ "${attempts}" =~ ^[1-9][0-9]*$ ]]; then
    echo "install-apt-packages: ESHU_CI_APT_RIPGREP_DOWNLOAD_ATTEMPTS must be a positive integer, got \"${attempts}\"" >&2
    exit 2
  fi
  if ! [[ "${delay}" =~ ^[0-9]+$ ]]; then
    echo "install-apt-packages: ESHU_CI_APT_RIPGREP_DOWNLOAD_RETRY_DELAY must be a non-negative integer, got \"${delay}\"" >&2
    exit 2
  fi
  if ! [[ "${max_time}" =~ ^[1-9][0-9]*$ ]]; then
    echo "install-apt-packages: ESHU_CI_APT_RIPGREP_DOWNLOAD_MAX_TIME must be a positive integer, got \"${max_time}\"" >&2
    exit 2
  fi

  local attempt
  for attempt in $(seq 1 "${attempts}"); do
    if curl -fsSL --connect-timeout 10 --max-time "${max_time}" --speed-limit 1024 --speed-time 30 -o "${dest}" "${url}"; then
      if [ "${attempt}" -gt 1 ]; then
        echo "install-apt-packages: ripgrep download succeeded on attempt ${attempt}/${attempts}" >&2
      fi
      return 0
    fi
    rm -f "${dest}"
    if [ "${attempt}" -eq "${attempts}" ]; then
      echo "install-apt-packages: ripgrep download failed after ${attempts} attempt(s) from ${url}" >&2
      return 1
    fi
    echo "install-apt-packages: ripgrep download attempt ${attempt}/${attempts} failed; retrying in ${delay}s" >&2
    sleep "${delay}"
    delay=$((delay * 2))
  done
  return 1
}

# verify_ripgrep_checksum fails closed: a mismatch, or the absence of any
# sha256 tool, is a non-zero return, and the caller must not extract or
# install an unverified archive.
#
# The `|| return 1` on sha_cmd is load-bearing, not defensive padding.
# sha256_command's `exit 1` runs inside a command substitution, so it exits
# that SUBSHELL, not the script, and `set -e` cannot rescue it because this
# function is called in an `if !` condition, which suppresses errexit for the
# whole body. Without the guards, execution continues with sha_cmd empty and
# `${sha_cmd} "${archive}"` makes the DOWNLOADED ARCHIVE the command word --
# bash tries to execute the unverified download. That currently fails only
# because curl -o creates the file 0644, which is far too thin a thing to
# rest on in the one function whose job is to not trust the download. The
# `|| return 1` is what actually fires: sha256_command reports "neither
# sha256sum nor shasum is available" and its subshell exit becomes a non-zero
# substitution status, turning a misleading empty "checksum mismatch: got "
# into an accurate diagnostic. The -z check below is belt-and-braces for a
# future sha256_command that returns empty WITHOUT a non-zero status.
verify_ripgrep_checksum() {
  local archive="$1" expected="$2"
  local sha_cmd actual
  sha_cmd="$(sha256_command)" || return 1
  if [ -z "${sha_cmd}" ]; then
    echo "install-apt-packages: no sha256 tool available to verify ripgrep; refusing to install unverified" >&2
    return 1
  fi
  actual="$(${sha_cmd} "${archive}" | awk '{print $1}')"
  if [ "${actual}" != "${expected}" ]; then
    echo "install-apt-packages: ripgrep checksum mismatch: expected ${expected}, got ${actual}" >&2
    return 1
  fi
  echo "install-apt-packages: ripgrep checksum verified (${expected})" >&2
  return 0
}

# install_ripgrep_pinned_binary downloads, verifies, extracts, and installs
# the pinned ripgrep release. It never falls back to apt on any failure --
# a silent apt fallback would reintroduce the exact hang this function
# exists to remove -- it always exits non-zero instead.
# assert_ripgrep_arch_supported fails at INSTALL time if the machine is not
# the architecture the pin is built for. The default pin is
# x86_64-unknown-linux-musl and every caller runs on ubuntu-latest today, so
# this never fires now. It exists because the failure it prevents is
# expensive to read: on an arm64 runner the install would SUCCEED, and the
# breakage would surface later as "cannot execute binary file" at whatever
# unrelated `rg` call ran first -- a confusing failure far from its cause, in
# a change whose whole point is making CI failures mean something. Skipped
# when the URL is overridden, since a caller pointing at their own artifact
# (a test fixture, or an arm64 build) knows what they are fetching.
assert_ripgrep_arch_supported() {
  if [ -n "${ESHU_CI_APT_RIPGREP_URL:-}" ]; then
    return 0
  fi

  local machine
  machine="$(uname -m)"
  case "${machine}" in
    x86_64 | amd64) ;;
    *)
      echo "install-apt-packages: pinned ripgrep is x86_64-unknown-linux-musl but this machine is ${machine};" >&2
      echo "install-apt-packages: installing it would place a binary that cannot execute. Set ESHU_CI_APT_RIPGREP_URL and ESHU_CI_APT_RIPGREP_SHA256 to a matching build for this architecture." >&2
      return 1
      ;;
  esac
}

install_ripgrep_pinned_binary() {
  local url="${ESHU_CI_APT_RIPGREP_URL:-${ESHU_CI_APT_PINNED_RIPGREP_URL}}"
  local expected_sha256="${ESHU_CI_APT_RIPGREP_SHA256:-${ESHU_CI_APT_PINNED_RIPGREP_SHA256}}"
  local bin_dir="${ESHU_CI_APT_BIN_DIR:-/usr/local/bin}"

  RIPGREP_WORK_DIR="$(mktemp -d)"
  local archive="${RIPGREP_WORK_DIR}/ripgrep.tar.gz"

  if ! assert_ripgrep_arch_supported; then
    exit 1
  fi

  echo "install-apt-packages: installing ripgrep from pinned binary release (${url})" >&2
  if ! download_ripgrep_archive "${url}" "${archive}"; then
    exit 1
  fi

  if ! verify_ripgrep_checksum "${archive}" "${expected_sha256}"; then
    exit 1
  fi

  local extract_dir="${RIPGREP_WORK_DIR}/extract"
  mkdir -p "${extract_dir}"
  tar -xzf "${archive}" -C "${extract_dir}"

  # The release tarball has exactly one top-level directory containing `rg`;
  # matched by glob rather than a hardcoded versioned path so extraction does
  # not depend on the directory name matching ESHU_CI_APT_PINNED_RIPGREP_URL's
  # version string (also what lets a test fixture use any directory name).
  local rg_bin_matches=("${extract_dir}"/*/rg)
  if [ "${#rg_bin_matches[@]}" -ne 1 ] || [ ! -f "${rg_bin_matches[0]}" ]; then
    echo "install-apt-packages: could not find a single rg binary inside the ripgrep archive" >&2
    exit 1
  fi

  run_privileged mkdir -p "${bin_dir}"
  run_privileged install -m 0755 "${rg_bin_matches[0]}" "${bin_dir}/rg"
  echo "install-apt-packages: installed rg to ${bin_dir}/rg" >&2
}

missing_packages=()
missing_ripgrep=0

mark_missing() {
  local package="$1"
  if [ "${package}" = "ripgrep" ]; then
    missing_ripgrep=1
  else
    missing_packages+=("${package}")
  fi
}

if [ "${ESHU_CI_APT_FORCE_INSTALL:-0}" = "1" ]; then
  for package in "$@"; do
    mark_missing "${package}"
  done
else
  for package in "$@"; do
    command_name="$(command_for_package "${package}")"
    if command -v "${command_name}" >/dev/null 2>&1; then
      echo "${package} already available as ${command_name}" >&2
      continue
    fi
    mark_missing "${package}"
  done
fi

if [ "${missing_ripgrep}" -eq 1 ]; then
  if [ "${ESHU_CI_APT_SKIP_INSTALL:-0}" = "1" ]; then
    echo "install-apt-packages: skipping ripgrep pinned-binary install: ESHU_CI_APT_SKIP_INSTALL=1" >&2
  else
    install_ripgrep_pinned_binary
  fi
fi

if [ "${#missing_packages[@]}" -eq 0 ]; then
  exit 0
fi

disable_unstable_runner_sources

if [ "${ESHU_CI_APT_SKIP_INSTALL:-0}" = "1" ]; then
  echo "Skipping apt install: ${missing_packages[*]}" >&2
  exit 0
fi

run_privileged apt-get update
run_privileged apt-get install -y --no-install-recommends "${missing_packages[@]}"
