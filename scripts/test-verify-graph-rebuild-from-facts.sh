#!/usr/bin/env bash
# Test mirror and BITES proof for the identity-snapshot safety check in
# scripts/verify-graph-rebuild-from-facts.sh (#4594).
#
# That gate compares two graphs by their node and edge identities. The check
# under test is what stops it comparing identities that cannot tell anything
# apart -- a file of interchangeable keys diffs clean against any other such
# file, so the gate would report a match while proving nothing.
#
# This sources the REAL function out of the gate script (ESHU_DR_SOURCE_ONLY
# stops the procedure itself from running), so every assertion here exercises
# the shipped code rather than a copy of it. No Docker, no Compose, no network.
#
# Usage: scripts/test-verify-graph-rebuild-from-facts.sh
set -uo pipefail

script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=verify-graph-rebuild-from-facts.sh
ESHU_DR_SOURCE_ONLY=true source "${script_root}/verify-graph-rebuild-from-facts.sh"

# The gate sets -e for its own run; sourcing it applies that here too, and the
# whole point of this file is to call a function that returns non-zero on
# purpose. Turn it back off after the source, not before.
set +e

pass_count=0
fail_count=0

record_pass() { pass_count=$((pass_count + 1)); printf 'PASS: %s\n' "$1"; }
record_fail() {
	fail_count=$((fail_count + 1))
	printf 'FAIL: %s\n' "$1"
	[[ -n "${2:-}" ]] && printf '  %s\n' "$2"
	return 0
}

# new_snapshot writes a nodes.txt and an edges.txt into a fresh directory and
# prints its path. Arguments are the two file bodies, in that order.
new_snapshot() {
	local dir
	dir="$(mktemp -d)"
	printf '%s' "$1" >"${dir}/nodes.txt"
	printf '%s' "$2" >"${dir}/edges.txt"
	printf '%s\n' "${dir}"
}

# A healthy snapshot: every key carries content that distinguishes it. The
# trailing separators are normal -- most labels populate two or three of the
# concatenated properties.
healthy_nodes='File|src/main.go||src/main.go|repo-1|
Module|||||go
Repository|repo-1|eshu|||
'
healthy_edges='File|src/main.go||src/main.go|repo-1|||IMPORTS||Module|||||go
Repository|repo-1|eshu||||CONTAINS||File|src/main.go||src/main.go|repo-1|
'

# assert_refuses runs the check and expects a refusal.
assert_refuses() {
	local name="$1" dir="$2" output status
	output="$(assert_identity_snapshot_sane "${dir}" 2>&1)"
	status=$?
	rm -rf "${dir}"
	if [[ "${status}" -eq 0 ]]; then
		record_fail "${name}" "the check returned 0; this snapshot compares equal to anything"
		return 0
	fi
	record_pass "${name}"
}

# assert_accepts runs the check and expects it to pass.
assert_accepts() {
	local name="$1" dir="$2" output status
	output="$(assert_identity_snapshot_sane "${dir}" 2>&1)"
	status=$?
	rm -rf "${dir}"
	if [[ "${status}" -ne 0 ]]; then
		record_fail "${name}" "the check refused a legitimate snapshot: ${output}"
		return 0
	fi
	record_pass "${name}"
}

assert_accepts "a snapshot whose keys carry content is compared" \
	"$(new_snapshot "${healthy_nodes}" "${healthy_edges}")"

# The identity is a concatenation of coalesce()d properties. A node carrying
# none of them yields nothing but separators, and the label prefix makes the
# line non-empty, so an emptiness check passes it while every such node compares
# equal to every other.
assert_refuses "node keys that are nothing but separators are refused" \
	"$(new_snapshot 'Module|||||
File|src/main.go||src/main.go|repo-1|
' "${healthy_edges}")"

# The same collapse with the whole concatenation returning null: jq renders it
# as an empty line, and the label prefix leaves `Module|`.
assert_refuses "a node key that collapsed to the label prefix is refused" \
	"$(new_snapshot 'Module|
File|src/main.go||src/main.go|repo-1|
' "${healthy_edges}")"

# An edge endpoint that collapsed the same way leaves six separators against the
# `||` type delimiter -- at the head for the source, at the tail for the target.
assert_refuses "an edge whose source identity collapsed is refused" \
	"$(new_snapshot "${healthy_nodes}" '||||||IMPORTS||Module|||||go
')"
assert_refuses "an edge whose target identity collapsed is refused" \
	"$(new_snapshot "${healthy_nodes}" 'File|src/main.go||src/main.go|repo-1|||IMPORTS||||||
')"

assert_refuses "an empty node snapshot is refused" \
	"$(new_snapshot '' "${healthy_edges}")"

# An empty edge file diffs clean against any other empty edge file, and this
# backend has been seen returning zero rows for a query shape it dislikes
# without raising an error.
assert_refuses "an empty edge snapshot is refused" \
	"$(new_snapshot "${healthy_nodes}" '')"

# The documented false positive: `null` unanchored also matches real Terraform
# resource names, which would fail the gate on good data.
assert_accepts "a key containing null_resource is not mistaken for a null key" \
	"$(new_snapshot 'TerraformResource|null_resource.network_placeholder|network_placeholder|main.tf|repo-1|
File|src/main.go||src/main.go|repo-1|
' 'TerraformResource|null_resource.network_placeholder|network_placeholder|main.tf|repo-1|||DECLARED_IN||File|src/main.go||src/main.go|repo-1|
')"

# A retry whose scheduled time is already due is still active until a worker
# claims and completes it. Model that row in the SQL scalar and assert the
# active-work count stays nonzero between scheduler and worker polls.
psql_scalar() {
	local statement="$1"
	if [[ "$statement" == *'FROM shared_projection_intents'* ]]; then
		if [[ "$statement" == *"'retrying'"* && "$statement" != *'next_attempt_at'* ]]; then
			printf '1\n'
		else
			printf '0\n'
		fi
	else
		printf '0\n'
	fi
}
retry_active_count="$(queue_active_count)"
if [[ "$retry_active_count" != "1" ]]; then
	record_fail "a due retry stays active until its worker completes" \
		"active count=${retry_active_count}, want 1 for a due retry"
else
	record_pass "a due retry stays active until its worker completes"
fi

# The interrupted pass must kill workers while both graph output and queued
# work exist. A fixed sleep can land before projection begins or after it
# finishes, turning the recovery proof into a vacuous restart. Stub the live
# probes with two samples: the helper must ignore the zero-node sample and
# capture the first genuinely in-progress state.
interrupt_mock_dir="$(mktemp -d)"
printf '0\n' >"${interrupt_mock_dir}/graph-calls"
printf '0\n' >"${interrupt_mock_dir}/queue-calls"
graph_scalar() {
	local calls
	calls="$(<"${interrupt_mock_dir}/graph-calls")"
	printf '%s\n' "$((calls + 1))" >"${interrupt_mock_dir}/graph-calls"
	if [[ "${calls}" -eq 0 ]]; then printf '0\n'; else printf '7\n'; fi
}
queue_active_count() {
	local calls
	calls="$(<"${interrupt_mock_dir}/queue-calls")"
	printf '%s\n' "$((calls + 1))" >"${interrupt_mock_dir}/queue-calls"
	if [[ "${calls}" -eq 0 ]]; then printf '5\n'; else printf '3\n'; fi
}
sleep() { :; }

INTERRUPT_NODES=0
REMAINING=0
wait_for_interrupt_point 2 >/dev/null 2>&1
interrupt_status=$?
if [[ "${interrupt_status}" -ne 0 ]]; then
	record_fail "interrupted rebuild waits for a real in-progress checkpoint" \
		"wait_for_interrupt_point returned ${interrupt_status}"
elif [[ "${INTERRUPT_NODES}" != "7" || "${REMAINING}" != "3" ]]; then
	record_fail "interrupted rebuild waits for a real in-progress checkpoint" \
		"captured nodes=${INTERRUPT_NODES} work=${REMAINING}, want nodes=7 work=3"
else
	record_pass "interrupted rebuild waits for a real in-progress checkpoint"
fi
rm -rf "${interrupt_mock_dir}"

# NornicDB v1.3.2 can intermittently return zero for a whole-graph count even
# while a distinguishing identity scan returns thousands of nodes. The
# interruption checkpoint must use a row-existence probe rather than trusting
# that aggregate, or a real completed rebuild is misreported as an empty graph.
graph_scalar() {
	local statement="$1"
	if [[ "${statement}" == *'RETURN labels(n)[0] AS present LIMIT 1'* ]]; then
		printf 'Repository\n'
	else
		# Mirrors v1.3.2: literal-only and numeric scalar projections return no
		# data, and the scalar helper normalizes that empty row set to zero.
		printf '0\n'
	fi
}
queue_active_count() { printf '3\n'; }

INTERRUPT_NODES=0
REMAINING=0
wait_for_interrupt_point 1 >/dev/null 2>&1
interrupt_status=$?
if [[ "${interrupt_status}" -ne 0 ]]; then
	record_fail "interrupted rebuild ignores the flaky whole-graph count" \
		"wait_for_interrupt_point returned ${interrupt_status}"
elif [[ "${INTERRUPT_NODES}" != "Repository" || "${REMAINING}" != "3" ]]; then
	record_fail "interrupted rebuild ignores the flaky whole-graph count" \
		"captured sentinel=${INTERRUPT_NODES} work=${REMAINING}, want sentinel=Repository work=3"
else
	record_pass "interrupted rebuild ignores the flaky whole-graph count"
fi

# A recovery request retires relationship generations behind an in-flight
# reducer fence. Starting the workers before that request creates a claim race
# that is correctly refused with HTTP 500. Pin the orchestration order so the
# harness queues the rebuild while workers are still stopped, then starts them.
sequence_mock_dir="$(mktemp -d)"
request_rebuild() {
	printf 'request\n' >>"${sequence_mock_dir}/calls"
	printf '67\n'
}
start_services() { printf 'start\n' >>"${sequence_mock_dir}/calls"; }

enqueued="$(enqueue_rebuild_then_start_workers "rebuild-key")"
sequence_status=$?
sequence_calls="$(<"${sequence_mock_dir}/calls")"
if [[ "${sequence_status}" -ne 0 ]]; then
	record_fail "rebuild is enqueued before projection workers start" \
		"enqueue_rebuild_then_start_workers returned ${sequence_status}"
elif [[ "${enqueued}" != "67" || "${sequence_calls}" != $'request\nstart' ]]; then
	record_fail "rebuild is enqueued before projection workers start" \
		"enqueued=${enqueued} calls=${sequence_calls//$'\n'/,}, want enqueued=67 calls=request,start"
else
	record_pass "rebuild is enqueued before projection workers start"
fi

: >"${sequence_mock_dir}/calls"
request_rebuild() {
	printf 'request\n' >>"${sequence_mock_dir}/calls"
	return 1
}
start_services() { printf 'start\n' >>"${sequence_mock_dir}/calls"; }

enqueue_rebuild_then_start_workers "rejected-key" >/dev/null
rejected_status=$?
rejected_calls="$(<"${sequence_mock_dir}/calls")"
if [[ "${rejected_status}" -eq 0 ]]; then
	record_fail "rejected rebuild keeps projection workers stopped" \
		"enqueue_rebuild_then_start_workers unexpectedly succeeded"
elif [[ "${rejected_calls}" != "request" ]]; then
	record_fail "rejected rebuild keeps projection workers stopped" \
		"calls=${rejected_calls//$'\n'/,}, want request only"
else
	record_pass "rejected rebuild keeps projection workers stopped"
fi
rm -rf "${sequence_mock_dir}"

# Schema reapplication must not resolve Compose dependencies. `up -d
# db-migrate` re-resolves depends_on and restarts the exited bootstrap-index
# one-shot; its restart reopens reducer work and backfills evidence facts into
# the wiped graph, and both rebuild legs then reproduce rows the rebuild
# command under test never issued (#6184 runs 5-6: identical pass extras in
# both legs, neither in the baseline).
compose_mock_dir="$(mktemp -d)"
MOCK_BOOTSTRAP_STARTED_AT="2026-09-17T03:08:43.020746764Z"
mock_compose() {
	printf '%s\n' "$*" >>"${compose_mock_dir}/calls"
	if [[ "$1" == "logs" ]]; then
		printf 'bootstrap.graph.applied\n'
	elif [[ "$1" == "ps" ]]; then
		printf 'bootstrap-container-id\n'
	fi
	return 0
}
COMPOSE_CMD=(mock_compose)
docker() {
	if [[ "$1" == "inspect" ]]; then
		printf '%s\n' "${MOCK_BOOTSTRAP_STARTED_AT}"
		return 0
	fi
	return 0
}
wait_for_service_exit() { return 0; }

: >"${compose_mock_dir}/calls"
reapply_graph_schema >/dev/null 2>&1
reapply_status=$?
reapply_up_line="$(rg '^up ' "${compose_mock_dir}/calls" || true)"
if [[ "${reapply_status}" -ne 0 ]]; then
	record_fail "schema reapplication succeeds" \
		"reapply_graph_schema returned ${reapply_status}"
elif [[ "${reapply_up_line}" != *'--no-deps'* ]]; then
	record_fail "schema reapplication never resolves dependencies" \
		"up line=${reapply_up_line:-<none>}, want --no-deps db-migrate"
else
	record_pass "schema reapplication never resolves dependencies"
fi

# The rebuild proof is void if bootstrap-index ran again after the baseline:
# a restart is the same container started again, so the pin must be the
# start timestamp, not the container id (which survives a restart).
BOOTSTRAP_STARTED_AT=""
record_bootstrap_container >/dev/null 2>&1
if [[ "${BOOTSTRAP_STARTED_AT}" != "${MOCK_BOOTSTRAP_STARTED_AT}" ]]; then
	record_fail "bootstrap start is pinned after the initial run" \
		"pinned=${BOOTSTRAP_STARTED_AT:-<empty>}, want ${MOCK_BOOTSTRAP_STARTED_AT}"
elif ! assert_bootstrap_container_unchanged >/dev/null 2>&1; then
	record_fail "an unchanged bootstrap start passes the tripwire" \
		"assert failed on the pinned timestamp"
else
	record_pass "an unchanged bootstrap start passes the tripwire"
fi

MOCK_BOOTSTRAP_STARTED_AT="2026-09-17T03:14:06.699757153Z"
restart_output="$(assert_bootstrap_container_unchanged 2>&1)"
if [[ "$?" -eq 0 ]]; then
	record_fail "a restarted bootstrap fails the tripwire" \
		"assert passed after the start timestamp changed"
elif [[ "${restart_output}" != *'bootstrap-index'* ]]; then
	record_fail "a restarted bootstrap fails the tripwire" \
		"failure names no cause: ${restart_output:-<empty>}"
else
	record_pass "a restarted bootstrap fails the tripwire"
fi

BOOTSTRAP_STARTED_AT=""
if assert_bootstrap_container_unchanged >/dev/null 2>&1; then
	record_fail "a missing bootstrap pin fails the tripwire" \
		"assert passed with no recorded start timestamp"
else
	record_pass "a missing bootstrap pin fails the tripwire"
fi

# Surgical restarts must not resolve Compose dependencies: `docker compose
# start` (v5) starts the stopped dependency closure, including the
# bootstrap-index one-shot whose restart voids the rebuild proof (#6184 run
# 7). Restarts go through daemon-level `docker start` on the listed containers
# only; `ps -q` merely resolves their ids.
compose_mock_dir="$(mktemp -d)"
: >"${compose_mock_dir}/calls"
: >"${compose_mock_dir}/docker-calls"
GRAPH_WRITERS=(eshu mcp-server ingester resolution-engine projector)
API_BASE="http://localhost:1"
wait_for_http() { return 0; }
# An earlier block stubbed the start orchestration; restore the real lib
# functions under test here.
# shellcheck source=scripts/lib/graph_rebuild_runtime.sh
source "${script_root}/lib/graph_rebuild_runtime.sh"
MOCK_PS_IDS="cid-eshu"
mock_compose() {
	printf '%s\n' "$*" >>"${compose_mock_dir}/calls"
	if [[ "$1" == "logs" ]]; then
		printf 'bootstrap.graph.applied\n'
	elif [[ "$1" == "ps" ]]; then
		printf '%s\n' "${MOCK_PS_IDS}"
	fi
	return 0
}
docker() {
	printf 'docker %s\n' "$*" >>"${compose_mock_dir}/docker-calls"
	if [[ "$1" == "inspect" ]]; then
		if [[ "$*" == *'State.Status'* ]]; then
			printf 'exited\n'
		else
			printf '%s\n' "${MOCK_BOOTSTRAP_STARTED_AT}"
		fi
	fi
	return 0
}
start_recovery_api >/dev/null 2>&1
if rg -q '^(up|start)( |$)' "${compose_mock_dir}/calls"; then
	record_fail "recovery api start resolves no dependencies" \
		"compose calls=$(paste -sd, "${compose_mock_dir}/calls"), want ps only"
elif ! rg -qx 'docker start cid-eshu' "${compose_mock_dir}/docker-calls"; then
	record_fail "recovery api start resolves no dependencies" \
		"docker calls=$(paste -sd, "${compose_mock_dir}/docker-calls"), want 'docker start cid-eshu'"
else
	record_pass "recovery api start resolves no dependencies"
fi

: >"${compose_mock_dir}/calls"
: >"${compose_mock_dir}/docker-calls"
MOCK_PS_IDS=$'cid-a\ncid-b'
start_services >/dev/null 2>&1
if rg -q '^(up|start)( |$)' "${compose_mock_dir}/calls"; then
	record_fail "worker restart resolves no dependencies" \
		"compose calls=$(paste -sd, "${compose_mock_dir}/calls"), want ps only"
elif ! rg -qx 'docker start cid-a cid-b' "${compose_mock_dir}/docker-calls"; then
	record_fail "worker restart resolves no dependencies" \
		"docker calls=$(paste -sd, "${compose_mock_dir}/docker-calls"), want 'docker start cid-a cid-b'"
else
	record_pass "worker restart resolves no dependencies"
fi
rm -rf "${compose_mock_dir}"

printf '\n%d passed, %d failed\n' "${pass_count}" "${fail_count}"
[[ "${fail_count}" -eq 0 ]]
