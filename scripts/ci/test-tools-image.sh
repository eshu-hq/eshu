#!/usr/bin/env bash
# Smoke the prebuilt static-contract-gates tool image against this checkout.
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <local-image-ref>" >&2
  exit 64
fi

image="$1"
platform="${CI_TOOLS_PLATFORM:-linux/amd64}"
repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
cache_volume="$(docker volume create)"
trap 'docker volume rm "${cache_volume}" >/dev/null' EXIT

docker run --rm --platform "${platform}" \
  --mount "type=bind,src=${repo_root},dst=/workspace,readonly" \
  --mount "type=volume,src=${cache_volume},dst=/ci-cache" \
  --workdir /workspace \
  --env GOCACHE=/ci-cache/go-build \
  --env GOMODCACHE=/ci-cache/go-mod \
  "${image}" bash -euo pipefail -c '
    test "$(go version | awk "{print \$3}")" = go1.26.9
    getconf GNU_LIBC_VERSION | rg "^glibc 2\\.39$"
    for tool in bash git curl rg jq yq python3 gh gcc pkg-config docker; do
      command -v "${tool}" >/dev/null
    done
    for daemon_package in docker.io containerd; do
      if dpkg-query -W "${daemon_package}" >/dev/null 2>&1; then
        echo "unneeded daemon package: ${daemon_package}" >&2
        exit 1
      fi
    done
    docker compose version
    test -s /usr/local/share/ci-tools/packages.tsv
    test -s /usr/local/share/ci-tools/downloads.tsv
    for license in /usr/local/go/LICENSE /usr/local/share/licenses/ci-tools/{ripgrep,yq,docker-cli,docker-compose}.LICENSE; do
      test -s "${license}"
    done
    mkdir -p "${GOCACHE}" "${GOMODCACHE}"
    test -w "${GOCACHE}" && test -w "${GOMODCACHE}"
    cd go
    go test ./internal/content/shape -run TestContentEntityBucketsMatchCollectorTwin -count=1
    go test -race ./internal/ifa/saturation/ -run "^Test" -count=1
    cd ..
    bash scripts/verify-openapi.sh
  '

if [[ "${CI_TOOLS_DOCKER_SOCKET:-0}" == 1 ]]; then
  docker run --rm --platform "${platform}" \
    --mount "type=bind,src=${repo_root},dst=/workspace,readonly" \
    --mount type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock \
    --workdir /workspace \
    --env CI_TOOLS_NESTED_IMAGE="${CI_TOOLS_NESTED_IMAGE:-${image}}" \
    "${image}" bash -euo pipefail -c '
      docker info --format "{{.ServerVersion}}" >/dev/null
      docker run --rm --mount type=bind,src=/workspace,dst=/nested,readonly \
        "${CI_TOOLS_NESTED_IMAGE:?set CI_TOOLS_NESTED_IMAGE to a local Linux image}" \
        test -f /nested/go/go.mod
    '
fi
