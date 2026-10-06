#!/usr/bin/env bash
#
# test-pg-statement-report.sh - hermetic tests for scripts/pg-statement-report.sh
# (#7596). A stub psql on PATH answers each query by its SQL text and logs every
# call, so the tests need no Postgres server.
#
# Cases:
#   1. pg_stat_statements not preloaded -> exit 3, one sentence naming
#      shared_preload_libraries and the restart, no report.
#   2. preloaded but extension missing -> exit 3, no DDL.
#   3. loaded -> both statement sections plus the residency section, every
#      query in a READ ONLY transaction with a statement_timeout, query text cut
#      to 200 characters, min 20 calls on the mean list.
#   4. no --install -> no CREATE EXTENSION reaches psql.
#   5. --install -> CREATE EXTENSION for both modules reaches psql.
#   6. pg_buffercache missing -> residency section says it is skipped.
#   7. --dsn reaches psql as -d and never appears in the output.
#   8. unknown argument -> exit 2.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
script="${repo_root}/scripts/pg-statement-report.sh"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

mkdir -p "${tmp_root}/bin"
cat >"${tmp_root}/bin/psql" <<'STUB'
#!/usr/bin/env bash
# Stub psql: log the arguments and the SQL, then answer by SQL text.
sql="$(cat)"
{
  printf 'ARGS: %s\n' "$*"
  printf '%s\n' "$sql"
  printf -- '-----\n'
} >>"${STUB_LOG}"
case "$sql" in
  *"SHOW shared_preload_libraries"*) echo "${STUB_PRELOAD}" ;;
  *"CREATE EXTENSION"*) echo "CREATE EXTENSION" ;;
  *"extname = 'pg_stat_statements'"*) echo "${STUB_PGSS_EXT}" ;;
  *"extname = 'pg_buffercache'"*) echo "${STUB_BUF_EXT}" ;;
  *"pg_stat_statements_info"*) echo "stub-window-row" ;;
  *"ORDER BY s.total_exec_time"*) echo "stub-total-row" ;;
  *"ORDER BY s.mean_exec_time"*) echo "stub-mean-row" ;;
  *"pg_buffercache b"*) echo "stub-residency-row" ;;
esac
exit 0
STUB
chmod +x "${tmp_root}/bin/psql"

PASS=0
FAIL=0
record_pass() { PASS=$((PASS + 1)); printf 'ok - %s\n' "$1"; }
record_fail() { FAIL=$((FAIL + 1)); printf 'not ok - %s\n' "$1" >&2; }

# run_case <preload> <pgss_ext_count> <buf_ext_count> [script args...]
# Sets: rc, out_file (stdout+stderr), log_file (stub psql log).
run_case() {
  export STUB_PRELOAD="$1" STUB_PGSS_EXT="$2" STUB_BUF_EXT="$3"
  shift 3
  out_file="${tmp_root}/out.txt"
  log_file="${tmp_root}/psql.log"
  : >"${log_file}"
  export STUB_LOG="${log_file}"
  rc=0
  PATH="${tmp_root}/bin:${PATH}" bash "${script}" "$@" >"${out_file}" 2>&1 || rc=$?
}

has() { rg -q --fixed-strings -- "$1" "$2"; }

[ -f "${script}" ] || {
  record_fail "scripts/pg-statement-report.sh exists"
  printf '%s passed, %s failed\n' "${PASS}" "${FAIL}"
  exit 1
}

# 1. Not preloaded.
run_case "pg_cron" 1 1
if [ "${rc}" -eq 3 ]; then record_pass "not preloaded exits 3"; else record_fail "not preloaded exits 3 (got ${rc})"; fi
if has "shared_preload_libraries" "${out_file}" && has "restart Postgres" "${out_file}"; then
  record_pass "not preloaded names shared_preload_libraries and the restart"
else
  record_fail "not preloaded names shared_preload_libraries and the restart"
fi
if [ "$(wc -l <"${out_file}" | tr -d ' ')" = "1" ]; then
  record_pass "not preloaded prints one line"
else
  record_fail "not preloaded prints one line"
fi
if has "Top 20" "${out_file}"; then record_fail "not preloaded prints no report"; else record_pass "not preloaded prints no report"; fi

# 2. Preloaded, extension missing.
run_case "pg_stat_statements" 0 0
if [ "${rc}" -eq 3 ]; then record_pass "missing extension exits 3"; else record_fail "missing extension exits 3 (got ${rc})"; fi
if ! has "CREATE EXTENSION IF NOT EXISTS" "${log_file}"; then
  record_pass "missing extension runs no DDL"
else
  record_fail "missing extension runs no DDL"
fi

# 3. Loaded.
run_case "pg_stat_statements" 1 1
if [ "${rc}" -eq 0 ]; then record_pass "loaded exits 0"; else record_fail "loaded exits 0 (got ${rc})"; cat "${out_file}" >&2; fi
for needle in "Top 20 statements by total_exec_time" "Top 20 statements by mean_exec_time (at least 20 calls)" \
  "Shared-buffer residency" "stub-total-row" "stub-mean-row" "stub-residency-row" "stub-window-row"; do
  if has "${needle}" "${out_file}"; then record_pass "report has: ${needle}"; else record_fail "report has: ${needle}"; fi
done
if has "s.calls >= 20" "${log_file}"; then record_pass "mean list requires 20 calls"; else record_fail "mean list requires 20 calls"; fi
if has "left(regexp_replace(s.query" "${log_file}" && has ", 200) AS query" "${log_file}"; then
  record_pass "query text cut to 200 characters"
else
  record_fail "query text cut to 200 characters"
fi
# Every SQL script sent to psql, except the DDL, must be READ ONLY with a timeout.
calls="$(rg -c '^ARGS:' "${log_file}")"
ro="$(rg -c '^BEGIN READ ONLY;' "${log_file}")"
to="$(rg -c "^SET LOCAL statement_timeout = '15s';" "${log_file}")"
if [ "${calls}" = "${ro}" ] && [ "${calls}" = "${to}" ]; then
  record_pass "all ${calls} queries are READ ONLY with a statement_timeout"
else
  record_fail "all queries are READ ONLY with a statement_timeout (calls=${calls} ro=${ro} timeout=${to})"
fi

# 4. No DDL without --install.
if has "CREATE EXTENSION IF NOT EXISTS" "${log_file}"; then record_fail "no DDL without --install"; else record_pass "no DDL without --install"; fi

# 5. --install runs the DDL.
run_case "pg_stat_statements" 1 1 --install
if [ "${rc}" -eq 0 ] && has "CREATE EXTENSION IF NOT EXISTS pg_stat_statements" "${log_file}" &&
  has "CREATE EXTENSION IF NOT EXISTS pg_buffercache" "${log_file}"; then
  record_pass "--install creates both extensions"
else
  record_fail "--install creates both extensions (rc=${rc})"
fi

# 6. pg_buffercache missing.
run_case "pg_stat_statements" 1 0
if [ "${rc}" -eq 0 ] && has "pg_buffercache is not installed" "${out_file}" && ! has "stub-residency-row" "${out_file}"; then
  record_pass "missing pg_buffercache skips residency"
else
  record_fail "missing pg_buffercache skips residency (rc=${rc})"
fi

# 7. --dsn reaches psql and stays out of the output.
run_case "pg_stat_statements" 1 1 --dsn "postgresql://u:s3cret-pw@db.example:5432/eshu"
if has "-d postgresql://u:s3cret-pw@db.example:5432/eshu" "${log_file}" && ! has "s3cret-pw" "${out_file}"; then
  record_pass "--dsn is passed to psql and not printed"
else
  record_fail "--dsn is passed to psql and not printed"
fi

# 8. Bad usage.
run_case "pg_stat_statements" 1 1 --bogus
if [ "${rc}" -eq 2 ]; then record_pass "unknown argument exits 2"; else record_fail "unknown argument exits 2 (got ${rc})"; fi

printf '%s passed, %s failed\n' "${PASS}" "${FAIL}"
[ "${FAIL}" -eq 0 ]
