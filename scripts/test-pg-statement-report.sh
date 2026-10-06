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
#   9. the SQL binds the privacy filter (statement kinds), the self-exclusion,
#      and the current-database scope.
#  10. preload match is an exact list element; extension below 1.9 -> exit 3.
#  11. block read time is selected only when track_io_timing is on, with the
#      Postgres 17 column name or the older one.
#  12. a psql failure -> exit 1.
#  14. an empty, blank, or lookalike preload value exits 3; $libdir/ and quoted
#      entries count; the test runs the script with the bash that runs the test.
#  13. --no-buffers skips the residency section and never touches pg_buffercache.
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
if [ -n "${STUB_FAIL}" ] && [[ "$sql" == *"${STUB_FAIL}"* ]]; then
  echo "stub psql: forced failure" >&2
  exit 1
fi
{
  printf 'ARGS: %s\n' "$*"
  printf '%s\n' "$sql"
  printf -- '-----\n'
} >>"${STUB_LOG}"
case "$sql" in
  *"SHOW shared_preload_libraries"*) echo "${STUB_PRELOAD}" ;;
  *"CREATE EXTENSION"*) echo "CREATE EXTENSION" ;;
  *"extname = 'pg_stat_statements'"*) echo "${STUB_PGSS_EXT}" ;;
  *"server_version_num"*) echo "${STUB_IO}" ;;
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

# run_case <preload> <pgss_ext_version> <buf_ext_count> [script args...]
# Optional env per call: STUB_IO ("t|t" io timing on, PG17+; "t|f" on, PG16-;
# default "f|t" off) and STUB_FAIL (a SQL substring that makes psql fail).
# Sets: rc, out_file (stdout+stderr), log_file (stub psql log).
run_case() {
  export STUB_PRELOAD="$1" STUB_PGSS_EXT="$2" STUB_BUF_EXT="$3"
  export STUB_IO="${STUB_IO:-f|t}" STUB_FAIL="${STUB_FAIL:-}"
  shift 3
  out_file="${tmp_root}/out.txt"
  log_file="${tmp_root}/psql.log"
  : >"${log_file}"
  export STUB_LOG="${log_file}"
  rc=0
  PATH="${tmp_root}/bin:${PATH}" "${BASH}" "${script}" "$@" >"${out_file}" 2>&1 || rc=$?
  # Reset the per-call knobs: bash 3.2 keeps `VAR=x function` assignments.
  unset STUB_IO STUB_FAIL
}

has() { rg -q --fixed-strings -- "$1" "$2"; }

[ -f "${script}" ] || {
  record_fail "scripts/pg-statement-report.sh exists"
  printf '%s passed, %s failed\n' "${PASS}" "${FAIL}"
  exit 1
}

# 1. Not preloaded.
run_case "pg_cron" "1.12" 1
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
run_case "pg_stat_statements" "" 0
if [ "${rc}" -eq 3 ]; then record_pass "missing extension exits 3"; else record_fail "missing extension exits 3 (got ${rc})"; fi
if ! has "CREATE EXTENSION IF NOT EXISTS" "${log_file}"; then
  record_pass "missing extension runs no DDL"
else
  record_fail "missing extension runs no DDL"
fi

# 3. Loaded.
run_case "pg_stat_statements" "1.12" 1
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
run_case "pg_stat_statements" "1.12" 1 --install
if [ "${rc}" -eq 0 ] && has "CREATE EXTENSION IF NOT EXISTS pg_stat_statements" "${log_file}" &&
  has "CREATE EXTENSION IF NOT EXISTS pg_buffercache" "${log_file}"; then
  record_pass "--install creates both extensions"
else
  record_fail "--install creates both extensions (rc=${rc})"
fi

# 6. pg_buffercache missing.
run_case "pg_stat_statements" "1.12" 0
if [ "${rc}" -eq 0 ] && has "pg_buffercache is not installed" "${out_file}" && ! has "stub-residency-row" "${out_file}"; then
  record_pass "missing pg_buffercache skips residency"
else
  record_fail "missing pg_buffercache skips residency (rc=${rc})"
fi

# 7. --dsn reaches psql and stays out of the output.
run_case "pg_stat_statements" "1.12" 1 --dsn "postgresql://u:s3cret-pw@db.example:5432/eshu"
if has "-d postgresql://u:s3cret-pw@db.example:5432/eshu" "${log_file}" && ! has "s3cret-pw" "${out_file}"; then
  record_pass "--dsn is passed to psql and not printed"
else
  record_fail "--dsn is passed to psql and not printed"
fi

# 8. Bad usage.
run_case "pg_stat_statements" "1.12" 1 --bogus
if [ "${rc}" -eq 2 ]; then record_pass "unknown argument exits 2"; else record_fail "unknown argument exits 2 (got ${rc})"; fi

# 9. The SQL binds the privacy filter, the self-exclusion, and the database scope.
run_case "pg_stat_statements" "1.12" 1
if has "s.query ~* '^\\s*(select|insert|update|delete|merge|with)\\M'" "${log_file}"; then
  record_pass "SQL filters to normalized statement kinds"
else
  record_fail "SQL filters to normalized statement kinds"
fi
if has "s.query !~* 'pg_stat_statements|pg_buffercache|pg_extension|current_setting'" "${log_file}"; then
  record_pass "SQL excludes the report's own queries"
else
  record_fail "SQL excludes the report's own queries"
fi
if has "s.dbid = (SELECT oid FROM pg_database WHERE datname = current_database())" "${log_file}"; then
  record_pass "SQL scopes statements to the current database"
else
  record_fail "SQL scopes statements to the current database"
fi

# 10. Exact preload element; extension version floor.
run_case "pg_stat_statements_extra,pg_cron" "1.12" 1
if [ "${rc}" -eq 3 ]; then record_pass "preload substring does not count"; else record_fail "preload substring does not count (got ${rc})"; fi
run_case "pg_cron, pg_stat_statements" "1.12" 1
if [ "${rc}" -eq 0 ]; then record_pass "preload element in a spaced list counts"; else record_fail "preload element in a spaced list counts (got ${rc})"; fi
run_case "pg_stat_statements" "1.8" 1
if [ "${rc}" -eq 3 ] && has "ALTER EXTENSION pg_stat_statements UPDATE" "${out_file}"; then
  record_pass "extension below 1.9 exits 3 with the update sentence"
else
  record_fail "extension below 1.9 exits 3 with the update sentence (got ${rc})"
fi
run_case "pg_stat_statements" "1.9" 1
if [ "${rc}" -eq 0 ]; then record_pass "extension 1.9 passes"; else record_fail "extension 1.9 passes (got ${rc})"; fi

# 11. Block read time only when track_io_timing is on.
STUB_IO="t|t" run_case "pg_stat_statements" "1.12" 1
if has "s.shared_blk_read_time" "${log_file}" && has "AS read_ms" "${log_file}"; then
  record_pass "io timing on, PG17+: shared_blk_read_time selected"
else
  record_fail "io timing on, PG17+: shared_blk_read_time selected"
fi
STUB_IO="t|f" run_case "pg_stat_statements" "1.12" 1
if has "s.blk_read_time" "${log_file}" && ! has "shared_blk_read_time" "${log_file}"; then
  record_pass "io timing on, PG16-: blk_read_time selected"
else
  record_fail "io timing on, PG16-: blk_read_time selected"
fi
run_case "pg_stat_statements" "1.12" 1
if has "read_ms" "${log_file}"; then record_fail "io timing off: no read time column"; else record_pass "io timing off: no read time column"; fi

# 12. A psql failure exits 1.
STUB_FAIL="ORDER BY s.total_exec_time" run_case "pg_stat_statements" "1.12" 1
if [ "${rc}" -eq 1 ]; then record_pass "psql failure exits 1"; else record_fail "psql failure exits 1 (got ${rc})"; fi

# 13. --no-buffers.
run_case "pg_stat_statements" "1.12" 1 --no-buffers
if [ "${rc}" -eq 0 ] && has "stub-mean-row" "${out_file}" && ! has "Shared-buffer residency" "${out_file}" &&
  ! has "extname = 'pg_buffercache'" "${log_file}" && ! has "FROM pg_buffercache b" "${log_file}"; then
  record_pass "--no-buffers skips residency and never queries pg_buffercache"
else
  record_fail "--no-buffers skips residency and never queries pg_buffercache (rc=${rc})"
fi

# 14. Empty, blank, and lookalike preload values; $libdir/ and quoted entries.
for blank in "" "   "; do
  run_case "${blank}" "1.12" 1
  if [ "${rc}" -eq 3 ] && has "shared_preload_libraries" "${out_file}" &&
    [ "$(wc -l <"${out_file}" | tr -d ' ')" = "1" ]; then
    record_pass "blank preload value exits 3 with one line"
  else
    record_fail "blank preload value exits 3 with one line (rc=${rc})"
  fi
done
run_case "xpg_stat_statements,pg_stat_statements_foo" "1.12" 1
if [ "${rc}" -eq 3 ]; then record_pass "lookalike preload names do not count"; else record_fail "lookalike preload names do not count (rc=${rc})"; fi
run_case "\$libdir/pg_stat_statements" "1.12" 1
if [ "${rc}" -eq 0 ]; then record_pass "\$libdir/ prefixed preload counts"; else record_fail "\$libdir/ prefixed preload counts (rc=${rc})"; fi
run_case "pg_cron, '\$libdir/pg_stat_statements'" "1.12" 1
if [ "${rc}" -eq 0 ]; then record_pass "quoted \$libdir/ entry in a list counts"; else record_fail "quoted \$libdir/ entry in a list counts (rc=${rc})"; fi

printf '%s passed, %s failed\n' "${PASS}" "${FAIL}"
[ "${FAIL}" -eq 0 ]
