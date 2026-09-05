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
# fail. The sibling hand-authored literal is
# ifa_det_family_cases_hand_authored (scripts/lib/test-ifa-determinism-family-
# cases.sh:222), which carries the same rule for the determinism side; an
# earlier version of this sentence pointed at a probe block in
# test-verify-ifa-fault-injection.sh that states no such rule.
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
	local repo_root script shard_lib workflow registry
	repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	script="${repo_root}/scripts/verify-ifa-fault-injection.sh"
	shard_lib="${repo_root}/scripts/lib/ifa_fault_shard.sh"
	workflow="${repo_root}/.github/workflows/ifa-determinism-gate.yml"
	registry="${repo_root}/specs/ci-gates.v1.yaml"

	[[ -f "${script}" ]] || test_ifa_fault_shard_cases_fail "missing ${script}"
	[[ -f "${shard_lib}" ]] || test_ifa_fault_shard_cases_fail "missing ${shard_lib}"
	[[ -f "${workflow}" ]] || test_ifa_fault_shard_cases_fail "missing ${workflow}"
	[[ -f "${registry}" ]] || test_ifa_fault_shard_cases_fail "missing ${registry}"
	local symbol_runtime_cells="${repo_root}/scripts/lib/ifa_fault_injection_symbol_runtime_cells.sh"
	local kill_cell
	for kill_cell in handles_route runs_in invokes_cloud_action; do
		rg --quiet --line-regexp -- "cell_killworker_${kill_cell}\\(\\) \\{" "${symbol_runtime_cells}" || test_ifa_fault_shard_cases_fail "missing custom symbol-runtime kill cell for ${kill_cell}"
	done
	rg --fixed-strings --quiet -- '_ifa_symbol_runtime_wait_for_code_calls_control "${CLAIMED_ROW_WAIT_TIMEOUT}"' "${symbol_runtime_cells}" \
		|| test_ifa_fault_shard_cases_fail "symbol-runtime kill cells do not require the independent code_calls completion control before kill"
	rg --fixed-strings --quiet -- "projection_domain = 'code_calls'" "${symbol_runtime_cells}" \
		|| test_ifa_fault_shard_cases_fail "symbol-runtime control is not scoped to the independent code_calls projection domain"
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
		cell_baseline_repo_dependency
		cell_killworker_repo_dependency
		cell_failgraphwrite_repo_dependency
		cell_baseline_submodule_pin
		cell_killworker_submodule_pin
		cell_failgraphwrite_submodule_pin
		cell_baseline_kubernetes_namespace_environment cell_killworker_kubernetes_namespace_environment cell_failgraphwrite_kubernetes_namespace_environment cell_baseline_iam_instance_profile_role cell_killworker_iam_instance_profile_role cell_failgraphwrite_iam_instance_profile_role
		cell_baseline_inheritance
		cell_killworker_inheritance
		cell_failgraphwrite_inheritance
		cell_baseline_shell_exec
		cell_killworker_shell_exec
		cell_failgraphwrite_shell_exec
		cell_baseline_workload_dependency
		cell_killworker_workload_dependency
		cell_failgraphwrite_workload_dependency
		cell_baseline_symbol_runtime
		cell_failgraphwrite_handles_route
		cell_failgraphwrite_runs_in
		cell_failgraphwrite_invokes_cloud_action
		cell_killworker_handles_route
		cell_killworker_runs_in
		cell_killworker_invokes_cloud_action
	)

	local actual_full
	actual_full="$("${script}" --list-cells)" \
		|| test_ifa_fault_shard_cases_fail "--list-cells exited non-zero"

	local expected_full
	expected_full="$(printf '%s\n' "${ifa_full_cell_list_literal[@]}")"
	[[ "${actual_full}" == "${expected_full}" ]] \
		|| test_ifa_fault_shard_cases_fail "--list-cells output does not match the hand-authored literal list (see file header) -- actual:
${actual_full}"

	# CELL-COUNT PIN (F-4). The expected count is derived from the literal
	# roster above; no stale prose number needs a second manual update. It is
	# a COUNT pin, deliberately
	# not weakened to an existence check, and it belongs beside the
	# hand-authored cell list: the person who changes the number is the person
	# adding cells, and they need to land in this file -- where the literal
	# list and the set-equality check below will also demand their attention
	# -- rather than in an unrelated family's module where bumping the number
	# is the whole fix.
	local dispatch_count
	dispatch_count="$(rg --count --line-regexp -- 'ifa_fault_shard_run cell_[a-z_]+' "${script}")"
	[[ "${dispatch_count}" == "${#ifa_full_cell_list_literal[@]}" ]] \
		|| test_ifa_fault_shard_cases_fail "gate dispatches ${dispatch_count} cells via ifa_fault_shard_run, but the hand-authored literal list has ${#ifa_full_cell_list_literal[@]}"

	# REVERSE DIRECTION (F-4): every cell the gate DISPATCHES must also be in
	# the partitioner's cell list. The literal check above only proves the
	# other way round -- that each known name is dispatched -- so a
	# cell added to the gate's dispatch block but NOT to IFA_FAULT_ALL_CELLS
	# is assigned to no shard and silently runs in NONE of them, while all
	# four shards report green. Proven reachable: a dispatched-but-unlisted
	# cell prints "skipped (not assigned to shard k/n)" for every k and the
	# gate still exits 0.
	#
	# This is not self-referential. The gate's dispatch block and the
	# partitioner's IFA_FAULT_ALL_CELLS are authored independently, in
	# different files, by whoever lands a family -- which is exactly the
	# cross-check rationale this file already accepts for the workflow
	# matrix-cardinality pin. What makes the old one-directional form
	# dangerous is that the ONLY thing catching a new dispatch line was the
	# hardcoded cell-count pin, and that pin lived in an unrelated family's
	# case module where a family author would never look.
	local dispatched_cells
	dispatched_cells="$(rg --only-matching --replace '$1' -- '^ifa_fault_shard_run (\S+)$' "${script}" | sort -u)" \
		|| test_ifa_fault_shard_cases_fail "could not extract ifa_fault_shard_run dispatch lines from the gate"
	local listed_cells
	listed_cells="$(printf '%s\n' "${actual_full}" | sort -u)"
	[[ "${dispatched_cells}" == "${listed_cells}" ]] \
		|| test_ifa_fault_shard_cases_fail "gate dispatch block and --list-cells disagree -- a cell dispatched but not listed runs in NO shard (and one listed but not dispatched never runs at all).
dispatched only:
$(comm -23 <(printf '%s\n' "${dispatched_cells}") <(printf '%s\n' "${listed_cells}"))
listed only:
$(comm -13 <(printf '%s\n' "${dispatched_cells}") <(printf '%s\n' "${listed_cells}"))"

	# DISPATCH-ANCHOR CHECKS (moved here from scripts/test-verify-ifa-fault-injection.sh
	# to give that file real line-count headroom. The rationale below is the
	# The full-cell shape: the shared baseline plus every cell with a live
	# seam -- four original recovery cells, two SQL-targeted (#5555), two
	# delivery-shaped (#5544), two code-call-targeted (#5991), two
	# documentation-targeted (#5994), two rationale-targeted (#5998), a
	# family-scoped baseline plus two recovery cells each for
	# deployable_unit_edges (#5993), codeowners_ownership_edges (#6160),
	# repo_dependency (#5999), submodule_pin_edges (#6002), inheritance_edges
	# (#5996), shell_exec (#6001), workload_dependency (#6003), kubernetes_namespace_environment
	# + iam_instance_profile_role (#6309), and ONE shared baseline plus six family-targeted
	# recovery cells for the handles_route/runs_in/invokes_cloud_action trio (#5995/#6000/#5997).
	# All cells in the literal roster run by default.
	# Every cell is anchored to its own invocation line, never matched by bare name.
	# A bare-name needle is satisfied by prose and by longer siblings: "cell_baseline"
	# matches this file's own comments AND cell_baseline_deployable_unit, so deleting
	# the cell_baseline dispatch line left the mirror green -- and cell_baseline is
	# the sole writer of digests[baseline], so every assert_matches_baseline call
	# that does not name a family-scoped baseline would then compare against an
	# unset key. The anchored form was previously applied to only five cells; it
	# now covers every cell in the roster.
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
		cell_failgraphwrite_deployable_unit \
		cell_baseline_codeowners cell_killworker_codeowners cell_failgraphwrite_codeowners \
		cell_baseline_repo_dependency cell_killworker_repo_dependency cell_failgraphwrite_repo_dependency \
		cell_baseline_submodule_pin cell_killworker_submodule_pin cell_failgraphwrite_submodule_pin cell_baseline_kubernetes_namespace_environment cell_killworker_kubernetes_namespace_environment cell_failgraphwrite_kubernetes_namespace_environment cell_baseline_iam_instance_profile_role cell_killworker_iam_instance_profile_role cell_failgraphwrite_iam_instance_profile_role \
		cell_baseline_inheritance cell_killworker_inheritance cell_failgraphwrite_inheritance \
		cell_baseline_shell_exec cell_killworker_shell_exec cell_failgraphwrite_shell_exec \
		cell_baseline_workload_dependency cell_killworker_workload_dependency cell_failgraphwrite_workload_dependency \
		cell_baseline_symbol_runtime cell_failgraphwrite_handles_route \
		cell_failgraphwrite_runs_in cell_failgraphwrite_invokes_cloud_action \
		cell_killworker_handles_route cell_killworker_runs_in \
		cell_killworker_invokes_cloud_action; do
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
	# entirely -- the identical defect class the thirty-cell dispatch-anchor
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

	# CHECK_NAMES CARDINALITY PIN. The matrix pin above binds the workflow to
	# IFA_FAULT_SHARD_DEFAULT_N; it says nothing about specs/ci-gates.v1.yaml's
	# ci.check_names, and until this assert existed nothing did.
	# internal/cigates/drift.go validates check_names as a SUBSET of the
	# concrete matrix names, so removing a shard is caught (a listed name the
	# matrix no longer produces) while ADDING one is not: the four listed names
	# stay a valid subset and the fifth shard's check belongs to no gate. A
	# check belonging to no gate is invisible to required-gates-complete, which
	# is the same consequence the registry comment spells out for an empty
	# check_names -- a red shard nobody waits on.
	#
	# Both halves are asserted: the COUNT must equal n, and every entry must end
	# in "/n)" so a 5-shard matrix cannot be satisfied by four stale "/4"
	# strings that happen to number four.
	local registry_gate_block check_names_count
	registry_gate_block="$(sed -n '/^  - id: ifa-fault-injection$/,/^  - id: /p' "${registry}")"
	[[ -n "${registry_gate_block}" ]] \
		|| test_ifa_fault_shard_cases_fail "could not find the ifa-fault-injection gate block in ${registry##*/}"
	check_names_count="$(printf '%s\n' "${registry_gate_block}" \
		| rg --count --only-matching -- '^        - "fault-injection \(shard [0-9]+/[0-9]+\)"$' || true)"
	[[ "${check_names_count}" == "${n}" ]] \
		|| test_ifa_fault_shard_cases_fail "ifa-fault-injection declares ${check_names_count:-0} ci.check_names entries but IFA_FAULT_SHARD_DEFAULT_N=${n} -- a shard whose check name is unlisted belongs to no gate, so a red shard is invisible to required-gates-complete"
	printf '%s\n' "${registry_gate_block}" \
		| rg --count --only-matching -- "^        - \"fault-injection \(shard [0-9]+/${n}\)\"\$" \
		| rg --quiet --line-regexp -- "${n}" \
		|| test_ifa_fault_shard_cases_fail "ifa-fault-injection's ci.check_names do not all carry the /${n} denominator -- stale names can keep the right COUNT while naming checks the matrix no longer emits"

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

# run_ifa_fault_injection_deployable_unit_ordering_cases (deployable_unit
# trio dispatch-anchor + ordering proof) moved to its own sibling module,
# scripts/lib/test-ifa-fault-injection-deployable-unit-ordering-cases.sh, to
# keep this file under the repository's 500-line cap -- extraction, not
# deletion; every comment there is the full, unabridged rationale. Sourced
# alongside this file by scripts/test-verify-ifa-fault-injection.sh, before
# deployable_unit_cases_lib calls it.

# run_ifa_fault_injection_atomic_group_ordering_cases generalizes the ordering
# proof above over EVERY entry in IFA_FAULT_ATOMIC_GROUPS, instead of naming one
# family's trio.
#
# The specific gap it closes: the codeowners trio was added to
# IFA_FAULT_ALL_CELLS and IFA_FAULT_ATOMIC_GROUPS when #6160 landed mid-branch,
# but no ordering assert came with it -- and cell_baseline_codeowners is the
# sole writer of digests[baseline_codeowners] and baseline_codeowners_retried,
# which both of its fault cells read. Reordering those three dispatch lines
# passed every static check: set-equality is order-insensitive, and an atomic
# group only guarantees CO-LOCATION in one shard, not order within it. The
# failure would have surfaced as an unset-digest read inside a ~22-minute
# Docker shard.
#
# Written as a loop over the groups rather than a second hand-written trio so
# the next family to declare an atomic group inherits the check instead of
# needing someone to remember this file. A group whose first member is not a
# baseline cell is left alone: co-location is the only property those have.
run_ifa_fault_injection_atomic_group_ordering_cases() {
	local script="$1"
	local group members baseline_cell baseline_line member member_line

	# Make sure the partition data is actually in scope. Without it
	# IFA_FAULT_ATOMIC_GROUPS is unset here, the loop below runs zero times, and
	# this whole check passes no matter how the dispatch lines are ordered --
	# which is exactly what the first version of it did: reordering the
	# codeowners trio left the mirror green. Caught by deliberately breaking the
	# gate and finding the check silent.
	#
	# Read from a FRESH bash process rather than sourcing into this one. Two
	# facts make that the only working option, and both are easy to get wrong:
	# ifa_fault_shard.sh declares its arrays with a bare `declare`, so sourcing
	# it inside a function makes them FUNCTION-LOCAL and they vanish when
	# run_ifa_fault_injection_shard_cases returns (the same bash trap that broke
	# the family registry, fixed there with -g); while its scalars are
	# `readonly`, which IS global and survives -- so a re-source in this process
	# aborts with "readonly variable" instead of reloading. A subshell inherits
	# readonly attributes too, so `( source ... )` fails the same way. A separate
	# `bash -c` has neither problem, and matches how the other checks in this
	# file reach the partition: through a subprocess, never in-process state.
	local groups_raw
	groups_raw="$(bash -c 'set -euo pipefail; source "$1"; printf "%s\n" "${IFA_FAULT_ATOMIC_GROUPS[@]}"' _ "${shard_lib}")" \
		|| test_ifa_fault_shard_cases_fail "could not read IFA_FAULT_ATOMIC_GROUPS from ${shard_lib##*/}"
	local -a IFA_FAULT_ATOMIC_GROUPS=()
	while IFS= read -r group; do
		[[ -n "${group}" ]] && IFA_FAULT_ATOMIC_GROUPS+=("${group}")
	done <<<"${groups_raw}"

	# And prove the data actually arrived. A rename or a failed source would
	# otherwise restore the vacuum this comment exists to describe.
	[[ "${#IFA_FAULT_ATOMIC_GROUPS[@]}" -gt 0 ]] \
		|| test_ifa_fault_shard_cases_fail "IFA_FAULT_ATOMIC_GROUPS is empty or unset after sourcing ${shard_lib##*/} -- the atomic-group ordering check would pass vacuously"

	local checked=0
	for group in "${IFA_FAULT_ATOMIC_GROUPS[@]}"; do
		read -r -a members <<<"${group}"
		baseline_cell="${members[0]}"
		[[ "${baseline_cell}" == cell_baseline_* ]] || continue

		baseline_line="$(rg -n --line-regexp -- "ifa_fault_shard_run ${baseline_cell}" "${script}" | cut -d: -f1 || true)"
		[[ "${baseline_line}" =~ ^[0-9]+$ ]] 			|| test_ifa_fault_shard_cases_fail "atomic group \"${group}\": ${baseline_cell} is not dispatched via ifa_fault_shard_run on its own line, so its dispatch order cannot be checked"

		for member in "${members[@]:1}"; do
			member_line="$(rg -n --line-regexp -- "ifa_fault_shard_run ${member}" "${script}" | cut -d: -f1 || true)"
			[[ "${member_line}" =~ ^[0-9]+$ ]] \
				|| test_ifa_fault_shard_cases_fail "atomic group \"${group}\": ${member} is not dispatched via ifa_fault_shard_run on its own line"
			[[ "${baseline_line}" -lt "${member_line}" ]] \
				|| test_ifa_fault_shard_cases_fail "atomic group \"${group}\": ${baseline_cell} (line ${baseline_line}) must be dispatched BEFORE ${member} (line ${member_line}); it is the sole writer of the baseline digest and retry baseline that member reads"
			checked=$((checked + 1))
		done
	done

	# Every baseline-led group must have contributed at least one comparison, or
	# the loop found groups but compared nothing inside them.
	[[ "${checked}" -gt 0 ]] \
		|| test_ifa_fault_shard_cases_fail "atomic-group ordering check made zero comparisons across ${#IFA_FAULT_ATOMIC_GROUPS[@]} group(s) -- no baseline-led group was found, so nothing was proven"
	printf 'test-ifa-fault-injection-shard-cases: atomic-group dispatch order proven (%d member comparisons across %d group(s))\n' \
		"${checked}" "${#IFA_FAULT_ATOMIC_GROUPS[@]}"
}
