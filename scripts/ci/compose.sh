#!/usr/bin/env bash
# Run one CI Compose command with the same final image override as the gates.
# Usage: compose.sh <compose flags> -- <subcommand and args>
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose_args=()
while [[ "$#" -gt 0 && "$1" != "--" ]]; do
    compose_args+=("$1")
    shift
done
if [[ "${1:-}" != "--" || "$#" -lt 2 ]]; then
    printf 'usage: compose.sh <compose flags> -- <command>\n' >&2
    exit 2
fi
shift
# shellcheck source=scripts/lib/ci-image-compose.sh
source "${repo_root}/scripts/lib/ci-image-compose.sh"
if [[ "${ESHU_CI_IMAGES:-0}" == "1" && -n "${ESHU_CI_COMPOSE_OVERLAY:-}" ]]; then
    signature="$(eshu_ci_compose_signature "${repo_root}" "${compose_args[@]}")"
    if [[ ! -s "${ESHU_CI_COMPOSE_OVERLAY}" || "${signature}" != "${ESHU_CI_COMPOSE_SIGNATURE:-}" ]]; then
        printf 'ci-compose: prepared overlay no longer matches Compose files, environment, or pins\n' >&2
        exit 1
    fi
else
    eshu_ci_compose_prepare "${repo_root}" "${compose_args[@]}"
    trap eshu_ci_compose_cleanup EXIT
fi
if [[ -n "${ESHU_CI_COMPOSE_OVERLAY}" ]]; then
    compose_args+=(-f "${ESHU_CI_COMPOSE_OVERLAY}")
fi
docker compose "${compose_args[@]}" "$@"
