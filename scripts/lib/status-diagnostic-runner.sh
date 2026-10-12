#!/usr/bin/env bash
# Opt-in status diagnostics: bounded graceful shutdown of this runner's child.

status_diagnostic_capacity() {
 python3 - "$1" "$2" "$3" <<'PYCAPACITY'
import sys
values = sys.argv[1:]
if any(not value.isascii() or not value.isdecimal() or len(value) > 3 for value in values):
    raise SystemExit(1)
iterations, runs, requests = map(int, values)
raise SystemExit(0 if min(iterations, runs, requests) > 0 and 2 + iterations * runs + requests <= 256 else 1)
PYCAPACITY
}

status_diagnostic_prepare() {
 # Atomic creation refuses both previous results and symlink destinations.
 mkdir -m 700 "$1"
}

status_diagnostic_stop() {
 [[ "$1" =~ ^[1-9][0-9]*$ ]] || return 1
 kill -TERM "$1" 2>/dev/null || true
 status_diagnostic_wait "$1" "${2:-30}"
}

status_diagnostic_wait() {
 local owned_pid="$1" timeout_seconds="$2" status=0 watchdog
 [[ "$owned_pid" =~ ^[1-9][0-9]*$ && "$timeout_seconds" =~ ^[1-9][0-9]*$ ]] || return 1
 (
  sleeper=""
  trap '[[ -z "$sleeper" ]] || kill "$sleeper" 2>/dev/null || true; [[ -z "$sleeper" ]] || wait "$sleeper" 2>/dev/null || true; exit 0' TERM
  sleep "$timeout_seconds" &
  sleeper=$!
  wait "$sleeper" || exit 0
  if kill -0 "$owned_pid" 2>/dev/null; then
   kill -KILL "$owned_pid" 2>/dev/null || true
  fi
 ) &
 watchdog=$!
 wait "$owned_pid" || status=$?
 kill "$watchdog" 2>/dev/null || true
 wait "$watchdog" 2>/dev/null || true
 [[ "$status" -eq 0 ]]
}

status_diagnostic_validate() {
 local root="$1" directory="$2" expected="$3"
 python3 "$root/scripts/lib/status_diagnostic_artifacts.py" \
  "$directory/server.json" "$directory/client.json" --expected "$expected" --statements "$directory/statements.json" --plan "$directory/plan.json" --resources "$directory/resources.jsonl"
}

status_diagnostic_resource_start() {
 local root="$1" directory="$2" api_pid="$3" project="$4" container postgres_pid
 container="$(docker ps -q --filter "label=com.docker.compose.project=$project" --filter "label=com.docker.compose.service=postgres")" || return 1
 [[ "$container" =~ ^[a-f0-9]+$ ]] || return 1
 postgres_pid="$(docker inspect --format '{{.State.Pid}}' "$container")" || return 1
 [[ "$postgres_pid" =~ ^[1-9][0-9]*$ ]] || return 1
 python3 "$root/scripts/lib/status_diagnostic_resources.py" \
  --api-pid "$api_pid" --postgres-pid "$postgres_pid" \
  --output "$directory/resources.jsonl" --stop "$directory/resource-stop" --ready "$directory/resource-ready" &
 export status_diagnostic_resource_pid=$!
 for _ in {1..50}; do
  [[ ! -f "$directory/resource-ready" ]] || return 0
  kill -0 "$status_diagnostic_resource_pid" 2>/dev/null || return 1
  sleep 0.1
 done
 return 1
}

status_diagnostic_resource_stop() {
 local directory="$1" pid="$2"
 touch "$directory/resource-stop"
 status_diagnostic_wait "$pid" 5
}
