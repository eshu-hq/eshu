#!/usr/bin/env bash
# Hermetic mirror for scripts/lib/ifa_fault_shard.sh, the fault-injection
# gate's cell-shard selector. No Docker, no compose, no Postgres, no binary
# builds -- most of this module only exercises
# `scripts/verify-ifa-fault-injection.sh --list-cells[ --shard k/n]`, which
# is required to be fully hermetic and to exit before the gate reaches any
# stack setup or build step (the dispatch-anchor checks below additionally
# read the gate script's source text directly, also hermetic). Sourced by
# scripts/test-verify-ifa-fault-injection.sh, which calls
# run_ifa_fault_injection_shard_cases as part of the normal mirror suite.
#
# THE LOAD-BEARING RULE OF THIS FILE: ifa_full_cell_list_literal below is a
# HAND-AUTHORED LITERAL list of every cell name, typed out by a human/agent
# reading scripts/verify-ifa-fault-injection.sh's dispatch block, NEVER
# generated from, sourced from, or derived from the gate script or
# scripts/lib/ifa_fault_shard.sh. A check whose expected side is enumerated
# from the artifact under test compares the artifact to itself and can never
# fail -- see scripts/test-verify-ifa-fault-injection.sh:177-186 for the
# sibling literal this mirrors, and its own comment on the same rule.
# FUTURE MAINTAINERS: do not "simplify" this by calling
# ifa_fault_shard_all_cells or by reading IFA_FAULT_ALL_CELLS /
# IFA_FAULT_ATOMIC_GROUPS to build this list -- including after
# IFA_FAULT_ALL_CELLS itself becomes registry-derived (see
# ifa_fault_shard.sh's header). If the gate's dispatch block changes, update
# this list BY HAND, by re-reading the dispatch block, so a silent drift
# between what the gate runs and what this test expects is caught instead of
# masked.
#
# The one constant this file DOES read from ifa_fault_shard.sh is
# IFA_FAULT_SHARD_DEFAULT_N -- that is not the self-referential trap above:
# it asserts the exact-cover property holds for whatever shard count the
# library declares as its default, not a value copied from the gate's cell
# set or partition table. The CI-wiring checks below read the same constant
# for the identical reason, and separately compare it against
# .github/workflows/ifa-determinism-gate.yml's own matrix.shard list --
# TWO independently authored artifacts, so that comparison is not
# self-referential either (see the checks themselves for why).

test_ifa_fault_shard_cases_fail() {
	printf 'test-ifa-fault-injection-shard-cases: %s\n' "$*" >&2
	exit 1
}

run_ifa_fault_injection_shard_cases() {
	local repo_root script shard_lib workflow
	repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	script="${repo_root}/scripts/verify-ifa-fault-injection.sh"
	shard_lib="${repo_root}/scripts/lib/ifa_fault_shard.sh"
	workflow="${repo_root}/.github/workflows/ifa-determinism-gate.yml"

	[[ -f "${script}" ]] || test_ifa_fault_shard_cases_fail "missing ${script}"
	[[ -f "${shard_lib}" ]] || test_ifa_fault_shard_cases_fail "missing ${shard_lib}"
	[[ -f "${workflow}" ]] || test_ifa_fault_shard_cases_fail "missing ${workflow}"
	bash -n "${script}" || test_ifa_fault_shard_cases_fail "verify-ifa-fault-injection.sh has a syntax error"
	bash -n "${shard_lib}" || test_ifa_fault_shard_cases_fail "ifa_fault_shard.sh has a syntax error"

	# shellcheck source=scripts/lib/ifa_fault_shard.sh
	source "${shard_lib}"
	local n="${IFA_FAULT_SHARD_DEFAULT_N}"

	# The full, hand-authored, ordered cell list -- see the file header.
	local -a ifa_full_cell_list_literal=(
		cell_baseline
		cell_killworker
		cell_expirelease
		cell_failgraphwrite
		cell_restartbackend
		cell_killworker_sql
		cell_killworker_code_calls
		cell_killworker_documentation
		cell_killworker_rationale
		cell_duplicatedelivery
		cell_deltaretract
		cell_failgraphwrite_sql
		cell_failgraphwrite_code_calls
		cell_failgraphwrite_documentation
		cell_failgraphwrite_rationale
		cell_baseline_deployable_unit
		cell_killworker_deployable_unit
		cell_failgraphwrite_deployable_unit
		cell_baseline_codeowners
		cell_killworker_codeowners
		cell_failgraphwrite_codeowners
	)

	local actual_full
	actual_full="$("${script}" --list-cells)" \
		|| test_ifa_fault_shard_cases_fail "--list-cells exited non-zero"

	local expected_full
	expected_full="$(printf '%s\n' "${ifa_full_cell_list_literal[@]}")"
	[[ "${actual_full}" == "${expected_full}" ]] \
		|| test_ifa_fault_shard_cases_fail "--list-cells output does not match the hand-authored literal list (see file header) -- actual:
${actual_full}"

	# DISPATCH-ANCHOR CHECKS (moved here from scripts/test-verify-ifa-fault-injection.sh
	# to give that file real line-count headroom -- extraction, not deletion;
	# every comment below is the original, unabridged rationale).
	#
	# The eighteen-cell shape: baseline plus seventeen cells with a live seam --
	# four original recovery cells, two SQL-targeted (#5555), two delivery-shaped
	# (#5544), two code-call-targeted (#5991), two documentation-targeted (#5994),
	# two rationale-targeted (#5998), and a family-scoped baseline plus two
	# recovery cells for deployable_unit_edges (#5993). All eighteen run by
	# default.
	# Every cell is anchored to its own invocation line, never matched by bare name.
	# A bare-name needle is satisfied by prose and by longer siblings: "cell_baseline"
	# matches this file's own comments AND cell_baseline_deployable_unit, so deleting
	# the cell_baseline dispatch line left the mirror green -- and cell_baseline is
	# the sole writer of digests[baseline], so all sixteen assert_matches_baseline
	# calls would then compare against an unset key. The anchored form was
	# previously applied to only five of the eighteen; it now covers all of them.
	# rg without --fixed-strings so ^...$ binds.
	#
	# Prefixed with "ifa_fault_shard_run " (scripts/lib/ifa_fault_shard.sh): every
	# dispatch line now routes through that wrapper so --shard can skip a cell
	# not assigned to the requested shard, and cell_baseline runs first in every
	# shard regardless. The end-anchor ("${cell}\$") is unchanged and still does
	# the actual collision-avoidance work described above -- only the start of
	# the needle moved, not the guarantee.
	#
	# STRICTLY STRONGER than before, not merely updated: this also catches a
	# cell dispatched WITHOUT the wrapper (bare "cell_X" instead of
	# "ifa_fault_shard_run cell_X"). An unwrapped cell silently ignores --shard
	# and runs in EVERY shard -- a real defect with no other detector, since the
	# gate would still pass everywhere, just doing 4x the work for that cell
	# while quietly diverging from the partition this mirror claims to prove.
	# Do not simplify the prefix back out of the needle.
	for cell in \
		cell_baseline cell_killworker cell_expirelease cell_failgraphwrite cell_restartbackend \
		cell_killworker_sql cell_killworker_code_calls cell_killworker_documentation \
		cell_killworker_rationale cell_duplicatedelivery cell_deltaretract \
		cell_failgraphwrite_sql cell_failgraphwrite_code_calls \
		cell_failgraphwrite_documentation cell_failgraphwrite_rationale \
		cell_baseline_deployable_unit cell_killworker_deployable_unit \
		cell_failgraphwrite_deployable_unit; do
		rg --quiet -- "^ifa_fault_shard_run ${cell}\$" "${script}" \
			|| test_ifa_fault_shard_cases_fail "verifier does not invoke ${cell} via ifa_fault_shard_run on its own line -- missing entirely, or dispatched WITHOUT the wrapper (which would silently run every shard, ignoring --shard)"
	done

	# The SQL cell is a permanent matrix member, not a temporary experiment
	# (#5974 -- it spent months incorrectly held out on a wrong root cause;
	# see scripts/verify-ifa-fault-injection.sh's own header for the full
	# history). Pin both the invocation and the reason, so re-holding it out
	# is a deliberate edit to a stated rule rather than a quiet deletion on a
	# red run. Prefixed and strictly stronger for the same reason as the
	# anchored loop above.
	rg --quiet --line-regexp -- 'ifa_fault_shard_run cell_failgraphwrite_sql' "${script}" \
		|| test_ifa_fault_shard_cases_fail "cell_failgraphwrite_sql is no longer invoked via ifa_fault_shard_run in the default matrix -- missing entirely, held out (#5974), or dispatched WITHOUT the wrapper"

	# GENERIC DISPATCHER DELEGATION (code_calls and rationale_edges, the two
	# families scripts/lib/ifa_fault_generic_cells.sh's WIRING header names as
	# actually swapped onto it). Anchored multiline so each needle proves the
	# real DELEGATION -- declaration plus body -- not merely that the wrapper
	# function still exists. A bare substring needle like
	# "cell_killworker_family code_calls" is satisfied by prose anywhere in
	# the file (this module's own header comments say "rationale_edges"
	# repeatedly) and would keep passing even if the delegation were deleted
	# entirely -- the identical defect class the eighteen-cell dispatch-anchor
	# loop above already documents. Do not weaken these back to bare-word
	# checks.
	local code_call_cells_lib rationale_cells_lib
	code_call_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_code_call_cells.sh"
	rationale_cells_lib="${repo_root}/scripts/lib/ifa_fault_injection_rationale_cells.sh"
	rg -U --fixed-strings --quiet -- $'cell_killworker_code_calls() {\n\tcell_killworker_family code_calls\n}' "${code_call_cells_lib}" \
		|| test_ifa_fault_shard_cases_fail "code-call kill cell did not delegate to the generic shared-intent-lock dispatcher"
	rg -U --fixed-strings --quiet -- $'cell_failgraphwrite_code_calls() {\n\tcell_failgraphwrite_family code_calls\n}' "${code_call_cells_lib}" \
		|| test_ifa_fault_shard_cases_fail "code-call graph-fault cell did not delegate to the generic fresh-domain-guard dispatcher"
	rg -U --fixed-strings --quiet -- $'cell_killworker_rationale() {\n\tcell_killworker_family rationale_edges\n}' "${rationale_cells_lib}" \
		|| test_ifa_fault_shard_cases_fail "rationale kill cell did not delegate to the generic shared-intent-lock dispatcher"
	rg -U --fixed-strings --quiet -- $'cell_failgraphwrite_rationale() {\n\tcell_failgraphwrite_family rationale_edges\n}' "${rationale_cells_lib}" \
		|| test_ifa_fault_shard_cases_fail "rationale graph-fault cell did not delegate to the generic fresh-domain-guard dispatcher"

	# The old bespoke cells' wait_key and retry-baseline-variable literals
	# (e.g. "rationale_materialization", "baseline_code_call_retried") moved
	# one level further than the dispatcher: they are now registry DATA, not
	# dispatcher code, in scripts/lib/ifa_family_registry/rows/. The
	# dispatcher reads them generically (ifa_family_wait_key /
	# ifa_family_retry_baseline_var); only the registry row still says which
	# literal a given family binds to.
	local code_call_registry_row rationale_registry_row
	code_call_registry_row="${repo_root}/scripts/lib/ifa_family_registry/rows/02_code_calls.sh"
	rationale_registry_row="${repo_root}/scripts/lib/ifa_family_registry/rows/04_rationale_edges.sh"
	[[ -f "${code_call_registry_row}" ]] || test_ifa_fault_shard_cases_fail "missing ${code_call_registry_row}"
	[[ -f "${rationale_registry_row}" ]] || test_ifa_fault_shard_cases_fail "missing ${rationale_registry_row}"
	rg --fixed-strings --quiet -- 'IFA_FAMILY_WAIT_KEY[code_calls]="code_call_materialization"' "${code_call_registry_row}" \
		|| test_ifa_fault_shard_cases_fail "code_calls registry row does not bind wait_key to code_call_materialization"
	rg --fixed-strings --quiet -- 'IFA_FAMILY_RETRY_BASELINE_VAR[code_calls]="baseline_code_call_retried"' "${code_call_registry_row}" \
		|| test_ifa_fault_shard_cases_fail "code_calls registry row does not bind its retry-baseline variable to baseline_code_call_retried"
	rg --fixed-strings --quiet -- 'IFA_FAMILY_WAIT_KEY[rationale_edges]="rationale_materialization"' "${rationale_registry_row}" \
		|| test_ifa_fault_shard_cases_fail "rationale_edges registry row does not bind wait_key to rationale_materialization"
	rg --fixed-strings --quiet -- 'IFA_FAMILY_RETRY_BASELINE_VAR[rationale_edges]="baseline_rationale_retried"' "${rationale_registry_row}" \
		|| test_ifa_fault_shard_cases_fail "rationale_edges registry row does not bind its retry-baseline variable to baseline_rationale_retried"

	# CI WIRING INTENT (migrated from a pin the workflow's shard rollout
	# broke): before sharding, the fault-injection job ran as one step whose
	# label named "18 cells" -- a hand-written count -- and a pin here
	# asserted that literal text so the workflow could not silently stop
	# covering the full cell set. That step is gone now that the job is
	# sharded across matrix.shard, and re-pinning a NEW label string would
	# just repeat the same brittleness (label text is free to change; what
	# must not silently change is the WIRING). So this proves the same
	# intent behaviorally instead of textually: each shard job's
	# IFA_FAULT_SHARD env var must come from matrix.shard (never a hardcoded
	# value, which would make every job run the SAME shard and silently
	# drop every other shard's cells from CI), and it must actually reach
	# the gate's --shard flag with the SAME denominator this library
	# declares as its default.
	rg --fixed-strings --quiet -- 'IFA_FAULT_SHARD: ${{ matrix.shard }}' "${workflow}" \
		|| test_ifa_fault_shard_cases_fail "workflow does not template IFA_FAULT_SHARD from matrix.shard -- every shard job could silently run the same shard, dropping the others from CI"
	rg --fixed-strings --quiet -- "--shard \"\${IFA_FAULT_SHARD}/${n}\"" "${workflow}" \
		|| test_ifa_fault_shard_cases_fail "workflow does not pass --shard \"\${IFA_FAULT_SHARD}/${n}\" (n=IFA_FAULT_SHARD_DEFAULT_N) to the gate"

	# MATRIX CARDINALITY: the workflow's strategy.matrix.shard list and this
	# library's IFA_FAULT_SHARD_DEFAULT_N are two INDEPENDENTLY authored
	# artifacts -- a YAML list a human edits in the workflow vs. a bash
	# constant a human edits in the library -- so comparing them is not the
	# self-referential trap the hand-authored-literal rule above guards
	# against: either one can be wrong on its own, and only a genuine
	# cross-check catches that. Deleting a shard from the matrix, or
	# changing the constant without touching the matrix, must go RED --
	# otherwise a shard's cells silently stop running in every CI run while
	# the gate still reports green.
	local matrix_shard_raw
	matrix_shard_raw="$(rg --no-filename -o 'shard:\s*\[([^]]*)\]' -r '$1' "${workflow}" | head -1)"
	[[ -n "${matrix_shard_raw}" ]] \
		|| test_ifa_fault_shard_cases_fail "could not find strategy.matrix.shard in ${workflow}"
	local -a matrix_shard_items
	IFS=',' read -ra matrix_shard_items <<<"${matrix_shard_raw}"
	[[ "${#matrix_shard_items[@]}" -eq "${n}" ]] \
		|| test_ifa_fault_shard_cases_fail "workflow matrix.shard has ${#matrix_shard_items[@]} entries but IFA_FAULT_SHARD_DEFAULT_N=${n} -- keep them in lockstep or CI silently drops a shard's cells"

	# Invalid --shard input must fail loudly with exit 2 (never a silent
	# fallback, never exit 0/1, which would read as either "ran everything"
	# or a generic script error rather than a rejected argument).
	local rc
	rc=0; "${script}" --list-cells --shard "0/${n}" >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] || test_ifa_fault_shard_cases_fail "k=0 must exit 2, got ${rc}"
	rc=0; "${script}" --list-cells --shard "$((n + 1))/${n}" >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] || test_ifa_fault_shard_cases_fail "k>n must exit 2, got ${rc}"
	rc=0; "${script}" --list-cells --shard "1/0" >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] || test_ifa_fault_shard_cases_fail "n=0 must exit 2, got ${rc}"
	rc=0; "${script}" --list-cells --shard "1" >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] || test_ifa_fault_shard_cases_fail "malformed (missing slash) must exit 2, got ${rc}"
	rc=0; "${script}" --list-cells --shard "a/${n}" >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] || test_ifa_fault_shard_cases_fail "non-numeric k must exit 2, got ${rc}"
	rc=0; "${script}" --list-cells --shard "1/a" >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 2 ]] || test_ifa_fault_shard_cases_fail "non-numeric n must exit 2, got ${rc}"

	# Exact cover: shard 1..n, concatenated, must reproduce the full list
	# EXACTLY, except cell_baseline (see the file header): it is deliberately
	# emitted in every shard, so the concatenation carries (n-1) extra
	# cell_baseline lines beyond the single line the full list has. The
	# expected multiset below is built from the already-pinned literal list
	# above plus that known, structural (n-1) extra count -- this is not the
	# self-referential trap the header warns about: it asserts a general
	# invariant of an n-way shard of the ALREADY hand-pinned full list, not a
	# value copied from the implementation under test.
	local k
	local -a concatenated_actual=()
	for ((k = 1; k <= n; k++)); do
		local shard_out
		shard_out="$("${script}" --list-cells --shard "${k}/${n}")" \
			|| test_ifa_fault_shard_cases_fail "--list-cells --shard ${k}/${n} exited non-zero"
		local line
		while IFS= read -r line; do
			[[ -n "${line}" ]] && concatenated_actual+=("${line}")
		done <<<"${shard_out}"
	done

	local -a expected_multiset=("${ifa_full_cell_list_literal[@]}")
	local extra
	for ((extra = 1; extra < n; extra++)); do
		expected_multiset+=(cell_baseline)
	done

	local sorted_actual sorted_expected
	sorted_actual="$(printf '%s\n' "${concatenated_actual[@]}" | sort)"
	sorted_expected="$(printf '%s\n' "${expected_multiset[@]}" | sort)"
	[[ "${sorted_actual}" == "${sorted_expected}" ]] \
		|| test_ifa_fault_shard_cases_fail "shard concatenation is not an exact cover of the full cell list plus cell_baseline's (n-1) extra shard occurrences --
actual (sorted):
${sorted_actual}

expected (sorted):
${sorted_expected}"

	printf 'test-ifa-fault-injection-shard-cases: pass (%d cells, %d shards, exact cover proven)\n' \
		"${#ifa_full_cell_list_literal[@]}" "${n}"
}

# run_ifa_fault_injection_deployable_unit_ordering_cases verifies the
# deployable_unit trio's dispatch-anchor invocation and dispatch ORDER.
# Extracted from scripts/lib/test-ifa-fault-injection-deployable-unit-cases.sh
# to keep that file under the line cap (extraction, not deletion -- every
# comment below is the full, unabridged rationale). "$1" is the gate script
# path; the caller passes its own ${script} rather than this module
# recomputing repo_root, since deployable-unit-cases.sh is always sourced
# alongside a main mirror that already resolved it.
run_ifa_fault_injection_deployable_unit_ordering_cases() {
	local script="$1"

	# Anchored the same way as the eighteen-cell loop above: needles carry
	# the ifa_fault_shard_run prefix (scripts/lib/ifa_fault_shard.sh), since
	# every dispatch line now routes through that wrapper for --shard skip
	# support. STRICTLY STRONGER than a bare-name check: an unwrapped cell
	# would silently ignore --shard and run in every shard instead of just
	# its own -- a real defect with no other detector, since the gate would
	# still pass everywhere, just doing 4x the work for these three cells
	# while quietly diverging from the partition this mirror claims to prove.
	local cell
	for cell in cell_baseline_deployable_unit cell_killworker_deployable_unit cell_failgraphwrite_deployable_unit; do
		rg --quiet -- "^ifa_fault_shard_run ${cell}\$" "${script}" \
			|| test_ifa_fault_shard_cases_fail "verifier does not invoke ${cell} via ifa_fault_shard_run on its own line -- missing entirely, or dispatched WITHOUT the wrapper"
	done

	# The baseline cell must dispatch before both fault cells: it is the sole
	# writer of digests[baseline_deployable_unit]
	# (scripts/lib/ifa_fault_injection_deployable_unit_cells.sh's
	# assert_matches_baseline calls), which the two fault cells below read.
	# This is also the co-location property the shard PARTITIONER depends on
	# for its one atomic group (scripts/lib/ifa_fault_shard.sh's
	# IFA_FAULT_ATOMIC_GROUPS): the trio must land in the same shard, in this
	# order, or a fault cell reads an unset digests key. A line-number
	# extraction that returns empty (rather than a real line) would otherwise
	# fail below with a misleading "not a number" instead of naming the real
	# cause, so each extracted line is validated as numeric first.
	local baseline_du_line killworker_du_line failgraphwrite_du_line
	baseline_du_line="$(rg -n --line-regexp -- 'ifa_fault_shard_run cell_baseline_deployable_unit' "${script}" | cut -d: -f1 || true)"
	killworker_du_line="$(rg -n --line-regexp -- 'ifa_fault_shard_run cell_killworker_deployable_unit' "${script}" | cut -d: -f1 || true)"
	failgraphwrite_du_line="$(rg -n --line-regexp -- 'ifa_fault_shard_run cell_failgraphwrite_deployable_unit' "${script}" | cut -d: -f1 || true)"
	[[ "${baseline_du_line}" =~ ^[0-9]+$ && "${killworker_du_line}" =~ ^[0-9]+$ && "${failgraphwrite_du_line}" =~ ^[0-9]+$ \
		&& "${baseline_du_line}" -lt "${killworker_du_line}" && "${baseline_du_line}" -lt "${failgraphwrite_du_line}" ]] \
		|| test_ifa_fault_shard_cases_fail "cell_baseline_deployable_unit must be dispatched before both deployable-unit fault cells"
}
