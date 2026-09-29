#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Reproduction harness for docs/internal/evidence/7265-liveness-recovery-progress-window.md.
# It drives a disposable Postgres container you started yourself, for example:
#
#   docker run -d --name eshu7265-pg -e POSTGRES_USER=eshu -e POSTGRES_PASSWORD=eshu \
#     -e POSTGRES_DB=eshu postgres:18-alpine -c shared_buffers=256MB -c work_mem=16MB
#
# Subcommands (run from anywhere inside the repository):
#
#   explain.sh schema          apply the production DDL the liveness queries touch
#   explain.sh seed            load seed.sql (about 122K in-window completions)
#   explain.sh seed-doubled    load seed.sql with exact completions packed into
#                              5 minutes (about 250K in-window completions)
#   explain.sh extract         write recover.sql and count.sql from the Go consts
#   explain.sh bench [rounds]  interleaved EXPLAIN (ANALYZE, BUFFERS) of the
#                              extracted recover and count queries, then the
#                              bucket counts; default 7 rounds
#
# Every EXPLAIN runs inside BEGIN/ROLLBACK because the recovery query writes.
# Never point it at a database you care about: seeding TRUNCATEs the queue
# tables and only runs with ESHU_7265_I_AM_DISPOSABLE=1.
set -euo pipefail

container="${ESHU_7265_CONTAINER:-eshu7265-pg}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(git -C "${here}" rev-parse --show-toplevel)"
work="${ESHU_7265_WORKDIR:-${TMPDIR:-/tmp}/eshu-7265-explain}"
mkdir -p "${work}"

psql_exec() { docker exec -i "${container}" psql -U eshu -X -q -v ON_ERROR_STOP=1 "$@"; }

vacuum_all() {
	local table
	for table in shared_projection_intents fact_work_items scope_generations ingestion_scopes; do
		psql_exec -c "VACUUM ANALYZE ${table}"
	done
}

reset_and_seed() {
	# TRUNCATE ... CASCADE wipes the queue tables, so seeding refuses to run
	# unless the caller states the target container is a disposable fixture.
	if [ "${ESHU_7265_I_AM_DISPOSABLE:-}" != "1" ]; then
		echo "refusing to TRUNCATE container '${container}': set ESHU_7265_I_AM_DISPOSABLE=1 to confirm it is a throwaway fixture database" >&2
		return 1
	fi
	psql_exec -c "TRUNCATE shared_projection_intents, fact_work_items, scope_generations, ingestion_scopes CASCADE"
	psql_exec <"$1"
	vacuum_all
	psql_exec -tA -c "SELECT 'in-window completions: ' || count(*) FROM shared_projection_intents WHERE completed_at > now() - interval '10 minutes'"
}

# explain_one <sql-file> <recover|count>: EXPLAIN one extracted query with the
# fixture's bind values (30m deadline, 5 attempts, batch 200, 10m window).
explain_one() {
	local types args
	case "$2" in
	recover)
		types="timestamptz,int,int,timestamptz,timestamptz"
		args="now()-interval '30 minutes', 5, 200, now(), now()-interval '10 minutes'"
		;;
	count)
		types="timestamptz,timestamptz,timestamptz,timestamptz"
		args="now()-interval '15 minutes', now()-interval '30 minutes', now(), now()-interval '10 minutes'"
		;;
	esac
	{
		echo "BEGIN;"
		echo "PREPARE q(${types}) AS"
		cat "$1"
		echo ";"
		echo "EXPLAIN (ANALYZE, BUFFERS, SETTINGS) EXECUTE q(${args});"
		echo "ROLLBACK;"
	} | psql_exec
}

case "${1:-}" in
schema)
	for migration in 001_ingestion_scopes 002_scope_generations 005_fact_work_items \
		008_shared_projection_intents 043_dead_letter_poison_idx \
		091_ingestion_scopes_active_state_snapshot_index \
		108_shared_projection_generation_pending_index \
		113_fact_work_items_cross_scope_source_v2_idx; do
		psql_exec <"${repo_root}/go/internal/storage/postgres/migrations/${migration}.sql"
	done
	;;
seed)
	reset_and_seed "${here}/seed.sql"
	;;
seed-doubled)
	sed "s/WHEN 3 THEN now() - (i % 1200) \* interval/WHEN 3 THEN now() - (i % 300) * interval/" \
		"${here}/seed.sql" >"${work}/seed-doubled.sql"
	reset_and_seed "${work}/seed-doubled.sql"
	;;
extract)
	python3 - "${repo_root}/go/internal/storage/postgres/generation_liveness_sql.go" "${work}" <<'PY'
import re
import sys

source = open(sys.argv[1]).read()
predicate = re.search(r"const generationIntentProgressingPredicate = `(.*?)`\n(?:\n|$)", source, re.S).group(1)
for const, name in (("recoverWedgedActiveGenerationsQuery", "recover"), ("countActiveGenerationsByAgeQuery", "count")):
    body = re.search(r"const " + const + r" = `(.*?)`\n(?:\n|$)", source, re.S).group(1)
    body = body.replace("` + generationIntentProgressingPredicate + `", predicate)
    if "`" in body:
        sys.exit(f"extract: {const} still contains a Go concatenation")
    open(f"{sys.argv[2]}/{name}.sql", "w").write(body)
PY
	echo "wrote ${work}/recover.sql and ${work}/count.sql"
	;;
bench)
	rounds="${2:-7}"
	for ((round = 1; round <= rounds; round++)); do
		psql_exec -c "VACUUM fact_work_items"
		explain_one "${work}/recover.sql" recover >"${work}/recover.run${round}.txt"
		explain_one "${work}/count.sql" count >"${work}/count.run${round}.txt"
	done
	for name in recover count; do
		printf '%s ms: ' "${name}"
		cat "${work}/${name}".run*.txt | sed -n 's/.*Execution Time: \([0-9.]*\) ms.*/\1/p' | sort -n | tr '\n' ' '
		echo
	done
	{
		echo "PREPARE c(timestamptz,timestamptz,timestamptz,timestamptz) AS"
		cat "${work}/count.sql"
		echo ";"
		echo "EXECUTE c(now()-interval '15 minutes', now()-interval '30 minutes', now(), now()-interval '10 minutes');"
	} | psql_exec -tA
	echo "plans: ${work}/{recover,count}.run*.txt"
	;;
*)
	sed -n '5,20p' "$0"
	exit 2
	;;
esac
