#!/usr/bin/env bash
# Sample only the bounded concurrent GET stage; never archive SQL text or DSNs.
set -euo pipefail
capture_dir="$1"
project="$2"
stop_file="${capture_dir}/sampler-stop"
start_file="${capture_dir}/get_start.txt"
end_file="${capture_dir}/get_end.txt"
while [[ ! -e "${start_file}" && ! -e "${stop_file}" ]]; do sleep 0.25; done
while [[ ! -e "${end_file}" && ! -e "${stop_file}" ]]; do
  {
    date -u +%FT%T.%NZ
    cat /proc/loadavg /proc/pressure/cpu /proc/pressure/io /proc/pressure/memory
    cat /sys/fs/cgroup/cpu.max /sys/fs/cgroup/cpu.stat 2>/dev/null || true
  } >> "${capture_dir}/host-pressure.txt"
  docker compose -p "${project}" -f docker-compose.neo4j.yml -f docker-compose.read-api-latency-gate.yaml \
    exec -T postgres psql -v ON_ERROR_STOP=1 -U eshu -d eshu -tA -F , \
    -c "SELECT clock_timestamp(),pid,backend_type,state,wait_event_type,wait_event,query_id FROM pg_stat_activity WHERE datname='eshu' ORDER BY pid" \
    >> "${capture_dir}/pg-activity.csv"
  sleep 0.25
done
