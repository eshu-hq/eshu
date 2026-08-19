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
# _ifa_count_code_matches counts lines of ${2} containing ${1} whose first
# non-whitespace character is NOT `#`. Every generic pin below routes through it
# because the plain `rg --fixed-strings` form is defeatable by a COMMENT: a
# reviewer proved that commenting out both real call sites (`# was: <call>`)
# left the count at 2 and the mirror green with ZERO real invocations, and that
# replacing the baseline cell's assertion with `# NOTE: this used to call ...`
# also passed. A pin satisfiable by prose about the code is not a pin on the
# code. require_line above already carries this lesson for ${script} -- it was in
# this same file when these helpers were written, and they did not use it.
_ifa_count_code_matches() {
	local needle="$1" file="$2" n=0 line stripped
	while IFS= read -r line || [[ -n "${line}" ]]; do
		stripped="${line#"${line%%[![:space:]]*}"}"
		[[ "${stripped}" == "#"* ]] && continue
		[[ "${line}" == *"${needle}"* ]] && n=$((n + 1))
	done < "${file}"
	printf '%s\n' "${n}"
}
require_generic_cells() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${generic_cells_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (generic cells lib), or it survives only inside a comment: ${needle}"
}
# require_generic_cells_count pins a needle that must appear an EXACT number of
# times, not merely at least once. `rg --quiet` proves >=1 and stops looking, so
# a needle occurring at N distinct call sites is satisfied by any one of them
# surviving -- deleting the other N-1 leaves the mirror green. That is not
# hypothetical: the assert-edges invocation appears at BOTH the kill-worker body
# and the fail-graph-write cell, and deleting only the fail-graph-write one left
# this mirror at exit 0 while cell_failgraphwrite_code_calls and
# cell_failgraphwrite_rationale silently stopped asserting their family's exact
# edge set. The expected count is hand-written by the caller and deliberately NOT
# derived from the file under test -- a count read out of the artifact it checks
# proves only that the artifact equals itself.
require_generic_cells_count() {
	local label="$1" needle="$2" want="$3" got
	got="$(_ifa_count_code_matches "${needle}" "${generic_cells_lib}")"
	[[ "${got}" == "${want}" ]] \
		|| fail "${label}: expected ${want} call site(s) of this invocation in ${generic_cells_lib##*/}, found ${got} -- a deleted call site is a proof that silently stopped running: ${needle}"
}
# require_generic_baseline pins the contents of the generic BASELINE cell, which
# lives in its own file since the 500-line split. Without this, that file is
# pinned by nothing at all: require_generic_cells reads only generic_cells_lib,
# so the split moved the baseline cell's assertions somewhere no pin could see
# them. Proven at the time: deleting the baseline cell's own _ifa_generic_assert_edges
# call left this mirror at exit 0, silently permitting the very thing that cell's
# comment says it exists to prevent -- a fixture that materializes nothing
# establishing an empty baseline every recovery cell then matches.
require_generic_baseline() {
	local label="$1" needle="$2"
	[[ "$(_ifa_count_code_matches "${needle}" "${generic_baseline_lib}")" -ge 1 ]] \
		|| fail "missing ${label} (generic baseline cell), or it survives only inside a comment: ${needle}"
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
