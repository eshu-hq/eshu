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
	# Diagnostic only: guarded with the same `out="$(cmd)" && status=0 ||
	# status=$?` idiom pg_diag uses below, so a `pg` connection/query error
	# here cannot abort the whole gate under this script's `set -euo
	# pipefail` -- a bare assignment would (see the HARD REQUIREMENT
	# docstring on print_local_backend_drift_diagnostics above; this is the
	# same failure class, just in the staging function instead of the
	# reporting one). Zero rows is a legitimate, non-error outcome (`pg`'s
	# `-tA` prints nothing and the pipeline still exits 0); a real query
	# failure is reported distinctly instead of being silently folded into
	# "<none>".
	local repo_fact_generation active_file_generation
	local repo_fact_generation_status active_file_generation_status
	repo_fact_generation="$(pg "
		SELECT generation_id FROM fact_records
		WHERE fact_kind = 'repository' AND source_system = 'git'
		  AND payload->>'name' = '${local_backend_fixture_repo_name}'
		ORDER BY observed_at DESC, fact_id DESC LIMIT 1;
	" | tr -d '[:space:]')" && repo_fact_generation_status=0 || repo_fact_generation_status=$?
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
	" | tr -d '[:space:]')" && active_file_generation_status=0 || active_file_generation_status=$?
	if [[ "${repo_fact_generation_status}" -ne 0 || "${active_file_generation_status}" -ne 0 ]]; then
		printf 'local-backend fixture repository/active-file generation_id diagnostic query FAILED (non-fatal, diagnostic only): repo_fact_generation exit=%s value=%s; active_file_generation exit=%s value=%s\n' \
			"${repo_fact_generation_status}" "${repo_fact_generation:-<none>}" \
			"${active_file_generation_status}" "${active_file_generation:-<none>}"
	else
		printf 'local-backend fixture repository fact generation_id=%s; active backend-bearing file fact generation_id=%s (must match for the canonical join to see repo_local_path)\n' \
			"${repo_fact_generation:-<none>}" "${active_file_generation:-<none>}"
	fi

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
# called earlier in the same script run), pg(), log_dir.
#
# Intent: KEEP THIS PERMANENTLY. It costs one script block and a handful of
# bounded queries against a corpus this small, and it turns "which of five
# failure points broke" from a guessing game back into a single gate run —
# the same value a live-gate operator would want for any other domain this
# gate exercises, not just while #5594 is open.
#
# HARD REQUIREMENT (issue #5594 review): this block must never be able to
# fail the gate or short-circuit the phases that run after it. The first
# version violated this twice, both from the same root cause -- a bare
# `var="$(cmd)"` assignment, under this script's `set -euo pipefail`, aborts
# the WHOLE gate the instant `cmd` exits non-zero, and the caller never finds
# out why (stderr goes nowhere useful, and a downstream `|| true` on a
# DIFFERENT line cannot retroactively protect it): a blanket `pg "..." ||
# true` on the query calls hid real query failures behind what looked like
# "zero rows" (pg()'s `-tA` prints nothing for a genuine empty result set
# too, so the two were indistinguishable), and one unguarded
# `matches="$(jq ...)"` assignment had no protection at all and killed the
# run before the closing print, let alone the assertion phase after this
# function returns. Every external command below is captured via the
# `out="$(cmd)" && status=0 || status=$?` idiom specifically because a bare
# assignment is NOT exempt from `set -e` but an assignment on the `&&`/`||`
# side of a tested command IS -- see https://mywiki.wooledge.org/BashFAQ/105.
print_local_backend_drift_diagnostics() {
	printf '\n--- local-backend drift diagnostics (scope_id=%s) ---\n' "${local_backend_scope_id}"
	pg_diag "[1a] ingestion_scopes row for this scope_id (did the cassette land, and is its generation active?)" "
		SELECT scope_id, active_generation_id, status
		FROM ingestion_scopes WHERE scope_id = '${local_backend_scope_id}';
	"
	pg_diag "[1b] terraform_state_snapshot / terraform_state_candidate facts for this scope_id" "
		SELECT fact_kind, generation_id, is_tombstone, observed_at
		FROM fact_records
		WHERE scope_id = '${local_backend_scope_id}'
		  AND fact_kind IN ('terraform_state_snapshot', 'terraform_state_candidate')
		ORDER BY fact_kind, observed_at DESC;
	"
	pg_diag "[2] fact_work_items rows for (scope_id, domain=config_state_drift) -- was an intent enqueued and claimed?" "
		SELECT work_item_id, status, attempt_count, failure_class, failure_message, updated_at
		FROM fact_work_items
		WHERE scope_id = '${local_backend_scope_id}' AND domain = 'config_state_drift'
		ORDER BY updated_at DESC;
	"
	pg_diag "[3] reducer_terraform_config_state_drift_finding facts for this scope_id, by outcome/drift_kind -- what did it produce?" "
		SELECT payload->>'outcome' AS outcome, COALESCE(payload->>'drift_kind', '<none>') AS drift_kind, count(*)
		FROM fact_records
		WHERE scope_id = '${local_backend_scope_id}'
		  AND fact_kind = 'reducer_terraform_config_state_drift_finding'
		GROUP BY 1, 2
		ORDER BY 1, 2;
	"
	log_diag_local_backend_ownership_lines
	printf -- '--- end local-backend drift diagnostics ---\n\n'
	return 0
}

# pg_diag <label> <sql> runs one diagnostic SQL query and prints exactly one
# of three DISTINGUISHABLE outcomes, labelled, so "the query legitimately
# returned nothing" is never confused with "the query itself failed" (issue
# #5594 review: a blanket `|| true` made these look identical, since pg()'s
# `-tA` flag already prints nothing for a genuine empty result set). Captures
# pg()'s exit status via the `&&`/`||`-guarded assignment idiom rather than a
# bare one, so a failing query cannot trip this script's `set -e` and abort
# the run. Always returns 0.
pg_diag() {
	local label="$1" sql="$2"
	local output status
	output="$(pg "${sql}" 2>&1)" && status=0 || status=$?
	if [[ "${status}" -ne 0 ]]; then
		printf '%s: QUERY FAILED (exit %s):\n%s\n' "${label}" "${status}" "${output}"
	elif [[ -z "${output}" ]]; then
		printf '%s: (0 rows)\n' "${label}"
	else
		printf '%s:\n%s\n' "${label}" "${output}"
	fi
	return 0
}

# log_diag_local_backend_ownership_lines answers diagnostic [4]: if [3] shows
# zero findings, did ownership resolve at all? go/internal/reducer/terraform_config_state_drift.go
# JSON-logs (slog.LevelInfo, always enabled -- internal/telemetry/logging.go)
# either "drift candidate rejected" (failure_class/rejection.reason -- e.g.
# no_config_repo_owns_backend) or "drift candidate resolved via defaulted
# locator" (ownership resolved, so a downstream zero-admitted-candidates
# outcome is the more precise next question) for every scope the handler
# actually processes. Uses grep, not jq: grep never errors on a line that
# happens not to be valid JSON (a real risk in a log this script does not
# fully control the content of -- see the docstring above), so it cannot
# repeat the failure mode this diagnostic itself exists to catch. Every `|| true`
# below is INSIDE the command substitution, guarding the one command that can
# fail, not appended after the closing `)"` -- a trailing `|| true` on the
# assignment line does not protect the assignment itself from `set -e`.
log_diag_local_backend_ownership_lines() {
	local label="[4] reducer structured log for this scope_id (ownership resolved vs rejected -- both outcomes are logged even when [3] writes nothing to fact_records)"
	local history="${log_dir}/reducer-config-state-drift-history.log"
	if [[ ! -f "${history}" ]]; then
		printf '%s: (no reducer log history file at %s)\n' "${label}" "${history}"
		return 0
	fi
	local scoped_lines
	scoped_lines="$(grep -F "${local_backend_scope_id}" "${history}" 2>/dev/null || true)"
	if [[ -z "${scoped_lines}" ]]; then
		printf '%s: (0 log lines mention this scope_id across any drain pass -- the intent was likely never claimed; see [2])\n' "${label}"
		return 0
	fi
	local outcome_lines
	outcome_lines="$(printf '%s\n' "${scoped_lines}" | grep -E 'drift candidate (rejected|resolved via defaulted locator)' 2>/dev/null || true)"
	if [[ -z "${outcome_lines}" ]]; then
		printf '%s: this scope_id appears in the reducer log, but never on a "drift candidate rejected"/"drift candidate resolved via defaulted locator" line. Every log line mentioning this scope_id (check for resolver_unavailable/evidence_loader_unavailable/scope_not_state_snapshot failure_class too):\n%s\n' \
			"${label}" "${scoped_lines}"
	else
		printf '%s:\n%s\n' "${label}" "${outcome_lines}"
	fi
	return 0
}
