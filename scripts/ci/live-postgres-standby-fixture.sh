#!/usr/bin/env bash
# Start/stop only the named PostgreSQL 18 physical standby for the hosted
# live-postgres-readiness job. Its primary is the job's own service container.
set -euo pipefail

die() { printf 'live-postgres-standby-fixture: %s\n' "$*" >&2; exit 1; }

[[ $# -ge 2 ]] || die 'usage: start PRIMARY_CONTAINER_ID FIXTURE_ID | stop FIXTURE_ID'
action="$1"
case "${action}" in
  start)
    [[ $# -eq 3 ]] || die 'start requires primary container ID and fixture ID'
    primary="$2"
    [[ "${primary}" =~ ^[a-f0-9]{12,64}$ ]] || die 'invalid primary container ID'
    fixture_id="$3"
    ;;
  stop)
    [[ $# -eq 2 ]] || die 'stop requires fixture ID'
    fixture_id="$2"
    ;;
  *) die 'action must be start or stop' ;;
esac
[[ "${fixture_id}" =~ ^[A-Za-z0-9][A-Za-z0-9_-]{0,55}$ ]] || die 'invalid fixture ID'
command -v docker >/dev/null 2>&1 || die 'docker is required'

name="eshu-live-readiness-standby-${fixture_id}"
volume="${name}-data"
label='eshu.fixture=live-postgres-readiness'
id_label="eshu.fixture-id=${fixture_id}"
image='postgres:18.6-bookworm'
primary_port=15432
standby_port=15433

owned_container() {
  [[ "$(docker inspect --format '{{index .Config.Labels "eshu.fixture"}}' "${name}" 2>/dev/null)" == 'live-postgres-readiness' ]] &&
    [[ "$(docker inspect --format '{{index .Config.Labels "eshu.fixture-id"}}' "${name}" 2>/dev/null)" == "${fixture_id}" ]]
}
owned_volume() {
  [[ "$(docker volume inspect --format '{{index .Labels "eshu.fixture"}}' "${volume}" 2>/dev/null)" == 'live-postgres-readiness' ]] &&
    [[ "$(docker volume inspect --format '{{index .Labels "eshu.fixture-id"}}' "${volume}" 2>/dev/null)" == "${fixture_id}" ]]
}
cleanup() {
  if docker inspect "${name}" >/dev/null 2>&1; then
    owned_container || die "refusing to remove unowned container ${name}"
    docker rm -f "${name}" >/dev/null || die "cannot remove ${name}"
  fi
  if docker volume inspect "${volume}" >/dev/null 2>&1; then
    owned_volume || die "refusing to remove unowned volume ${volume}"
    docker volume rm "${volume}" >/dev/null || die "cannot remove ${volume}"
  fi
}

if [[ "${action}" == stop ]]; then
  cleanup
  printf 'live-postgres-standby-fixture: stopped %s\n' "${name}"
  exit 0
fi

primary_running="$(docker inspect --format '{{.State.Running}}' "${primary}" 2>/dev/null)" ||
  die 'primary container does not exist'
[[ "${primary_running}" == true ]] || die 'primary container is not running'
[[ "$(docker inspect --format '{{.Config.Image}}' "${primary}")" == "${image}" ]] ||
  die 'primary container has the wrong image'
docker inspect "${name}" >/dev/null 2>&1 && die "fixture container ${name} already exists"
docker volume inspect "${volume}" >/dev/null 2>&1 && die "fixture volume ${volume} already exists"

# A failed base backup, startup, or replay check must leave neither resource.
trap 'cleanup' EXIT
started="${SECONDS}"
docker volume create --label "${label}" --label "${id_label}" "${volume}" >/dev/null

# The role is restricted to physical replication on this disposable primary.
# The Docker service's bridge gateway is not always a fixed CIDR, so host auth
# is password-gated for this one role instead of trusting a guessed subnet.
docker exec -u postgres "${primary}" psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres \
  -c "CREATE ROLE eshu_readiness_replication WITH LOGIN REPLICATION PASSWORD 'local-proof-only'" >/dev/null
docker exec -u postgres "${primary}" bash -ceu \
  'printf "%s\n" "host replication eshu_readiness_replication all scram-sha-256" >> "${PGDATA}/pg_hba.conf"'
docker exec -u postgres "${primary}" psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres \
  -c 'SELECT pg_reload_conf()' >/dev/null

docker run -d --name "${name}" --label "${label}" --label "${id_label}" \
  --network host --mount "type=volume,source=${volume},target=/var/lib/postgresql" \
  --env "PGPASSWORD=local-proof-only" --entrypoint bash "${image}" -ceu '
    install -d -o postgres -g postgres -m 0700 "$PGDATA"
    gosu postgres pg_basebackup -h 127.0.0.1 -p 15432 -U eshu_readiness_replication \
      -D "$PGDATA" -R -X stream --checkpoint=fast --no-password
    exec gosu postgres postgres -D "$PGDATA" -c port=15433 -c listen_addresses=127.0.0.1
  ' >/dev/null

# No test may start until the server is a hot standby, shares the primary's
# cluster identity, and has replayed a write made after it started.
primary_sysid="$(docker exec -u postgres "${primary}" psql -X -Atq -v ON_ERROR_STOP=1 \
  -U postgres -d postgres -c 'SELECT system_identifier FROM pg_control_system()')"
[[ "${primary_sysid}" =~ ^[0-9]+$ ]] || die 'invalid primary system identifier'
marker="fixture_${fixture_id//[^A-Za-z0-9]/_}"
ready=0
for ((attempt = 0; attempt < 90; attempt++)); do
  if docker exec -u postgres "${name}" psql -X -Atq -v ON_ERROR_STOP=1 \
    -h 127.0.0.1 -p "${standby_port}" -U postgres -d postgres \
    -c 'SELECT pg_is_in_recovery(), current_setting('\''transaction_read_only'\''), system_identifier FROM pg_control_system()' \
    2>/dev/null | rg -q "^t\\|on\\|${primary_sysid}$"; then
    ready=1
    break
  fi
  sleep 1
done
[[ "${ready}" -eq 1 ]] || { docker logs --tail 40 "${name}" >&2; die 'standby did not enter read-only recovery'; }

docker exec -u postgres "${primary}" psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres \
  -c 'CREATE TABLE public.eshu_readiness_standby_probe (marker text PRIMARY KEY)' \
  -c "INSERT INTO public.eshu_readiness_standby_probe(marker) VALUES ('${marker}')" >/dev/null
replayed=0
for ((attempt = 0; attempt < 30; attempt++)); do
  if docker exec -u postgres "${name}" psql -X -Atq -v ON_ERROR_STOP=1 \
    -h 127.0.0.1 -p "${standby_port}" -U postgres -d postgres \
    -c "SELECT marker FROM public.eshu_readiness_standby_probe WHERE marker = '${marker}'" \
    2>/dev/null | rg -qx "${marker}"; then
    replayed=1
    break
  fi
  sleep 1
done
[[ "${replayed}" -eq 1 ]] || die 'standby did not replay the post-start marker'
[[ "$(docker exec -u postgres "${name}" psql -X -Atq -v ON_ERROR_STOP=1 \
  -h 127.0.0.1 -p "${standby_port}" -U postgres -d postgres \
  -c "SELECT status FROM pg_stat_wal_receiver")" == streaming ]] || die 'WAL receiver is not streaming'

trap - EXIT
printf 'live-postgres-standby-fixture: ready %s setup_elapsed=%ss primary_port=%s standby_port=%s\n' \
  "${name}" "$((SECONDS - started))" "${primary_port}" "${standby_port}"
