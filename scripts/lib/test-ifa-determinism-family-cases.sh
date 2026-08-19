#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# fail(), require(), require_fixture(), require_delta_lib(),
# require_code_call_lib(), require_deployable_unit_lib(), require_rationale_lib(),
# require_documentation_lib(), and the ${script}/${delta_lib}/${registry_family_lib}/etc.
# path variables are all defined by scripts/test-verify-ifa-determinism.sh
# before it sources this file; shellcheck cannot see that from this file alone.
# Per-family static structural cases for scripts/test-verify-ifa-determinism.sh,
# sourced so the top-level mirror stays below the repository's 500-line cap
# (mirroring the fault-injection sibling's per-family case-module split, e.g.
# scripts/lib/test-ifa-fault-injection-documentation-cases.sh). Covers the
# fixture/assertion contract for the SQL relationship family (#5351), the
# code_calls family (#5991), the deployable_unit_edges standalone cell
# (#5993), the rationale_edges family (#5998), and the documentation_edges
# family (#5994) -- plus the cross-family delta-ordering assertions that tie
# the SQL and rationale delta replay together, since those checks read as
# part of the same family narrative in the original script and moving them
# apart would separate a check from the family it is about.
#
# #6147 PR-0 family-registry extraction: the gate no longer hand-lists
# sql_relationships/code_calls/documentation_edges/rationale_edges inline --
# it iterates scripts/lib/ifa_family_registry.sh's shared_cell=1 rows and
# dispatches through ifa_family_registry_drive/ifa_family_registry_assert.
# The trailing "shared-cell family-registry dispatch loop" section proves the
# loop shape, totality against this module's own hand-authored family list
# (both directions, read live through the real registry accessors, never
# re-derived from the registry's own array literals), and that no leftover
# literal per-family drive/assert call survives outside the loop. Per the
# arbiter's D1 ruling (2026-08-18): mirror pins must not be co-derived from
# the registry/partitioner -- hand-written literal lists plus a bidirectional
# totality diff plus an executable declare -F dispatch check.
run_ifa_determinism_family_cases() {
# SQL relationship family cassette (#5351): the committed cassette driven into
# every cell so the ifa-determinism lane actually replays the SQL relationship
# materialization family (backing the materialized_edges:sql_relationships
# manifest row's proof_gate: ifa-determinism claim), plus the per-cell
# absolute-set assertion (`ifa assert-edges`) that the P2 digest cannot make: a
# family silently empty in ALL cells has an identical digest in every cell and
# passes the digest comparison vacuously; the absolute expected set catches it.
require_fixture "SQL cassette path" "testdata/cassettes/sqlrelationships/ifa-sql-family.json"
require_fixture "SQL expected-edge set path" "go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-expected-edges.json"
require_fixture "SQL delta cassette path" "testdata/cassettes/sqlrelationships/ifa-sql-family-delta.json"
require_fixture "SQL delta-live expected-edge set path" "go/internal/ifa/testdata/sqlrelationships/ifa-sql-family-delta-live-expected-edges.json"
require_fixture "SQL cassette existence guard" 'SQL cassette not found'
require_fixture "SQL expected-edge set existence guard" 'SQL expected-edge set not found'
require_fixture "SQL delta cassette existence guard" 'SQL delta cassette not found'
require_fixture "SQL delta expected-edge set existence guard" 'SQL delta expected-edge set not found'
require_delta_lib "SQL cassette drive into every cell" 'eshu-ifa" drive -cassette "${sql_cassette}" -workers "${n}"'
require "SQL delta helper invocation in every cell" "ifa_det_run_sql_delta_live"
require_delta_lib "SQL delta cassette drive into every cell" 'eshu-ifa" drive -cassette "${sql_delta_cassette}" -workers "${n}"'
require_delta_lib "SQL delta populated guard" "SQL delta drive enqueued 0 new fact_work_items rows"
require_delta_lib "rationale delta populated guard" "rationale delta drive enqueued 0 new fact_work_items rows"
require_delta_lib "assert-edges verb invocation" '"${bin_dir}/eshu-ifa" assert-edges'
require_delta_lib "assert-edges domain flag" "-domain sql_relationships"
require_delta_lib "assert-edges expected flag" '-expected "${sql_expected_edges}"'
require_delta_lib "assert-edges non-vacuity framing" "non-vacuity"
require_delta_lib "assert-edges no-normalize-away directive" "do NOT normalize this away"
require_delta_lib "delta assert-edges expected flag" '-expected "${sql_delta_expected_edges}"'
require_delta_lib "delta assert-edges exactness framing" "SQL delta-live materialized edge set did not match the expected accumulated set"

# code_calls (#5991): every N cell must drive the committed cassette and assert
# its hand-derived five-edge set. Digest equality cannot detect empty == empty.
require_fixture "code-call cassette path" "testdata/cassettes/codecalls/ifa-code-call-family.json"
require_fixture "code-call expected-edge set path" "go/internal/ifa/testdata/codecalls/ifa-code-call-family-expected-edges.json"
require_fixture "code-call cassette existence guard" "code-call cassette not found"
require_fixture "code-call expected-edge set existence guard" "code-call expected-edge set not found"
require_code_call_lib "code-call cassette drive" 'eshu-ifa" drive -cassette "${cassette}" -workers "${workers}"'
require_code_call_lib "code-call assert-edges domain" "-domain code_calls"
require_code_call_lib "code-call expected-set argument" '-expected "${expected_edges}"'
require_code_call_lib "code-call non-vacuity framing" "five-edge exact set"

# deployable_unit_edges (#5993): a STANDALONE cell, run once after the N-loop,
# not one of the N={1,2,4} cells and not folded into any of them -- unlike
# sql_relationships/code_calls, this family needs a bootstrap-index
# maintenance pass to materialize anything, so its live proof is not a
# worker-count invariance test.
require_fixture "deployable-unit cassette path" "testdata/cassettes/deployableunit/ifa-deployable-unit-family.json"
require_fixture "deployable-unit expected-edge set path" "go/internal/ifa/testdata/deployableunit/ifa-deployable-unit-family-expected-edges.json"
require_fixture "deployable-unit cassette existence guard" "deployable-unit cassette not found"
require_fixture "deployable-unit expected-edge set existence guard" "deployable-unit expected-edge set not found"
require "sixth binary: bootstrap-index build" "ifa_det_build_bin \"\${bin_dir}\" bootstrap-index"
require "standalone cell helper invocation" "ifa_deployable_unit_live_run_standalone_cell"
require_deployable_unit_lib "standalone cell function definition" "ifa_deployable_unit_live_run_standalone_cell()"
require_deployable_unit_lib "drive helper invocation inside the standalone cell" "ifa_deployable_unit_live_drive"
require_deployable_unit_lib "pre-maintenance drain before the maintenance pass" "ifa_deployable_unit_live_drain pre"
require_deployable_unit_lib "empty-before-maintenance non-vacuity assertion" "ifa_deployable_unit_live_assert_empty_before_maintenance"
require_deployable_unit_lib "bootstrap-index maintenance pass invocation" "eshu-bootstrap-index"
require_deployable_unit_lib "post-maintenance drain" "ifa_deployable_unit_live_drain post"
require_deployable_unit_lib "exact-set assert-edges domain" "-domain deployable_unit_edges"
require_deployable_unit_lib "exact-set assert-edges expected flag" '-expected "${expected_edges}"'
# The standalone cell must run strictly AFTER the N-loop (the "done" closing
# it) and BEFORE the digest comparison -- it must never land inside the loop,
# which would fold its bootstrap-index maintenance pass into every N cell's
# digest terminal for a reason unrelated to what that loop tests. The N-loop's
# own "done" is the first bare `done` line AFTER the loop's `for n in
# "${worker_counts[@]}"; do` header (the script also has bare `done` lines
# closing the argument-parsing and --contention/--teeth loops).
n_loop_for_line="$(rg -n --fixed-strings -- 'for n in "${worker_counts[@]}"; do' "${script}" | head -1 | cut -d: -f1 || true)"
n_loop_done_line="$(awk -v start="${n_loop_for_line:-0}" 'NR > start && $0 == "done" { print NR; exit }' "${script}")"
standalone_cell_line="$(rg -n --fixed-strings -- '"${bin_dir}" "${deployable_unit_cassette}" "${deployable_unit_expected_edges}"' "${script}" | cut -d: -f1 || true)"
compare_digests_line="$(rg -n --fixed-strings -- 'log "compare digests across N=${worker_counts[*]}"' "${script}" | cut -d: -f1 || true)"
[[ "${n_loop_done_line}" =~ ^[0-9]+$ && "${standalone_cell_line}" =~ ^[0-9]+$ && "${compare_digests_line}" =~ ^[0-9]+$ \
	&& "${n_loop_done_line}" -lt "${standalone_cell_line}" && "${standalone_cell_line}" -lt "${compare_digests_line}" ]] \
	|| fail "the deployable_unit_edges standalone cell must run after the N-loop closes and before the digest comparison"

# rationale_edges (#5998): every worker-count cell drives the production-shaped
# cassette, exact-asserts all three EXPLAINS records, and fails closed unless
# the durable queue/intent lifecycle is exactly 1|1|0|4|3|1|4|0 with one
# accepted generation. The shared SQL and rationale delta replay must then
# converge to exact-one EXPLAINS plus the exact Charge node before graph dump.
require_fixture "rationale cassette path" "testdata/cassettes/rationale/ifa-rationale-family.json"
require_fixture "rationale expected-edge set path" "go/internal/ifa/testdata/rationale/ifa-rationale-family-expected-edges.json"
require_fixture "rationale delta cassette path" "testdata/cassettes/rationale/ifa-rationale-family-delta.json"
require_fixture "rationale delta expected-record set path" "go/internal/ifa/testdata/rationale/ifa-rationale-family-delta-live-expected-records.json"
require_fixture "rationale cassette existence guard" "rationale cassette not found"
require_fixture "rationale expected-edge set existence guard" "rationale expected-edge set not found"
require_fixture "rationale delta cassette existence guard" "rationale delta cassette not found"
require_fixture "rationale delta expected-record existence guard" "rationale delta expected-record set not found"
require "rationale durable-count helper invocation in every cell" "ifa_rationale_assert_work_counts"
require_rationale_lib "rationale cassette drive" 'eshu-ifa" drive -cassette "${cassette}" -workers "${workers}"'
require_rationale_lib "rationale assert-edges domain" "-domain rationale_edges"
require_rationale_lib "rationale expected-set argument" '-expected "${expected_edges}"'
# Pinned to the ASSIGNMENT, not the bare tuple. The header comment a hundred
# lines below quotes both tuples verbatim, so a bare-value needle stayed
# green with the real oracles set to garbage -- the durable-lifecycle oracle
# for this family could be replaced wholesale and no gate would notice.
require_rationale_lib "exact durable tuple" 'ifa_rationale_expected_tuple="1|1|0|4|3|1|4|0"'
require_rationale_lib "exact delta-generation durable tuple" 'ifa_rationale_delta_expected_tuple="1|1|0|1|0|1|1|0"'
require_rationale_lib "accepted generation exact count" "shared_projection_acceptance"
require_delta_lib "rationale delta is driven before the shared drain" 'ifa_rationale_drive "delta-n${n}"'
sql_delta_drive_line="$(rg -n --fixed-strings -- 'eshu-ifa" drive -cassette "${sql_delta_cassette}"' "${delta_lib}" | cut -d: -f1 || true)"
rationale_delta_drive_line="$(rg -n --fixed-strings -- 'ifa_rationale_drive "delta-n${n}"' "${delta_lib}" | cut -d: -f1 || true)"
shared_delta_drain_line="$(rg -n --fixed-strings -- 'drain SQL + rationale delta through caller-owned running projector + reducer' "${delta_lib}" | cut -d: -f1 || true)"
[[ "${sql_delta_drive_line}" =~ ^[0-9]+$ && "${rationale_delta_drive_line}" =~ ^[0-9]+$ && "${shared_delta_drain_line}" =~ ^[0-9]+$ \
	&& "${sql_delta_drive_line}" -lt "${rationale_delta_drive_line}" \
	&& "${rationale_delta_drive_line}" -lt "${shared_delta_drain_line}" ]] \
	|| fail "SQL and rationale delta cassettes must both be driven before their shared drain"
delta_call_line="$(rg -n -- '^[[:space:]]*ifa_det_run_sql_delta_live' "${script}" | cut -d: -f1 || true)"
post_delta_code_call_line="$(rg -n --fixed-strings -- 'ifa_code_call_assert "post-delta N=${n}"' "${script}" | cut -d: -f1 || true)"
post_delta_documentation_line="$(rg -n --fixed-strings -- 'ifa_documentation_assert "post-delta N=${n}"' "${script}" | cut -d: -f1 || true)"
post_delta_rationale_line="$(rg -n --fixed-strings -- 'ifa_rationale_assert_delta_truth "post-delta N=${n}"' "${script}" | cut -d: -f1 || true)"
post_delta_dump_line="$(rg -n --fixed-strings -- 'log "N=${n}: canonicalize post-delta graph (ifa graph-dump)"' "${script}" | cut -d: -f1 || true)"
[[ "${delta_call_line}" =~ ^[0-9]+$ && "${post_delta_code_call_line}" =~ ^[0-9]+$ \
	&& "${post_delta_documentation_line}" =~ ^[0-9]+$ && "${post_delta_rationale_line}" =~ ^[0-9]+$ \
	&& "${post_delta_dump_line}" =~ ^[0-9]+$ \
	&& "${delta_call_line}" -lt "${post_delta_code_call_line}" \
	&& "${post_delta_code_call_line}" -lt "${post_delta_documentation_line}" \
	&& "${post_delta_documentation_line}" -lt "${post_delta_rationale_line}" \
	&& "${post_delta_rationale_line}" -lt "${post_delta_dump_line}" ]] \
	|| fail "every N cell must exact-assert code_calls, documentation, rationale exact-one, and the delta-generation Charge survivor after the shared delta drain and before its graph dump"

# documentation_edges (#5994): every N cell must drive the committed cassette
# and assert its hand-derived three-edge set (Function, Class, and the
# SqlTable-target case, batchCanonicalDocumentationEntityEdgeCypher's MATCH
# label alternation). Digest equality cannot detect empty == empty.
require_documentation_lib "documentation cassette drive" 'eshu-ifa" drive -cassette "${cassette}" -workers "${workers}"'
require_documentation_lib "documentation assert-edges domain" "-domain documentation_edges"
require_documentation_lib "documentation expected-set argument" '-expected "${expected_edges}"'
require_documentation_lib "documentation non-vacuity framing" "three-edge exact set"
require "post-delta rationale durable generation" '"${ifa_rationale_delta_generation_id}"'
require "post-delta rationale durable tuple" '"${ifa_rationale_delta_expected_tuple}"'
delta_function="$(rg -U --pcre2 --only-matching -- '(?ms)^ifa_det_run_sql_delta_live\(\) \{.*?^\}' "${delta_lib}")"
[[ "${delta_function}" != *"ifa_det_start_bg"* ]] \
	|| fail "SQL+rationale delta helper must reuse the caller-owned projector/reducer lifecycle"
[[ "${delta_function}" == *"caller-owned running projector + reducer"* ]] \
	|| fail "SQL+rationale delta helper must document its one-lifecycle worker ownership"

# ============================================================================
# Shared-cell family-registry dispatch loop (#6147 PR-0)
# ============================================================================
# The gate's drive/assert loops iterate ifa_family_registry_names, filtered
# to shared_cell=1 rows, and dispatch through ifa_family_registry_drive /
# ifa_family_registry_assert -- replacing the four hand-copied inline
# drive/assert calls the require()s above used to pin directly. "The loop
# exists" alone is nearly vacuous (it passes whether or not the loop body
# does anything useful); the totality check below is what gives it teeth.
# Pinned on the REDIRECT, not the loop header. The loop reads names on stdin, so
# `done < <(ifa_family_registry_names)` is what supplies the data -- a
# `while IFS= read -r family` whose redirect was dropped reads EOF immediately
# and iterates zero times, passing every downstream assertion vacuously. Binding
# the header alone would not catch that.
require "drive loop iterates the family registry, not a hardcoded list" 'done < <(ifa_family_registry_names)'
# The filter is two statements, not one, and that is the point: a bare
# `[[ "$(accessor)" == "1" ]] || continue` cannot tell shared_cell=0 apart
# from an accessor FAILURE on a row missing IFA_FAMILY_SHARED_CELL -- a
# failed command substitution yields empty stdout and the comparison is
# false either way, so a malformed row was silently treated as "not a
# shared-cell family" and never driven or asserted while the gate stayed
# green. Pin BOTH halves so the fail-closed check cannot be collapsed back
# into the one-liner.
require "drive/assert loop captures the accessor's exit status separately" 'family_shared_cell="$(ifa_family_shared_cell "${family}")"'
require "drive/assert loop dies when the shared_cell accessor fails" 'refusing to silently skip this family'
require "drive/assert loop skips non-shared_cell families" '[[ "${family_shared_cell}" == "1" ]] || continue'
require "drive loop dispatches through the registry, not a family-specific call" 'ifa_family_registry_drive "${family}" "${n}" "${bin_dir}" "${log_dir}"'
require "assert loop dispatches through the registry, not a family-specific call" 'ifa_family_registry_assert "${family}" "${n}" "${bin_dir}"'
# The registry-fed redirect must appear exactly twice (drive, then assert) -- a
# gate half-migrated to the registry (e.g. drive converted but assert still
# hand-listing families) would satisfy a single occurrence.
loop_header_count="$(rg --count --fixed-strings -- 'done < <(ifa_family_registry_names)' "${script}")"
[[ "${loop_header_count}" -eq 2 ]] \
	|| fail "expected exactly 2 occurrences of the family-registry drive/assert loop redirect (drive + assert), found ${loop_header_count} -- a while-read loop whose redirect was dropped iterates zero times and passes vacuously"

# Totality, both directions, read LIVE through the real registry accessors
# (never by re-deriving the expected set from ifa_family_registry.sh's own
# array literals -- a check that copies its expected values from the
# artifact it is checking can never fail). This module's hand-authored list
# below is independent of the registry file; only the comparison touches it.
#
# The map VALUE is each family's expected `-domain <family>` assert-edges
# needle, not a bare `=1` acknowledgement. A bare acknowledgement is
# satisfied by adding a key with zero real coverage anywhere in this module
# -- `[handles_route_edges]=1` alone made the totality check below green
# with that family having no fixture or assertion of any kind here. Storing
# the real expected needle as the value lets the loop right after this map
# verify it actually appears in one of this module's own target lib files,
# so a bare entry can no longer pass.
# codeowners_ownership_edges (#5992, wired into the shared determinism cell by
# #6160). Its drive/assert helpers live in scripts/lib/ifa_codeowners_live.sh
# and follow the majority labeled signature, so the registry loop dispatches
# them with no shim. The exact-set assert is what makes the coverage real: the
# MERGE key for this family deliberately includes pattern and source_path, so
# distinct CODEOWNERS rules must not collapse onto one edge.
require_codeowners_lib "codeowners assert-edges domain" "-domain codeowners_ownership_edges"
require_codeowners_lib "codeowners drive takes the labeled signature" 'local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"'
require_codeowners_lib "codeowners assert is exact-set, not a digest" '-expected "${expected_edges}"'

declare -A ifa_det_family_cases_hand_authored=(
	[sql_relationships]="-domain sql_relationships"
	[code_calls]="-domain code_calls"
	[documentation_edges]="-domain documentation_edges"
	[rationale_edges]="-domain rationale_edges"
	[codeowners_ownership_edges]="-domain codeowners_ownership_edges"
)
# First, the map value must literally be this family's own `-domain
# <family>` flag -- never a bare placeholder like `1` and never another
# family's value copy-pasted in. A bare-string check alone (e.g. an
# fixed-string rg search for whatever the value happens to be) is not
# enough on its own: `1` is common enough to appear somewhere in these
# multi-hundred-line lib files by accident, which would let a lazy `=1`
# entry slide through a needle search that never verifies the needle looks
# like a real domain flag in the first place.
#
# Second, with the shape confirmed, the needle must actually appear in one
# of this module's own target lib files -- i.e. a real require_*_lib call
# for it exists somewhere in this module, not merely a map entry. Each
# needle is family-specific by construction (`-domain <family>` embeds the
# family name), so searching the union of the target lib files is safe: two
# different family keys can never produce the same needle, so this cannot
# pass by matching a sibling family's real assertion.
for family in "${!ifa_det_family_cases_hand_authored[@]}"; do
	domain_needle="${ifa_det_family_cases_hand_authored[${family}]}"
	expected_needle="-domain ${family}"
	[[ "${domain_needle}" == "${expected_needle}" ]] \
		|| fail "family registry totality: '${family}' is hand-authored in ifa_det_family_cases_hand_authored with value '${domain_needle}', which is not this family's own '${expected_needle}' assert-edges flag -- the value must be the family's real domain needle, never a bare acknowledgement or a value copied from another family"
	rg --fixed-strings --quiet -- "${domain_needle}" \
		"${delta_lib}" "${code_call_lib}" "${documentation_lib}" "${rationale_lib}" "${codeowners_lib}" \
		|| fail "family registry totality: '${family}' is hand-authored in ifa_det_family_cases_hand_authored with expected needle '${domain_needle}' but it does not appear in any of this module's target lib files -- add fixtures + a require_*_lib drive/assert-shape needle for it, a bare map-key acknowledgement is not acceptable"
done
# shellcheck source=scripts/lib/ifa_family_registry.sh
source "${registry_family_lib}"
declare -F ifa_family_registry_names >/dev/null \
	|| fail "${registry_family_lib} does not define ifa_family_registry_names -- its accessor API changed"
declare -F ifa_family_shared_cell >/dev/null \
	|| fail "${registry_family_lib} does not define ifa_family_shared_cell -- its accessor API changed"
declare -A ifa_det_family_cases_seen_shared_cell=()
while IFS= read -r family; do
	[[ -n "${family}" ]] || continue
	[[ "$(ifa_family_shared_cell "${family}")" == "1" ]] || continue
	ifa_det_family_cases_seen_shared_cell["${family}"]=1
	[[ -n "${ifa_det_family_cases_hand_authored[${family}]+set}" ]] \
		|| fail "family registry totality: ${registry_family_lib} declares shared_cell family '${family}' which has no hand-authored section in scripts/lib/test-ifa-determinism-family-cases.sh -- add one (fixtures + require_*_lib drive/assert-shape needles), do not silently skip it"
done < <(ifa_family_registry_names)
for family in "${!ifa_det_family_cases_hand_authored[@]}"; do
	[[ -n "${ifa_det_family_cases_seen_shared_cell[${family}]+set}" ]] \
		|| fail "family registry totality: this module hand-authors a shared-cell section for '${family}' which ${registry_family_lib} no longer declares shared_cell=1 for -- remove the stale section or confirm the family was genuinely dropped from the shared cell"
done

# No leftover literal per-family drive/assert call survives outside the
# loop. A stray one would double-drive/assert that family in every N-loop
# cell, silently changing what every digest in the matrix covers -- exactly
# the defect class a half-finished refactor leaves behind. The four drive
# functions and SQL's assert function are never legitimately called by name
# in the gate script anymore (dispatch is entirely through the registry's
# function-name table), so their absence is asserted outright. rationale's
# bare assert function is included in that same blanket list: unlike
# code_calls/documentation, rationale has no legitimate bare-name call
# anywhere else in the gate -- its post-delta re-assertion uses the
# DIFFERENT function ifa_rationale_assert_delta_truth, and its durable-
# lifecycle assertion (ifa_rationale_assert_work_counts, the one deliberate
# exception noted above) is also a different function name, so neither can
# collide with this pattern.
# codeowners joins the blanket list on the same footing as the other drives:
# #6160 gave this family a shared-cell drive, this change removed its inline
# call along with the other four, and it has no legitimate bare-name drive
# anywhere else in the gate (`rg -c 'ifa_codeowners_drive "'` on the gate
# returns 0). It was missed when the family landed mid-branch.
for leftover_fn in ifa_det_drive_sql_baseline ifa_code_call_drive ifa_documentation_drive ifa_rationale_drive \
	ifa_codeowners_drive ifa_det_assert_sql_baseline ifa_rationale_assert; do
	if rg --fixed-strings --quiet -- "${leftover_fn} \"" "${script}"; then
		fail "leftover literal per-family call survives outside the registry loop: ${leftover_fn} (would double-drive/assert that family and change what every N-loop digest covers)"
	fi
done
# code_calls and documentation each keep exactly ONE legitimate bare-name
# call outside the loop: the post-delta re-assertion after the shared SQL +
# rationale delta replay (asserted to run in order above). A second
# occurrence would be a genuine leftover double-assert; a blanket-absence
# check would wrongly flag the legitimate one.
code_call_assert_count="$(rg --count --fixed-strings -- 'ifa_code_call_assert "' "${script}")"
[[ "${code_call_assert_count}" -eq 1 ]] \
	|| fail "expected exactly 1 occurrence of ifa_code_call_assert (the post-delta re-assertion) outside the registry loop; found ${code_call_assert_count} -- an extra occurrence would double-assert this family in every N-loop cell"
documentation_assert_count="$(rg --count --fixed-strings -- 'ifa_documentation_assert "' "${script}")"
[[ "${documentation_assert_count}" -eq 1 ]] \
	|| fail "expected exactly 1 occurrence of ifa_documentation_assert (the post-delta re-assertion) outside the registry loop; found ${documentation_assert_count} -- an extra occurrence would double-assert this family in every N-loop cell"
# codeowners has the same single legitimate post-delta re-assertion shape as
# the two above, so it gets a count pin rather than a place in the blanket
# list.
codeowners_assert_count="$(rg --count --fixed-strings -- 'ifa_codeowners_assert "' "${script}")"
[[ "${codeowners_assert_count}" -eq 1 ]] \
	|| fail "expected exactly 1 occurrence of ifa_codeowners_assert (the post-delta re-assertion) outside the registry loop; found ${codeowners_assert_count} -- an extra occurrence would double-assert this family in every N-loop cell"
}
