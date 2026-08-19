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
# _ifa_count_code_matches counts lines of ${2} where ${1} appears in the CODE
# portion -- the part before any `#`. Lines whose first non-whitespace character
# is `#` are skipped outright.
#
# Both halves are load-bearing, and each was defeated in review before it was
# added. Skipping only leading-`#` lines left `true  # was: <needle>` counting as
# a live call, so commenting out every call site that way passed with ZERO real
# invocations. Truncating at the first `#` closes that, and also stops a needle
# quoted inside a trailing comment, a here-doc line, or a disabled continuation
# from standing in for the code it describes.
#
# Direction of the residual imprecision is deliberate: a `#` inside a quoted
# string truncates the code portion early, so a needle after it reads as absent
# and the pin REDS. That is a false alarm a human resolves in seconds, never a
# false pass. Counting lines rather than matches is the same trade -- two needles
# on one line read 1, which can only over-report absence.
#
# require_line above pins whole lines for the same reason, and its docstring is
# this lesson from an earlier incident. It was in this file when the weaker form
# was written here; read the siblings before adding a pin helper.
# assert_no_private_data scans the gate plus EVERY *_lib it declares for
# hostnames, IPs, cloud account IDs, keys and internal paths.
#
# The file list is DERIVED from the declarations, not hand-typed. The hand-typed
# version listed 36 of 42 *_lib vars; the six it missed included this file, and
# it silently lost the library the 500-line split created until a reviewer
# caught it. A hand-typed list does not grow when the tree does.
#
# Lives here rather than in the mirror because that file sits against the
# 500-line cap and this is a whole coherent unit -- the same reason, and the
# same shape, as the other extractions this branch made.
assert_no_private_data() {
	local private_pattern f
	# Every alternative below brackets one character, so the pattern does not
	# contain its own literals. A bracketed single-character class matches exactly
	# the same text as the bare character, so detection is unchanged.
	#
	# This is required now that the scan covers every declared *_lib, including
	# this file. Spelling any of these tokens plainly in this function -- even in
	# prose explaining them -- makes the scanner flag itself on every run. That
	# happened twice while writing this comment, which is why it now describes the
	# tokens rather than quoting them.
	#
	# The hazard was latent before, not absent: the literal used to live in the
	# mirror, whose ${script} points at the GATE, so the mirror never scanned
	# itself and never noticed.
	private_pattern='gh[p]_|github_pa[t]_|glpa[t]-|AKI[A]|ASI[A]|xo[x][baprs]-|arn:aw[s]:|(^|[^0-9])[0-9]{12}([^0-9]|$)|/[U]sers/|/[h]ome/[a-z]'
	for f in "$@" $(compgen -v | rg '_lib$' | sort | while IFS= read -r v; do printf '%s\n' "${!v}"; done); do
		[[ -f "${f}" ]] || fail "assert_no_private_data: ${f} does not exist -- a scan that skips a missing file proves nothing"
		if rg --pcre2 --quiet -- "${private_pattern}" "${f}"; then
			fail "$(basename "${f}") looks like it contains private data"
		fi
	done
}
_ifa_count_code_matches() {
	local needle="$1" file="$2" n=0 line stripped code
	while IFS= read -r line || [[ -n "${line}" ]]; do
		stripped="${line#"${line%%[![:space:]]*}"}"
		[[ "${stripped}" == "#"* ]] && continue
		code="${line%%#*}"
		[[ "${code}" == *"${needle}"* ]] && n=$((n + 1))
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
