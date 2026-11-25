#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2154
# Kit-pin trigger cases for test-verify-ci-gates-registry.sh.
# Sourced by the parent test, which owns repo_root, registry, and fail();
# not intended to run standalone.

check_kit_pin_installer_selects_kit_gates() {
	local script='scripts/ci/install-eshu-agent-kit.sh'
	local gate selection

	for gate in codex-goal-continuation; do
		selection="$(
			printf '%s\n' "${script}" |
				(cd "${repo_root}/go" && go run ./cmd/ci-gates select \
					--registry "${registry}" --tier pre-pr --paths-from - --explain)
		)"
		printf '%s\n' "${selection}" |
			rg "^SELECTED[[:space:]]+${gate}[[:space:]]" >/dev/null ||
			fail "a kit-pin-only change (${script}) did not select ${gate}"
	done
}
