#!/usr/bin/env bash
# shellcheck shell=bash
# shellcheck disable=SC2034,SC2317
# SC2034: every declare -A table below is populated by the row files under
# ifa_family_registry/rows/ (sourced later in this same file) and read only
# through _ifa_family_registry_get's `local -n arr="${array_name}"` nameref
# indirection; shellcheck cannot trace either hop, so it flags every table as
# unused. They are all written and all read -- through every row file and
# every ifa_family_* accessor below.
# SC2317: the idempotency guard's `return 0 2>/dev/null || exit 0` is meant
# to be partially unreachable -- `exit 0` only runs on the rare path where
# this file was executed directly rather than sourced, and `return` fails.
# One row per materialized-edge family exercised by the Ifá live gates
# (scripts/verify-ifa-determinism.sh and scripts/verify-ifa-fault-injection.sh).
#
# Before this file, adding a family cost ~4 hand-copied inline lines in EACH
# gate script -- one drive call and one assert call per script, times two
# scripts -- and both scripts are already at or over the repo's 500-line cap
# (scripts/test-verify-ifa-determinism.sh alone is mid-split for exactly this
# reason as of this writing). After this file, a new shared-cell family costs
# one row here plus its own `ifa_<family>_drive` / `ifa_<family>_assert`
# functions in its own lib file; nothing in either gate script's loop changes.
#
# ROWS LIVE ONE FILE PER FAMILY, not inline here: ifa_family_registry/rows/
# (this file's own sibling directory). This file used to hold every family's
# row inline and hit 415 lines for SIX families -- roughly 65 lines each,
# almost all citations and rationale that must not be thinned (they are how
# real defects, like codeowners' vacuous lock, get caught). Fourteen total
# families would have meant ~900+ lines in one file, blowing the cap
# mid-program. Splitting by FAMILY rather than by mechanism (contrast
# ifa_fault_generic_cells.sh's mechanism split) is deliberate: blocker_kind
# and cell_kind get corrected in place as the program learns things --
# codeowners_ownership_edges' blocker_kind was corrected in place
# from ack_barrier to table_lock:fact_records once its landed cell was
# re-read -- and a
# mechanism-grouped rows file would turn every such correction into a file
# MOVE. A per-family file never moves regardless of what any of its fields
# currently say. Filenames are ordinal-prefixed (01_, 02_, ... -- the same
# convention go/internal/storage/postgres/migrations/ already uses for
# "files that must load in a specific order") so IFA_FAMILY_NAMES' order
# stays exactly what it was before the split: the first four preserve
# scripts/verify-ifa-determinism.sh's pre-registry inline drive/assert order
# (sql, code_call, documentation, rationale) byte-for-byte -- that loop reads
# this order directly rather than restating it. A new family's row file gets the
# next unused prefix; nothing about an insertion at the end requires
# renumbering anything else.
#
# Sourcing contract: source this file directly; it declares no dependency on
# anything sourced before it. It does NOT declare drive/assert functions
# itself -- callers must source each family's own lib file (e.g.
# ifa_rationale_live.sh, ifa_code_call_live.sh) separately, exactly as they do
# today, before invoking a name this registry hands back.
#
# ----------------------------------------------------------------------------
# Schema (seven fields, verified against the live code -- not hand-guessed --
# for every row; see each row file's own comments for the file:line read):
#
#   family       e.g. rationale_edges. The materialized_edges:<domain> name.
#   blocker_kind one of: shared_intent_lock, runner_lease_hold,
#                table_lock:<tablename>, ack_barrier, none. What a
#                fault-injection kill/fail cell
#                must engage to hold the handler in flight long enough to
#                observe a genuinely blocked in-progress write.
#   wait_stage   handler | runner. Which queue the non-vacuity precondition
#                polls before the kill/fail fires: "handler" waits on
#                fact_work_items (the first-stage claim), "runner" waits on
#                shared_projection_intents (the second-stage shared-projection
#                claim) -- see ifa_family_wait_key below for which value goes
#                with which stage.
#   wait_key     for wait_stage=handler, the fact_work_items.domain value the
#                wait predicate scopes to. For wait_stage=runner, the
#                shared_projection_intents.projection_domain value instead.
#                Scoping matters: an unscoped wait latches whichever domain
#                the driven cassettes happen to schedule first, not this
#                family's own work (issue #5555's confirmed-false SQL
#                coverage claim).
#   shared_cell  1 if this family is driven into every shared N={1,2,4} cell of
#                the DETERMINISM gate, 0 if it runs in its own scoped cell(s)
#                there instead.
#
#                This says nothing about the FAULT gate. An earlier wording
#                defined it as "driven by drive_all_cassettes into every shared
#                N={1,2,4} cell", which names the fault gate's driver
#                (ifa_fault_injection_driver.sh) in the definition of a
#                determinism-gate property -- and it is false for
#                codeowners_ownership_edges, which is shared_cell=1 yet has
#                never been part of drive_all_cassettes. Applying that wording
#                literally registers 0 for a family that belongs in the N-loop,
#                after which it is never driven and never asserted there and
#                the gate still reports green. Fault-side drive membership is
#                NOT a registry field at all: it is decided by which cells call
#                drive_all_cassettes (scripts/lib/ifa_fault_injection_driver.sh),
#                and by repo convention that shared drive is not extended for a
#                new family -- a new family's own cells drive its cassette. Read
#                that from the driver's call sites, never from this field.
#   anchor       the MERGE operation-match string this family's graph write
#                targets; the once-fault decorator matches Cypher statement
#                text against this substring (see
#                ifa_fault_injection_common.sh's ifa_fault_write_once_script
#                doc comment for why a substring, not a fixed ordinal).
#   cell_kind    generic | custom | none. This records DISPATCH REALITY: does
#                the gate reach this family's fault cells through
#                cell_killworker_family / cell_failgraphwrite_family (generic),
#                by naming its own hand-written cell functions (custom), or does
#                the fault gate not reach this family at all (none)? It is NOT
#                "could the generic dispatcher express this family's blocker in
#                principle".
#
#                `none` is for a family registered for the DETERMINISM gate
#                alone: no cell function, no IFA_FAULT_ALL_CELLS entry, no
#                dispatch in scripts/verify-ifa-fault-injection.sh. Both direct
#                families (rows/12, rows/13) are that today. Recording custom
#                for one of them -- the first cut of rows/12 did -- asserts a
#                dispatch that does not exist and makes the family read as
#                covered while nothing drives it, which is the exact failure
#                #6181 was filed over. A family moving to custom or generic is
#                a change that lands WITH the cells, never ahead of them.
#
#                Those two readings disagree for real families, and the wrong
#                one is dangerous. sql_relationships' blocker_kind=none IS a
#                shape the dispatcher supports, but its cells are hand-written
#                and dispatched by name, so it is custom. Reading it as generic
#                invites someone to "finish the migration" by pointing its
#                dispatch at cell_killworker_family -- which routes it to the
#                no-blocker path: no lock, no precondition, and correctly no
#                retry-baseline proof. That is strictly weaker than the bespoke
#                cell it replaced, and the row would have read as sanctioning
#                it. deployable_unit_edges' table_lock:admission_decisions is
#                a supported shape too, and is custom for the same reason.
#
#                Verify from the gate's call sites, never by inferring from
#                blocker_kind.
#
# Two more field GROUPS exist below the seven above. They are deliberately
# NOT part of the seven-field schema a mirror-pin author checks against --
# they are execution-dispatch metadata the determinism-gate loop
# (scripts/verify-ifa-determinism.sh) and the generic fault cells
# (scripts/lib/ifa_fault_generic_cells.sh) need to actually CALL a family's
# functions, since those functions do not follow one uniform name or
# signature across every family (see each field's own comment for the
# irregularity it exists to absorb):
#
#   drive_fn / assert_fn         shared-cell families only (scripts/verify-
#                                 ifa-determinism.sh's N-loop).
#   cassette_var / expected_var  shared-cell families, plus any family whose
#                                 own cells need the paths (deployable_unit_edges
#                                 is shared_cell=0 and declares both); the *name* of the
#                                 global variable (sourced from
#                                 ifa_family_fixtures.sh) holding this
#                                 family's cassette / expected-edge-set path.
#   retry_baseline_var            shared_intent_lock families only; the *name*
#                                 of the shell variable holding this family's
#                                 fault-free retry count, which the generic
#                                 kill cell compares against. NOT optional for
#                                 that blocker kind:
#                                 _ifa_generic_require_retry_baseline
#                                 (scripts/lib/ifa_fault_generic_cells.sh)
#                                 dies naming the family and this row when it
#                                 is missing. Omitting it does not fail
#                                 statically -- it fails part-way into a live
#                                 four-shard CI run, which is the expensive
#                                 place to find out.
#   handler_go_file               consumed only by
#                                 _ifa_generic_require_intent_writer, so required
#                                 for shared_intent_lock families; other rows may
#                                 record it deliberately (rows/06 explains why it
#                                 keeps one). The Go
#                                 source path ifa_fault_generic_cells.sh's
#                                 mandatory precondition assert greps for an
#                                 IntentWriter before ever acquiring the lock.
# ----------------------------------------------------------------------------

# Idempotency guard MUST run before any array below is (re)declared. A
# second `source ifa_family_registry.sh` in the same shell (this file has
# two known callers today -- verify-ifa-determinism.sh directly, and
# ifa_fault_generic_cells.sh's own guard-source) must not re-run the
# row-loading block and duplicate every IFA_FAMILY_NAMES entry -- but
# checking that AFTER resetting the arrays to empty (the previous shape of
# this guard) would wipe an already-populated global array and then skip
# repopulating it, leaving it permanently empty. Checks that
# IFA_FAMILY_NAMES is both DECLARED and non-empty: proof data survived a
# prior source, not merely that this file's functions exist. Functions are
# always global regardless of scoping bugs elsewhere (see the -g note
# below), so `declare -F ifa_family_registry_names` -- the guard's original,
# WRONG shape -- could report true while the data arrays it was supposed to
# be guarding were function-local and long gone.
if declare -p IFA_FAMILY_NAMES &>/dev/null && [[ "${#IFA_FAMILY_NAMES[@]}" -gt 0 ]]; then
	return 0 2>/dev/null || exit 0
fi

# -g (global) is not optional here, on every single line: `declare -A x=()`
# WITHOUT -g, executed from inside a function's scope (this file's
# documented callers include being sourced from inside a bash function --
# e.g. scripts/lib/test-ifa-determinism-family-cases.sh's
# run_ifa_determinism_family_cases), creates a variable LOCAL to that
# function. It vanishes the instant that function returns, while every
# function THIS file defines (ifa_family_registry_names, every accessor)
# persists globally regardless of where it was defined -- functions are
# never scoped to their defining call frame. The result: every accessor
# survives, every array it reads does not, and every lookup reports
# "unknown family" for a family that plainly has a row. Confirmed live: the
# arrays were function-local and this exact failure mode broke
# scripts/test-verify-ifa-determinism.sh mid-session. IFA_FAMILY_NAMES uses
# -ga for the same reason and for consistency, even though a bare `X=()`
# assignment (no `declare` at all) is already implicitly global in bash --
# relying on that implicit rule, silently, right next to twelve lines that
# need the explicit flag to behave the same way, is exactly the kind of
# inconsistency that made this bug invisible on review.
declare -ga IFA_FAMILY_NAMES=()
declare -gA IFA_FAMILY_BLOCKER_KIND=()
declare -gA IFA_FAMILY_WAIT_STAGE=()
declare -gA IFA_FAMILY_WAIT_KEY=()
declare -gA IFA_FAMILY_SHARED_CELL=()
declare -gA IFA_FAMILY_ANCHOR=()
declare -gA IFA_FAMILY_CELL_KIND=()
declare -gA IFA_FAMILY_DRIVE_FN=()
declare -gA IFA_FAMILY_ASSERT_FN=()
declare -gA IFA_FAMILY_CASSETTE_VAR=()
declare -gA IFA_FAMILY_EXPECTED_VAR=()
declare -gA IFA_FAMILY_RETRY_BASELINE_VAR=()
# FAULT_SHARED_DRIVE answers the one question the generic fault cells cannot
# answer for themselves: is this family already produced by drive_all_cassettes
# (scripts/lib/ifa_fault_injection_driver.sh), or must the cell drive the
# family's own cassette through DRIVE_FN/CASSETTE_VAR?
#
# 1 means the shared drive already covers it, and driving again would double the
# family's fact_work_items rows and make the retry-above-baseline comparison
# ill-defined. 0 means the cell must drive it, and that the family needs its own
# fault-free baseline digest too -- the shared cell_baseline never drove its
# cassette, so comparing against digests[baseline] would fail by construction.
#
# This is NOT the same question as SHARED_CELL and the two must never be
# conflated: SHARED_CELL describes the DETERMINISM gate's shared N={1,2,4} cell,
# and the answers already diverge -- codeowners_ownership_edges is SHARED_CELL=1
# yet has never been part of drive_all_cassettes (see rows/06).
#
# No default, deliberately. A row that omits this makes _ifa_family_registry_get
# return 1, and callers MUST capture that rc separately rather than testing the
# value inline: `[[ "$(fn x)" == 1 ]]` cannot tell "the row says 0" from "the
# accessor failed", which is the conflation verify-ifa-determinism.sh already
# solves by capturing first. Forgetting the field is a hard stop, never a silent
# drive-skip and never a silent double-drive.
declare -gA IFA_FAMILY_FAULT_SHARED_DRIVE=()
declare -gA IFA_FAMILY_HANDLER_GO_FILE=()

# ----------------------------------------------------------------------------
# FAIL-CLOSED row loading. If ifa_family_registry/rows/*.sh matched nothing --
# directory missing, renamed, a bad relative path, a glob that does not
# expand -- every array above stays empty, ifa_family_registry_names yields
# zero families, both live gates' loops iterate zero times, and every gate
# passes having driven no families at all. Silent, total coverage loss that
# looks exactly like success -- the single most dangerous failure mode this
# file could ship, and one this exact refactor could introduce. Every check
# below is a hard `exit`, not a soft `return`: this file has no `die` of its
# own to call (it may be sourced before its caller defines one), and a
# guard this critical must stop the whole run, not just this source
# operation. Do not weaken any of these to a warning.
# ----------------------------------------------------------------------------
_ifa_family_registry_lib_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
_ifa_family_registry_rows_dir="${_ifa_family_registry_lib_dir}/ifa_family_registry/rows"

if [[ ! -d "${_ifa_family_registry_rows_dir}" ]]; then
	printf 'ifa_family_registry.sh: FATAL: rows directory missing: %s -- every gate that iterates ifa_family_registry_names would silently drive ZERO families and still report success; refusing to continue\n' \
		"${_ifa_family_registry_rows_dir}" >&2
	exit 1
fi

_ifa_family_registry_row_files=()
for _ifa_family_registry_row_file in "${_ifa_family_registry_rows_dir}"/*.sh; do
	# The portable "no nullglob" idiom: with zero matches the loop variable
	# holds the literal, unexpanded pattern, and -e on that literal is false
	# (short of a file literally named "*.sh" existing). Deliberately not
	# `shopt -s nullglob`, which would leak a shell-option change into
	# whichever script sources this file.
	[[ -e "${_ifa_family_registry_row_file}" ]] && _ifa_family_registry_row_files+=("${_ifa_family_registry_row_file}")
done

if [[ "${#_ifa_family_registry_row_files[@]}" -eq 0 ]]; then
	printf 'ifa_family_registry.sh: FATAL: %s/*.sh matched zero row files -- every gate that iterates ifa_family_registry_names would silently drive ZERO families and still report success; refusing to continue\n' \
		"${_ifa_family_registry_rows_dir}" >&2
	exit 1
fi

# Explicit LC_ALL=C sort: do not rely on locale-dependent glob expansion
# order. The 01_/02_/... ordinal prefixes make this sort exactly reproduce
# the intended load order regardless of locale.
mapfile -t _ifa_family_registry_row_files < <(printf '%s\n' "${_ifa_family_registry_row_files[@]}" | LC_ALL=C sort)

for _ifa_family_registry_row_file in "${_ifa_family_registry_row_files[@]}"; do
	# shellcheck disable=SC1090
	source "${_ifa_family_registry_row_file}"
done
unset _ifa_family_registry_row_file _ifa_family_registry_row_files _ifa_family_registry_rows_dir _ifa_family_registry_lib_dir

if [[ "${#IFA_FAMILY_NAMES[@]}" -eq 0 ]]; then
	printf 'ifa_family_registry.sh: FATAL: sourced every row file under ifa_family_registry/rows/ but IFA_FAMILY_NAMES is still empty (a row file that forgot its own IFA_FAMILY_NAMES+=() append?) -- refusing to continue with zero registered families\n' >&2
	exit 1
fi

# ifa_family_registry_names prints the ordered family list, one per line.
ifa_family_registry_names() {
	printf '%s\n' "${IFA_FAMILY_NAMES[@]}"
}

# _ifa_family_registry_get is the shared accessor body: array_name is the
# name of one of the declare -A tables above (read via a bash 4.3+ nameref,
# safe here since this repo's determinism gate already refuses to run under
# bash < 4.4), family is the row key, label is the caller's own name (used
# only in the error message so a failure names the accessor that raised it,
# not this internal helper).
_ifa_family_registry_get() {
	local array_name="$1" family="$2" label="$3"
	local -n arr="${array_name}"
	if [[ -z "${arr[${family}]+x}" ]]; then
		printf '%s: unknown family %q (not a row in ifa_family_registry/rows/)\n' "${label}" "${family}" >&2
		return 1
	fi
	printf '%s' "${arr[${family}]}"
}

ifa_family_blocker_kind()          { _ifa_family_registry_get IFA_FAMILY_BLOCKER_KIND          "$1" ifa_family_blocker_kind; }
ifa_family_wait_stage()            { _ifa_family_registry_get IFA_FAMILY_WAIT_STAGE            "$1" ifa_family_wait_stage; }
ifa_family_wait_key()              { _ifa_family_registry_get IFA_FAMILY_WAIT_KEY              "$1" ifa_family_wait_key; }
ifa_family_shared_cell()           { _ifa_family_registry_get IFA_FAMILY_SHARED_CELL           "$1" ifa_family_shared_cell; }
ifa_family_anchor()                { _ifa_family_registry_get IFA_FAMILY_ANCHOR                "$1" ifa_family_anchor; }
ifa_family_cell_kind()             { _ifa_family_registry_get IFA_FAMILY_CELL_KIND             "$1" ifa_family_cell_kind; }
ifa_family_drive_fn()              { _ifa_family_registry_get IFA_FAMILY_DRIVE_FN              "$1" ifa_family_drive_fn; }
ifa_family_assert_fn()             { _ifa_family_registry_get IFA_FAMILY_ASSERT_FN             "$1" ifa_family_assert_fn; }
ifa_family_cassette_var()          { _ifa_family_registry_get IFA_FAMILY_CASSETTE_VAR          "$1" ifa_family_cassette_var; }
ifa_family_expected_var()          { _ifa_family_registry_get IFA_FAMILY_EXPECTED_VAR          "$1" ifa_family_expected_var; }
ifa_family_handler_go_file()       { _ifa_family_registry_get IFA_FAMILY_HANDLER_GO_FILE       "$1" ifa_family_handler_go_file; }
ifa_family_retry_baseline_var()    { _ifa_family_registry_get IFA_FAMILY_RETRY_BASELINE_VAR    "$1" ifa_family_retry_baseline_var; }
ifa_family_fault_shared_drive()     { _ifa_family_registry_get IFA_FAMILY_FAULT_SHARED_DRIVE   "$1" ifa_family_fault_shared_drive; }

# ---------------------------------------------------------------------------
# determinism-loop dispatchers (scripts/verify-ifa-determinism.sh's N-loop).
#
# These absorb the one real call-shape irregularity across the four
# shared_cell families: sql_relationships' ifa_det_drive_sql_baseline /
# ifa_det_assert_sql_baseline (scripts/lib/ifa_sql_delta_live.sh) take (n,
# bin_dir, cassette[, log_dir]); code_calls/documentation/rationale's
# ifa_<family>_drive / ifa_<family>_assert all take (label, bin_dir,
# cassette, n, log_dir) / (label, bin_dir, expected) instead. Every future
# shared_cell family is expected to follow the majority (labeled) shape --
# sql_relationships is grandfathered, not a second precedent.
# ---------------------------------------------------------------------------

ifa_family_registry_drive() {
	local family="$1" n="$2" bin_dir="$3" log_dir="$4"
	local drive_fn cassette_var cassette
	drive_fn="$(ifa_family_drive_fn "${family}")" || return 1
	cassette_var="$(ifa_family_cassette_var "${family}")" || return 1
	if [[ -z "${drive_fn}" || -z "${cassette_var}" ]]; then
		printf 'ifa_family_registry_drive: %s has no drive_fn/cassette_var registered (not a determinism-loop family)\n' "${family}" >&2
		return 1
	fi
	cassette="${!cassette_var}"
	if [[ "${family}" == "sql_relationships" ]]; then
		"${drive_fn}" "${n}" "${bin_dir}" "${cassette}" "${log_dir}"
	else
		"${drive_fn}" "n${n}" "${bin_dir}" "${cassette}" "${n}" "${log_dir}"
	fi
}

ifa_family_registry_assert() {
	local family="$1" n="$2" bin_dir="$3"
	local assert_fn expected_var expected
	assert_fn="$(ifa_family_assert_fn "${family}")" || return 1
	expected_var="$(ifa_family_expected_var "${family}")" || return 1
	if [[ -z "${assert_fn}" || -z "${expected_var}" ]]; then
		printf 'ifa_family_registry_assert: %s has no assert_fn/expected_var registered (not a determinism-loop family)\n' "${family}" >&2
		return 1
	fi
	expected="${!expected_var}"
	if [[ "${family}" == "sql_relationships" ]]; then
		"${assert_fn}" "${n}" "${bin_dir}" "${expected}"
	else
		"${assert_fn}" "N=${n}" "${bin_dir}" "${expected}"
	fi
}
