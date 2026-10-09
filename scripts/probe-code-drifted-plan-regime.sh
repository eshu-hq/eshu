#!/usr/bin/env bash
# probe-code-drifted-plan-regime: reproduce the #7531 acceptance-1 probe.
#
# Builds the 5x band shim (11.5M rows, #7254 sizes) and runs the auto-mode
# sixth-execution probe of the shipped listCodeDriftedPairsQuery text, plus
# wall medians and an ANALYZE-sample flip survey. The query text is derived
# from the Go const at probe time, never hand-copied; table DDL comes from
# the migration files verbatim.
#
# Usage:
#   probe-code-drifted-plan-regime.sh shim <admin-dsn> <base-db>
#   probe-code-drifted-plan-regime.sh probe <dsn> <auto|force_custom_plan|force_generic_plan> [out-file]
#   probe-code-drifted-plan-regime.sh wall <with-dsn> <noidx-dsn> [rounds]
#   probe-code-drifted-plan-regime.sh survey <dsn> [samples]
#
# <admin-dsn> must be a superuser DSN for CREATE DATABASE (used only by
# shim). All other subcommands take DSNs of populated shim databases.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
sql_dir="${repo_root}/go/internal/storage/postgres"
query_const="${sql_dir}/code_drifted_evidence_sql.go"

usage() {
	sed -n '2,/^$/p' "$0" >&2
	exit "${1:-1}"
}

db_dsn() { # <admin-dsn> <db>: the same DSN pointed at another database.
	local base="${1%%\?*}" query=""
	[[ "$1" == *\?* ]] && query="?${1#*\?}"
	printf '%s/%s%s\n' "${base%/*}" "$2" "${query}"
}

extract_query() { # <outfile>: verbatim listCodeDriftedPairsQuery text.
	awk '/^const listCodeDriftedPairsQuery = `/{flag=1;next}/^`/{if(flag){exit}}flag' \
		"${query_const}" >"$1"
	rg -q 'kept_buckets' "$1" || {
		echo "probe: query extraction failed (kept_buckets missing)" >&2
		exit 1
	}
}

require_local_dsn() { # <dsn>: refuse to drop databases anywhere but localhost.
	case "$1" in
	*127.0.0.1* | *localhost*) return 0 ;;
	*)
		echo "probe: refusing non-localhost DSN: $1" >&2
		exit 1
		;;
	esac
}

cmd_shim() { # <admin-dsn> <base-db>
	[ $# -eq 2 ] || usage
	local admin_dsn="$1" base="$2" load db
	require_local_dsn "${admin_dsn}"
	load="$(mktemp)"
	trap 'rm -f "${load:-}"' EXIT
	db="$(db_dsn "${admin_dsn}" "${base}")"
	{
		echo "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '${base}' AND pid <> pg_backend_pid();"
		echo "DROP DATABASE IF EXISTS ${base};"
		echo "CREATE DATABASE ${base};"
	} >"${load}"
	psql "${admin_dsn}" -v ON_ERROR_STOP=1 -q -f "${load}"
	psql "${db}" -v ON_ERROR_STOP=1 -q -f "${sql_dir}/migrations/111_code_function_fingerprint.sql" >/dev/null
	psql "${db}" -v ON_ERROR_STOP=1 -q -f "${sql_dir}/migrations/117_code_function_fingerprint_shingles.sql" >/dev/null
	{
		echo "ALTER TABLE code_fingerprint_band SET (autovacuum_enabled = false);"
		echo "ALTER TABLE code_function_fingerprint SET (autovacuum_enabled = false);"
		echo "SELECT setseed(0.7531);"
		echo "INSERT INTO code_fingerprint_band (repo_id, band_no, band_hash, entity_id)"
		echo "SELECT 'repo-5x', b, md5('fam' || ((g / 25)) || 'b' || b), 'repo-5x:e' || g"
		echo "FROM generate_series(0, 249) AS g, generate_series(0, 31) AS b;"
		echo "INSERT INTO code_fingerprint_band (repo_id, band_no, band_hash, entity_id)"
		echo "SELECT 'repo-5x', b, md5(floor(random() * 130000)::text), 'repo-5x:e' || g"
		echo "FROM generate_series(250, 24974) AS g, generate_series(0, 31) AS b;"
		echo "INSERT INTO code_fingerprint_band (repo_id, band_no, band_hash, entity_id)"
		echo "SELECT 'repo-1x', b, md5('fam1x' || ((g / 25)) || 'b' || b), 'repo-1x:e' || g"
		echo "FROM generate_series(0, 199) AS g, generate_series(0, 31) AS b;"
		echo "INSERT INTO code_fingerprint_band (repo_id, band_no, band_hash, entity_id)"
		echo "SELECT 'repo-1x', b, md5(floor(random() * 30000)::text), 'repo-1x:e' || g"
		echo "FROM generate_series(200, 4994) AS g, generate_series(0, 31) AS b;"
		echo "INSERT INTO code_fingerprint_band (repo_id, band_no, band_hash, entity_id)"
		echo "SELECT 'repo-f' || r, s % 32, md5('f' || r || 's' || s), 'repo-f' || r || ':e' || (s / 32)"
		echo "FROM generate_series(1, 200) AS r, generate_series(0, 52511) AS s;"
		echo "INSERT INTO code_function_fingerprint (entity_id, repo_id, fp_exact, fp_renamed, sketch, token_count, shingles, indexed_at)"
		echo "SELECT 'repo-5x:e' || g, 'repo-5x', md5('e5x' || g), md5('r5x' || g), '00', 50 + (g % 451), 'aa', now()"
		echo "FROM generate_series(0, 24974) AS g;"
		echo "INSERT INTO code_function_fingerprint (entity_id, repo_id, fp_exact, fp_renamed, sketch, token_count, shingles, indexed_at)"
		echo "SELECT 'repo-1x:e' || g, 'repo-1x', md5('e1x' || g), md5('r1x' || g), '00', 50 + (g % 451), 'aa', now()"
		echo "FROM generate_series(0, 4994) AS g;"
		echo "ANALYZE code_fingerprint_band; ANALYZE code_function_fingerprint;"
	} >"${load}"
	psql "${db}" -v ON_ERROR_STOP=1 -q -f "${load}"
	rm -f "${load}"
	trap - EXIT
	printf 'probe: shim database %s ready (ANALYZEd, autovacuum held off)\n' "${base}"
}

cmd_probe() { # <dsn> <mode> [out-file]
	[ $# -ge 2 ] || usage
	local dsn="$1" mode="$2" out="${3:-/dev/stdout}" qfile
	case "${mode}" in
	auto | force_custom_plan | force_generic_plan) ;;
	*)
		echo "probe: mode must be auto, force_custom_plan, or force_generic_plan" >&2
		exit 1
		;;
	esac
	qfile="$(mktemp)"
	trap 'rm -f "${qfile:-}"' EXIT
	extract_query "${qfile}"
	{
		echo "SET plan_cache_mode = ${mode};"
		printf 'PREPARE drift(text,int,int,int) AS\n'
		cat "${qfile}"
		echo ';'
		echo '\o /dev/null'
		for _ in 1 2 3 4 5; do echo "EXECUTE drift('repo-5x',50,200,200);"; done
		echo '\o'
		echo "EXPLAIN (ANALYZE, BUFFERS) EXECUTE drift('repo-5x',50,200,200);"
		echo "SELECT name, generic_plans, custom_plans FROM pg_prepared_statements WHERE name='drift';"
	} | psql "${dsn}" -v ON_ERROR_STOP=1 -q >"${out}" 2>&1
	rm -f "${qfile}"
	trap - EXIT
	printf 'probe: sixth-execution %s plan written\n' "${mode}"
}

cmd_wall() { # <with-dsn> <noidx-dsn> [rounds]
	[ $# -ge 2 ] || usage
	local with_dsn="$1" noidx_dsn="$2" rounds="${3:-5}" qfile prep
	qfile="$(mktemp)"
	prep="$(mktemp)"
	trap 'rm -f "${qfile:-}" "${prep:-}"' EXIT
	extract_query "${qfile}"
	{
		echo "SET plan_cache_mode = force_custom_plan;"
		printf 'PREPARE driftc(text,int,int,int) AS\n'
		cat "${qfile}"
		echo ';'
		echo "SET plan_cache_mode = force_generic_plan;"
		printf 'PREPARE driftg(text,int,int,int) AS\n'
		cat "${qfile}"
		echo ';'
	} >"${prep}"
	for round in $(seq 1 "${rounds}"); do
		for arm in WITH NONE; do
			dsn="${with_dsn}"
			[ "${arm}" = NONE ] && dsn="${noidx_dsn}"
			for mode in driftc driftg; do
				# plan_cache_mode is consulted at EXECUTE time, not PREPARE
				# time: the SET for this timing must immediately precede
				# its EXECUTE or both columns measure the same plan.
				plan_mode="force_custom_plan"
				[ "${mode}" = driftg ] && plan_mode="force_generic_plan"
				t0=$(date +%s.%N)
				{
					cat "${prep}"
					echo "SET plan_cache_mode = ${plan_mode};"
					echo "EXECUTE ${mode}('repo-5x',50,200,200);"
				} | psql "${dsn}" -v ON_ERROR_STOP=1 -q -o /dev/null
				t1=$(date +%s.%N)
				printf 'round=%s arm=%s mode=%s secs=%s\n' "${round}" "${arm}" "${mode}" \
					"$(awk -v a="${t1}" -v b="${t0}" 'BEGIN{print a - b}')"
			done
		done
	done
	rm -f "${qfile}" "${prep}"
	trap - EXIT
}

plan_signature() { # <planfile>: one-line join/access summary.
	local sig=""
	rg -q "Merge Join" "$1" && sig="${sig}merge;"
	rg -q "Hash Join" "$1" && sig="${sig}hash;"
	rg -q "Nested Loop" "$1" && sig="${sig}nest;"
	rg -q "code_fingerprint_band_pkey" "$1" && sig="${sig}pkey;"
	rg -q "code_fingerprint_band_lookup_idx" "$1" && sig="${sig}lookup;"
	rg -q "code_fingerprint_band_entity_idx" "$1" && sig="${sig}entity;"
	rg -q "Seq Scan on code_fingerprint_band" "$1" && sig="${sig}seq;"
	rg -q "Bitmap Heap Scan on code_fingerprint_band" "$1" && sig="${sig}bitmap;"
	rg -q "Index Only Scan" "$1" && sig="${sig}idxonly;"
	printf '%s\n' "${sig}"
}

cmd_survey() { # <dsn> [samples]
	[ $# -ge 1 ] || usage
	local dsn="$1" samples="${2:-40}" qfile plan
	qfile="$(mktemp)"
	plan="$(mktemp)"
	trap 'rm -f "${qfile:-}" "${plan:-}"' EXIT
	extract_query "${qfile}"
	for sample in $(seq 1 "${samples}"); do
		psql "${dsn}" -q -c "ANALYZE code_fingerprint_band;" >/dev/null
		{
			echo "SET plan_cache_mode = force_custom_plan;"
			printf 'PREPARE drift(text,int,int,int) AS\n'
			cat "${qfile}"
			echo ';'
			echo "EXPLAIN EXECUTE drift('repo-5x',50,200,200);"
		} | psql "${dsn}" -v ON_ERROR_STOP=1 -q >"${plan}" 2>&1
		printf 'sample=%s sig=%s\n' "${sample}" "$(plan_signature "${plan}")"
	done
	rm -f "${qfile}" "${plan}"
	trap - EXIT
}

[ $# -ge 1 ] || usage
sub="$1"
shift
case "${sub}" in
shim) cmd_shim "$@" ;;
probe) cmd_probe "$@" ;;
wall) cmd_wall "$@" ;;
survey) cmd_survey "$@" ;;
*) usage ;;
esac
