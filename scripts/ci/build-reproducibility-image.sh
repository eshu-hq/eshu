#!/usr/bin/env bash
# Build the release subject with mirrored bases and unchanged reproducibility flags.
set -euo pipefail
[[ $# == 2 && "$1" == /* && "$1" != *,* && "$2" =~ ^[0-9]+$ ]] || {
  echo 'usage: build-reproducibility-image.sh ABSOLUTE_OUTPUT SOURCE_DATE_EPOCH' >&2
  exit 64
}
root="$(cd "$(dirname "$0")/../.." && pwd)"
for name in ESHU_XX_IMAGE ESHU_GO_IMAGE ESHU_ALPINE_IMAGE; do
  expected="$(jq -r --arg name "${name}" '.images[] | select(.env == $name) | .destination + "@" + .digest' "${root}/specs/ci-images.v1.json")"
  [[ "${!name:-}" == "${expected}" ]] || {
    printf 'Reproducibility build requires canonical %s mirror\n' "${name}" >&2
    exit 1
  }
done
cd "${root}"
docker buildx build \
  --platform linux/amd64 \
  --build-arg "SOURCE_DATE_EPOCH=$2" \
  --build-arg ESHU_VERSION=reproducibility-test \
  --build-arg "ESHU_XX_IMAGE=${ESHU_XX_IMAGE}" \
  --build-arg "ESHU_GO_IMAGE=${ESHU_GO_IMAGE}" \
  --build-arg "ESHU_ALPINE_IMAGE=${ESHU_ALPINE_IMAGE}" \
  --provenance=false \
  --no-cache \
  --output "type=local,dest=$1" \
  .
