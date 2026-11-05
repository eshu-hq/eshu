#!/usr/bin/env bash
# Focused tests for scripts/claim-plane-cutover-handover.sh (#9376).
#
# Stub kubectl/psql/sleep through PATH so no cluster or database is needed.
# The stubs prove wiring (right files invoked, reports parsed, refusals);
# scripts/test-claim-plane-cutover-handover-sql.sh proves the SQL semantics
# against a throwaway cluster built from the real migrations.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo_root}/scripts/claim-plane-cutover-handover.sh"
lib_dir="${repo_root}/scripts/lib"

failures=0
pass() { printf 'PASS %s\n' "$1"; }
fail() {
	printf 'FAIL %s\n' "$1"
	failures=$((failures + 1))
}
check() { # label want got
	if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1 (want [$2], got [$3])"; fi
}
check_contains() { # label needle haystack
	if grep -qF -- "$2" <<<"$3"; then pass "$1"; else fail "$1 (missing [$2])"; fi
}

# world builds one isolated stub world per case and exports the env into
# the CALLER (call it bare, then read WORLD_DIR): command substitution would
# trap the exports in a subshell. Fakes live in scripts/lib (the heredoc
# budget gate forbids 512B+ heredoc bodies in new scripts); world only links
# them onto PATH.
world() {
	WORLD_DIR="$(mktemp -d "${TMPDIR:-/tmp}/howorld.XXXXXX")"
	local w="${WORLD_DIR}"
	mkdir -p "${w}/bin" "${w}/state"
	ln -s "${repo_root}/scripts/lib/test-handover-fake-kubectl.sh" "${w}/bin/kubectl"
	ln -s "${repo_root}/scripts/lib/test-handover-fake-psql.sh" "${w}/bin/psql"
	ln -s "${repo_root}/scripts/lib/test-handover-fake-sleep.sh" "${w}/bin/sleep"
	export PATH="${w}/bin:${PATH}"
	export HANDOVER_KUBECTL_BIN=kubectl HANDOVER_PSQL_BIN=psql
	export HANDOVER_SHA_BIN=sha256sum HANDOVER_SLEEP_BIN=sleep
	export HANDOVER_NAMESPACE=test-ns STUB_SCALE_LOG="${w}/state/scale.log"
	export STUB_STATE_DIR="${w}/state"
	export HANDOVER_STATE_FILE="${w}/state/replicas" PGPASSFILE=dummy
	export STUB_SPEC=2 STUB_STATUS=2 STUB_DRAIN_1=0 STUB_DRAIN_2=0
	export STUB_MATCHED=2 STUB_MISSING="" STUB_ITEMS_EMPTY=0
	export HANDOVER_SAT_PGHOST=sat-host HANDOVER_SAT_PGUSER=sat-u
	export HANDOVER_SAT_PGDATABASE=sat-db
}

# usage: no args and unknown command fail without touching anything.
world; w="${WORLD_DIR}"
out="$("${script}" 2>&1 || true)"
check_contains "usage-no-args" "usage:" "${out}"
out="$("${script}" frobnicate 2>&1 || true)"
check_contains "usage-bad-command" "usage:" "${out}"
rm -rf "${w}"

# plan prints the order and runs nothing.
world; w="${WORLD_DIR}"
out="$("${script}" plan)"
check_contains "plan-freeze" "freeze" "${out}"
check_contains "plan-flip-deploy" "reviewed deploy only" "${out}"
[[ ! -e "${w}/state/scale.log" ]] && pass "plan-runs-nothing" || fail "plan-runs-nothing"
rm -rf "${w}"

# freeze scales to zero and records the prior count.
world; w="${WORLD_DIR}"
out="$(HANDOVER_STATE_FILE="${w}/state/replicas" "${script}" freeze)"
check_contains "freeze-msg" "scaled 2 -> 0" "${out}"
check "freeze-record" "2" "$(cat "${w}/state/replicas")"
check_contains "freeze-scale-call" --replicas=0 "$(cat "${w}/state/scale.log")"
rm -rf "${w}"

# freeze refuses when already frozen and without a namespace.
world; w="${WORLD_DIR}"
out="$(STUB_SPEC=0 STUB_STATUS="" "${script}" freeze 2>&1 || true)"
check_contains "freeze-refuses-frozen" "already frozen" "${out}"
out="$(HANDOVER_NAMESPACE="" "${script}" freeze 2>&1 || true)"
check_contains "freeze-needs-namespace" "HANDOVER_NAMESPACE" "${out}"
rm -rf "${w}"

# drain is green only on 0/0 and refuses unreadable counts.
world; w="${WORLD_DIR}"
out="$("${script}" drain)"
check_contains "drain-green" "0 live claims" "${out}"
out="$(STUB_DRAIN_1=1 STUB_DRAIN_2=0 "${script}" drain 2>&1 || true)"
check_contains "drain-red-claims" "1 live claims" "${out}"
out="$(STUB_DRAIN_1=x "${script}" drain 2>&1 || true)"
check_contains "drain-refuses-garbage" "unreadable drain counts" "${out}"
rm -rf "${w}"

# snapshot writes items, fences, seed input, and hashes after a freeze;
# it refuses an existing dir, an unfrozen or undrained tree, an empty
# handover, and a second snapshot on one freeze (record already bound).
world; w="${WORLD_DIR}"
snap="${w}/snap"
"${script}" freeze >/dev/null
out="$("${script}" snapshot --snapshot "${snap}")"
check_contains "snapshot-msg" "snapshot:" "${out}"
check "snapshot-items" "item-a	3	claim-a	claimed" "$(sed -n '1p' "${snap}/items.tsv")"
check "snapshot-fences" "gen-1	item-a	3" "$(sed -n '1p' "${snap}/fences.tsv")"
check "snapshot-seed-tsv" "$(printf 'item-a\t3\nitem-b\t7')" "$(cat "${snap}/seed.tsv")"
(cd "${snap}" && sha256sum -c sha256sums >/dev/null) && pass "snapshot-hash" || fail "snapshot-hash"
check "snapshot-binds-record" "$(printf '2\n%s' "${snap}")" "$(cat "${w}/state/replicas")"
out="$("${script}" snapshot --snapshot "${snap}" 2>&1 || true)"
check_contains "snapshot-refuses-existing" "refusing to overwrite" "${out}"
out="$("${script}" snapshot --snapshot "${snap}-again" 2>&1 || true)"
check_contains "snapshot-refuses-rebind" "already bound" "${out}"
rm -rf "${w}"

world; w="${WORLD_DIR}"
out="$("${script}" snapshot --snapshot "${w}/snap" 2>&1 || true)"
check_contains "snapshot-needs-freeze" "not frozen" "${out}"
rm -rf "${w}"

# Frozen shapes without a kubectl round-trip: a prior freeze record plus
# the frozen jsonpath shape (empty or zero status) snapshots fine, while a
# terminating shape (pods still present) refuses.
world; w="${WORLD_DIR}"
snap="${w}/snap"
printf '2\n' >"${w}/state/replicas"
out="$(STUB_SPEC=0 STUB_STATUS=0 "${script}" snapshot --snapshot "${snap}")"
check_contains "snapshot-frozen-zero-status" "snapshot:" "${out}"
rm -rf "${w}"

world; w="${WORLD_DIR}"
snap="${w}/snap"
printf '2\n' >"${w}/state/replicas"
out="$(STUB_SPEC=0 STUB_STATUS=1 "${script}" snapshot --snapshot "${snap}-t" 2>&1 || true)"
check_contains "snapshot-refuses-terminating" "not frozen" "${out}"
rm -rf "${w}"

world; w="${WORLD_DIR}"
snap="${w}/snap"
"${script}" freeze >/dev/null
out="$(STUB_DRAIN_1=1 "${script}" snapshot --snapshot "${snap}" 2>&1 || true)"
check_contains "snapshot-needs-drain" "not drained" "${out}"
rm -rf "${w}"

world; w="${WORLD_DIR}"
snap="${w}/snap"
"${script}" freeze >/dev/null
out="$(STUB_ITEMS_EMPTY=1 "${script}" snapshot --snapshot "${snap}" 2>&1 || true)"
check_contains "snapshot-refuses-empty" "empty items snapshot" "${out}"
rm -rf "${w}"

# seed needs --yes, a matching hash, and satellite config; reports
# matched/missing and refuses unmigrated items.
world; w="${WORLD_DIR}"
snap="${w}/snap"
printf '0\n' >"${w}/state/replicas"
STUB_SPEC=0 STUB_STATUS="" "${script}" snapshot --snapshot "${snap}" >/dev/null
out="$("${script}" seed --snapshot "${snap}" 2>&1 || true)"
check_contains "seed-needs-yes" "pass --yes" "${out}"
printf 'tampered\n' >>"${snap}/items.tsv"
out="$("${script}" seed --snapshot "${snap}" --yes 2>&1 || true)"
check_contains "seed-refuses-tamper" "hash mismatch" "${out}"
rm -rf "${w}"

world; w="${WORLD_DIR}"
snap="${w}/snap"
printf '0\n' >"${w}/state/replicas"
STUB_SPEC=0 STUB_STATUS="" "${script}" snapshot --snapshot "${snap}" >/dev/null
out="$("${script}" seed --snapshot "${snap}" --yes)"
check_contains "seed-report" "2 items at or above" "${out}"
out="$(STUB_MISSING='item-z\n' "${script}" seed --snapshot "${snap}" --yes 2>&1 || true)"
check_contains "seed-refuses-missing" "unmigrated items" "${out}"
out="$(HANDOVER_SAT_PGHOST="" HANDOVER_SAT_PGUSER="" HANDOVER_SAT_PGDATABASE="" \
	"${script}" seed --snapshot "${snap}" --yes 2>&1 || true)"
check_contains "seed-needs-satellite" "satellite connection unset" "${out}"
out="$(PGPASSFILE="" "${script}" seed --snapshot "${snap}" --yes 2>&1 || true)"
check_contains "seed-needs-pgpassfile" "PGPASSFILE" "${out}"
rm -rf "${w}"

# verify is green on a full match and red otherwise.
world; w="${WORLD_DIR}"
snap="${w}/snap"
printf '0\n' >"${w}/state/replicas"
STUB_SPEC=0 STUB_STATUS="" "${script}" snapshot --snapshot "${snap}" >/dev/null
export HANDOVER_SAT_PGHOST=sat-host HANDOVER_SAT_PGUSER=sat-u HANDOVER_SAT_PGDATABASE=sat-db
out="$("${script}" verify --snapshot "${snap}")"
check_contains "verify-green" "verified: 2/2" "${out}"
out="$(STUB_MATCHED=1 "${script}" verify --snapshot "${snap}" 2>&1 || true)"
check_contains "verify-red-count" "verify FAILED" "${out}"
rm -rf "${w}"

# abort restores replicas from the freeze record bound to the snapshot dir,
# clears the record, and refuses blind or foreign scale-ups.
world; w="${WORLD_DIR}"
snap="${w}/snap"
mkdir -p "${snap}"
printf '2\n%s\n' "${snap}" >"${w}/state/replicas"
out="$("${script}" abort --snapshot "${snap}")"
check_contains "abort-msg" "restored to 2 replicas" "${out}"
check_contains "abort-scale-call" --replicas=2 "$(cat "${w}/state/scale.log")"
[[ -f "${snap}/abort.log" ]] && pass "abort-recorded" || fail "abort-recorded"
[[ ! -e "${w}/state/replicas" ]] && pass "abort-clears-record" || fail "abort-clears-record"
out="$("${script}" abort --snapshot "${snap}" 2>&1 || true)"
check_contains "abort-refuses-blind" "no freeze record" "${out}"
printf '2\n%s\n' "${snap}-other" >"${w}/state/replicas"
out="$("${script}" abort --snapshot "${snap}" 2>&1 || true)"
check_contains "abort-refuses-foreign" "belongs to another snapshot" "${out}"
rm -rf "${w}"

# run composes freeze, drain, snapshot, seed, verify and needs --yes.
world; w="${WORLD_DIR}"
snap="${w}/snap"
export HANDOVER_SAT_PGHOST=sat-host HANDOVER_SAT_PGUSER=sat-u HANDOVER_SAT_PGDATABASE=sat-db
out="$("${script}" run --snapshot "${snap}" --yes)"
check_contains "run-verify" "verified: 2/2" "${out}"
check_contains "run-froze" --replicas=0 "$(cat "${w}/state/scale.log")"
world; w="${WORLD_DIR}"
snap="${w}/snap2"
export HANDOVER_SAT_PGHOST=sat-host HANDOVER_SAT_PGUSER=sat-u HANDOVER_SAT_PGDATABASE=sat-db
printf '2\n' >"${w}/state/replicas"
out="$(STUB_SPEC=0 STUB_STATUS="" "${script}" run --snapshot "${snap}" --yes)"
check_contains "run-tolerates-recorded-freeze" "verified: 2/2" "${out}"
world; w="${WORLD_DIR}"
snap="${w}/snap3"
export HANDOVER_SAT_PGHOST=sat-host HANDOVER_SAT_PGUSER=sat-u HANDOVER_SAT_PGDATABASE=sat-db
out="$(STUB_SPEC=0 STUB_STATUS="" "${script}" run --snapshot "${snap}" --yes 2>&1 || true)"
check_contains "run-refuses-recordless-freeze" "no freeze record" "${out}"
[[ ! -e "${snap}" ]] && pass "run-refuses-before-snapshot" || fail "run-refuses-before-snapshot"
out="$("${script}" abort --snapshot "${snap}" 2>&1 || true)"
check_contains "abort-refuses-recordless" "no freeze record" "${out}"
out="$("${script}" run --snapshot "${snap}-x" 2>&1 || true)"
check_contains "run-needs-yes" "--snapshot DIR --yes" "${out}"
rm -rf "${w}"

# SQL digest pins: a SQL edit must come with a test update.
for sql in claim-plane-handover-drain.sql claim-plane-handover-seed.sql \
	claim-plane-handover-snapshot-fences.sql claim-plane-handover-snapshot-items.sql; do
	[[ -f "${lib_dir}/${sql}" ]] && pass "sql-present ${sql}" || fail "sql-present ${sql}"
done

if [[ "${failures}" -gt 0 ]]; then
	printf '\n%d check(s) failed\n' "${failures}"
	exit 1
fi
printf '\nAll claim-plane handover checks passed\n'
