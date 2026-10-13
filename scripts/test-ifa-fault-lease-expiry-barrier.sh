#!/usr/bin/env bash
# Behavioral contract for claim-bound expiry, replacement, and barrier release.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${repo_root}/scripts/lib/ifa_fault_lease_expiry_barrier.sh"
scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT
barrier="${scratch}/barrier"
printf '%s\n' '{"work_item_id":"owned","claimed_at":"2026-10-11T15:00:00.123Z","attempt_count":1,"domain":"gcp_resource_materialization"}' >"${barrier}.held.json"
capture="${scratch}/sql"
ifa_det_pg() { printf '%s' "$4" >"${capture}"; printf '%s\n' "${response}"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

response=''
if ifa_fault_expire_held_claim project 0 dsn compose "${barrier}" >/dev/null; then
	fail 'zero affected rows accepted as expiry proof'
fi
response='{"work_item_id":"other","claimed_at":"2026-10-11T15:00:00.123Z","attempt_count":1}'
if ifa_fault_expire_held_claim project 0 dsn compose "${barrier}" >/dev/null; then
	fail 'wrong item accepted as expiry proof'
fi
response='{"work_item_id":"owned","claimed_at":"2026-10-11T15:00:01Z","attempt_count":1}'
if ifa_fault_expire_held_claim project 0 dsn compose "${barrier}" >/dev/null; then
	fail 'wrong claim epoch accepted as expiry proof'
fi
response='{"work_item_id":"owned","claimed_at":"2026-10-11T15:00:00.123Z","attempt_count":1}'
ifa_fault_expire_held_claim project 0 dsn compose "${barrier}" >/dev/null \
	|| fail 'exact committed expiry rejected'
sql="$(cat "${capture}")"
[[ "${sql}" == *"work_item_id = 'owned'"* && "${sql}" == *"last_attempt_at = '2026-10-11T15:00:00.123Z'::timestamptz"* && "${sql}" == *"FOR NO KEY UPDATE"* ]] \
	|| fail 'expiry did not fence and lock exact held claim'

response='{"work_item_id":"owned","claimed_at":"2026-10-11T15:00:00.123Z","attempt_count":1,"status":"succeeded"}'
if ifa_fault_wait_for_held_replacement project 0 dsn compose "${barrier}" 0 >/dev/null; then
	fail 'same epoch/attempt accepted as replacement'
fi
response='{"work_item_id":"owned","claimed_at":"2026-10-11T15:00:02Z","attempt_count":2,"status":"succeeded"}'
ifa_fault_wait_for_held_replacement project 0 dsn compose "${barrier}" 0 >/dev/null \
	|| fail 'same item replacement rejected while first handler held'
printf '%s\n' '{"state":"cancelled"}' >"${barrier}.finished.json"
if ifa_fault_wait_for_held_replacement project 0 dsn compose "${barrier}" 0 >/dev/null; then
	fail 'replacement accepted after original handler stopped'
fi
rm "${barrier}.finished.json"
ifa_fault_release_held_claim "${barrier}"
jq -e --slurpfile held "${barrier}.held.json" '.work_item_id == $held[0].work_item_id and .claimed_at == $held[0].claimed_at' "${barrier}.release" >/dev/null \
	|| fail 'release was not bound to held identity'
printf '%s\n' '{"work_item_id":"bad'"'"'id","claimed_at":"2026-10-11T15:00:00Z","attempt_count":1,"domain":"gcp_resource_materialization"}' >"${barrier}.held.json"
if ifa_fault_expire_held_claim project 0 dsn compose "${barrier}" >/dev/null; then
	fail 'unsafe identity accepted'
fi

# Stale ACK observation runs after release, when the barrier has already
# written finished.json, so it must not depend on the pre-release reader.
printf '%s\n' '{"work_item_id":"owned","claimed_at":"2026-10-11T15:00:00.123Z","attempt_count":1,"domain":"gcp_resource_materialization"}' >"${barrier}.held.json"
printf '%s\n' '{"work_item_id":"owned","claimed_at":"2026-10-11T15:00:00.123Z","state":"released"}' >"${barrier}.finished.json"
reducer_log="${scratch}/reducer-expirelease.log"
ok='{"message":"reducer execution succeeded","intent_id":"owned","status":"succeeded"}'
fence='{"message":"reducer ack rejected stale claim","intent_id":"owned","status":"ack_claim_rejected"}'
printf '%s\n%s\n' "${ok}" "${fence}" >"${reducer_log}"
ifa_fault_assert_stale_ack_fenced "${barrier}" "${reducer_log}" 0 >/dev/null \
	|| fail 'one succeeded plus one fence record rejected'
printf 'GREEN stale-ack case 1: one succeeded + one ack_claim_rejected passes\n'
printf '%s\n%s\n%s\n' "${ok}" "${ok}" "${fence}" >"${reducer_log}"
if ifa_fault_assert_stale_ack_fenced "${barrier}" "${reducer_log}" 0 >/dev/null 2>&1; then
	fail 'two succeeded records accepted: the stale ACK was not fenced'
fi
printf 'RED stale-ack case 2: two succeeded + one fence fails\n'
printf '%s\n' "${ok}" >"${reducer_log}"
if ifa_fault_assert_stale_ack_fenced "${barrier}" "${reducer_log}" 1 >/dev/null 2>&1; then
	fail 'no fence record within the budget accepted'
fi
printf 'RED stale-ack case 3: one succeeded, no fenced outcome within 1 s fails\n'
printf 'panic: not json\n%s\n%s\n' "${ok}" "${fence}" >"${reducer_log}"
ifa_fault_assert_stale_ack_fenced "${barrier}" "${reducer_log}" 0 >/dev/null \
	|| fail 'a non-JSON log line made the observation fatal'
printf 'GREEN stale-ack case 4: non-JSON noise plus case 1 passes\n'
printf '%s\n%s\n' "${ok}" '{"intent_id":"other","status":"ack_claim_rejected"}' >"${reducer_log}"
if ifa_fault_assert_stale_ack_fenced "${barrier}" "${reducer_log}" 0 >/dev/null 2>&1; then
	fail 'a fence record for a different work item accepted'
fi
printf 'RED stale-ack case 5: fence for a different intent_id only fails\n'
# A second succeeded record that lands just after the first poll break must be
# caught by the settle re-read. The appender writes it 0.3 s after the call
# starts; the helper breaks on its first read and re-reads after settling.
printf '%s\n%s\n' "${ok}" "${fence}" >"${reducer_log}"
( sleep 0.3; printf '%s\n' "${ok}" >>"${reducer_log}" ) &
late_pid=$!
if ifa_fault_assert_stale_ack_fenced "${barrier}" "${reducer_log}" 0 >/dev/null 2>&1; then
	wait "${late_pid}"
	fail 'a late second succeeded record after the poll break was missed'
fi
wait "${late_pid}"
printf 'RED stale-ack case 6: late second succeeded record after the break fails\n'
printf '%s\n%s\n' "${ok}" "${fence}" >"${reducer_log}"
( sleep 0.3; printf '%s\n' '{"intent_id":"other","status":"succeeded"}' >>"${reducer_log}" ) &
late_pid=$!
ifa_fault_assert_stale_ack_fenced "${barrier}" "${reducer_log}" 0 >/dev/null \
	|| fail 'a late record for a different work item made the observation fail'
wait "${late_pid}"
printf 'GREEN stale-ack case 7: late record for a different intent_id passes\n'
# One flush batch carries the stale and the successor ACK of the held item: the
# Service then logs both as ack_outcome_unknown and writes no succeeded record
# (run-2 shape of the real cell). That is correct queue behavior and must pass.
unknown='{"message":"reducer ack outcome unknown","intent_id":"owned","status":"ack_outcome_unknown"}'
lost='{"message":"lease lost","intent_id":"owned","status":"lease_lost_during_execution"}'
printf '%s\n%s\n' "${unknown}" "${unknown}" >"${reducer_log}"
ifa_fault_assert_stale_ack_fenced "${barrier}" "${reducer_log}" 0 >/dev/null \
	|| fail 'two ack_outcome_unknown records (one flush batch) rejected'
printf 'GREEN stale-ack case 8: two ack_outcome_unknown, no succeeded, passes\n'
printf '%s\n%s\n' "${ok}" "${lost}" >"${reducer_log}"
ifa_fault_assert_stale_ack_fenced "${barrier}" "${reducer_log}" 0 >/dev/null \
	|| fail 'one succeeded plus one lease_lost_during_execution rejected'
printf 'GREEN stale-ack case 9: one succeeded + one lease_lost_during_execution passes\n'
printf '%s\n' "${unknown}" >"${reducer_log}"
if ifa_fault_assert_stale_ack_fenced "${barrier}" "${reducer_log}" 1 >/dev/null 2>&1; then
	fail 'a single ack_outcome_unknown record accepted as two ACK outcomes'
fi
printf 'RED stale-ack case 10: one ack_outcome_unknown only within 1 s fails\n'

# The terminal-row read is one SQL statement with every inequality in SQL.
printf '%s\n' '{"work_item_id":"owned","claimed_at":"2026-10-11T15:00:00.123Z","attempt_count":1,"domain":"gcp_resource_materialization"}' >"${barrier}.held.json"
response='{"work_item_id":"owned","status":"succeeded","attempt_count":2,"claimed_at":"2026-10-11T15:00:02+00:00"}'
ifa_fault_assert_held_item_terminal project 0 dsn compose "${barrier}" >/dev/null \
	|| fail 'a matching terminal succeeded row rejected'
sql="$(cat "${capture}")"
[[ "${sql}" == *"work_item_id = 'owned'"* && "${sql}" == *"stage = 'reducer'"* && "${sql}" == *"status = 'succeeded'"* \
	&& "${sql}" == *"attempt_count = 1 + 1"* && "${sql}" == *"last_attempt_at <> '2026-10-11T15:00:00.123Z'::timestamptz"* \
	&& "${sql}" == *"lease_owner IS NULL AND claim_until IS NULL"* ]] \
	|| fail 'terminal-row read did not fence status, attempt, epoch, and lease in SQL'
printf 'GREEN terminal-row case 11: matching succeeded row at the replacement claim passes\n'
response=''
if ifa_fault_assert_held_item_terminal project 0 dsn compose "${barrier}" >/dev/null 2>&1; then
	fail 'an empty row result accepted as terminal'
fi
printf 'RED terminal-row case 12: no matching row fails\n'
response='{"work_item_id":"other","status":"succeeded","attempt_count":2,"claimed_at":"2026-10-11T15:00:02+00:00"}'
if ifa_fault_assert_held_item_terminal project 0 dsn compose "${barrier}" >/dev/null 2>&1; then
	fail 'a row for a different work item accepted as terminal'
fi
printf 'RED terminal-row case 13: row for a different work item fails\n'
printf 'PASS: claim-bound expiry, replacement, release, and stale ACK fence contracts\n'
