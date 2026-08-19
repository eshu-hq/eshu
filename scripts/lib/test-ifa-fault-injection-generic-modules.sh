#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2154
# Runner for the generic-mechanism hermetic case modules, extracted from
# scripts/test-verify-ifa-fault-injection.sh to keep that file under the 500-line
# cap (the same extraction the shard and marker cases already took).
#
# Adding the next mechanism costs one entry here rather than a source/call block
# in the parent. Each entry is "lib_var:runner"; the runner is checked with
# declare -F after sourcing, so a module that lands without its entry point --
# renamed, or never defined -- fails loudly here instead of contributing zero
# cases silently. That silent-skip shape is the defect class these modules exist
# to close, and it would be embarrassing to reintroduce it in their runner.
#
# shellcheck source=scripts/lib/test-ifa-fault-injection-generic-table-lock-cases.sh
# shellcheck source=scripts/lib/test-ifa-fault-injection-generic-shared-intent-lock-cases.sh
# shellcheck source=scripts/lib/test-ifa-fault-injection-generic-family-drive-cases.sh
run_ifa_fault_injection_generic_modules() {
	local module lib_var runner ran=0
	for module in \
		"table_lock_cases_lib:run_ifa_fault_injection_generic_table_lock_cases" \
		"shared_intent_lock_cases_lib:run_ifa_fault_injection_generic_shared_intent_lock_cases" \
		"family_drive_cases_lib:run_ifa_fault_injection_generic_family_drive_cases"; do
		lib_var="${module%%:*}"
		runner="${module##*:}"
		[[ -n "${!lib_var:-}" ]] || fail "generic case module list names ${lib_var}, which the parent does not define"
		source "${!lib_var}"
		declare -F "${runner}" >/dev/null \
			|| fail "${!lib_var##*/} does not define ${runner} -- a module that contributes no cases must fail, not skip"
		"${runner}"
		ran=$((ran + 1))
	done

	# STRAY-FILE GUARD. The list above is hand-typed, so a fourth
	# test-ifa-fault-injection-generic-*-cases.sh can land, get bash -n'd by the
	# mirror's derived syntax loop, and contribute ZERO cases while this runner
	# still reports "N generic mechanism case module(s) run". That is the exact
	# silent-skip shape this file's header says these modules exist to close, so
	# it must not be reachable through the runner itself. The sibling pins module
	# carries the same guard for the same reason.
	local on_disk expected=0 f
	for f in "${repo_root}"/scripts/lib/test-ifa-fault-injection-generic-*-cases.sh; do
		[[ -e "${f}" ]] || continue
		expected=$((expected + 1))
	done
	[[ "${expected}" -eq "${ran}" ]] \
		|| fail "generic case modules: ${expected} test-ifa-fault-injection-generic-*-cases.sh file(s) on disk but ${ran} registered in this runner's list -- a module that is not listed contributes no cases and is never missed"
	# A loop that ran zero modules would pass silently.
	[[ "${ran}" -gt 0 ]] || fail "generic case module runner executed no modules"
	printf 'test-verify-ifa-fault-injection: %d generic mechanism case module(s) run\n' "${ran}"
}
