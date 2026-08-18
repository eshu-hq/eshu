#!/usr/bin/env bash
# shellcheck disable=SC2154  # Sourced test helper reads parent-owned paths.
# File-scoped assertion helpers for scripts/test-verify-ifa-fault-injection.sh.
# The parent verifier owns strict mode, fail(), and all target path variables.

require() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}
# require_fixture asserts a needle that lives in the shared family-fixtures lib
# the gate sources, not in the gate script: the committed cassette and
# expected-set paths plus their fail-fast existence guards. Kept separate from
# require() so moving anything ELSE out of the gate script still fails.
# require_line pins a WHOLE line, so a needle that also appears inside a comment
# cannot satisfy it. Strict mode needs this: the gate script names
# `set -euo pipefail` in its bash>=4.4 header comment as well as running it, so
# the fixed-strings form still passed with the real line deleted.
require_line() {
	local label="$1" needle="$2"
	rg --line-regexp --quiet -- "${needle}" "${script}" || fail "missing ${label}: ${needle}"
}
require_fixture() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${fixtures_lib}" || fail "missing ${label} (fixtures lib): ${needle}"
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
require_documentation_lib() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${documentation_lib}" || fail "missing ${label} (documentation lib): ${needle}"
}
require_documentation_cells() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${documentation_cells_lib}" || fail "missing ${label} (documentation cells lib): ${needle}"
}
require_documentation_barrier() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${documentation_barrier_lib}" || fail "missing ${label} (documentation ACK barrier lib): ${needle}"
}
require_documentation_barrier_setup() {
	local label="$1" needle="$2"
	rg --fixed-strings --quiet -- "${needle}" "${documentation_barrier_setup_lib}" || fail "missing ${label} (documentation ACK barrier setup lib): ${needle}"
}

# Deleting only the function name from a continued call leaves its arguments
# behind, so multiline call assertions use -U and bind the whole invocation.
require_delivery_cells_multiline() {
	local label="$1" needle="$2"
	rg -U --fixed-strings --quiet -- "${needle}" "${delivery_cells_lib}" || fail "missing ${label} (delivery cells lib, multiline): ${needle}"
}
