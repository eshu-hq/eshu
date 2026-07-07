#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: $0 <apt-package>..." >&2
  exit 64
fi

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

missing_packages=()
if [ "${ESHU_CI_APT_FORCE_INSTALL:-0}" = "1" ]; then
  missing_packages=("$@")
else
  for package in "$@"; do
    command_name="$(command_for_package "${package}")"
    if command -v "${command_name}" >/dev/null 2>&1; then
      echo "${package} already available as ${command_name}" >&2
      continue
    fi
    missing_packages+=("${package}")
  done
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
