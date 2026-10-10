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
workspace_volume=""
cleanup() {
  docker volume rm "${cache_volume}" >/dev/null
  if [[ -n "${workspace_volume}" ]]; then
    docker volume rm "${workspace_volume}" >/dev/null
  fi
}
trap cleanup EXIT
docker run --rm --user 0 \
  --mount "type=volume,src=${cache_volume},dst=/tmp/eshu-ci" \
  --entrypoint chown "${image}" 1001:1001 /tmp/eshu-ci

docker run --rm --platform "${platform}" \
  --mount "type=bind,src=${repo_root},dst=/workspace,readonly" \
  --mount "type=volume,src=${cache_volume},dst=/tmp/eshu-ci" \
  --workdir /workspace \
  --env GOCACHE=/tmp/eshu-ci/go-build \
  --env GOMODCACHE=/tmp/eshu-ci/go-mod \
  "${image}" bash -euo pipefail -c '
    if [[ "$(id -u)" == 0 ]]; then
      echo "CI tools image runs as root" >&2
      exit 1
    fi
    if [[ "$(id -un)" != ci-tools ]]; then
      echo "CI tools image has no ci-tools account for its runtime UID" >&2
      exit 1
    fi
    if [[ -z "${HOME}" || ! -w "${HOME}" ]]; then
      echo "CI tools image has no writable HOME" >&2
      exit 1
    fi
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

workspace_volume="$(docker volume create)"
docker run --rm --user 0 \
  --mount "type=volume,src=${workspace_volume},dst=/workspace" \
  --entrypoint chown "${image}" 1001:1001 /workspace
docker run --rm --platform "${platform}" \
  --mount "type=volume,src=${workspace_volume},dst=/workspace" \
  --workdir /workspace \
  "${image}" bash -euo pipefail -c '
    git init --bare --initial-branch=main /workspace/remote.git
    git init --initial-branch=main /workspace/source
    git -C /workspace/source config user.name "CI tools smoke test"
    git -C /workspace/source config user.email ci-tools@example.invalid
    printf "checkout ownership proof\\n" > /workspace/source/README
    git -C /workspace/source add README
    git -C /workspace/source commit -m "initial"
    git -C /workspace/source remote add origin /workspace/remote.git
    git -C /workspace/source push --quiet origin main
    git clone --quiet /workspace/remote.git /workspace/checkout
    git -C /workspace/checkout fetch --quiet origin main
    test "$(git -C /workspace/checkout rev-parse HEAD)" = "$(git -C /workspace/checkout rev-parse origin/main)"
    test -z "$(git -C /workspace/checkout status --porcelain)"
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
