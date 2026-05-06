#!/usr/bin/env bash

set -euo pipefail

INTERVAL_SECONDS="${ESHU_MONITOR_INTERVAL_SECONDS:-30}"
SAMPLES="${ESHU_MONITOR_SAMPLES:-0}"
RUN_DIR="${ESHU_MONITOR_RUN_DIR:-}"
POSTGRES_DSN="${ESHU_MONITOR_POSTGRES_DSN:-}"

usage() {
	cat <<'USAGE'
Usage: monitor_local_authoritative_run.sh [--run-dir DIR] [--postgres-dsn DSN] [--interval SECONDS] [--samples COUNT]

Samples host resources, Eshu/NornicDB processes, and local-authoritative queue
state during a dogfood or subset run. COUNT=0 means run until interrupted.

Examples:
  scripts/monitor_local_authoritative_run.sh --run-dir /tmp/eshu-run --interval 30
  ESHU_MONITOR_POSTGRES_DSN=postgresql://eshu:change-me@127.0.0.1:15432/eshu scripts/monitor_local_authoritative_run.sh
USAGE
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--run-dir)
			RUN_DIR="${2:-}"
			shift 2
			;;
		--postgres-dsn)
			POSTGRES_DSN="${2:-}"
			shift 2
			;;
		--interval)
			INTERVAL_SECONDS="${2:-}"
			shift 2
			;;
		--samples)
			SAMPLES="${2:-}"
			shift 2
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			echo "Unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
done

timestamp_utc() {
	date -u '+%Y-%m-%dT%H:%M:%SZ'
}

owner_record_path() {
	local candidate_root="$RUN_DIR"
	if [[ -z "$candidate_root" || ! -d "$candidate_root" ]]; then
		return 1
	fi
	rg --files "$candidate_root" 2>/dev/null | rg '/owner\.json$' | sed -n '1p'
}

postgres_dsn_from_owner() {
	local owner_path="$1"
	local port
	if [[ -z "$owner_path" || ! -f "$owner_path" ]]; then
		return 1
	fi
	if ! command -v jq >/dev/null 2>&1; then
		return 1
	fi
	port="$(jq -r '.postgres_port // empty' "$owner_path")"
	if [[ -z "$port" || "$port" == "0" ]]; then
		return 1
	fi
	printf 'postgresql://eshu:change-me@127.0.0.1:%s/postgres?sslmode=disable\n' "$port"
}

print_queue_summary() {
	local dsn="$1"
	if [[ -z "$dsn" ]]; then
		echo "queue_summary=unavailable"
		return 0
	fi
	if ! command -v psql >/dev/null 2>&1; then
		echo "queue_summary=unavailable"
		return 0
	fi
	PGPASSWORD="${ESHU_MONITOR_POSTGRES_PASSWORD:-change-me}" psql "$dsn" -X -A -t -c "
WITH totals AS (
  SELECT stage, status, count(*) AS count
  FROM fact_work_items
  GROUP BY stage, status
),
failures AS (
  SELECT failure_class, count(*) AS count
  FROM fact_work_items
  WHERE failure_class IS NOT NULL AND failure_class <> ''
  GROUP BY failure_class
)
SELECT 'queue=' || COALESCE(
  (SELECT string_agg(stage || ':' || status || '=' || count, ', ' ORDER BY stage, status) FROM totals),
  'empty'
) || E'\n' || 'failures=' || COALESCE(
  (SELECT string_agg(failure_class || '=' || count, ', ' ORDER BY failure_class) FROM failures),
  'none'
);
" 2>/dev/null || echo "queue_summary=error"
}

print_vm_sample() {
	if command -v vmstat >/dev/null 2>&1; then
		echo "vmstat:"
		vmstat 1 2 | sed -n '$p' || echo "vmstat=error"
	else
		echo "vmstat=unavailable"
	fi
}

print_disk_sample() {
	if command -v iostat >/dev/null 2>&1; then
		echo "iostat:"
		if iostat -dx 1 2 >/tmp/eshu-monitor-iostat.$$ 2>/dev/null; then
			sed -n '/Device/,$p' /tmp/eshu-monitor-iostat.$$
		elif iostat -d -w 1 -c 2 >/tmp/eshu-monitor-iostat.$$ 2>/dev/null; then
			cat /tmp/eshu-monitor-iostat.$$
		else
			echo "iostat=error"
		fi
		rm -f /tmp/eshu-monitor-iostat.$$
	else
		echo "iostat=unavailable"
	fi
}

print_process_sample() {
	echo "top_processes:"
	if ps -eo pid,ppid,pcpu,pmem,etime,comm,args --sort=-pcpu >/dev/null 2>&1; then
		ps -eo pid,ppid,pcpu,pmem,etime,comm,args --sort=-pcpu | sed -n '1,12p'
	else
		ps -eo pid,ppid,pcpu,pmem,etime,comm,args | sed -n '1,12p'
	fi
	echo "eshu_processes:"
	ps -eo pid,ppid,pcpu,pmem,etime,comm,args | rg 'eshu|nornicdb|postgres' | rg -v 'rg ' || true
}

sample_once() {
	local dsn="$1"
	echo "===== eshu local-authoritative sample $(timestamp_utc) ====="
	uptime || true
	print_vm_sample
	print_disk_sample
	print_process_sample
	print_queue_summary "$dsn"
	echo
}

if [[ -z "$POSTGRES_DSN" ]]; then
	OWNER_RECORD="$(owner_record_path || true)"
	POSTGRES_DSN="$(postgres_dsn_from_owner "$OWNER_RECORD" || true)"
fi

count=0
while true; do
	if [[ -z "$POSTGRES_DSN" ]]; then
		OWNER_RECORD="$(owner_record_path || true)"
		POSTGRES_DSN="$(postgres_dsn_from_owner "$OWNER_RECORD" || true)"
	fi
	sample_once "$POSTGRES_DSN"
	count=$((count + 1))
	if [[ "$SAMPLES" != "0" && "$count" -ge "$SAMPLES" ]]; then
		break
	fi
	sleep "$INTERVAL_SECONDS"
done
