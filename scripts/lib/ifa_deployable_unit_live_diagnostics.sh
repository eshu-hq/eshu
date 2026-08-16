#!/usr/bin/env bash
# deployable_unit_edges post-maintenance DIAGNOSTIC probes (#5993), split out
# of ifa_deployable_unit_live.sh (readiness-review follow-up items) to keep
# that file under the repository's 500-line cap. Sourced by
# verify-ifa-determinism.sh and verify-ifa-fault-injection.sh alongside
# ifa_deployable_unit_live.sh, which the caller owns strict mode, logging,
# and ifa_det_pg for -- same convention as that file's own header.
#
# Every function here is diagnostic ONLY: none of them ever gates a cell.
# ifa_deployable_unit_live_assert (in ifa_deployable_unit_live.sh) remains
# the sole pass/fail authority for this family's live cells. They exist to
# turn a single ambiguous outcome -- readiness=pass, intents=0,
# edges="got 0" -- into several distinguishable causes: a writer endpoint
# MATCH miss, correlation producing nothing, or the unproven
# persisted-resolution -> reopened-correlation link never firing.

# ifa_deployable_unit_live_report_intents_after_maintenance is a DIAGNOSTIC
# companion to ifa_deployable_unit_live_assert_empty_before_maintenance, not a
# second pass/fail gate: ifa_deployable_unit_live_assert (ifa_deployable_unit_live.sh) remains the
# sole authority on whether this cell passes or fails. This is a real problem
# to fix, not scaffolding: the live gate's edge assertion fails as a single
# symptom, "expected 1, got 0", with two indistinguishable causes --
#
#   (a) correlation admitted rows, but the WRITER dropped them. The two MATCH
#       clauses in canonical_deployable_unit_edges.go
#       (MATCH (source_repo:Repository {id: row.repo_id}) and
#       MATCH (deployment_repo:Repository {id: row.deployment_repo_id})) are
#       filters, not outer joins: if either endpoint Repository node is
#       absent, the row is eliminated, the statement still succeeds, zero
#       rows are affected, and nothing counts it -- no error, no dead letter.
#   (b) correlation produced nothing at all (the reopen either did not fire
#       or found nothing to correlate).
#
# Re-running the SAME shared_projection_intents count query used by the
# BEFORE probe, but AFTER the maintenance pass and BEFORE the final edge
# assertion, separates them: a nonzero count here proves admitted rows
# existed, so a subsequent "expected 1, got 0" implicates the writer; a zero
# count here means correlation itself produced nothing.
#
# What this function actually guarantees: it REPORTS, it never GATES, and a
# failed query degrades to an explicit "unavailable" reading rather than
# failing the cell or silently reading as zero. The call site (in
# ifa_deployable_unit_live.sh's standalone cell and the fault cells) has no
# `|| return 1` on purpose, which means the caller's `set -e` (this file is
# sourced into verify-ifa-fault-injection.sh's `set -euo pipefail` shell) is
# NOT suppressed at that call site -- a function called without a `||` guard
# still runs under `set -e`, so an internally-unguarded query failure here
# would abort the whole cell before the real edge assertion ran, attributed
# to an assertion that never got to run -- exactly the false-red this probe
# exists to avoid, and a worse bug than the ambiguity it fixes.
#
# Harmonized with ifa_deployable_unit_live_assert_empty_before_maintenance's
# rc-capture shape (ifa_deployable_unit_live.sh) rather than piping the query capture
# through `tr` and guarding with `if !` / `|| [[ -z ]]`: capturing
# admitted_rc directly from a BARE (unpiped) ifa_det_pg call means success or
# failure here never depends on the caller having set pipefail at all, one
# pattern instead of two slightly different ones for the same underlying
# hazard. The three failure modes this function must never let through as a
# false "0" -- the query itself failing, empty output, and non-numeric
# output -- collapse to the SAME "unavailable" reading (unlike the BEFORE
# probe, which reports each distinctly and gates on all three, since this
# function never gates on any of them). An empty-string or non-numeric count
# read as a real value would be its own false signal -- "intents == 0
# implicates correlation" is the WRONG diagnosis if the query never ran at
# all, so any of the three failure modes gets the same distinct,
# unmistakable reading instead.
#
# Args: compose_project use_compose dsn compose_file
ifa_deployable_unit_live_report_intents_after_maintenance() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local admitted admitted_rc
	if admitted="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"SELECT count(*) FROM shared_projection_intents WHERE projection_domain = 'deployable_unit_edges';" \
		"${compose_file}")"; then
		admitted_rc=0
	else
		admitted_rc=$?
	fi
	if [[ "${admitted_rc}" -eq 0 ]]; then
		admitted="$(printf '%s' "${admitted}" | tr -d '[:space:]')"
	fi
	if [[ "${admitted_rc}" -ne 0 || -z "${admitted}" || ! "${admitted}" =~ ^[0-9]+$ ]]; then
		admitted="unavailable (query failed)"
	fi
	printf 'deployable_unit_edges: post-maintenance shared_projection_intents count = %s (diagnostic only, not a gate -- intents > 0 with 0 edges implicates the writer: an endpoint MATCH miss in canonical_deployable_unit_edges.go; intents == 0 implicates correlation: no admitted rows produced; a failed query reports as unavailable rather than failing this cell or reading as zero)\n' "${admitted}"
}

# ifa_deployable_unit_live_report_resolved_deploys_from_count and
# ifa_deployable_unit_live_report_correlation_reopen (readiness-review
# follow-up, #5993 item 4) are the two separators for the residual junction
# this family has never covered end to end: the unproven link between
# persisted resolution and reopened correlation. Both a "correlation
# genuinely found nothing" outcome and "the persisted-resolution ->
# reopened-correlation link is broken" outcome present IDENTICALLY as
# readiness=pass, intents=0, edges="got 0" -- the current observables cannot
# tell them apart, because both mean no admitted rows. Neither probe needs a
# new mechanism; both are diagnostic only, never a gate, matching
# ifa_deployable_unit_live_report_intents_after_maintenance above.
#
# ifa_deployable_unit_live_report_resolved_deploys_from_count answers "did
# resolution persist anything?": a count of resolved_relationships WHERE
# relationship_type = 'DEPLOYS_FROM' (relationships.RelDeploysFrom /
# edgetype.DeploysFrom; the table and its schema are
# go/internal/storage/postgres/relationship_schema.go). resolved > 0 means
# material existed for deployable_unit_correlation to read; resolved == 0
# means there was nothing to correlate, a DIFFERENT cause than the reopen
# firing and finding admitted rows the writer then dropped. Harmonized to the
# same rc-capture shape as the other probes in this file (unpiped capture,
# then a separate empty/non-numeric check, collapsing any of the three
# failure modes to "unavailable" rather than a false "0").
#
# Args: compose_project use_compose dsn compose_file
ifa_deployable_unit_live_report_resolved_deploys_from_count() {
	local compose_project="$1" use_compose="$2" dsn="$3" compose_file="$4"
	local resolved resolved_rc
	if resolved="$(ifa_det_pg "${compose_project}" "${use_compose}" "${dsn}" \
		"SELECT count(*) FROM resolved_relationships WHERE relationship_type = 'DEPLOYS_FROM';" \
		"${compose_file}")"; then
		resolved_rc=0
	else
		resolved_rc=$?
	fi
	if [[ "${resolved_rc}" -eq 0 ]]; then
		resolved="$(printf '%s' "${resolved}" | tr -d '[:space:]')"
	fi
	if [[ "${resolved_rc}" -ne 0 || -z "${resolved}" || ! "${resolved}" =~ ^[0-9]+$ ]]; then
		resolved="unavailable (query failed)"
	fi
	printf 'deployable_unit_edges: resolved DEPLOYS_FROM relationship count = %s (diagnostic only, not a gate -- resolved > 0 means resolution persisted material for deployable_unit_correlation to read; resolved == 0 means there was nothing to correlate, a different cause than the reopen finding admitted rows the writer then dropped)\n' "${resolved}"
}

# ifa_deployable_unit_live_report_correlation_reopen answers "did the reopen
# fire?": it tails the bootstrap-index maintenance-pass log for
# ReopenSucceededReducerWorkItems's per-domain line
# (go/internal/storage/postgres/ingestion_reopen_correlation.go:
# `log.Printf("reducer_work_items_reopened domain=%s count=%d", ...)`),
# logged UNCONDITIONALLY for every domain in crossScopeCorrelationReopenDomains
# -- deployable_unit_correlation included -- even when its count is 0. The
# line's ABSENCE means the reopen phase itself never ran (a different, more
# serious cause than a legitimate zero); its PRESENCE with count=0 means it
# ran and genuinely found nothing to replay. Bash substring match, NOT rg,
# for the same #5974 reason ifa_deployable_unit_live_assert_readiness_opened
# (ifa_deployable_unit_live.sh) documents -- this function is called from
# the same gates.
#
# Args: log_dir pass_label (the bootstrap-index maintenance-pass log is
# ${log_dir}/bootstrap-index-deployable-unit-${pass_label}.log, matching
# ifa_deployable_unit_live_run_maintenance_pass's own naming in
# ifa_deployable_unit_live.sh)
ifa_deployable_unit_live_report_correlation_reopen() {
	local log_dir="$1" pass_label="$2"
	local bootstrap_index_log="${log_dir}/bootstrap-index-deployable-unit-${pass_label}.log"
	local bootstrap_contents=""
	local reopen_marker="reducer_work_items_reopened domain=deployable_unit_correlation count="
	if [[ -f "${bootstrap_index_log}" ]]; then
		bootstrap_contents="$(cat "${bootstrap_index_log}")" || bootstrap_contents=""
	fi
	if [[ "${bootstrap_contents}" == *"${reopen_marker}"* ]]; then
		local after="${bootstrap_contents#*"${reopen_marker}"}"
		local count_str="${after%%[!0-9]*}"
		printf 'deployable_unit_edges: correlation-reopen count for domain=deployable_unit_correlation = %s (diagnostic only, not a gate -- the reopen ran; count > 0 means work items were reopened for correlation to re-execute, count == 0 means the reopen found nothing to replay)\n' "${count_str:-unknown}"
	else
		printf 'deployable_unit_edges: correlation-reopen: bootstrap-index maintenance-pass log (%s) never logged reducer_work_items_reopened for domain=deployable_unit_correlation -- the reopen phase may not have run at all (diagnostic only, not a gate)\n' "${bootstrap_index_log}"
	fi
}
