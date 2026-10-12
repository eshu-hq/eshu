#!/usr/bin/env bash
# Fail the gate early when the CI image overlay cannot cover the caller's Compose
# stack, after the gate holds the live-gate lock. The pulling call (up) goes through
# scripts/ci/compose.sh, which renders and appends its own overlay; the polls, the
# psql helper and the teardown are non-pulling Compose calls and stay direct, so
# the readiness and settle loops do not render an overlay per iteration.
# The gate shell holds no prepared overlay on purpose: the wrapper hashes the
# value of every variable a root Compose file names, and the gate exports such
# variables after this point (the Ask and metrics sources, the runtime
# environment), so a prepared overlay would refuse the first call that follows.
# shellcheck disable=SC2154  # The sourced library reads gate-owned globals.

golden_corpus_preflight_ci_overlay() {
	[[ "${use_compose}" -eq 1 ]] || return 0
	# The same variable prefix as the first up call, so the render sees one env.
	ESHU_NEO4J_PASSWORD="${ESHU_NEO4J_PASSWORD}" ESHU_POSTGRES_PASSWORD="${ESHU_POSTGRES_PASSWORD}" \
		bash "${repo_root}/scripts/ci/compose.sh" "${compose_args[@]}" -- config --quiet >/dev/null || {
		release_live_gate_lock
		die "CI image overlay preflight failed"
	}
}
