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
printf 'PASS: claim-bound expiry, replacement, and release contracts\n'
