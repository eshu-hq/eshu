#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# golden-corpus-delta-reader.sh — the #10022 delta-reader leg of the golden
# corpus gate (scripts/verify-golden-corpus-gate.sh).
#
# WHY A GIT ORIGIN AND NOT A CASSETTE
#
# Every other corpus repository is collected by eshu-bootstrap-index in
# filesystem mode, and filesystem mode never produces a git delta generation:
# its selection passes no deltas (selection_native.go), and bootstrap-index
# wires no delta baseline resolver. Cassette replay cannot express one either:
# replay/cassette/source.go builds every generation with IsDelta unset, and
# scope_generations.is_delta is persisted only from that field. So the leg
# stages tests/fixtures/golden_delta_union/base as a bare git origin and runs
# eshu-collector-git in explicit mode against it, through a private
# GIT_CONFIG_GLOBAL whose insteadOf maps https://github.com/ to that origin
# (the #7389 remote-run transport). After the base full generation is active,
# delta/ is committed on top and a second sync takes the real cumulative-delta
# path, producing a delta generation that touches app/main.go (a trailing
# comment, so no function moves), app/moved.go (its function moves), and
# deploy/claim.yaml.
#
# Order in the orchestrator:
#   golden_delta_reader_collect_full     after the first drain, before the
#                                        maintenance passes. A new repository
#                                        changes the repository catalog, and
#                                        the reducer's activation-obligation
#                                        consumer then refuses partition-scoped
#                                        maintenance (catalog_changed), so only
#                                        a corpus-wide deferred maintenance pass
#                                        (bootstrap-index, maintenance pass 1)
#                                        publishes its backward evidence.
#                                        Collected before the first drain, its
#                                        deployment_mapping waits on that pass
#                                        and wedges the first drain.
#   golden_delta_reader_run_delta_leg    after the maintenance drains, inside a
#                                        graph_query exclusion bracket: the
#                                        full-stage check, the delta commit and
#                                        sync, and one projector+reducer drain
#   -phase=...,delta-readers             in the final gate invocation (stage
#                                        delta)
#
# Requires (set by the orchestrator): repo_root, work_dir, bin_dir, log_dir,
# GATE_DRAIN_TIMEOUT, and log(), die(), pg(), start_bg(), golden_corpus_git().
# GATE_DELTA_READER_IGNORE_EXPECTED_FAILURES=1 asserts every edge as plainly
# required, which captures the RED of the open defects.
# shellcheck disable=SC2154

golden_delta_reader_fixture="${repo_root}/tests/fixtures/golden_delta_union"
golden_delta_reader_slug="acme/golden-delta-reader"
# repository:r_<first 8 hex of sha1("https://github.com/acme/golden-delta-reader")>,
# the canonical id CanonicalRepositoryID derives from the synthesized remote.
golden_delta_reader_scope_id="git-repository-scope:repository:r_0c077ab3"
: "${GATE_DELTA_READER_SETTLE_SECONDS:=90}"

golden_delta_reader_dir() { printf '%s/delta-reader\n' "${work_dir}"; }

# golden_delta_reader_commit <checkout> <message> <date> commits every staged
# change with a fixed identity and date, so the two commits are reproducible.
golden_delta_reader_commit() {
	local checkout="$1" message="$2" date="$3"
	golden_corpus_git -C "${checkout}" add -A >/dev/null 2>&1 || die "delta-reader: git add failed"
	GIT_AUTHOR_NAME="Golden Gate" GIT_AUTHOR_EMAIL="gate@eshu.local" GIT_AUTHOR_DATE="${date}" \
		GIT_COMMITTER_NAME="Golden Gate" GIT_COMMITTER_EMAIL="gate@eshu.local" GIT_COMMITTER_DATE="${date}" \
		golden_corpus_git -C "${checkout}" -c commit.gpgsign=false commit -q -m "${message}" >/dev/null 2>&1 \
		|| die "delta-reader: git commit '${message}' failed"
}

# golden_delta_reader_publish updates the bare origin's main from the checkout.
# The origin fetches from the checkout rather than the checkout pushing to it.
golden_delta_reader_publish() {
	local dir origin
	dir="$(golden_delta_reader_dir)"
	origin="${dir}/origins/${golden_delta_reader_slug}.git"
	golden_corpus_git -C "${origin}" fetch -q "${dir}/checkout" "+refs/heads/main:refs/heads/main" \
		|| die "delta-reader: update origin main failed"
}

golden_delta_reader_stage_origin() {
	local dir
	dir="$(golden_delta_reader_dir)"
	[[ -d "${golden_delta_reader_fixture}/base" && -d "${golden_delta_reader_fixture}/delta" ]] \
		|| die "delta-reader: fixture base/ and delta/ are missing under ${golden_delta_reader_fixture}"
	rm -rf "${dir}"
	mkdir -p "${dir}/origins/acme" "${dir}/repos" "${dir}/home" || die "delta-reader: mkdir failed"
	cp -R "${golden_delta_reader_fixture}/base" "${dir}/checkout" || die "delta-reader: copy base failed"
	golden_corpus_git -C "${dir}/checkout" init -q -b main || die "delta-reader: git init failed"
	golden_delta_reader_commit "${dir}/checkout" "base" "2026-11-01T00:00:00Z"
	golden_corpus_git init -q --bare -b main "${dir}/origins/${golden_delta_reader_slug}.git" \
		|| die "delta-reader: init bare origin failed"
	golden_delta_reader_publish
	printf '[safe]\n\tdirectory = *\n[url "file://%s/origins/"]\n\tinsteadOf = https://github.com/\n' "${dir}" \
		>"${dir}/gitconfig"
}

# golden_delta_reader_generation_count <is_delta> counts the scope's committed
# generations of one kind.
golden_delta_reader_generation_count() {
	pg "SELECT count(*) FROM scope_generations
WHERE scope_id = '${golden_delta_reader_scope_id}' AND is_delta = $1;"
}

# golden_delta_reader_sync <want_is_delta> <label> runs one collector-git
# explicit-mode sync and waits until a new generation of the wanted kind has
# committed, then stops the collector. A deadline, not a sleep. The pid
# variable must not be named pid: start_bg's own local of that name would
# shadow it and its printf -v would never reach this function.
golden_delta_reader_sync() {
	local want="$1" label="$2" dir before now deadline collector_pid
	dir="$(golden_delta_reader_dir)"
	before="$(golden_delta_reader_generation_count "${want}")" || die "delta-reader: count generations failed"
	start_bg "collector-git-delta-reader-${label}" collector_pid \
		env -u GIT_CONFIG_COUNT -u GIT_CONFIG_PARAMETERS \
		GIT_CONFIG_GLOBAL="${dir}/gitconfig" GIT_CONFIG_SYSTEM=/dev/null \
		ESHU_REPO_SOURCE_MODE=explicit \
		ESHU_REPOSITORY_RULES_JSON="[{\"exact\":\"${golden_delta_reader_slug}\"}]" \
		ESHU_REPOS_DIR="${dir}/repos" ESHU_HOME="${dir}/home" \
		ESHU_GIT_AUTH_METHOD=none ESHU_GITHUB_ORG=acme \
		"${bin_dir}/eshu-collector-git"
	deadline=$(( $(date +%s) + GATE_DELTA_READER_SETTLE_SECONDS ))
	while :; do
		now="$(golden_delta_reader_generation_count "${want}")" || now="${before}"
		[[ "${now}" -gt "${before}" ]] && break
		if ! kill -0 "${collector_pid}" 2>/dev/null; then
			tail -30 "${log_dir}/collector-git-delta-reader-${label}.log" >&2 || true
			die "delta-reader: collector-git exited before the ${label} generation committed"
		fi
		if (( $(date +%s) >= deadline )); then
			kill "${collector_pid}" >/dev/null 2>&1 || true
			tail -30 "${log_dir}/collector-git-delta-reader-${label}.log" >&2 || true
			die "delta-reader: no ${label} generation (is_delta=${want}) for ${golden_delta_reader_scope_id} within ${GATE_DELTA_READER_SETTLE_SECONDS}s"
		fi
		sleep 1
	done
	kill "${collector_pid}" >/dev/null 2>&1 || true
	wait "${collector_pid}" 2>/dev/null || true
	printf 'delta-reader: %s generation committed for %s\n' "${label}" "${golden_delta_reader_scope_id}"
}

# golden_delta_reader_collect_full stages the origin and collects the base
# full generation; the maintenance passes publish its backward evidence and
# their drains project and activate it.
golden_delta_reader_collect_full() {
	log "delta-reader leg (#10022): stage git origin and collect the base full generation"
	golden_delta_reader_stage_origin
	golden_delta_reader_sync false full
}

# golden_delta_reader_require_active <is_delta> <context> dies unless the
# scope's active generation has the wanted kind.
golden_delta_reader_require_active() {
	local want="$1" context="$2" got
	got="$(pg "SELECT COALESCE(g.is_delta::text, 'none')
FROM ingestion_scopes s
LEFT JOIN scope_generations g ON g.scope_id = s.scope_id AND g.generation_id = s.active_generation_id
WHERE s.scope_id = '${golden_delta_reader_scope_id}';")" || die "delta-reader: read active generation failed"
	[[ "${got}" == "${want}" ]] \
		|| die "delta-reader: ${context}: active generation is_delta=${got:-<no scope>}, want ${want}"
}

# golden_delta_reader_assert_stage <full|delta> runs the delta-readers phase.
# The final gate invocation runs stage delta through its own phase list and
# passes the same -delta-reader-ignore-expected-failures value.
golden_delta_reader_assert_stage() {
	local stage="$1"
	"${bin_dir}/eshu-golden-corpus-gate" -phase=delta-readers -delta-reader-stage="${stage}" \
		-delta-reader-ignore-expected-failures="${GATE_DELTA_READER_IGNORE_EXPECTED_FAILURES:-0}" \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json \
		|| die "delta-reader: stage ${stage} assertions failed"
}

# golden_delta_reader_run_delta_leg asserts the converged full generation,
# commits delta/, syncs the real delta generation, and drains it once. It runs
# inside the graph_query window, so it opens and closes its own exclusion
# bracket (phase_graph_query_excluded_starts/_ends, #5837): its syncs and drain
# are assertion work, not api/mcp startup.
golden_delta_reader_run_delta_leg() {
	local dir projector_pid reducer_pid deadline
	phase_graph_query_excluded_starts+=("$(date +%s)")
	dir="$(golden_delta_reader_dir)"
	log "delta-reader leg (#10022): assert the full generation, then sync and drain the delta"
	golden_delta_reader_require_active false "before the delta sync"
	golden_delta_reader_assert_stage full
	cp -R "${golden_delta_reader_fixture}/delta/." "${dir}/checkout/" || die "delta-reader: apply delta/ failed"
	golden_delta_reader_commit "${dir}/checkout" "delta" "2026-11-02T00:00:00Z"
	golden_delta_reader_publish
	golden_delta_reader_sync true delta
	start_bg projector-delta-reader projector_pid "${bin_dir}/eshu-projector"
	start_bg reducer-delta-reader reducer_pid "${bin_dir}/eshu-reducer"
	# The drain poll alone could read 0/0 before the projector claims the
	# delta, so wait for the projector to activate it first; its reducer work
	# is enqueued by then and the drain waits for that to finish.
	deadline=$(( $(date +%s) + GATE_DELTA_READER_SETTLE_SECONDS ))
	until [[ "$(pg "SELECT COALESCE(g.is_delta::text, 'none') FROM ingestion_scopes s
LEFT JOIN scope_generations g ON g.scope_id = s.scope_id AND g.generation_id = s.active_generation_id
WHERE s.scope_id = '${golden_delta_reader_scope_id}';")" == "true" ]]; do
		if (( $(date +%s) >= deadline )); then
			tail -30 "${log_dir}/projector-delta-reader.log" >&2 || true
			die "delta-reader: the projector did not activate the delta within ${GATE_DELTA_READER_SETTLE_SECONDS}s"
		fi
		sleep 1
	done
	"${bin_dir}/eshu-golden-corpus-gate" -phase=drains -snapshot=testdata/golden/e2e-20repo-snapshot.json \
		-drain-timeout="${GATE_DRAIN_TIMEOUT}" \
		|| { tail -30 "${log_dir}/reducer-delta-reader.log" || true; die "delta-reader: delta drain failed"; }
	kill "${projector_pid}" "${reducer_pid}" >/dev/null 2>&1 || true
	golden_delta_reader_require_active true "after the delta drain"
	phase_graph_query_excluded_ends+=("$(date +%s)")
}
