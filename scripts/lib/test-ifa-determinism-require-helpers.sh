#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# _ifa_det_count_code_matches(), and every *_lib path variable referenced
# below (lib, lifecycle_lib, delta_lib, code_call_lib, documentation_lib,
# deployable_unit_lib, rationale_lib, codeowners_lib, submodule_pin_lib), are
# all defined by scripts/test-verify-ifa-determinism.sh before it sources
# this file; shellcheck cannot see that from this file alone.
#
# Per-family require_*_lib() helpers for scripts/test-verify-ifa-determinism.sh,
# sourced so the top-level mirror stays below the repository's 500-line cap
# (mirroring the fault-injection sibling's per-mechanism case-module split).
# Each wraps the shared code-portion matcher against exactly one family's
# gate-adjacent library, so a needle satisfied only by a comment quoting it
# still fails -- the same reason require_code() exists for the gate script
# itself. Every helper here routes through _ifa_det_count_code_matches: the
# generic pin-behaviour probe (test-ifa-determinism-pin-behaviour-cases.sh)
# discovers every `require*` function and asserts each rejects a
# comment-only and heredoc-only needle, so a bare `rg --fixed-strings` form
# fails that probe the same as a hand-typed comment-immunity gap would.
require_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_matches "${needle}" "${lib}")" -ge 1 ]] \
		|| fail "missing ${label} (lib): ${needle}, or it survives only inside a comment"
}
require_lifecycle_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_matches "${needle}" "${lifecycle_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (lifecycle lib): ${needle}, or it survives only inside a comment"
}
require_delta_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_matches "${needle}" "${delta_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (delta lib): ${needle}, or it survives only inside a comment"
}
require_code_call_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_matches "${needle}" "${code_call_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (code-call lib): ${needle}, or it survives only inside a comment"
}
require_documentation_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_matches "${needle}" "${documentation_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (documentation lib): ${needle}, or it survives only inside a comment"
}
require_deployable_unit_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_matches "${needle}" "${deployable_unit_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (deployable-unit lib): ${needle}, or it survives only inside a comment"
}

require_rationale_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_matches "${needle}" "${rationale_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (rationale lib): ${needle}, or it survives only inside a comment"
}

# Code-binding, so it goes through the same code-portion matcher as require_code:
# every needle it carries asserts the codeowners live lib DOES something (its
# assert-edges domain, its labeled signature, its exact-set framing), and a
# comment quoting any of them must not stand in for the call.
require_codeowners_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_matches "${needle}" "${codeowners_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (codeowners lib), or it survives only inside a comment: ${needle}"
}

require_submodule_pin_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_matches "${needle}" "${submodule_pin_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (submodule-pin lib): ${needle}, or it survives only inside a comment"
}

# One helper for all three trio families (handles_route/runs_in/
# invokes_cloud_action, #5995/#6000/#5997): their drive/assert callbacks
# live in the SAME shared lib file, scripts/lib/ifa_symbol_runtime_live.sh,
# since all three share one cassette and one builder pass.
require_symbol_runtime_lib() {
	local label="$1" needle="$2"
	[[ "$(_ifa_det_count_code_matches "${needle}" "${symbol_runtime_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (symbol-runtime lib): ${needle}, or it survives only inside a comment"
}
