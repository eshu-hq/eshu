#!/usr/bin/env bash
set -euo pipefail

# These are immutable, exact upstream indexes used by Eshu's CI service jobs
# and by the Dockerfile and BuildKit bases of its image builds. Do not accept
# image references from workflow inputs or the environment. Every copy keeps its
# index digest: the GHCR digest is the upstream digest, so a digest pin in the
# tree keeps its value after a consumer switches registry.
# Fields: name | upstream reference without digest | GHCR package | index digest.
readonly mirror_rows=(
  'postgres-alpine|mirror.gcr.io/library/postgres:18-alpine|ci-postgres-alpine|sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
  'postgres-bookworm|mirror.gcr.io/library/postgres:18.6-bookworm|ci-postgres-bookworm|sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c'
  'neo4j-community|mirror.gcr.io/library/neo4j:2026-community|ci-neo4j-community|sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f'
  'neo4j-query-plan|mirror.gcr.io/library/neo4j|ci-neo4j-query-plan|sha256:6c162e2432f861f2c4e3da77a6ba478e7f10e2160b870541f85294532bc6ff5f'
  'golang-alpine|mirror.gcr.io/library/golang:1.26.9-alpine|ci-golang-alpine|sha256:cdfd4fe2da6b225d8b40c6b7a105736e548e83ff56d5d8f9394446eeb5eb84e0'
  'alpine|mirror.gcr.io/library/alpine:3.21|ci-alpine|sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d'
  'buildx-xx|mirror.gcr.io/tonistiigi/xx:1.5.0|ci-buildx-xx|sha256:0c6a569797744e45955f39d4f7538ac344bfb7ebf0a54006a0a4297b153ccf0f'
  'buildkit|mirror.gcr.io/moby/buildkit:buildx-stable-1|ci-buildkit|sha256:cec9f139f45e93c5c69c60f8b07cfad9f43f4ef6b6a6cd917527fea5ff2e3dea'
)
readonly crane_bin="${CRANE_BIN:-crane}"

if [[ "$#" -ne 1 ]]; then
  printf 'usage: %s publish|verify-public\n' "$0" >&2
  exit 2
fi

case "$1" in
  publish)
    if [[ "${GITHUB_REPOSITORY:-}" != 'eshu-hq/eshu' ||
          "${GITHUB_EVENT_NAME:-}" != 'workflow_dispatch' ||
          "${GITHUB_REF:-}" != 'refs/heads/main' ]]; then
      printf 'publish requires a manual run on eshu-hq/eshu main\n' >&2
      exit 2
    fi
    if [[ ! "${EXPECTED_REVIEWED_SHA:-}" =~ ^[0-9a-f]{40}$ ||
          "${GITHUB_SHA:-}" != "${EXPECTED_REVIEWED_SHA}" ]]; then
      printf 'publish requires expected_sha to equal the reviewed workflow commit\n' >&2
      exit 2
    fi
    ;;
  verify-public) ;;
  *)
    printf 'unknown operation: %s\n' "$1" >&2
    exit 2
    ;;
esac

if ! command -v "${crane_bin}" >/dev/null 2>&1; then
  printf 'crane executable is missing: %s\n' "${crane_bin}" >&2
  exit 2
fi

publish_one() {
  local name="$1" source="$2" target="$3" expected="$4" observed

  if observed="$("${crane_bin}" digest "${target}" 2>/dev/null)"; then
    if [[ "${observed}" != "${expected}" ]]; then
      printf '%s existing digest conflict: got %s, want %s\n' "${name}" "${observed}" "${expected}" >&2
      exit 1
    fi
    printf 'already published %s %s %s\n' "${name}" "${target}" "${observed}"
    return 0
  fi

  observed="$("${crane_bin}" digest "${source}")"
  if [[ "${observed}" != "${expected}" ]]; then
    printf '%s source digest mismatch: got %s, want %s\n' "${name}" "${observed}" "${expected}" >&2
    exit 1
  fi

  # The destination is a digest reference, never a mutable tag. If another
  # publisher wins the race after our precheck, accept only the exact digest.
  if ! "${crane_bin}" cp --no-clobber "${source}" "${target}"; then
    if ! observed="$("${crane_bin}" digest "${target}" 2>/dev/null)" ||
        [[ "${observed}" != "${expected}" ]]; then
      printf '%s copy failed and destination is not the approved digest\n' "${name}" >&2
      exit 1
    fi
  fi
  observed="$("${crane_bin}" digest "${target}")"
  if [[ "${observed}" != "${expected}" ]]; then
    printf '%s destination digest mismatch: got %s, want %s\n' "${name}" "${observed}" "${expected}" >&2
    exit 1
  fi
  printf 'published %s %s %s\n' "${name}" "${target}" "${observed}"
}

verify_public_one() {
  local name="$1" target="$2" expected="$3" observed
  observed="$("${crane_bin}" digest "${target}")"
  if [[ "${observed}" != "${expected}" ]]; then
    printf '%s public digest mismatch: got %s, want %s\n' "${name}" "${observed}" "${expected}" >&2
    exit 1
  fi
  printf 'public %s %s %s\n' "${name}" "${target}" "${observed}"
}

# Split one row into name, source, target and digest. The source and the target
# both carry the row's own digest, so a copy cannot change it.
split_row() {
  local row="$1" repository package
  IFS='|' read -r row_name repository package row_digest <<< "${row}"
  row_source="${repository}@${row_digest}"
  row_target="ghcr.io/eshu-hq/${package}@${row_digest}"
}

if [[ "$1" == publish ]]; then
  for row in "${mirror_rows[@]}"; do
    split_row "${row}"
    publish_one "${row_name}" "${row_source}" "${row_target}" "${row_digest}"
  done
  printf 'New GHCR packages may be private. Make all %s public, then run verify-public before changing consumers.\n' "${#mirror_rows[@]}"
  exit 0
fi

# Anonymous verification must not reuse the authenticated publish job's Docker
# config; forks and merge-group service pulls do not have package credentials.
anonymous_config="$(mktemp -d)"
trap 'rm -r -- "${anonymous_config}"' EXIT
export DOCKER_CONFIG="${anonymous_config}"
for row in "${mirror_rows[@]}"; do
  split_row "${row}"
  verify_public_one "${row_name}" "${row_target}" "${row_digest}"
done
