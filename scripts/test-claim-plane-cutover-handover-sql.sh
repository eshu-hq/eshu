#!/usr/bin/env bash
# Real-Postgres proof for the SQL under scripts/lib/claim-plane-handover-*.sql
# (#9376). The stub test (scripts/test-claim-plane-cutover-handover.sh)
# cannot see a column rename or a wrong predicate, so this script runs every
# SQL file exactly as the handover script does, against a private throwaway
# cluster built from the real migrations, and asserts the answers.
#
# It needs initdb, pg_ctl, and psql (PG_BIN=/path/to/bin overrides the
# default /usr/lib/postgresql/18/bin). The satellite schema comes from the
# eshu-workflow checkout (SAT_MIGRATIONS=/path/to/store/migrations
# overrides the ../eshu-workflow default). It is a local proof tool, not a
# CI gate: CI has no guaranteed Postgres. A SQL edit that changes answers
# fails here and must come with a test update.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
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
for f in claim-plane-handover-snapshot-items.sql claim-plane-handover-snapshot-fences.sql \
	claim-plane-handover-drain.sql claim-plane-handover-seed.sql; do
	[[ -f "${sql_dir}/${f}" ]] || fail "SQL file missing: ${f}"
done

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
q postgres "INSERT INTO workflow_runs(run_id, trigger_kind, status, created_at, updated_at) VALUES ('run-1','manual','active',now(),now())" >/dev/null
q postgres "INSERT INTO ingestion_scopes(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status) VALUES ('scope-1','repo','jira','k','jira','p',now(),now(),'active') ON CONFLICT DO NOTHING" >/dev/null
q postgres "INSERT INTO scope_generations(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status) VALUES ('gen-1','scope-1','manual',now(),now(),'active') ON CONFLICT DO NOTHING" >/dev/null
q postgres "INSERT INTO workflow_work_items(work_item_id, run_id, collector_kind, collector_instance_id, source_system, scope_id, acceptance_unit_id, source_run_id, generation_id, status, current_claim_id, current_fencing_token, created_at, updated_at) VALUES
 ('item-a','run-1','jira','jira-primary','jira','scope-1','au-1','sr-1','gen-1','claimed','claim-a',3,now(),now()),
 ('item-b','run-1','jira','jira-primary','jira','scope-1','au-1','sr-1','gen-1','pending',NULL,7,now(),now()),
 ('item-c','run-1','jira','jira-primary','jira','scope-1','au-1','sr-1','gen-1','pending',NULL,1,now(),now())" >/dev/null
q postgres "INSERT INTO workflow_claims(claim_id, work_item_id, fencing_token, owner_id, status, claimed_at, heartbeat_at, lease_expires_at, created_at, updated_at) VALUES
 ('claim-a','item-a',3,'owner-1','active',now(),now(),now() + interval '1 hour',now(),now()),
 ('claim-c','item-c',1,'owner-1','expired',now(),now(),now() - interval '1 hour',now(),now())" >/dev/null
q postgres "INSERT INTO generation_commit_fences(generation_id, work_item_id, scope_id, claim_id, fencing_token, commit_id, committed_at) VALUES ('gen-1','item-a','scope-1','claim-a',3,'commit-a',now())" >/dev/null

# Snapshot answers are exact and deterministic across runs.
want_items="$(printf 'item-a\t3\tclaim-a\tclaimed\nitem-b\t7\t\tpending\nitem-c\t1\t\tpending')"
check "snapshot-items" "${want_items}" "$(sql postgres claim-plane-handover-snapshot-items.sql)"
check "snapshot-items-idempotent" "$(sql postgres claim-plane-handover-snapshot-items.sql)" "$(sql postgres claim-plane-handover-snapshot-items.sql)"
check "snapshot-fences" "$(printf 'gen-1\titem-a\t3')" "$(sql postgres claim-plane-handover-snapshot-fences.sql)"

# Drain sees the live claim, then goes quiet once the lease lapses.
check "drain-live" "$(printf '1\n1')" "$(sql postgres claim-plane-handover-drain.sql)"
q postgres "UPDATE workflow_claims SET lease_expires_at = now() - interval '1 minute' WHERE claim_id = 'claim-a'" >/dev/null
check "drain-quiet" "$(printf '0\n0')" "$(sql postgres claim-plane-handover-drain.sql)"

# Satellite fixture: item-a behind the snapshot, item-b already ahead.
q sat "INSERT INTO workflow_runs(run_id, trigger_kind, status, created_at, updated_at) VALUES ('run-1','manual','active',now(),now())" >/dev/null
q sat "INSERT INTO workflow_work_items(work_item_id, run_id, collector_kind, collector_instance_id, scope_id, generation_id, status, current_fencing_token, created_at, updated_at) VALUES
 ('item-a','run-1','jira','jira-primary','scope-1','gen-1','pending',1,now(),now()),
 ('item-b','run-1','jira','jira-primary','scope-1','gen-1','pending',7,now(),now())" >/dev/null

# seed runs the committed seed file with a loaded temp table, in one session,
# via the same \copy path the handover script uses.
seed_with() { # snapshot-tsv-content
	local session="${work}/session.sql" tsv="${work}/snap.tsv"
	{
		printf 'CREATE TEMP TABLE handover_snapshot(work_item_id TEXT PRIMARY KEY, h BIGINT NOT NULL);\n'
		printf "\\copy handover_snapshot FROM '%s' WITH (FORMAT text, DELIMITER E'\\t');\n" "${tsv}"
	} >"${session}"
	printf '%b' "$1" >"${tsv}"
	"${PSQL}" -X -q -v ON_ERROR_STOP=1 -A -t -d sat -f "${session}" -f "${sql_dir}/claim-plane-handover-seed.sql"
}
snap="$(printf 'item-a\t3\nitem-b\t7\nitem-z\t5')"

# Seed advances behind items to max, never lowers, and reports the missing.
out="$(seed_with "${snap}")"
check "seed-matched" "2" "$(printf '%s' "${out}" | sed -n '1p')"
check "seed-missing" "item-z" "$(printf '%s' "${out}" | sed -n '2p')"
check "seed-advanced" "3" "$(q sat "SELECT current_fencing_token FROM workflow_work_items WHERE work_item_id = 'item-a'")"
check "seed-kept-ahead" "7" "$(q sat "SELECT current_fencing_token FROM workflow_work_items WHERE work_item_id = 'item-b'")"

# Re-seeding an older snapshot is a no-op (tokens never go back).
out="$(seed_with "$(printf 'item-a\t2\nitem-b\t7')")"
check "reseed-older-noop" "3" "$(q sat "SELECT current_fencing_token FROM workflow_work_items WHERE work_item_id = 'item-a'")"

# A newer snapshot advances again (idempotent re-seed is the verify path).
out="$(seed_with "$(printf 'item-a\t9\nitem-b\t7')")"
check "reseed-newer-advances" "9" "$(q sat "SELECT current_fencing_token FROM workflow_work_items WHERE work_item_id = 'item-a'")"
check "reseed-matched" "2" "$(printf '%s' "${out}" | sed -n '1p')"

if [[ "${failures}" -gt 0 ]]; then
	printf '\n%d handover SQL check(s) failed\n' "${failures}"
	exit 1
fi
printf '\nAll claim-plane handover SQL checks passed\n'
