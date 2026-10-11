#!/usr/bin/env bash
# Prepare the overlay for the caller's Compose file array after it acquires the
# live-gate lock. Every Compose call of the gate goes through scripts/ci/compose.sh,
# which appends the overlay itself; the array is not changed here.
# shellcheck disable=SC2154  # The sourced library reads gate-owned globals.

golden_corpus_prepare_ci_overlay() {
    [[ "${use_compose}" -eq 1 ]] || return 0
    # shellcheck source=scripts/lib/ci-image-compose.sh
    source "${repo_root}/scripts/lib/ci-image-compose.sh"
    eshu_ci_compose_prepare "${repo_root}" "${compose_args[@]}" || {
        release_live_gate_lock
        die "CI image overlay preflight failed"
    }
}
