#!/usr/bin/env bash
# Prove CI image overrides reach the Dockerfile and standby build consumers.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${root}"
failures=0
for variable in ESHU_XX_IMAGE ESHU_GO_IMAGE ESHU_ALPINE_IMAGE; do
  if ! rg -q "^ARG ${variable}=" Dockerfile || ! rg -q "^FROM .*\\\$\\{${variable}\\}" Dockerfile; then
    printf 'FAIL: Dockerfile has no %s override\n' "${variable}" >&2
    failures=$((failures + 1))
  fi
done
if ! rg -q 'image=.*ESHU_LIVE_POSTGRES_IMAGE' scripts/ci/live-postgres-standby-fixture.sh; then
  printf 'FAIL: standby fixture ignores CI image override\n' >&2
  failures=$((failures + 1))
fi
# Check the actual workflow environment, including callers outside the five
# direct Compose workflows. A correct helper is insufficient if CI omits pins.
for workflow in e2e-tests frontend test golden-corpus-gate ifa-determinism-gate live-backend-tests read-api-latency-gate; do
  for variable in POSTGRES_IMAGE NEO4J_IMAGE ESHU_XX_IMAGE ESHU_GO_IMAGE ESHU_ALPINE_IMAGE; do
    expected="$(jq -r --arg name "${variable}" '.images[] | select(.env == $name) | .destination + "@" + .digest' specs/ci-images.v1.json)"
    actual="$(yq -r ".env.${variable} // \"\"" ".github/workflows/${workflow}.yml")"
    if [[ "${actual}" != "${expected}" ]]; then
      printf 'FAIL: %s does not pass canonical %s pin\n' "${workflow}" "${variable}" >&2
      failures=$((failures + 1))
    fi
  done
  if [[ "$(yq -r '.env.ESHU_CI_IMAGES // ""' ".github/workflows/${workflow}.yml")" != 1 ]]; then
    printf 'FAIL: %s does not enable the CI overlay\n' "${workflow}" >&2
    failures=$((failures + 1))
  fi
done
for pair in 'live-postgres-readiness ESHU_LIVE_POSTGRES_IMAGE postgres' 'live-postgres-readiness NEO4J_IMAGE neo4j' 'reducer-contention-gate POSTGRES_IMAGE postgres'; do
  read -r workflow variable service <<< "${pair}"
  expected="$(jq -r --arg name "${variable}" '.images[] | select(.env == $name) | .destination + "@" + .digest' specs/ci-images.v1.json)"
  services="$(yq -o=json '.jobs' ".github/workflows/${workflow}.yml" | jq --arg name "${service}" '[.[] | .services[$name] // empty]')"
  if ! jq -e --arg expected "${expected}" 'length > 0 and all(.[]; .image == $expected and .credentials == null)' <<< "${services}" >/dev/null; then
    printf 'FAIL: %s %s service is not an anonymous canonical GHCR pull\n' "${workflow}" "${service}" >&2
    failures=$((failures + 1))
  fi
done
expected="$(jq -r '.images[] | select(.env == "CI_BUILDKIT_IMAGE") | .destination + "@" + .digest' specs/ci-images.v1.json)"
if [[ "$(yq -r '.env.CI_BUILDKIT_IMAGE // ""' .github/workflows/docker-publish.yml)" != "${expected}" ]]; then
  echo 'FAIL: Docker publication has no canonical BuildKit mirror' >&2
  failures=$((failures + 1))
fi
# Four Buildx setups: the main image, tag-promotion, and reproducibility jobs
# plus the scanner-images publish job (#8878). Every one must pin the
# canonical mirror image; the count pins against a silent fifth.
builders="$(yq -o=json '.jobs' .github/workflows/docker-publish.yml | jq '[.[] | .steps[]? | select(.uses == "docker/setup-buildx-action@v3")]')"
if ! jq -e 'length == 4 and all(.[]; .with["driver-opts"] == "image=${{ env.CI_BUILDKIT_IMAGE }}")' <<< "${builders}" >/dev/null; then
  echo 'FAIL: a Buildx setup still uses an implicit image' >&2
  failures=$((failures + 1))
fi
[[ "${failures}" == 0 ]]
