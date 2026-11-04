#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# test-golden-corpus-delta-reader.sh — static contract for the #10022
# delta-reader leg, sourced by scripts/test-verify-golden-corpus-gate.sh.
# Requires: repo_root, script (the orchestrator path), snapshot, and fail().
# shellcheck disable=SC2154,SC2016

delta_reader_lib="${repo_root}/scripts/lib/golden-corpus-delta-reader.sh"
[[ -f "${delta_reader_lib}" ]] || fail "missing delta-reader lib: ${delta_reader_lib}"
bash -n "${delta_reader_lib}" || fail "golden-corpus-delta-reader.sh has a syntax error"

# The leg is wired at all three points; dropping any one makes the
# delta-readers phase fail its stage check or never run.
for delta_reader_call in golden_delta_reader_collect_full golden_delta_reader_run_delta_leg \
	"build_bin collector-git" "delta-union,delta-readers"; do
	rg --fixed-strings --quiet -- "${delta_reader_call}" "${script}" \
		|| fail "orchestrator must call ${delta_reader_call} (#10022 delta-reader leg)"
done
# The full is collected after the first drain and before the maintenance
# passes: a new repository changes the repository catalog, so only a
# corpus-wide maintenance pass publishes its backward evidence, and a full
# collected earlier wedges the first drain (catalog_changed).
delta_reader_first_drain_line="$(rg --line-number --fixed-strings -- 'die "first drain pass failed"' "${script}" | cut -d: -f1)"
delta_reader_collect_line="$(rg --line-number '^golden_delta_reader_collect_full' "${script}" | cut -d: -f1)"
delta_reader_maintenance_line="$(rg --line-number '^run_maintenance_drain_cycles' "${script}" | cut -d: -f1)"
[[ -n "${delta_reader_first_drain_line}" && -n "${delta_reader_collect_line}" && -n "${delta_reader_maintenance_line}" &&
	"${delta_reader_collect_line}" -gt "${delta_reader_first_drain_line}" &&
	"${delta_reader_collect_line}" -lt "${delta_reader_maintenance_line}" ]] \
	|| fail "golden_delta_reader_collect_full must run after the first drain and before run_maintenance_drain_cycles"
# The delta leg runs after the graph_query window opens and brackets itself
# out of it, so its syncs and drain are never billed to api/mcp startup.
delta_reader_window_line="$(rg --line-number --fixed-strings -- 'phase_graph_query_start="${pipeline_end}"' "${script}" | cut -d: -f1)"
delta_reader_leg_line="$(rg --line-number '^golden_delta_reader_run_delta_leg' "${script}" | cut -d: -f1)"
[[ -n "${delta_reader_window_line}" && -n "${delta_reader_leg_line}" && "${delta_reader_leg_line}" -gt "${delta_reader_window_line}" ]] \
	|| fail "golden_delta_reader_run_delta_leg must run after phase_graph_query_start is set"
for delta_reader_stamp in 'phase_graph_query_excluded_starts+=("$(date +%s)")' 'phase_graph_query_excluded_ends+=("$(date +%s)")'; do
	[[ "$(rg --count --fixed-strings -- "${delta_reader_stamp}" "${delta_reader_lib}")" == "1" ]] \
		|| fail "golden_delta_reader_run_delta_leg must carry exactly one ${delta_reader_stamp}"
done

# The lib's scope id, the snapshot's scope and repository ids, and the id
# CanonicalRepositoryID derives from the synthesized remote must agree, or the
# phase reads an empty scope and fails as vacuous.
delta_reader_slug="$(rg --only-matching --replace '$1' '^golden_delta_reader_slug="([^"]+)"' "${delta_reader_lib}")"
delta_reader_lib_scope="$(rg --only-matching --replace '$1' '^golden_delta_reader_scope_id="([^"]+)"' "${delta_reader_lib}")"
delta_reader_hash="$(printf '%s' "https://github.com/${delta_reader_slug}" | sha1sum | cut -c1-8)"
delta_reader_repo_id="repository:r_${delta_reader_hash}"
[[ "${delta_reader_lib_scope}" == "git-repository-scope:${delta_reader_repo_id}" ]] \
	|| fail "delta-reader lib scope ${delta_reader_lib_scope} does not match the ${delta_reader_slug} remote id ${delta_reader_repo_id}"
[[ "$(jq -er '.graph.delta_reader_assertions.scope_id' "${snapshot}")" == "${delta_reader_lib_scope}" ]] \
	|| fail "B-12 delta_reader_assertions.scope_id must equal the lib's ${delta_reader_lib_scope}"
[[ "$(jq -er '.graph.delta_reader_assertions.repository_id' "${snapshot}")" == "${delta_reader_repo_id}" ]] \
	|| fail "B-12 delta_reader_assertions.repository_id must equal ${delta_reader_repo_id}"

# The delta must stay a one-file change under the cumulative-delta growth
# valve (touched < 0.5 of the base's tracked files), or the second sync takes
# a full snapshot and the delta stage fails.
delta_reader_base_files="$(rg --files --hidden "${repo_root}/tests/fixtures/golden_delta_union/base" | wc -l)"
delta_reader_delta_files="$(rg --files --hidden "${repo_root}/tests/fixtures/golden_delta_union/delta" | wc -l)"
(( delta_reader_delta_files * 2 < delta_reader_base_files )) \
	|| fail "delta-reader delta/ touches ${delta_reader_delta_files} of ${delta_reader_base_files} base files; it must stay under half"
