#!/usr/bin/env bash
# Real-Postgres proof for the claim-plane cutover handover (#9376). The stub
# test cannot see a column rename, a wrong predicate, or a shape mismatch
# between the snapshot output and the seed input, so this script drives the
# REAL handover script (fake kubectl, real psql) against a private throwaway
# cluster built from the real core and satellite migrations, and asserts the
# answers. The seed input is always the real snapshot output, never a
# hand-made fixture: seed.tsv is cut from items.tsv by the script itself.
#
# It needs initdb, pg_ctl, and psql (PG_BIN=/path/to/bin overrides the
# default /usr/lib/postgresql/18/bin). The satellite schema comes from the
# eshu-workflow checkout (SAT_MIGRATIONS overrides the default lookup). It
# is a local proof tool, not a CI gate: CI has no guaranteed Postgres.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
handover="${repo_root}/scripts/claim-plane-cutover-handover.sh"
fake_kubectl="${repo_root}/scripts/lib/test-handover-fake-kubectl.sh"
sql_dir="${repo_root}/scripts/lib"
migrations="${repo_root}/go/internal/storage/postgres/migrations"
if [[ -n "${SAT_MIGRATIONS:-}" ]]; then
	sat_migrations="${SAT_MIGRATIONS}"
elif [[ -d "${HOME}/personal-repos/eshu-workflow/store/migrations" ]]; then
	sat_migrations="${HOME}/personal-repos/eshu-workflow/store/migrations"
else
	sat_migrations="${repo_root}/../eshu-workflow/store/migrations"
fi

fail() { printf 'handover-sql: %s\n' "$*" >&2; exit 1; }

pg_bin="${PG_BIN:-/usr/lib/postgresql/18/bin}"
[[ -x "${pg_bin}/initdb" ]] || fail "initdb not found at ${pg_bin}: set PG_BIN"
[[ -d "${sat_migrations}" ]] || fail "satellite migrations not found at ${sat_migrations}: set SAT_MIGRATIONS"

work="$(mktemp -d "${TMPDIR:-/tmp}/hosql.XXXXXX")"
cleanup() {
	"${pg_bin}/pg_ctl" -D "${work}/data" -m immediate stop >/dev/null 2>&1 || true
	rm -rf "${work}"
}
trap cleanup EXIT

"${pg_bin}/initdb" -D "${work}/data" -A trust -U postgres --no-sync >/dev/null
mkdir -p "${work}/s"
"${pg_bin}/pg_ctl" -D "${work}/data" -l "${work}/pg.log" -w \
	-o "-c listen_addresses='' -c unix_socket_directories=${work}/s -c fsync=off -c synchronous_commit=off -c full_page_writes=off" start >/dev/null
export PGHOST="${work}/s" PGUSER=postgres
PSQL="${pg_bin}/psql"

# sql runs a committed SQL file exactly as the handover script does.
sql() { # db file
	"${PSQL}" -X -q -v ON_ERROR_STOP=1 -A -t -F "$(printf '\t')" -d "$1" -f "${sql_dir}/$2"
}
q() { # db sql...
	"${PSQL}" -X -q -v ON_ERROR_STOP=1 -A -t -d "$1" -c "$2"
}

failures=0
pass() { printf 'PASS %s\n' "$1"; }
mismatch() { printf 'FAIL %s\nwant:\n%s\ngot:\n%s\n' "$1" "$2" "$3"; failures=$((failures + 1)); }
check() { # label want got
	if [[ "$2" == "$3" ]]; then pass "$1"; else mismatch "$1" "$2" "$3"; fi
}

# Core schema: scopes, generations, control plane, commit fences.
for migration in 001_ingestion_scopes 002_scope_generations 014_workflow_control_plane 270_generation_commit_fences; do
	"${PSQL}" -X -q -v ON_ERROR_STOP=1 -d postgres -f "${migrations}/${migration}.sql" >/dev/null
done
# Satellite schema in its own database.
"${PSQL}" -X -q -c 'CREATE DATABASE sat' postgres >/dev/null
for migration in 001_claim_plane_base 002_pacing_quota 003_coordinator_state 004_commit_pin; do
	"${PSQL}" -X -q -v ON_ERROR_STOP=1 -d sat -f "${sat_migrations}/${migration}.sql" >/dev/null
done

# Core fixture: H in {3,7} per the rehearsal shape, one live claim, one
# expired claim, one committed fence row.
q postgres "INSERT INTO ingestion_scopes(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status) VALUES ('scope-1','repo','jira','k','jira','p',now(),now(),'active')" >/dev/null
q postgres "INSERT INTO scope_generations(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status) VALUES ('gen-1','scope-1','manual',now(),now(),'active')" >/dev/null
q postgres "INSERT INTO workflow_runs(run_id, trigger_kind, status, created_at, updated_at) VALUES ('run-1','manual','active',now(),now())" >/dev/null
q postgres "INSERT INTO workflow_work_items(work_item_id, run_id, collector_kind, collector_instance_id, source_system, scope_id, acceptance_unit_id, source_run_id, generation_id, status, current_claim_id, current_fencing_token, created_at, updated_at) VALUES
 ('item-a','run-1','jira','jira-primary','jira','scope-1','au-1','sr-1','gen-1','claimed','claim-a',3,now(),now()),
 ('item-b','run-1','jira','jira-primary','jira','scope-1','au-1','sr-1','gen-1','pending',NULL,7,now(),now()),
 ('item-c','run-1','jira','jira-primary','jira','scope-1','au-1','sr-1','gen-1','pending',NULL,1,now(),now())" >/dev/null
q postgres "INSERT INTO workflow_claims(claim_id, work_item_id, fencing_token, owner_id, status, claimed_at, heartbeat_at, lease_expires_at, created_at, updated_at) VALUES
 ('claim-a','item-a',3,'owner-1','active',now(),now(),now() + interval '1 hour',now(),now()),
 ('claim-c','item-c',1,'owner-1','expired',now(),now(),now() - interval '1 hour',now(),now())" >/dev/null
q postgres "INSERT INTO generation_commit_fences(generation_id, work_item_id, scope_id, claim_id, fencing_token, commit_id, committed_at) VALUES ('gen-1','item-a','scope-1','claim-a',3,'commit-a',now())" >/dev/null

# Satellite fixture: item-a behind the snapshot, item-b already ahead,
# item-c not yet migrated (the seed must refuse on it first).
q sat "INSERT INTO workflow_runs(run_id, trigger_kind, status, created_at, updated_at) VALUES ('run-1','manual','active',now(),now())" >/dev/null
q sat "INSERT INTO workflow_work_items(work_item_id, run_id, collector_kind, collector_instance_id, scope_id, generation_id, status, current_fencing_token, created_at, updated_at) VALUES
 ('item-a','run-1','jira','jira-primary','scope-1','gen-1','pending',1,now(),now()),
 ('item-b','run-1','jira','jira-primary','scope-1','gen-1','pending',7,now(),now())" >/dev/null

# Drive the real handover script: fake frozen kubectl, real databases.
export HANDOVER_KUBECTL_BIN="${fake_kubectl}" HANDOVER_PSQL_BIN="${PSQL}"
export HANDOVER_SHA_BIN=sha256sum HANDOVER_SLEEP_BIN=true
export HANDOVER_NAMESPACE=test-ns PGPASSFILE=dummy
export HANDOVER_STATE_FILE="${work}/replicas"
export HANDOVER_SAT_PGHOST="${work}/s" HANDOVER_SAT_PGPORT=5432
export HANDOVER_SAT_PGUSER=postgres HANDOVER_SAT_PGDATABASE=sat
export STUB_STATE_DIR="${work}/kstub" STUB_SPEC=0 STUB_STATUS=""
mkdir -p "${STUB_STATE_DIR}"
snap="${work}/snap"

# Drain sees the live claim through the real script, then goes quiet.
if "${handover}" drain >/dev/null 2>&1; then
	mismatch "drain-live" "exit 1" "exit 0"
else
	pass "drain-live"
fi
q postgres "UPDATE workflow_claims SET lease_expires_at = now() - interval '1 minute' WHERE claim_id = 'claim-a'" >/dev/null
"${handover}" drain >/dev/null && pass "drain-quiet" || mismatch "drain-quiet" "exit 0" "exit 1"

# Snapshot through the real script: exact 4-column items, fences, and the
# 2-column seed input cut from the real items output. The fake kubectl
# reports frozen without a freeze command, so plant the matching record.
printf '0\n' >"${work}/replicas"
"${handover}" snapshot --snapshot "${snap}" >/dev/null
check "snapshot-items" "$(printf 'item-a\t3\tclaim-a\tclaimed\nitem-b\t7\t\tpending\nitem-c\t1\t\tpending')" "$(cat "${snap}/items.tsv")"
check "snapshot-fences" "$(printf 'gen-1\titem-a\t3')" "$(cat "${snap}/fences.tsv")"
check "snapshot-seed-tsv" "$(printf 'item-a\t3\nitem-b\t7\nitem-c\t1')" "$(cat "${snap}/seed.tsv")"
(cd "${snap}" && sha256sum -c sha256sums >/dev/null) && pass "snapshot-hash" || mismatch "snapshot-hash" "verified" "mismatch"

# Seed through the real script refuses on the unmigrated item-c first.
if "${handover}" seed --snapshot "${snap}" --yes >/dev/null 2>&1; then
	mismatch "seed-refuses-missing" "exit 1" "exit 0"
else
	pass "seed-refuses-missing"
fi
q sat "INSERT INTO workflow_work_items(work_item_id, run_id, collector_kind, collector_instance_id, scope_id, generation_id, status, current_fencing_token, created_at, updated_at) VALUES ('item-c','run-1','jira','jira-primary','scope-1','gen-1','pending',0,now(),now())" >/dev/null
out="$("${handover}" seed --snapshot "${snap}" --yes)"
check "seed-report" "seeded: 3 items at or above snapshot H, 0 snapshot items missing from satellite" "${out}"
check "seed-advanced" "3" "$(q sat "SELECT current_fencing_token FROM workflow_work_items WHERE work_item_id = 'item-a'")"
check "seed-kept-ahead" "7" "$(q sat "SELECT current_fencing_token FROM workflow_work_items WHERE work_item_id = 'item-b'")"
"${handover}" verify --snapshot "${snap}" >/dev/null && pass "verify-green" || mismatch "verify-green" "exit 0" "exit 1"

# Reseed semantics straight at the seed file with shape-correct inputs
# derived from the real seed.tsv (older H is a no-op, newer H advances).
reseed_with() { # tsv-file
	local session="${work}/session.sql"
	{
		printf 'CREATE TEMP TABLE handover_snapshot(work_item_id TEXT PRIMARY KEY, h BIGINT NOT NULL);\n'
		printf "\\copy handover_snapshot FROM '%s' WITH (FORMAT text, DELIMITER E'\\t');\n" "$1"
		cat "${sql_dir}/claim-plane-handover-seed.sql"
	} >"${session}"
	"${PSQL}" -X -q -v ON_ERROR_STOP=1 -A -t -d sat -f "${session}" >/dev/null
}
awk -F'\t' 'BEGIN{OFS="\t"}{$2=$2-2;print}' "${snap}/seed.tsv" >"${work}/older.tsv"
reseed_with "${work}/older.tsv"
check "reseed-older-noop" "3" "$(q sat "SELECT current_fencing_token FROM workflow_work_items WHERE work_item_id = 'item-a'")"
awk -F'\t' 'BEGIN{OFS="\t"}{$2=$2+6;print}' "${snap}/seed.tsv" >"${work}/newer.tsv"
reseed_with "${work}/newer.tsv"
check "reseed-newer-advances" "9" "$(q sat "SELECT current_fencing_token FROM workflow_work_items WHERE work_item_id = 'item-a'")"

if [[ "${failures}" -gt 0 ]]; then
	printf '\n%d handover SQL check(s) failed\n' "${failures}"
	exit 1
fi
printf '\nAll claim-plane handover SQL checks passed\n'
