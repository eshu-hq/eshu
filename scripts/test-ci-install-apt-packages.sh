#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

source_dir="${tmp_root}/sources.list.d"
mkdir -p "${source_dir}"
touch "${source_dir}/microsoft-prod.list"
touch "${source_dir}/azure-cli.list"
touch "${source_dir}/ubuntu.sources"

ESHU_CI_APT_FORCE_INSTALL=1 \
  ESHU_CI_APT_NO_SUDO=1 \
  ESHU_CI_APT_SKIP_INSTALL=1 \
  ESHU_CI_APT_SOURCES_DIR="${source_dir}" \
  "${repo_root}/scripts/ci/install-apt-packages.sh" ripgrep jq

for disabled_source in \
  "${source_dir}/microsoft-prod.list.eshu-disabled" \
  "${source_dir}/azure-cli.list.eshu-disabled"; do
  if [ ! -e "${disabled_source}" ]; then
    echo "missing disabled source ${disabled_source}" >&2
    exit 1
  fi
done

for original_source in \
  "${source_dir}/microsoft-prod.list" \
  "${source_dir}/azure-cli.list"; do
  if [ -e "${original_source}" ]; then
    echo "source was not disabled: ${original_source}" >&2
    exit 1
  fi
done

if [ ! -e "${source_dir}/ubuntu.sources" ]; then
  echo "ubuntu source should remain enabled" >&2
  exit 1
fi
