#!/usr/bin/env bash
set -euo pipefail

helm registry login "${REGISTRY}" \
  --username "${GITHUB_ACTOR}" \
  --password "${GITHUB_TOKEN}"

set +e
push_output="$(helm push dist/eshu-*.tgz "${CHART_REGISTRY}" 2>&1)"
push_status=$?
set -e
if [[ "${push_status}" -eq 0 ]]; then
  printf '%s\n' "${push_output}"
  exit 0
fi

version="$(awk '/^version:/ {print $2; exit}' "${CHART_PATH}/Chart.yaml")"
remote_dir="$(mktemp -d)"
trap 'rm -r -- "${remote_dir}"' EXIT
if helm pull "${CHART_REGISTRY}/eshu" --version "${version}" \
    --destination "${remote_dir}" >/dev/null 2>&1; then
  for local_tgz in dist/eshu-*.tgz; do
    if cmp -s "${local_tgz}" "${remote_dir}/eshu-${version}.tgz"; then
      echo "::notice::Chart ${version} already published with identical bytes; treating the duplicate push as success."
      exit 0
    fi
  done
fi
printf '%s\n' "${push_output}" >&2
echo "::error::Helm push failed and the published ${version} differs (or is unreadable); not overwriting." >&2
exit "${push_status}"
