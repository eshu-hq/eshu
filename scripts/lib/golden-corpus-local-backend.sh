#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# golden-corpus-local-backend.sh — issue #5594 golden-corpus coverage for a
# bare `backend "local" {}` block (no explicit `path`).
#
# A BackendLocal locator (go/internal/collector/terraformstate/backend_config_local.go)
# is an ABSOLUTE PATH built from the fixture repo's real git-checkout root, and
# this orchestrator stages every fixture inside a fresh `mktemp -d` working
# directory on every run (see corpus_dir/ESHU_FILESYSTEM_ROOT above), so that
# absolute path -- and therefore its scope_id (terraformstate.ScopeLocatorHash)
# -- differs on every run and cannot be a literal in either
# testdata/cassettes/terraformstate/supply-chain-demo.json or
# testdata/golden/e2e-20repo-snapshot.json. Both files instead carry the
# $LOCAL_BACKEND_SCOPE_ID$ / $LOCAL_BACKEND_LOCATOR_HASH$ sentinels.
#
# stage_local_backend_cassette() runs after bootstrap-index has committed the
# terraform_local_backend_demo fixture's `repository` fact (so its local_path
# is queryable) and before the cassette collectors replay. It:
#   1. reads that fixture's local_path from Postgres -- from fact_records
#      WHERE fact_kind = 'repository', the EXACT table/predicate
#      go/internal/storage/postgres/tfstate_backend_canonical.go's
#      repo_local_paths CTE reads at resolution time (COALESCE to source_uri,
#      ORDER BY observed_at DESC, fact_id DESC). An earlier version of this
#      query read ingestion_scopes.payload instead -- a different table that,
#      while populated from the same repositoryidentity.Metadata in the git
#      collector's normal path, is not what the resolver's canonical query
#      actually consults; reading the identical source the resolver uses
#      removes that gap rather than relying on the two staying in sync,
#   2. invokes golden-corpus-gate -print-local-backend-scope-id to compute the
#      same join-key formula production code uses (see
#      go/cmd/golden-corpus-gate/local_backend_scope_id.go),
#   3. writes a RUNTIME COPY of the terraformstate cassette with both
#      sentinels substituted, leaving the committed cassette untouched, and
#   4. sets local_backend_scope_id and local_backend_cassette_path (globals,
#      read by the caller): the former is passed to the final gate invocation
#      via -local-backend-scope-id (which performs the same substitution on
#      the B-12 snapshot's query shapes in-process), the latter replaces the
#      terraformstate collector's -cassette-file argument in the replay loop.
#      Every OTHER collector keeps using its original, unmodified, committed
#      cassette file.
#
# Requires (set by the caller before invocation): repo_root, bin_dir, log_dir,
# work_dir, the pg() and die() functions (golden-corpus-host-helpers.sh).
local_backend_fixture_repo_name="terraform_local_backend_demo"

stage_local_backend_cassette() {
	local repo_local_path
	repo_local_path="$(pg "
		SELECT COALESCE(payload->>'local_path', source_uri, '')
		FROM fact_records
		WHERE fact_kind = 'repository'
		  AND source_system = 'git'
		  AND payload->>'name' = '${local_backend_fixture_repo_name}'
		ORDER BY observed_at DESC, fact_id DESC
		LIMIT 1;
	" | tr -d '[:space:]')"
	[[ -n "${repo_local_path}" ]] || die "local-backend fixture '${local_backend_fixture_repo_name}' has no repository fact with a local_path after bootstrap-index"

	# Diagnostic only (issue #5594 round-trip debugging): print the repository
	# fact's own generation_id next to the active FILE fact's generation_id for
	# this same repo. tfstate_backend_canonical.go's repo_local_paths CTE joins
	# repo_local_paths.generation_id = backend.generation_id (backend =
	# the active 'file' fact carrying parsed_file_data.terraform_backends); if
	# these two printed values ever diverge, the canonical join silently drops
	# repo_local_path to '' and every BackendLocal candidate resolves to
	# ok=false (backendConfigLocalCandidate's repoLocalPath=="" guard).
	local repo_fact_generation active_file_generation
	repo_fact_generation="$(pg "
		SELECT generation_id FROM fact_records
		WHERE fact_kind = 'repository' AND source_system = 'git'
		  AND payload->>'name' = '${local_backend_fixture_repo_name}'
		ORDER BY observed_at DESC, fact_id DESC LIMIT 1;
	" | tr -d '[:space:]')"
	active_file_generation="$(pg "
		SELECT fact.generation_id
		FROM fact_records AS fact
		JOIN ingestion_scopes AS scope ON scope.scope_id = fact.scope_id AND scope.active_generation_id = fact.generation_id
		JOIN scope_generations AS generation ON generation.scope_id = fact.scope_id AND generation.generation_id = fact.generation_id
		WHERE fact.fact_kind = 'file' AND fact.source_system = 'git' AND generation.status = 'active'
		  AND fact.payload->>'repo_id' = (
		    SELECT payload->>'repo_id' FROM fact_records
		    WHERE fact_kind = 'repository' AND source_system = 'git'
		      AND payload->>'name' = '${local_backend_fixture_repo_name}'
		    ORDER BY observed_at DESC, fact_id DESC LIMIT 1
		  )
		  AND jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_backends') = 'array'
		  AND jsonb_array_length(fact.payload->'parsed_file_data'->'terraform_backends') > 0
		ORDER BY fact.observed_at DESC LIMIT 1;
	" | tr -d '[:space:]')"
	printf 'local-backend fixture repository fact generation_id=%s; active backend-bearing file fact generation_id=%s (must match for the canonical join to see repo_local_path)\n' \
		"${repo_fact_generation:-<none>}" "${active_file_generation:-<none>}"

	local_backend_scope_id="$("${bin_dir}/eshu-golden-corpus-gate" -print-local-backend-scope-id="${repo_local_path}")"
	[[ -n "${local_backend_scope_id}" ]] || die "failed to compute the local-backend fixture's scope_id"
	local local_backend_hash="${local_backend_scope_id##*:}"
	printf 'local-backend fixture repo_local_path=%s scope_id=%s\n' "${repo_local_path}" "${local_backend_scope_id}"

	local committed_cassette="${repo_root}/testdata/cassettes/terraformstate/${cassette_recording}"
	local_backend_cassette_path="${work_dir}/terraformstate-local-backend-${cassette_recording}"
	sed \
		-e "s|\$LOCAL_BACKEND_SCOPE_ID\$|${local_backend_scope_id}|g" \
		-e "s|\$LOCAL_BACKEND_LOCATOR_HASH\$|${local_backend_hash}|g" \
		"${committed_cassette}" >"${local_backend_cassette_path}"
}

# print_local_backend_drift_diagnostics() answers, in order, the four
# questions that localize a zero-finding local-backend scope to one specific
# failure point in the sync -> discover -> parse -> emit facts -> enqueue
# work -> reducer -> query surface pipeline (issue #5594 second live-gate
# round trip: candidate DERIVATION was proven correct offline by
# TestEvaluateBackendConfigLocalBackendThroughRealParser, and the
# local_path-source alignment in stage_local_backend_cassette above did not
# fix the live failure, so the next round trip needs this instead of another
# hypothesis). Call once, after run_maintenance_drain_cycles returns (the
# config_state_drift intent for a cassette-landed scope is only enqueueable
# once bootstrap-index's Phase 3.5 re-runs during that loop, not before).
#
# Requires: local_backend_scope_id (set by stage_local_backend_cassette,
# called earlier in the same script run), pg(), log_dir. jq is required (the
# suppression-proof helper elsewhere in this gate already depends on it, so
# this adds no new tool requirement).
#
# Intent: KEEP THIS PERMANENTLY. It costs one script block and a handful of
# bounded queries against a corpus this small, and it turns "which of five
# failure points broke" from a guessing game back into a single gate run —
# the same value a live-gate operator would want for any other domain this
# gate exercises, not just while #5594 is open.
print_local_backend_drift_diagnostics() {
	printf '\n--- local-backend drift diagnostics (scope_id=%s) ---\n' "${local_backend_scope_id}"

	printf '\n[1] did the cassette entry land? (ingestion_scopes + terraform_state_snapshot/terraform_state_candidate facts)\n'
	pg "
		SELECT 'ingestion_scopes' AS source, scope_id, active_generation_id, status
		FROM ingestion_scopes WHERE scope_id = '${local_backend_scope_id}';
	" || true
	pg "
		SELECT 'fact_records' AS source, fact_kind, generation_id, is_tombstone, observed_at
		FROM fact_records
		WHERE scope_id = '${local_backend_scope_id}'
		  AND fact_kind IN ('terraform_state_snapshot', 'terraform_state_candidate')
		ORDER BY fact_kind, observed_at DESC;
	" || true

	printf '\n[2] was a config_state_drift intent enqueued? (fact_work_items rows for this scope + domain)\n'
	pg "
		SELECT work_item_id, status, attempt_count, failure_class, failure_message, updated_at
		FROM fact_work_items
		WHERE scope_id = '${local_backend_scope_id}' AND domain = 'config_state_drift'
		ORDER BY updated_at DESC;
	" || true

	printf '\n[3] did it execute, and what did it produce? (reducer_terraform_config_state_drift_finding facts, by outcome/drift_kind)\n'
	pg "
		SELECT payload->>'outcome' AS outcome, COALESCE(payload->>'drift_kind', '<none>') AS drift_kind, count(*)
		FROM fact_records
		WHERE scope_id = '${local_backend_scope_id}'
		  AND fact_kind = 'reducer_terraform_config_state_drift_finding'
		GROUP BY 1, 2
		ORDER BY 1, 2;
	" || true

	printf '\n[4] if zero findings above: was ownership resolved at all? (reducer structured log, both outcomes are logged even when nothing is written to fact_records)\n'
	local history="${log_dir}/reducer-config-state-drift-history.log"
	if [[ ! -f "${history}" ]]; then
		printf 'no reducer log history file found at %s\n' "${history}"
	elif ! command -v jq >/dev/null 2>&1; then
		printf 'jq not available; raw grep of %s for this scope_id:\n' "${history}"
		grep -F "${local_backend_scope_id}" "${history}" || printf '(no lines matched)\n'
	else
		local matches
		matches="$(jq -c --arg scope "${local_backend_scope_id}" '
			select(.scope_id == $scope and (.message == "drift candidate rejected" or .message == "drift candidate resolved via defaulted locator"))
		' "${history}" 2>/dev/null)"
		if [[ -z "${matches}" ]]; then
			printf 'no "drift candidate rejected" or "drift candidate resolved via defaulted locator" log line found for this scope_id across any drain pass. This means either the intent was never claimed (see [2]), or the handler returned before either log call -- check for "resolver_unavailable"/"evidence_loader_unavailable" failure_class lines too:\n'
			jq -c --arg scope "${local_backend_scope_id}" 'select(.scope_id == $scope)' "${history}" 2>/dev/null || true
		else
			printf '%s\n' "${matches}"
		fi
	fi
	printf -- '--- end local-backend drift diagnostics ---\n\n'
}
