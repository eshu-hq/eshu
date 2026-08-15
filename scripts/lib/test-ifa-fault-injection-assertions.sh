#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced test helper reads parent-owned paths.
# File-scoped assertion helpers for scripts/test-verify-ifa-fault-injection.sh.
# The parent verifier owns strict mode, fail(), and all target path variables.

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}
require_lib() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${fault_lib}" || fail "missing ${label} (lib): ${needle}"
}
require_driver() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${driver_lib}" || fail "missing ${label} (driver lib): ${needle}"
}
require_cells() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${cells_lib}" || fail "missing ${label} (cells lib): ${needle}"
}
require_sql_cells() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${sql_cells_lib}" || fail "missing ${label} (sql cells lib): ${needle}"
}
require_delivery_cells() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${delivery_cells_lib}" || fail "missing ${label} (delivery cells lib): ${needle}"
}
require_code_call_lib() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${code_call_lib}" || fail "missing ${label} (code-call lib): ${needle}"
}
require_code_call_cells() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${code_call_cells_lib}" || fail "missing ${label} (code-call cells lib): ${needle}"
}

# Deleting only the function name from a continued call leaves its arguments
# behind, so multiline call assertions use -U and bind the whole invocation.
require_delivery_cells_multiline() {
	local label="$1" needle="$2"
	rg -U --fixed-strings --quiet -- "${needle}" "${delivery_cells_lib}" || fail "missing ${label} (delivery cells lib, multiline): ${needle}"
}
