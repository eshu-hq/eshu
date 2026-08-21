#!/usr/bin/env bash
# shellcheck shell=bash
# Independent hand-derived pins for scripts/lib/ifa_family_registry.sh's
# per-family blocker_kind/wait_stage/wait_key/anchor/shared_cell/cell_kind
# declarations (#6147 PR-0 family registry). This module is the INDEPENDENT
# CHECKER: it exists to catch a registry declaration that is wrong, not to
# restate whatever the registry already says.
#
# `anchor` gets the same scrutiny as `blocker_kind`, because it fails the
# same silent way: an anchor that does not match the family's real MERGE
# means the scripted graph-write fault never fires, the cell drains cleanly
# against edges the first pass already wrote, and the run reports green
# having tested nothing -- the same failure shape as a lock aimed at a table
# the handler never touches, just at the fail-graph-write cell instead of
# the kill-worker cell.
#
# THIS FILE IS A THIN ORCHESTRATOR. The data and its derivation citations
# live in scripts/lib/ifa_family_registry_pins/<family>.sh, one file per
# family, so this module does not re-grow the same 500-line wall the
# registry itself hit (six families already put a single-file version of
# this module at ~490 lines; fourteen families would have blown well past
# the cap mid-program). A family's six pinned values (IFA_FAMILY_PIN_BLOCKER_KIND,
# _WAIT_STAGE, _WAIT_KEY, _ANCHOR, _SHARED_CELL, _CELL_KIND) sit directly
# beside the derivation comment that justifies them in that family's own
# file -- splitting by FIELD instead (one file per blocker_kind/anchor/etc.
# holding every family's value) was rejected because it would scatter one
# family's reasoning across up to six files: the "no IntentWriter" finding
# alone justifies blocker_kind AND informs cell_kind for
# documentation_edges and codeowners_ownership_edges, and a field-split
# would either duplicate that citation or fragment it. Keeping every
# citation adjacent to every value it justifies is the property this split
# optimizes for, because it is the entire reason a pin is trustworthy.
#
# HARD RULE #1, FOR EVERY MAINTAINER, INCLUDING FUTURE ONES WHO WANT TO
# "SIMPLIFY" THIS FILE OR ITS PER-FAMILY SIBLINGS: every IFA_FAMILY_PIN_*
# value in ifa_family_registry_pins/<family>.sh is HAND-TYPED literal text,
# derived by reading the reducer/Cypher/fault-cell source directly -- see
# the file:line citations inline. It is NEVER sourced, generated, or read
# back out of ifa_family_registry.sh. A check that copies its expected
# values from the artifact it is checking can never fail, no matter what
# the artifact says -- see
# scripts/lib/ifa_fault_injection_deployable_unit_lock.sh:45-98 for the
# recorded history of exactly that class of mistake in this suite: a fault
# cell whose lock target was wrong (shared_projection_intents, a table its
# handler never writes) "proved" ordinary crash recovery while its own name
# claimed a domain-scoped reclaim. The split into per-family files does not
# relax this rule; it only relocates where the literals live.
#
# HARD RULE #2: sourcing below is driven by the hand-written
# IFA_FAMILY_PINS_NAMES list, NEVER by a directory glob over
# ifa_family_registry_pins/. This is not a style preference -- the
# registry's own row split uses `for f in rows/*.sh` sourcing, and glob-driven
# sourcing has a real failure mode: if the glob ever matches nothing
# (directory renamed, wrong repo_root, an unexpanded pattern), it silently
# iterates zero times and every gate that depends on it passes having driven
# nothing. The registry had to bolt an explicit non-zero-count assertion
# onto its loader to close that gap. This module cannot have that failure
# mode by construction: IFA_FAMILY_PINS_NAMES is a literal array a
# filesystem accident cannot empty, a family named in it with no matching
# file is a loud, per-family sourcing failure (not a silent zero-iteration
# loop), and the totality diff below compares the REGISTRY's family list
# against this hand-written list, never against a directory listing -- so a
# stray extra file in ifa_family_registry_pins/ with no entry in the list
# still gets caught (checked explicitly, see the stray-file scan below), and
# an empty list is not something that can happen by accident. Do NOT
# "simplify" this into a glob; that trades this structural safety for the
# exact failure mode the registry needed a guard to close.

# shellcheck disable=SC2034  # consumed by the sourcing loop in run_ifa_family_registry_pins_cases below
IFA_FAMILY_PINS_NAMES=(
	sql_relationships
	code_calls
	rationale_edges
	documentation_edges
	deployable_unit_edges
	codeowners_ownership_edges
	repo_dependency
)

ifa_family_registry_pins_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/ifa_family_registry_pins"

# ============================================================================
# COMPARISON AGAINST THE REGISTRY
# ============================================================================
#
# scripts/lib/ifa_family_registry.sh's real accessor API (confirmed by
# reading that file directly, not assumed): ifa_family_registry_names prints
# the ordered family list one per line; ifa_family_blocker_kind /
# ifa_family_wait_stage / ifa_family_wait_key / ifa_family_anchor /
# ifa_family_shared_cell / ifa_family_cell_kind each take one family-name
# argument, print that field's row value on success, and return nonzero with
# no stdout for a family that is not a row in the registry
# (_ifa_family_registry_get). Isolated in ifa_family_registry_pins_lookup_*
# so a future accessor rename is a one-function edit here rather than a
# rewrite of the derivation or the comparison/totality logic below.
ifa_family_registry_pins_lookup_families() {
	ifa_family_registry_names
}

ifa_family_registry_pins_lookup_blocker_kind() {
	ifa_family_blocker_kind "$1"
}

ifa_family_registry_pins_lookup_wait_stage() {
	ifa_family_wait_stage "$1"
}

ifa_family_registry_pins_lookup_wait_key() {
	ifa_family_wait_key "$1"
}

ifa_family_registry_pins_lookup_anchor() {
	ifa_family_anchor "$1"
}

ifa_family_registry_pins_lookup_shared_cell() {
	ifa_family_shared_cell "$1"
}

ifa_family_registry_pins_lookup_cell_kind() {
	ifa_family_cell_kind "$1"
}

# _ifa_family_registry_pins_load_family sources one family's pin file after
# clearing the six IFA_FAMILY_PIN_* scalars, so a half-written file (one
# that forgets a field -- the likeliest mistake when someone adds family
# seven in a hurry) cannot silently inherit the PREVIOUS family's leftover
# value in this same loop iteration. Fails loudly, naming the family and
# (for a missing field) the exact field name, rather than comparing a stale
# value against the registry and reporting a confusing mismatch.
_ifa_family_registry_pins_load_family() {
	local family="$1"
	local pin_file="${ifa_family_registry_pins_dir}/${family}.sh"
	unset IFA_FAMILY_PIN_BLOCKER_KIND IFA_FAMILY_PIN_WAIT_STAGE IFA_FAMILY_PIN_WAIT_KEY \
		IFA_FAMILY_PIN_ANCHOR IFA_FAMILY_PIN_SHARED_CELL IFA_FAMILY_PIN_CELL_KIND
	[[ -f "${pin_file}" ]] \
		|| fail "family registry pins: IFA_FAMILY_PINS_NAMES lists family '${family}' but ${pin_file} does not exist"
	# shellcheck source=/dev/null
	source "${pin_file}"
	local field
	for field in IFA_FAMILY_PIN_BLOCKER_KIND IFA_FAMILY_PIN_WAIT_STAGE IFA_FAMILY_PIN_WAIT_KEY \
		IFA_FAMILY_PIN_ANCHOR IFA_FAMILY_PIN_SHARED_CELL IFA_FAMILY_PIN_CELL_KIND; do
		[[ -n "${!field+set}" ]] \
			|| fail "family registry pins: ${pin_file} does not set ${field} -- half-written per-family pin file for '${family}'"
	done
}

# run_ifa_family_registry_pins_cases is the sourced entry point. It requires
# `fail()` and `registry_family_lib` (the path to ifa_family_registry.sh) to
# already be defined by the caller, the same convention every sibling
# *-cases.sh module in this directory uses.
run_ifa_family_registry_pins_cases() {
	[[ -n "${registry_family_lib:-}" ]] \
		|| fail "run_ifa_family_registry_pins_cases: caller did not set \${registry_family_lib}"
	if [[ ! -f "${registry_family_lib}" ]]; then
		fail "\${registry_family_lib} (${registry_family_lib}) does not exist -- the caller must point it at scripts/lib/ifa_family_registry.sh before sourcing this module"
	fi
	# shellcheck source=/dev/null
	source "${registry_family_lib}"
	local accessor
	for accessor in ifa_family_registry_names ifa_family_blocker_kind ifa_family_wait_stage \
		ifa_family_wait_key ifa_family_anchor ifa_family_shared_cell ifa_family_cell_kind; do
		declare -F "${accessor}" >/dev/null \
			|| fail "${registry_family_lib} does not define ${accessor} -- its accessor API changed; update the matching ifa_family_registry_pins_lookup_* wrapper in scripts/lib/test-ifa-family-registry-derived-pins-cases.sh"
	done

	local family value
	declare -A ifa_family_pins_names_set=()
	for family in "${IFA_FAMILY_PINS_NAMES[@]}"; do
		ifa_family_pins_names_set["${family}"]=1
	done

	# Stray-file guard: every *.sh under ifa_family_registry_pins/ must have
	# an entry in IFA_FAMILY_PINS_NAMES. This is a DRIFT DETECTOR only, run
	# once here -- it never drives sourcing (that stays list-driven, per HARD
	# RULE #2 above); it exists solely to catch a file added without also
	# adding its name to the list.
	shopt -s nullglob
	local pin_files=("${ifa_family_registry_pins_dir}"/*.sh)
	shopt -u nullglob
	local pin_file stray_family
	for pin_file in "${pin_files[@]}"; do
		stray_family="$(basename "${pin_file}" .sh)"
		[[ -n "${ifa_family_pins_names_set[${stray_family}]+set}" ]] \
			|| fail "family registry pins: ${pin_file} exists but '${stray_family}' has no entry in IFA_FAMILY_PINS_NAMES (scripts/lib/test-ifa-family-registry-derived-pins-cases.sh) -- add it to the list or delete the stray file"
	done

	# Direction 1: every family THIS module pins must be declared by the
	# registry, with values matching the hand-derived expectation loaded from
	# its own per-family file.
	for family in "${IFA_FAMILY_PINS_NAMES[@]}"; do
		_ifa_family_registry_pins_load_family "${family}"

		value="$(ifa_family_registry_pins_lookup_blocker_kind "${family}")" \
			|| fail "family registry totality: pinned family '${family}' is not declared by ${registry_family_lib} (or its blocker_kind accessor failed)"
		[[ "${value}" == "${IFA_FAMILY_PIN_BLOCKER_KIND}" ]] \
			|| fail "family registry blocker_kind mismatch for '${family}': registry declares '${value}', hand-derived expectation is '${IFA_FAMILY_PIN_BLOCKER_KIND}' (see scripts/lib/ifa_family_registry_pins/${family}.sh)"

		value="$(ifa_family_registry_pins_lookup_wait_stage "${family}")" \
			|| fail "family registry totality: pinned family '${family}' is not declared by ${registry_family_lib} (or its wait_stage accessor failed)"
		[[ "${value}" == "${IFA_FAMILY_PIN_WAIT_STAGE}" ]] \
			|| fail "family registry wait_stage mismatch for '${family}': registry declares '${value}', hand-derived expectation is '${IFA_FAMILY_PIN_WAIT_STAGE}' (see scripts/lib/ifa_family_registry_pins/${family}.sh)"

		value="$(ifa_family_registry_pins_lookup_wait_key "${family}")" \
			|| fail "family registry totality: pinned family '${family}' is not declared by ${registry_family_lib} (or its wait_key accessor failed)"
		[[ "${value}" == "${IFA_FAMILY_PIN_WAIT_KEY}" ]] \
			|| fail "family registry wait_key mismatch for '${family}': registry declares '${value}', hand-derived expectation is '${IFA_FAMILY_PIN_WAIT_KEY}' (see scripts/lib/ifa_family_registry_pins/${family}.sh)"

		value="$(ifa_family_registry_pins_lookup_anchor "${family}")" \
			|| fail "family registry totality: pinned family '${family}' is not declared by ${registry_family_lib} (or its anchor accessor failed)"
		[[ "${value}" == "${IFA_FAMILY_PIN_ANCHOR}" ]] \
			|| fail "family registry anchor mismatch for '${family}': registry declares '${value}', hand-derived expectation is '${IFA_FAMILY_PIN_ANCHOR}' (see scripts/lib/ifa_family_registry_pins/${family}.sh) -- an anchor mismatch means the fail-graph-write fault never fires and the cell reports green having tested nothing"

		value="$(ifa_family_registry_pins_lookup_shared_cell "${family}")" \
			|| fail "family registry totality: pinned family '${family}' is not declared by ${registry_family_lib} (or its shared_cell accessor failed)"
		[[ "${value}" == "${IFA_FAMILY_PIN_SHARED_CELL}" ]] \
			|| fail "family registry shared_cell mismatch for '${family}': registry declares '${value}', hand-derived expectation is '${IFA_FAMILY_PIN_SHARED_CELL}' (see scripts/lib/ifa_family_registry_pins/${family}.sh)"

		value="$(ifa_family_registry_pins_lookup_cell_kind "${family}")" \
			|| fail "family registry totality: pinned family '${family}' is not declared by ${registry_family_lib} (or its cell_kind accessor failed)"
		[[ "${value}" == "${IFA_FAMILY_PIN_CELL_KIND}" ]] \
			|| fail "family registry cell_kind mismatch for '${family}': registry declares '${value}', hand-derived expectation is '${IFA_FAMILY_PIN_CELL_KIND}' (see scripts/lib/ifa_family_registry_pins/${family}.sh)"
	done

	# Direction 2: every family the registry declares must have a pin here,
	# AND every family this module pins must still appear in the registry's
	# own enumeration. Both built from ONE pass over
	# ifa_family_registry_pins_lookup_families (the registry's live
	# enumeration), never a directory listing (HARD RULE #2). This is not
	# redundant with Direction 1's per-field accessor calls above: a family
	# dropped from the registry's NAME ENUMERATION while its per-field
	# accessor arrays still hold stale data for it would make every Direction
	# 1 accessor call above keep succeeding (and, worse, possibly keep
	# matching) -- only diffing against the live enumeration catches that a
	# family quietly fell out of the registry's family list itself.
	declare -A ifa_family_pins_seen_in_registry_enum=()
	while IFS= read -r family; do
		[[ -n "${family}" ]] || continue
		ifa_family_pins_seen_in_registry_enum["${family}"]=1
		[[ -n "${ifa_family_pins_names_set[${family}]+set}" ]] \
			|| fail "family registry totality: ${registry_family_lib} declares family '${family}' which has no entry in IFA_FAMILY_PINS_NAMES (scripts/lib/test-ifa-family-registry-derived-pins-cases.sh) -- add a hand-derived expectation file (read the handler, do not copy the registry's value)"
	done < <(ifa_family_registry_pins_lookup_families)

	for family in "${IFA_FAMILY_PINS_NAMES[@]}"; do
		[[ -n "${ifa_family_pins_seen_in_registry_enum[${family}]+set}" ]] \
			|| fail "family registry totality: this file pins family '${family}' which ${registry_family_lib}'s own family enumeration no longer declares -- remove the stale pin file + IFA_FAMILY_PINS_NAMES entry, or confirm the family was genuinely dropped"
	done

	printf 'family registry pins: %d families checked (blocker_kind, wait_stage, wait_key, anchor, shared_cell, cell_kind) against %s, totality confirmed both directions\n' \
		"${#IFA_FAMILY_PINS_NAMES[@]}" "${registry_family_lib}"
}
