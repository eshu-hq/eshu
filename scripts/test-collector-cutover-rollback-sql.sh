#!/usr/bin/env bash
# Real-Postgres proof for the SQL under scripts/lib/collector-cutover-rollback-*.sql
# (#8895). The stub test (scripts/test-collector-cutover-rollback.sh) cannot see a
# column rename or a wrong predicate, so this script runs every SQL file against a
# private throwaway cluster built from the real migrations and asserts the answers.
#
#   bash scripts/test-collector-cutover-rollback-sql.sh            correctness checks
#   bash scripts/test-collector-cutover-rollback-sql.sh --bench N  also time the
#       truth query against the pre-fix sorted-aggregate form on N generated facts
#
# It needs initdb, pg_ctl, and psql (PG_BIN=/path/to/bin overrides PATH). It is a
# local proof tool, not a CI gate: CI has no guaranteed Postgres. The stub test
# pins each SQL file's digest, so a SQL edit cannot merge without someone re-running
# this script and updating the pin.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sql_dir="${repo_root}/scripts/lib"
migrations="${repo_root}/go/internal/storage/postgres/migrations"
bench_facts=0
if [[ "${1:-}" == "--bench" ]]; then
	bench_facts="${2:?--bench needs a fact count}"
fi

pg_bin="${PG_BIN:-}"
if [[ -z "${pg_bin}" ]]; then
	for candidate in "$(dirname "$(command -v initdb 2>/dev/null || echo /nonexistent/initdb)")" \
		/opt/homebrew/opt/postgresql@18/bin /opt/homebrew/opt/postgresql@17/bin /usr/lib/postgresql/17/bin /usr/lib/postgresql/16/bin; do
		if [[ -x "${candidate}/initdb" ]]; then
			pg_bin="${candidate}"
			break
		fi
	done
fi
[[ -n "${pg_bin}" ]] || {
	printf 'initdb not found: set PG_BIN to a PostgreSQL bin directory\n' >&2
	exit 2
}

work="$(mktemp -d "${TMPDIR:-/tmp}/rbsql.XXXXXX")"
cleanup() {
	"${pg_bin}/pg_ctl" -D "${work}/data" -m immediate stop >/dev/null 2>&1 || true
	rm -rf "${work}"
}
trap cleanup EXIT

"${pg_bin}/initdb" -D "${work}/data" -A trust -U postgres --no-sync >/dev/null
mkdir -p "${work}/s"
"${pg_bin}/pg_ctl" -D "${work}/data" -l "${work}/pg.log" -w \
	-o "-c listen_addresses='' -c unix_socket_directories=${work}/s -c fsync=off -c synchronous_commit=off -c full_page_writes=off" start >/dev/null
export PGHOST="${work}/s" PGUSER=postgres PGDATABASE=postgres
PSQL="${pg_bin}/psql"

for migration in 001_ingestion_scopes 002_scope_generations 003_fact_records 014_workflow_control_plane; do
	"${PSQL}" -X -q -v ON_ERROR_STOP=1 -f "${migrations}/${migration}.sql" >/dev/null
done

failures=0
pass() { printf 'PASS %s\n' "$1"; }
fail() {
	printf 'FAIL %s\n' "$1"
	failures=$((failures + 1))
}
check() { # label want got
	if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1 (want '$2', got '$3')"; fi
}

# sql runs a production SQL file exactly as the script does.
sql() { # name -v k=v ...
	local name="$1"
	shift
	"${PSQL}" -X -A -t -F '|' -v ON_ERROR_STOP=1 "$@" -f "${sql_dir}/collector-cutover-rollback-${name}.sql"
}
q() { "${PSQL}" -X -A -t -F '|' -v ON_ERROR_STOP=1 -c "$1"; }

# --- claim-table fixtures ---------------------------------------------------------------

q "INSERT INTO workflow_runs VALUES ('run', 'test', 'running', '[]', NULL, now(), now(), NULL)" >/dev/null
# item <id> <kind> <instance> <scope> <status>
item() {
	q "INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, source_system, scope_id,
	     acceptance_unit_id, source_run_id, generation_id, status, created_at, updated_at)
	   VALUES ('$1', 'run', '$2', '$3', 'src', '$4', 'au', 'sr', 'g', '$5', now(), now())" >/dev/null
}
# claim <id> <item> <fence> <status> <claimed_at> <lease_expires_at> <finished_at|NULL>
claim() {
	q "INSERT INTO workflow_claims VALUES ('$1', '$2', $3, 'owner', '$4', '$5'::timestamptz, '$5'::timestamptz,
	     '$6'::timestamptz, $7, NULL, NULL, now(), now())" >/dev/null
}

item i-ext-claimed jira ext-1 scope-a claimed
claim c-ext-active i-ext-claimed 1 active '2026-01-01 00:00:00+00' '2026-01-01 00:10:00+00' NULL
item i-ext-done jira ext-1 scope-b completed
claim c-ext-done i-ext-done 1 completed '2026-01-01 00:00:00+00' '2026-01-01 00:10:00+00' "'2026-01-01 00:05:00+00'"
item i-tree-claimed jira tree-1 scope-c claimed
claim c-tree-active i-tree-claimed 1 active '2026-01-02 00:00:00+00' '2026-01-02 00:10:00+00' NULL
item i-other-kind pagerduty ext-9 scope-d claimed
claim c-other-active i-other-kind 1 active '2026-01-02 00:00:00+00' '2026-01-02 00:10:00+00' NULL

check "drain counts a claimed item and its active claim held outside the in-tree set" "1|1" \
	"$(sql drain -v kind=jira -v in_tree=tree-1)"
check "drain does not depend on the external instance name (a typo cannot hide the claim)" "1|1" \
	"$(sql drain -v kind=jira -v in_tree=tree-1,tree-2)"
check "drain ignores other collector kinds and in-tree claims" "0|0" \
	"$(sql drain -v kind=jira -v in_tree=tree-1,ext-1)"
check "drain is empty for a kind with no work" "0|0" "$(sql drain -v kind=documentation -v in_tree=tree-1)"
check "dual reports external and in-tree claimed items with no overlap" "1|1|0" \
	"$(sql dual -v kind=jira -v in_tree=tree-1)"

# Overlap scenarios, one scope each.
item o1-ext jira ext-1 scope-o1 completed
claim o1-ext-c o1-ext 1 completed '2026-02-01 00:00:00+00' '2026-02-01 01:00:00+00' "'2026-02-01 00:30:00+00'"
item o1-tree jira tree-1 scope-o1 completed
claim o1-tree-c o1-tree 1 completed '2026-02-01 00:10:00+00' '2026-02-01 01:00:00+00' "'2026-02-01 00:40:00+00'"
check "dual detects overlapping claim intervals on one scope" "1|1|1" "$(sql dual -v kind=jira -v in_tree=tree-1)"

item o2-ext jira ext-1 scope-o2 completed
claim o2-ext-c o2-ext 1 completed '2026-02-02 00:00:00+00' '2026-02-02 00:10:00+00' "'2026-02-02 02:00:00+00'"
item o2-tree jira tree-1 scope-o2 completed
claim o2-tree-c o2-tree 1 completed '2026-02-02 00:30:00+00' '2026-02-02 01:00:00+00' "'2026-02-02 00:40:00+00'"
check "a late reap does not invent an overlap (the claim ended at its lease expiry)" "1|1|1" \
	"$(sql dual -v kind=jira -v in_tree=tree-1)"

item o3-ext jira ext-1 scope-o3 claimed
claim o3-ext-c o3-ext 1 active '2026-02-03 00:00:00+00' '2026-02-03 00:10:00+00' NULL
item o3-tree jira tree-1 scope-o3 completed
claim o3-tree-c o3-tree 1 completed '2026-02-04 00:00:00+00' '2026-02-04 01:00:00+00' "'2026-02-04 00:30:00+00'"
check "an unfinished external claim overlaps a later in-tree claim" "2|1|2" "$(sql dual -v kind=jira -v in_tree=tree-1)"

item o4-ext jira ext-1 scope-o4 completed
claim o4-ext-c o4-ext 1 completed '2026-02-05 00:00:00+00' '2026-02-05 01:00:00+00' "'2026-02-05 00:30:00+00'"
item o4-tree jira tree-1 scope-o4 completed
claim o4-tree-c o4-tree 1 completed '2026-02-06 00:00:00+00' '2026-02-06 01:00:00+00' "'2026-02-06 00:30:00+00'"
check "claims released before the other began do not overlap" "2|1|2" "$(sql dual -v kind=jira -v in_tree=tree-1)"

# The mirror of o2: the IN-TREE claim was reaped late (lease ended 00:10, finished
# 02:00), and the external claim began at 00:30, after the lease end. The in-tree claim ended at its lease expiry, so there is no overlap.
item o5-tree jira tree-1 scope-o5 completed
claim o5-tree-c o5-tree 1 completed '2026-02-07 00:00:00+00' '2026-02-07 00:10:00+00' "'2026-02-07 02:00:00+00'"
item o5-ext jira ext-1 scope-o5 completed
claim o5-ext-c o5-ext 1 completed '2026-02-07 00:30:00+00' '2026-02-07 01:00:00+00' "'2026-02-07 01:00:00+00'"
check "a late-reaped in-tree claim ended at its lease expiry and does not overlap" "2|1|2" \
	"$(sql dual -v kind=jira -v in_tree=tree-1)"

# --- replay and clock ------------------------------------------------------------------------

item r-pending jira tree-1 scope-r pending
item r-failed jira tree-1 scope-r2 failed_retryable
item r-expired jira tree-1 scope-r3 expired
item r-terminal jira tree-1 scope-r4 failed_terminal
q "UPDATE workflow_work_items SET updated_at = '2020-01-01' WHERE work_item_id IN ('r-failed')" >/dev/null
check "replay counts only in-tree work updated since the window start, retryable folded together" "1|1|5|1|1" \
	"$(sql replay -v kind=jira -v instances=tree-1 -v since='2025-01-01 00:00:00+00')"
skew_secs="$(q "SELECT round(extract(epoch FROM clock_timestamp() - ($(sql now -v skew=30 | sed "s/^/'/; s/\$/'::timestamptz/"))))")"
check "now returns the database clock minus the skew margin" "30" "${skew_secs}"
check "now with a zero margin is the database clock" "0" \
	"$(q "SELECT round(extract(epoch FROM clock_timestamp() - ($(sql now -v skew=0 | sed "s/^/'/; s/\$/'::timestamptz/"))))")"

# --- fact truth --------------------------------------------------------------------------------

# scope <id> <kind> <active-generation>; gen <id> <scope>
scope() {
	q "INSERT INTO ingestion_scopes VALUES ('$1', 'k', 's', '$1', NULL, '$2', 'p', now(), now(), 'active', NULL, '{}')" >/dev/null
}
gen() { # id scope [status]
	q "INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
	   VALUES ('$1', '$2', 't', now(), now(), '${3:-active}')" >/dev/null
}
activate() { q "UPDATE ingestion_scopes SET active_generation_id = '$2' WHERE scope_id = '$1'" >/dev/null; }
# fact <id> <scope> <gen> <kind> <key> <payload-json> [tombstone]
fact() {
	q "INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
	     observed_at, ingested_at, is_tombstone, payload)
	   VALUES ('$1', '$2', '$3', '$4', \$k\$$5\$k\$, 'src', 'sk', now(), now(), ${7:-false}, '$6')" >/dev/null
}

scope t-a jira
gen t-a-g1 t-a superseded
gen t-a-g2 t-a
activate t-a t-a-g2
fact f1 t-a t-a-g2 work_item.issue 'ABC-1' '{"s":"open"}'
fact f2 t-a t-a-g2 work_item.issue 'ABC-2' '{"s":"done"}'
fact f3 t-a t-a-g2 work_item.comment 'ABC-1#c1' '{"b":"hi"}'
fact f4 t-a t-a-g1 work_item.issue 'ABC-OLD' '{"s":"stale generation"}'
fact f5 t-a t-a-g2 work_item.issue 'ABC-GONE' '{"s":"x"}' true
scope t-other pagerduty
gen t-other-g t-other
activate t-other t-other-g
fact f6 t-other t-other-g incident.record 'INC-1' '{}'

truth1="$(sql truth -v kind=jira)"
check "truth reports the active generation's facts per kind, excluding tombstones, old generations, and other kinds" \
	"work_item.comment|1
work_item.issue|2" "$(printf '%s\n' "${truth1}" | awk -F'|' '{ print $1 "|" $2 }')"
check "truth is deterministic across runs" "${truth1}" "$(sql truth -v kind=jira)"
digest_field() { printf '%s\n' "$2" | awk -F'|' -v kind="$1" -v col="$3" '$1 == kind { print $col }'; }
check "the digest has the lane1:lane2 shape" "1" \
	"$(digest_field work_item.issue "${truth1}" 3 | awk -F: 'NF == 2 && $1 ~ /^-?[0-9]+$/ && $2 ~ /^-?[0-9]+$/ { print 1 }')"

# Same facts under a second collector kind, inserted in the opposite order: an
# order-independent digest must agree.
scope t-b jira2
gen t-b-g1 t-b
activate t-b t-b-g1
fact g3 t-b t-b-g1 work_item.comment 'ABC-1#c1' '{"b":"hi"}'
fact g2 t-b t-b-g1 work_item.issue 'ABC-2' '{"s":"done"}'
fact g1 t-b t-b-g1 work_item.issue 'ABC-1' '{"s":"open"}'
check "the digest does not depend on row order or fact IDs" "${truth1}" "$(sql truth -v kind=jira2)"

q "UPDATE fact_records SET payload = '{\"s\":\"CHANGED\"}' WHERE fact_id = 'g1'" >/dev/null
truth2="$(sql truth -v kind=jira2)"
check "a payload change keeps the stable-key digest" "$(digest_field work_item.issue "${truth1}" 3)" "$(digest_field work_item.issue "${truth2}" 3)"
if [[ "$(digest_field work_item.issue "${truth1}" 4)" != "$(digest_field work_item.issue "${truth2}" 4)" ]]; then
	pass "a payload change moves the payload digest"
else fail "a payload change did not move the payload digest"; fi
check "a payload change leaves untouched fact kinds equal" "$(digest_field work_item.comment "${truth1}" 4)" "$(digest_field work_item.comment "${truth2}" 4)"

q "UPDATE fact_records SET stable_fact_key = 'ABC-9' WHERE fact_id = 'g1'" >/dev/null
truth3="$(sql truth -v kind=jira2)"
if [[ "$(digest_field work_item.issue "${truth1}" 3)" != "$(digest_field work_item.issue "${truth3}" 3)" &&
	"$(digest_field work_item.issue "${truth1}" 4)" != "$(digest_field work_item.issue "${truth3}" 4)" ]]; then
	pass "a stable-key change moves both digests"
else fail "a stable-key change did not move both digests"; fi

q "UPDATE fact_records SET stable_fact_key = 'ABC-1', payload = '{\"s\":\"open\"}' WHERE fact_id = 'g1'" >/dev/null
fact g4 t-b t-b-g1 work_item.issue 'ABC-1' '{"s":"open"}'
truth4="$(sql truth -v kind=jira2)"
check "a duplicated row changes the count" "3" "$(digest_field work_item.issue "${truth4}" 2)"
if [[ "$(digest_field work_item.issue "${truth1}" 3)" != "$(digest_field work_item.issue "${truth4}" 3)" ]]; then
	pass "a duplicated row changes the digest"
else fail "a duplicated row left the digest unchanged"; fi

if awk '!/^[[:space:]]*--/ && /md5[[:space:]]*\(/ { found = 1 } END { exit !found }' "${sql_dir}/collector-cutover-rollback-truth.sql"; then
	fail "truth.sql uses md5, which a FIPS-mode server rejects"
else pass "truth.sql uses SHA-256 only"; fi

# The keys query escapes newlines and backslashes so one fact is one line.
scope t-c jira3
gen t-c-g t-c
activate t-c t-c-g
fact h1 t-c t-c-g work_item.issue "$(printf 'A\\B\nC')" '{"s":1}'
check "the keys query keeps one fact on one line with escapes" "1" "$(sql keys -v kind=jira3 | awk 'END { print NR }')"
check "the keys query escapes a backslash and a newline in the key" 'A\\B\nC' "$(sql keys -v kind=jira3 | awk -F'\t' '{ print $3 }')"

# --- the script end to end against real Postgres (truth, capture, drift report) -------

script="${repo_root}/scripts/collector-cutover-rollback.sh"
verify_args=(verify --collector-kind jira2 --external-instance ext-1 --in-tree-instance tree-1)
q "DELETE FROM fact_records WHERE fact_id = 'g4'" >/dev/null
q "DELETE FROM workflow_claims WHERE claim_id LIKE 'o%'" >/dev/null
q "DELETE FROM workflow_work_items WHERE work_item_id LIKE 'o%' OR work_item_id LIKE 'i-%'" >/dev/null
rc=0
"${script}" "${verify_args[@]}" --capture "${work}/base" --capture-keys >/dev/null 2>&1 || rc=$?
check "capture against real Postgres exits 0" 0 "${rc}"
rc=0
"${script}" "${verify_args[@]}" --baseline "${work}/base" >/dev/null 2>&1 || rc=$?
check "an unchanged scope verifies equal against real Postgres" 0 "${rc}"
q "UPDATE fact_records SET payload = '{\"s\":\"DRIFTED\"}' WHERE fact_id = 'g1'" >/dev/null
fact g5 t-b t-b-g1 work_item.issue 'ABC-NEW' '{"s":"new"}'
rc=0
drift_out="$("${script}" "${verify_args[@]}" --baseline "${work}/base" --report-drift 2>&1)" || rc=$?
check "drifted truth exits 6 against real Postgres" 6 "${rc}"
for want in "work_item.issue  added=1 removed=0 changed=1" "work_item.issue  added: ABC-NEW" "work_item.issue  changed: ABC-1"; do
	if [[ "${drift_out}" == *"${want}"* ]]; then pass "the drift report shows: ${want}"; else fail "the drift report lacks: ${want}"; fi
done

# --- optional benchmark ---------------------------------------------------------------------------

if [[ "${bench_facts}" -gt 0 ]]; then
	printf '\nBENCH: generating %s facts across 3 kinds (this takes a while)\n' "${bench_facts}"
	q "INSERT INTO ingestion_scopes
	     SELECT 'b-' || s, 'k', 's', 'b-' || s, NULL, 'bench', 'p', now(), now(), 'active', 'bg-' || s, '{}'
	     FROM generate_series(1, 300) AS s" >/dev/null
	q "INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
	     SELECT 'bg-' || s, 'b-' || s, 't', now(), now(), 'active' FROM generate_series(1, 300) AS s" >/dev/null
	q "INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
	     observed_at, ingested_at, payload)
	   SELECT 'bf-' || n, 'b-' || (n % 300 + 1), 'bg-' || (n % 300 + 1),
	          (ARRAY['work_item.issue', 'work_item.comment', 'work_item.transition'])[n % 3 + 1],
	          'PRJ-' || (n / 3) || '#' || (n % 3), 'src', 'sk', now(), now(),
	          jsonb_build_object('issue_key', 'PRJ-' || (n / 3), 'summary', md5(n::text) || encode(sha256(convert_to((n + 1)::text, 'UTF8')), 'hex'),
	                             'status', (ARRAY['open', 'done', 'review'])[n % 3 + 1], 'n', n,
	                             'labels', jsonb_build_array('a', 'b', encode(sha256(convert_to((n + 2)::text, 'UTF8')), 'hex')))
	   FROM generate_series(1, ${bench_facts}) AS n" >/dev/null
	q "ANALYZE" >/dev/null
	old_sql="${work}/old-truth.sql"
	cp "${sql_dir}/collector-cutover-rollback-old-truth.sql" "${old_sql}"
	timed() { # label file
		local start end
		start="$(date +%s.%N 2>/dev/null || python3 -c 'import time; print(time.time())')"
		"${PSQL}" -X -A -t -v ON_ERROR_STOP=1 -v kind=bench -f "$2" >/dev/null
		end="$(date +%s.%N 2>/dev/null || python3 -c 'import time; print(time.time())')"
		awk -v label="$1" -v s="${start}" -v e="${end}" 'BEGIN { printf "BENCH %s: %.2f s wall\n", label, e - s }'
	}
	# Alternate which form runs first so cache warmth does not favor one of them.
	for pass in 1 2 3; do
		if ((pass % 2 == 1)); then
			timed "pre-fix sorted string_agg (run ${pass})" "${old_sql}"
			timed "set digest, no sort (run ${pass})" "${sql_dir}/collector-cutover-rollback-truth.sql"
		else
			timed "set digest, no sort (run ${pass})" "${sql_dir}/collector-cutover-rollback-truth.sql"
			timed "pre-fix sorted string_agg (run ${pass})" "${old_sql}"
		fi
	done
	for variant in old new; do
		file="${old_sql}"
		[[ "${variant}" == "old" ]] || file="${sql_dir}/collector-cutover-rollback-truth.sql"
		printf 'BENCH EXPLAIN (%s):\n' "${variant}"
		{
			printf 'EXPLAIN (ANALYZE, BUFFERS)\n'
			sed "s/:'kind'/'bench'/" "${file}"
		} | "${PSQL}" -X -A -t | awk '/Execution Time|Sort Method|temp read|temp written|Planning Time|HashAggregate|GroupAggregate/' | sed 's/^/  /'
	done
fi

if [[ "${failures}" -gt 0 ]]; then
	printf '\n%d check(s) failed\n' "${failures}"
	exit 1
fi
printf '\nAll collector cutover rollback SQL checks passed against real Postgres\n'
