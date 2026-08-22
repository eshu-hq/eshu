#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# fail(), fixtures_lib, script, repo_root, and _ifa_det_unique_code_match_line
# are all defined by scripts/test-verify-ifa-determinism.sh (or its earlier
# sourced modules) before this file is sourced; shellcheck cannot see that
# from this file alone.
# Maintenance-backed family static structural cases for
# scripts/test-verify-ifa-determinism.sh, split out of
# scripts/lib/test-ifa-determinism-family-cases.sh to keep that file below
# the repository's 500-line cap (mirroring the fault-injection sibling's own
# mechanism split, e.g. test-ifa-fault-injection-shard-cases.sh's extraction
# of test-ifa-fault-injection-deployable-unit-ordering-cases.sh). Covers the
# repo_dependency (#5999) standalone cell and the workload_dependency (#6003)
# matrix cell -- both maintenance-backed families whose live proof needs a
# bootstrap-index maintenance pass, unlike the plain reducer families in the
# parent module. run_ifa_determinism_maintenance_family_cases is called
# from inside run_ifa_determinism_family_cases's body, in the same position
# this code used to occupy inline, so the repo_dependency/workload_dependency
# narrative still reads in family-lifecycle order relative to
# deployable_unit_edges immediately before it and rationale_edges immediately
# after.
run_ifa_determinism_maintenance_family_cases() {
# repo_dependency is a maintenance-backed standalone proof. It must carry the
# committed seven-edge oracle, rather than being folded into the N matrix.
require_fixture "repo-dependency cassette path" "testdata/cassettes/repodependency/ifa-repo-dependency-family.json"
require_fixture "repo-dependency expected-edge set path" "go/internal/ifa/testdata/repodependency/ifa-repo-dependency-family-expected-edges.json"
require_fixture "repo-dependency cassette existence guard" "repo-dependency cassette not found"
require_fixture "repo-dependency expected-edge set existence guard" "repo-dependency expected-edge set not found"
# Bind and validate the live variables, not only their source text. A
# wrong-but-existing sibling fixture is just as unsafe as a missing path: both
# make the static mirror certify a different artifact than the live cell reads.
# shellcheck source=scripts/lib/ifa_family_fixtures.sh
source "${fixtures_lib}"
repo_dependency_expected_cassette_path="${repo_root}/testdata/cassettes/repodependency/ifa-repo-dependency-family.json"
repo_dependency_expected_edges_path="${repo_root}/go/internal/ifa/testdata/repodependency/ifa-repo-dependency-family-expected-edges.json"
[[ "${repo_dependency_cassette}" == "${repo_dependency_expected_cassette_path}" && -f "${repo_dependency_cassette}" ]] \
	|| fail "repo_dependency cassette binding is not the committed family cassette: ${repo_dependency_cassette}"
[[ "${repo_dependency_expected_edges}" == "${repo_dependency_expected_edges_path}" && -f "${repo_dependency_expected_edges}" ]] \
	|| fail "repo_dependency expected-edge binding is not the committed seven-edge oracle: ${repo_dependency_expected_edges}"
repo_dependency_live_lib="${repo_root}/scripts/lib/ifa_repo_dependency_live.sh"
[[ -f "${repo_dependency_live_lib}" ]] || fail "missing repo_dependency live helper"
for repo_needle in 'ifa_repo_dependency_live_run_standalone_cell()' 'ifa_repo_dependency_live_drive' \
	'ifa_repo_dependency_live_drain pre' 'ifa_repo_dependency_live_assert_gated' \
	'materialize-platform-prerequisite' 'ifa_repo_dependency_live_run_maintenance_pass primary' \
	'ifa_repo_dependency_live_drain post' 'ifa_repo_dependency_live_assert_readiness_state' \
	'-domain repo_dependency -expected "${expected_edges}"'; do
	rg --fixed-strings --quiet -- "${repo_needle}" "${repo_dependency_live_lib}" \
		|| fail "repo_dependency live helper missing ${repo_needle}"
done
repo_standalone_function="$(rg -U --pcre2 --only-matching -- \
	'(?ms)^ifa_repo_dependency_live_run_standalone_cell\(\) \{.*?^\}' \
	"${repo_dependency_live_lib}")"
[[ -n "${repo_standalone_function}" ]] \
	|| fail "repo_dependency standalone function could not be isolated for ordering checks"
[[ "${repo_standalone_function}" != *"postgres_dsn"* ]] \
	|| fail "repo_dependency standalone helper carries an unused postgres_dsn parameter"
repo_drive_line="$(printf '%s\n' "${repo_standalone_function}" | rg -n --fixed-strings -- 'ifa_repo_dependency_live_drive' | cut -d: -f1)"
repo_pre_drain_line="$(printf '%s\n' "${repo_standalone_function}" | rg -n --fixed-strings -- 'ifa_repo_dependency_live_drain pre' | cut -d: -f1)"
repo_gated_log_line="$(printf '%s\n' "${repo_standalone_function}" | rg -n --fixed-strings -- '"reducer-repo-dependency-pre" 1' | cut -d: -f1)"
repo_zero_edge_line="$(printf '%s\n' "${repo_standalone_function}" | rg -n --fixed-strings -- 'ifa_repo_dependency_live_assert_gated' | cut -d: -f1)"
repo_prerequisite_line="$(printf '%s\n' "${repo_standalone_function}" | rg -n --fixed-strings -- 'ifa_repo_dependency_live_materialize_platform_prerequisite' | cut -d: -f1)"
repo_maintenance_line="$(printf '%s\n' "${repo_standalone_function}" | rg -n --fixed-strings -- 'ifa_repo_dependency_live_run_maintenance_pass primary' | cut -d: -f1)"
repo_post_drain_line="$(printf '%s\n' "${repo_standalone_function}" | rg -n --fixed-strings -- 'ifa_repo_dependency_live_drain post' | cut -d: -f1)"
repo_open_log_line="$(printf '%s\n' "${repo_standalone_function}" | rg -n --fixed-strings -- '"reducer-repo-dependency-post" 0' | cut -d: -f1)"
repo_exact_assert_line="$(printf '%s\n' "${repo_standalone_function}" | rg -n --fixed-strings -- 'ifa_repo_dependency_live_assert "${bin_dir}" "${expected_edges}"' | cut -d: -f1)"
repo_order_lines="${repo_drive_line} ${repo_pre_drain_line} ${repo_gated_log_line} ${repo_zero_edge_line} ${repo_prerequisite_line} ${repo_maintenance_line} ${repo_post_drain_line} ${repo_open_log_line} ${repo_exact_assert_line}"
[[ "${repo_order_lines}" =~ ^[0-9]+\ [0-9]+\ [0-9]+\ [0-9]+\ [0-9]+\ [0-9]+\ [0-9]+\ [0-9]+\ [0-9]+$ \
	&& "${repo_drive_line}" -lt "${repo_pre_drain_line}" \
	&& "${repo_pre_drain_line}" -lt "${repo_gated_log_line}" \
	&& "${repo_gated_log_line}" -lt "${repo_zero_edge_line}" \
	&& "${repo_zero_edge_line}" -lt "${repo_prerequisite_line}" \
	&& "${repo_prerequisite_line}" -lt "${repo_maintenance_line}" \
	&& "${repo_maintenance_line}" -lt "${repo_post_drain_line}" \
	&& "${repo_post_drain_line}" -lt "${repo_open_log_line}" \
	&& "${repo_open_log_line}" -lt "${repo_exact_assert_line}" ]] \
	|| fail "repo_dependency standalone lifecycle must stay drive -> pre-drain -> gated log/zero-edge proof -> Platform prerequisite -> maintenance -> post-drain -> open log -> exact assertion; got lines ${repo_order_lines}"
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
repo_standalone_line="$(rg -n --fixed-strings -- '"${bin_dir}" "${repo_dependency_cassette}" "${repo_dependency_expected_edges}"' "${script}" | cut -d: -f1 || true)"
compare_digests_line="$(rg -n --fixed-strings -- 'log "compare digests across N=${worker_counts[*]}"' "${script}" | cut -d: -f1 || true)"
[[ "${n_loop_done_line}" =~ ^[0-9]+$ && "${standalone_cell_line}" =~ ^[0-9]+$ && "${compare_digests_line}" =~ ^[0-9]+$ \
	&& "${n_loop_done_line}" -lt "${standalone_cell_line}" && "${standalone_cell_line}" -lt "${compare_digests_line}" ]] \
	|| fail "the deployable_unit_edges standalone cell must run after the N-loop closes and before the digest comparison"
[[ "${repo_standalone_line}" =~ ^[0-9]+$ && "${standalone_cell_line}" -lt "${repo_standalone_line}" && "${repo_standalone_line}" -lt "${compare_digests_line}" ]] \
	|| fail "repo_dependency standalone cell must run after deployable_unit and before digest comparison"

# workload_dependency is maintenance-backed, but unlike repo_dependency it is
# part of the worker-count matrix. Every fresh N stack must drive it with that
# cell's worker count and exact-assert its automatic replay before graph-dump,
# so the workload-owned edges participate in the compared canonical digest.
require_fixture "workload-dependency cassette path" "testdata/cassettes/workloaddependency/ifa-workload-dependency-family.json"
require_fixture "workload-dependency expected-edge set path" "go/internal/ifa/testdata/workloaddependency/ifa-workload-dependency-family-expected-edges.json"
require_fixture "workload-dependency repo-prerequisite expected-edge set path" "go/internal/ifa/testdata/workloaddependency/ifa-workload-dependency-family-repo-prerequisite-expected-edges.json"
require_fixture "workload-dependency cassette existence guard" "workload-dependency cassette not found"
require_fixture "workload-dependency expected-edge existence guard" "workload-dependency expected-edge set not found"
require_fixture "workload-dependency repo-prerequisite existence guard" "workload-dependency repo-prerequisite expected-edge set not found"
workload_dependency_live_lib="${repo_root}/scripts/lib/ifa_workload_dependency_live.sh"
[[ -f "${workload_dependency_live_lib}" ]] || fail "missing workload_dependency live helper"
for workload_needle in 'ifa_workload_dependency_live_run_matrix_cell()' \
	'eshu-ifa" drive -cassette "${cassette}" -workers "${workers}"' \
	'ifa_workload_dependency_live_drain pre' 'ifa_repo_dependency_live_run_maintenance_pass workload-dependency' \
	'ifa_workload_dependency_live_drain repo' 'ifa_workload_dependency_live_assert_repo_prerequisite' \
	'ifa_workload_dependency_live_assert_owned_absent' '-domain workload_dependency -expected "${expected_edges}"'; do
	rg --fixed-strings --quiet -- "${workload_needle}" "${workload_dependency_live_lib}" \
		|| fail "workload_dependency live helper missing ${workload_needle}"
done
workload_matrix_body="$(awk '/^ifa_workload_dependency_live_run_matrix_cell\(\)/,/^}/' "${workload_dependency_live_lib}")"
if printf '%s\n' "${workload_matrix_body}" | rg --quiet --fixed-strings -- 'ifa_workload_dependency_live_reopen_materialization'; then
	fail "workload_dependency matrix cell must rely on automatic replay, not manual reopen"
fi
workload_absent_line="$(printf '%s\n' "${workload_matrix_body}" | rg -n --fixed-strings -- 'ifa_workload_dependency_live_assert_owned_absent "${bin_dir}" pre-maintenance' | cut -d: -f1 || true)"
workload_maintenance_line="$(printf '%s\n' "${workload_matrix_body}" | rg -n --fixed-strings -- 'ifa_repo_dependency_live_run_maintenance_pass workload-dependency' | cut -d: -f1 || true)"
workload_repo_drain_line="$(printf '%s\n' "${workload_matrix_body}" | rg -n --fixed-strings -- 'ifa_workload_dependency_live_drain repo' | cut -d: -f1 || true)"
workload_repo_assert_line="$(printf '%s\n' "${workload_matrix_body}" | rg -n --fixed-strings -- 'ifa_workload_dependency_live_assert_repo_prerequisite' | cut -d: -f1 || true)"
workload_exact_assert_line="$(printf '%s\n' "${workload_matrix_body}" | rg -n --fixed-strings -- 'ifa_workload_dependency_live_assert "${bin_dir}" "${expected_edges}"' | cut -d: -f1 || true)"
workload_order_lines="${workload_absent_line} ${workload_maintenance_line} ${workload_repo_drain_line} ${workload_repo_assert_line} ${workload_exact_assert_line}"
[[ "${workload_order_lines}" =~ ^[0-9]+\ [0-9]+\ [0-9]+\ [0-9]+\ [0-9]+$ \
	&& "${workload_absent_line}" -lt "${workload_maintenance_line}" \
	&& "${workload_maintenance_line}" -lt "${workload_repo_drain_line}" \
	&& "${workload_repo_drain_line}" -lt "${workload_repo_assert_line}" \
	&& "${workload_repo_assert_line}" -lt "${workload_exact_assert_line}" ]] \
	|| fail "workload_dependency matrix lifecycle must stay absent -> maintenance -> repo drain -> repo exact assertion -> automatic workload exact assertion; got lines ${workload_order_lines}"
workload_matrix_line="$(_ifa_det_unique_code_match_line 'ifa_workload_dependency_live_run_matrix_cell' "${script}")" \
	|| fail "workload_dependency matrix call must have exactly one executable anchor"
workload_digest_assert_line="$(_ifa_det_unique_code_match_line 'ifa_workload_dependency_live_assert "${bin_dir}" "${workload_dependency_expected_edges}"' "${script}")" \
	|| fail "workload_dependency final assertion must have exactly one executable anchor"
graph_dump_line="$(_ifa_det_unique_code_match_line 'graph-dump -out "${work_dir}/graph-n${n}.dump"' "${script}")" \
	|| fail "canonical graph dump must have exactly one executable anchor"
digest_assignment_line="$(_ifa_det_unique_code_match_line 'digest_n="$("${bin_dir}/eshu-ifa" graph-dump -digest' "${script}")" \
	|| fail "compared digest assignment must have exactly one executable anchor"
workload_matrix_call="$(awk -v start="${workload_matrix_line}" 'NR >= start {print} /workload_dependency: N=.*matrix cell failed/{exit}' "${script}")"
workload_anchor_probe_dir="$(mktemp -d -t ifa-det-workload-anchor.XXXXXX)"
printf '# __ifa_workload_anchor__\n: <<\x27IFAEOF\x27\n__ifa_workload_anchor__\nIFAEOF\n' >"${workload_anchor_probe_dir}/decoys.sh"
printf '# __ifa_workload_anchor__\n: <<\x27IFAEOF\x27\n__ifa_workload_anchor__\nIFAEOF\n__ifa_workload_anchor__\n' >"${workload_anchor_probe_dir}/live.sh"
printf '__ifa_workload_anchor__\n__ifa_workload_anchor__\n' >"${workload_anchor_probe_dir}/duplicate.sh"
! _ifa_det_unique_code_match_line '__ifa_workload_anchor__' "${workload_anchor_probe_dir}/decoys.sh" >/dev/null \
	|| fail "comment/heredoc-only workload ordering anchors must not count"
[[ "$(_ifa_det_unique_code_match_line '__ifa_workload_anchor__' "${workload_anchor_probe_dir}/live.sh")" == "5" ]] \
	|| fail "workload ordering anchor extractor did not return the sole executable line"
! _ifa_det_unique_code_match_line '__ifa_workload_anchor__' "${workload_anchor_probe_dir}/duplicate.sh" >/dev/null \
	|| fail "duplicate executable workload ordering anchors must fail closed"
rm -rf "${workload_anchor_probe_dir}"
workload_layout_is_digest_bound() {
	local loop_start="$1" matrix_line="$2" assert_line="$3" dump_line="$4" loop_done="$5" digest_line="$6"
	[[ "${matrix_line}" =~ ^[0-9]+$ && "${assert_line}" =~ ^[0-9]+$ && "${dump_line}" =~ ^[0-9]+$ && "${digest_line}" =~ ^[0-9]+$ \
		&& "${loop_start}" -lt "${matrix_line}" && "${matrix_line}" -lt "${assert_line}" \
		&& "${assert_line}" -lt "${dump_line}" && "${dump_line}" -lt "${digest_line}" \
		&& "${digest_line}" -lt "${loop_done}" ]]
}
workload_call_uses_current_workers() {
	printf '%s\n' "$1" | rg --quiet --fixed-strings -- '"${n}"'
}
workload_layout_is_digest_bound "${n_loop_for_line}" "${workload_matrix_line}" "${workload_digest_assert_line}" "${graph_dump_line}" "${n_loop_done_line}" "${digest_assignment_line}" \
	|| fail "workload_dependency matrix cell must run inside every N loop before canonical graph output"
workload_call_uses_current_workers "${workload_matrix_call}" \
	|| fail "workload_dependency matrix cell must receive the current worker count"
! workload_layout_is_digest_bound "${n_loop_for_line}" "$((n_loop_done_line + 1))" "${workload_digest_assert_line}" "${graph_dump_line}" "${n_loop_done_line}" "${digest_assignment_line}" \
	|| fail "post-loop workload placement mutation escaped the digest-bound guard"
! workload_layout_is_digest_bound "${n_loop_for_line}" "${workload_matrix_line}" "${workload_digest_assert_line}" "$((workload_digest_assert_line - 1))" "${n_loop_done_line}" "${digest_assignment_line}" \
	|| fail "pre-workload graph-dump mutation escaped the digest-bound guard"
! workload_layout_is_digest_bound "${n_loop_for_line}" "${workload_matrix_line}" "${workload_digest_assert_line}" "${graph_dump_line}" "${n_loop_done_line}" "$((workload_digest_assert_line - 1))" \
	|| fail "early compared-digest mutation escaped the digest-bound guard"
mutated_workload_call="${workload_matrix_call//'"${n}"'/'"1"'}"
! workload_call_uses_current_workers "${mutated_workload_call}" \
	|| fail "fixed-worker workload call mutation escaped the current-worker guard"
}
