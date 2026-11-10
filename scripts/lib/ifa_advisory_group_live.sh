#!/usr/bin/env bash
# shellcheck shell=bash
# advisory_group is a winners-fed live family (#10466). Its cassette
# admits a committed supply_chain_impact pass's OUTPUT facts
# (reducer_supply_chain_impact_finding, members under provenance)
# directly, so the first drain only projects the Repository nodes the
# AdvisoryGroup writer MATCHes and materializes zero AFFECTED_BY edges.
# The mid-drive step then runs the production winners rebuild and the
# production AdvisoryGroupProjectionEnqueuer at their ports
# (eshu-ifa enqueue-advisory-groups) — never an invented marker fact —
# and the second drain claims, handles, and acks the real
# advisory_group_projection intent. The absent-then-exact pair is the
# non-vacuity proof that the enqueue step is load-bearing.

ifa_advisory_group_live_drive() {
	local bin_dir="$1" cassette="$2" workers="$3"
	"${bin_dir}/eshu-ifa" drive -cassette "${cassette}" -workers "${workers}"
}

ifa_advisory_group_live_drain() {
	local label="$1" bin_dir="$2" log_dir="$3" timeout="$4"
	local projector_pid reducer_pid pre_flag=()
	# The "pre" drain runs before any projection intent exists, so it
	# waits for quiescence rather than convergence (the workload shape):
	# the repo scopes' manifest-dependency marker may sit readiness-
	# deferred without that being this family's failure. The post drain
	# stays strict.
	if [[ "${label}" == "pre" ]]; then
		pre_flag=(-drain-allow-readiness-deferred)
	fi
	ifa_det_start_bg "${log_dir}" "projector-advisory-group-${label}" projector_pid "${bin_dir}/eshu-projector"
	ifa_det_start_bg "${log_dir}" "reducer-advisory-group-${label}" reducer_pid "${bin_dir}/eshu-reducer"
	"${bin_dir}/eshu-golden-corpus-gate" -phase=drains \
		-snapshot=testdata/golden/e2e-20repo-snapshot.json -drain-timeout="${timeout}" "${pre_flag[@]:-}" || return 1
	ifa_det_stop_join_untrack_bg_pid "${projector_pid}" TERM || return 1
	ifa_det_stop_join_untrack_bg_pid "${reducer_pid}" TERM || return 1
}

# Zero AFFECTED_BY edges before the enqueue proves the later exact set
# cannot be stale state from the first drain: nothing in the cassette
# drive or the projector can mint this family's edges.
ifa_advisory_group_live_assert_absent() {
	local bin_dir="$1" label="$2" dump_path count
	command -v jq >/dev/null 2>&1 || {
		printf '%s: jq is required for the advisory_group edge census\n' "${label}" >&2
		return 1
	}
	dump_path="$(mktemp)" || return 1
	if ! "${bin_dir}/eshu-ifa" graph-dump -out "${dump_path}"; then
		rm -f "${dump_path}"
		return 1
	fi
	count="$(jq '[.edges[] | select(.type == "AFFECTED_BY")] | length' "${dump_path}")" || {
		rm -f "${dump_path}"
		return 1
	}
	rm -f "${dump_path}"
	[[ "${count}" =~ ^[0-9]+$ && "${count}" -eq 0 ]] || {
		printf '%s: expected zero AFFECTED_BY edges before the advisory-group enqueue, got %q\n' "${label}" "${count}" >&2
		return 1
	}
	printf '%s: confirmed zero AFFECTED_BY edges before the advisory-group enqueue\n' "${label}"
}

# The mid-drive step, at production ports only: winners rebuild, then
# AdvisoryGroupProjectionEnqueuer, then a second rebuild so the
# watermark covers the enqueue. The group/scope/generation literals are
# the fixture's own identities, spelled out so a drifted cassette reds
# here instead of enqueueing for a group nothing rebuilt winners for.
ifa_advisory_group_live_enqueue() {
	local bin_dir="$1"
	"${bin_dir}/eshu-ifa" enqueue-advisory-groups \
		-group CVE-2026-10466 \
		-scope osv:eshu-fixture-advisory-findings \
		-generation gen-ifa-advisory-group-family-findings-1 \
		-source-system osv
}

ifa_advisory_group_live_assert() {
	local bin_dir="$1" expected_edges="$2"
	"${bin_dir}/eshu-ifa" assert-edges -domain advisory_group -expected "${expected_edges}"
}

# assert-edges proves the edge set; the member union lives on the
# AdvisoryGroup node, which no edge assertion reads. The canonical
# graph dump carries node props, so the union is asserted off it: the
# CVE-2026-10467 and GHSA-10466-cccc-dddd members arrive only through
# provenance members, so a members-less winners read reds here even
# though its edges would still match.
ifa_advisory_group_live_assert_node_union() {
	local bin_dir="$1" label="$2" dump_path union
	command -v jq >/dev/null 2>&1 || {
		printf '%s: jq is required for the advisory_group node-union census\n' "${label}" >&2
		return 1
	}
	dump_path="$(mktemp)" || return 1
	if ! "${bin_dir}/eshu-ifa" graph-dump -out "${dump_path}"; then
		rm -f "${dump_path}"
		return 1
	fi
	union="$(jq -r '[.nodes[] | select(.labels | index("AdvisoryGroup")) | "\(.props.id)|\(.props.cve_ids | sort | join(","))|\(.props.advisory_ids | sort | join(","))|\(.props.member_count)"] | join(";")' "${dump_path}")" || {
		rm -f "${dump_path}"
		return 1
	}
	rm -f "${dump_path}"
	local want='CVE-2026-10466|CVE-2026-10466,CVE-2026-10467|GHSA-10466-aaaa-bbbb,GHSA-10466-cccc-dddd|4'
	[[ "${union}" == "${want}" ]] || {
		printf '%s: AdvisoryGroup node union = %q, want %q\n' "${label}" "${union}" "${want}" >&2
		return 1
	}
	printf '%s: confirmed AdvisoryGroup node union %s\n' "${label}" "${union}"
}

ifa_advisory_group_live_run_matrix_cell() {
	local bin_dir="$1" cassette="$2" expected_edges="$3"
	local workers="$4" log_dir="$5" timeout="$6"
	ifa_advisory_group_live_drive "${bin_dir}" "${cassette}" "${workers}" || return 1
	ifa_advisory_group_live_drain pre "${bin_dir}" "${log_dir}" "${timeout}" || return 1
	ifa_advisory_group_live_assert_absent "${bin_dir}" pre-enqueue || return 1
	ifa_advisory_group_live_enqueue "${bin_dir}" || return 1
	ifa_advisory_group_live_drain post "${bin_dir}" "${log_dir}" "${timeout}" || return 1
	ifa_advisory_group_live_assert "${bin_dir}" "${expected_edges}" || return 1
	ifa_advisory_group_live_assert_node_union "${bin_dir}" post-enqueue || return 1
}
