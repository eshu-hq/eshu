#!/usr/bin/env bash
# Source this file, then prepare once per original Compose file stack.
# The returned path is safe to append as the final -f argument to every
# Compose command (including cleanup) in that stack's lifecycle.

eshu_ci_compose_signature() {
    local helper_root
    helper_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
    python3 "${helper_root}/scripts/lib/ci_image_compose_signature.py" "${helper_root}" "$@"
}

eshu_ci_compose_prepare() {
    local stack_root="$1" rendered stage directory helper_root signature
    shift
    ESHU_CI_COMPOSE_OVERLAY=""
    ESHU_CI_COMPOSE_SIGNATURE=""
    if [[ "${ESHU_CI_IMAGES:-0}" != "1" ]]; then
        return 0
    fi
    signature="$(eshu_ci_compose_signature "${stack_root}" "$@")" || return 1
    if ! rendered="$(docker compose "$@" --profile '*' config --format json 2>/dev/null)"; then
        printf 'ci-image-compose: Docker Compose render failed\n' >&2
        return 1
    fi
    if [[ "${signature}" != "$(eshu_ci_compose_signature "${stack_root}" "$@")" ]]; then
        printf 'ci-image-compose: Compose inputs changed during render\n' >&2
        return 1
    fi
    directory="$(mktemp -d "${TMPDIR:-/tmp}/eshu-ci-compose.XXXXXXXX")" || return 1
    chmod 700 "${directory}"
    stage="${directory}/overlay.partial.json"
    helper_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
    (umask 077; printf '%s' "${rendered}" | python3 "${helper_root}/scripts/lib/ci_image_compose.py" \
        --root "${stack_root}" --ledger "${helper_root}/specs/ci-images.v1.json" > "${stage}") || {
        rm -rf "${directory}"
        return 1
    }
    chmod 600 "${stage}"
    mv "${stage}" "${directory}/overlay.json"
    ESHU_CI_COMPOSE_OVERLAY="${directory}/overlay.json"
    ESHU_CI_COMPOSE_SIGNATURE="${signature}"
    export ESHU_CI_COMPOSE_OVERLAY ESHU_CI_COMPOSE_SIGNATURE
}

eshu_ci_compose_cleanup() {
    if [[ -n "${ESHU_CI_COMPOSE_OVERLAY:-}" ]]; then
        local directory
        directory="$(dirname "${ESHU_CI_COMPOSE_OVERLAY}")"
        if [[ "$(basename "${ESHU_CI_COMPOSE_OVERLAY}")" != "overlay.json" ||
              "$(basename "${directory}")" != eshu-ci-compose.* ]]; then
            printf 'ci-image-compose: refusing to remove unexpected overlay path\n' >&2
            return 1
        fi
        rm -rf "${directory}"
        ESHU_CI_COMPOSE_OVERLAY=""
        ESHU_CI_COMPOSE_SIGNATURE=""
        export ESHU_CI_COMPOSE_OVERLAY ESHU_CI_COMPOSE_SIGNATURE
    fi
}
