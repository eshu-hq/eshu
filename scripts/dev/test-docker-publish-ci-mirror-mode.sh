#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workflow="${1:-${repo_root}/.github/workflows/docker-publish.yml}"

job_condition() {
  local job="$1"
  awk -v wanted="  ${job}:" '
    /^  [a-zA-Z][a-zA-Z0-9-]*:$/ { in_job = ($0 == wanted); in_if = 0 }
    in_job && /^    if:/ { in_if = 1; print; next }
    in_if && /^      / { print; next }
    in_if { exit }
  ' "${workflow}"
}

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

rg -q '^  workflow_dispatch:$' "${workflow}" || fail 'manual dispatch missing'
rg -q '^        default: release$' "${workflow}" || fail 'normal dispatch default changed'

for job in changes verify-apk-floors build-and-push-image promote-moving-tags \
  verify-reproducibility attach-release-sbom package-and-push-chart; do
  condition="$(job_condition "${job}")"
  [[ "${condition}" == *"github.event_name != 'workflow_dispatch' || inputs.mode == 'release'"* ]] ||
    fail "${job} does not exclude mirror dispatches and allow release dispatches"
done

for spec in 'publish-ci-service-mirrors:ci-mirrors-publish' \
  'verify-public-ci-service-mirrors:ci-mirrors-verify-public'; do
  job="${spec%%:*}"
  mode="${spec#*:}"
  condition="$(job_condition "${job}")"
  [[ "${condition}" == *"github.event_name == 'workflow_dispatch' && inputs.mode == '${mode}'"* ]] ||
    fail "${job} does not require its exact manual mode"
  [[ "${condition}" == *"github.repository == 'eshu-hq/eshu'"* ]] ||
    fail "${job} does not require the parent repository"
  [[ "${condition}" == *"github.ref == 'refs/heads/fix/ci-owned-image-mirror-20261009'"* ]] ||
    fail "${job} cannot run on the reviewed bootstrap branch"
done

if [[ "$#" -eq 0 ]]; then
  scratch="$(mktemp -d)"
  trap 'rm -r -- "${scratch}"' EXIT
  sed "/^  changes:/,/^  verify-apk-floors:/s/inputs.mode == 'release'/inputs.mode == 'ci-mirrors-publish'/" \
    "${workflow}" > "${scratch}/bad.yml"
  if bash "$0" "${scratch}/bad.yml" > /dev/null 2>&1; then
    fail 'seeded mirror-dispatch violation was not detected'
  fi
fi

printf 'PASS: mirror modes exclude all seven release jobs; normal dispatch remains enabled\n'
