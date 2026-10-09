#!/usr/bin/env bash
set -euo pipefail

# These are immutable, exact upstream indexes used by Eshu's CI service jobs.
# Do not accept image references from workflow inputs or the environment.
readonly alpine_digest='sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873'
readonly bookworm_digest='sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c'
readonly neo4j_digest='sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f'
readonly crane_bin="${CRANE_BIN:-crane}"

if [[ "$#" -ne 1 ]]; then
  printf 'usage: %s publish|verify-public\n' "$0" >&2
  exit 2
fi

case "$1" in
  publish)
    if [[ "${GITHUB_REPOSITORY:-}" != 'eshu-hq/eshu' ||
          "${GITHUB_EVENT_NAME:-}" != 'workflow_dispatch' ||
          ( "${GITHUB_REF:-}" != 'refs/heads/main' &&
            "${GITHUB_REF:-}" != 'refs/heads/fix/ci-owned-image-mirror-20261009' ) ]]; then
      printf 'publish requires a manual run on eshu-hq/eshu main or its reviewed bootstrap branch\n' >&2
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
      printf '%s existing tag conflict: got %s, want %s\n' "${name}" "${observed}" "${expected}" >&2
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

  # A pre-existing tag cannot be overwritten. If another publisher wins the
  # race after our precheck, accept only its exact expected digest.
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

if [[ "$1" == publish ]]; then
  publish_one postgres-alpine \
    "mirror.gcr.io/library/postgres:18-alpine@${alpine_digest}" \
    ghcr.io/eshu-hq/ci-postgres-alpine:18 "${alpine_digest}"
  publish_one postgres-bookworm \
    "mirror.gcr.io/library/postgres:18.6-bookworm@${bookworm_digest}" \
    ghcr.io/eshu-hq/ci-postgres-bookworm:18.6 "${bookworm_digest}"
  publish_one neo4j-community \
    "mirror.gcr.io/library/neo4j:2026-community@${neo4j_digest}" \
    ghcr.io/eshu-hq/ci-neo4j-community:2026 "${neo4j_digest}"
  printf 'New GHCR packages may be private. Make all three public, then run verify-public before changing consumers.\n'
  exit 0
fi

# Anonymous verification must not reuse the authenticated publish job's Docker
# config; forks and merge-group service pulls do not have package credentials.
anonymous_config="$(mktemp -d)"
trap 'rm -r -- "${anonymous_config}"' EXIT
export DOCKER_CONFIG="${anonymous_config}"
verify_public_one postgres-alpine ghcr.io/eshu-hq/ci-postgres-alpine:18 "${alpine_digest}"
verify_public_one postgres-bookworm ghcr.io/eshu-hq/ci-postgres-bookworm:18.6 "${bookworm_digest}"
verify_public_one neo4j-community ghcr.io/eshu-hq/ci-neo4j-community:2026 "${neo4j_digest}"
