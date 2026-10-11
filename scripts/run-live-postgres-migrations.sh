#!/usr/bin/env bash
# Run the seven migration/ownership integration proofs on one owned PG18 fixture.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
checker="${root}/scripts/lib/live_postgres_migrations.py"
image_default='ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c'
image="${ESHU_POSTGRES_MIGRATIONS_IMAGE:-${image_default}}"
die() { printf 'live-postgres-migrations: %s\n' "$*" >&2; exit 1; }

[[ $# -eq 1 ]] || die 'usage: run-live-postgres-migrations.sh FIXTURE_ID'
fixture_id="$1"
[[ "${fixture_id}" =~ ^[A-Za-z0-9][A-Za-z0-9_-]{0,55}$ ]] || die 'invalid fixture ID'
case "${image}" in
  "${image_default}"|postgres:18.6-bookworm) ;;
  *) die 'image must be the approved PostgreSQL 18.6 fixture reference' ;;
esac
command -v docker >/dev/null 2>&1 || die 'docker is required'
for command in go python3 openssl; do
  command -v "${command}" >/dev/null 2>&1 || die "${command} is required"
done
if env | rg -q '^PG[A-Za-z_]*='; then
  die 'inherited PG* environment is unsafe for the owned fixture tests'
fi
python3 "${checker}" verify-ledger "${root}/specs/live-tests.v1.yaml" "${root}" ||
  die 'migration ledger selection is invalid'
pattern="$(python3 "${checker}" pattern)" || die 'migration test pattern is invalid'
[[ "${pattern}" == '^('*')$' ]] || die 'migration test pattern must be anchored'

name="eshu-live-postgres-migrations-${fixture_id}"
docker inspect "${name}" >/dev/null 2>&1 && die "fixture container ${name} already exists"
container_token="$(openssl rand -hex 16)" || die 'cannot generate fixture ownership token'
[[ "${container_token}" =~ ^[0-9a-f]{32}$ ]] || die 'fixture ownership token is invalid'
scratch="$(mktemp -d)"
started=0
cleanup() {
  if [[ "${started}" -eq 1 ]]; then
    if docker inspect "${name}" >/dev/null 2>&1; then
      local owner id token container_id
      owner="$(docker inspect --format '{{index .Config.Labels "eshu.fixture"}}' "${name}" 2>/dev/null)" ||
        die "cannot inspect fixture ownership for ${name}"
      id="$(docker inspect --format '{{index .Config.Labels "eshu.fixture-id"}}' "${name}" 2>/dev/null)" ||
        die "cannot inspect fixture ID for ${name}"
      token="$(docker inspect --format '{{index .Config.Labels "eshu.fixture-token"}}' "${name}" 2>/dev/null)" ||
        die "cannot inspect fixture ownership token for ${name}"
      container_id="$(docker inspect --format '{{.Id}}' "${name}" 2>/dev/null)" ||
        die "cannot inspect fixture container ID for ${name}"
      [[ "${owner}" == live-postgres-migrations && "${id}" == "${fixture_id}" && "${token}" == "${container_token}" ]] ||
        die "refusing to remove unowned container ${name}"
      [[ "${container_id}" =~ ^[0-9a-f]{64}$ ]] || die "invalid fixture container ID for ${name}"
      docker rm -f "${container_id}" >/dev/null || die "cannot remove ${name}"
    fi
  fi
  rm -rf "${scratch}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# PG18 stores its data under /var/lib/postgresql. A tmpfs avoids retaining an
# anonymous Docker volume; the exact named container is always removed.
started=1
docker run -d --name "${name}" \
  --label 'eshu.fixture=live-postgres-migrations' \
  --label "eshu.fixture-id=${fixture_id}" \
  --label "eshu.fixture-token=${container_token}" \
  --tmpfs '/var/lib/postgresql:rw,size=2g' \
  -p '127.0.0.1::5432' \
  -e 'POSTGRES_PASSWORD=local-proof-only' \
  -e 'POSTGRES_HOST_AUTH_METHOD=trust' \
  "${image}" >/dev/null || die 'cannot start owned PostgreSQL fixture'
ready=0
for ((attempt = 0; attempt < 30; attempt++)); do
  if docker exec -u postgres "${name}" pg_isready -U postgres -d postgres >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
[[ "${ready}" -eq 1 ]] || die 'PostgreSQL fixture did not become ready'
port_line="$(docker port "${name}" 5432/tcp)" || die 'fixture port is unavailable'
[[ "${port_line}" =~ ^127\.0\.0\.1:([1-9][0-9]{0,4})$ ]] || die 'fixture port is not loopback-only'
port="${BASH_REMATCH[1]}"
((port <= 65535)) || die 'fixture port is out of range'
identity="$(docker exec -u postgres "${name}" psql -X -Atq -v ON_ERROR_STOP=1 \
  -U postgres -d postgres \
  -c "SELECT system_identifier::text, current_setting('server_version_num') FROM pg_control_system()" \
  -F '|')" || die 'fixture identity query failed'
[[ "${identity}" =~ ^([1-9][0-9]*)\|180006$ ]] || die 'fixture is not PostgreSQL 18.6 with a canonical system ID'
system_id="${BASH_REMATCH[1]}"
token="$(openssl rand -hex 16)" || die 'cannot generate fixture token'
[[ "${token}" =~ ^[0-9a-f]{32}$ ]] || die 'fixture token is not 32 lowercase hex digits'

docker exec -u postgres "${name}" psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres \
  -c "CREATE ROLE eshu169fixture WITH LOGIN SUPERUSER PASSWORD '${token}'" >/dev/null ||
  die 'cannot create fixture role'
docker exec -u postgres "${name}" psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres \
  -c 'CREATE DATABASE eshu169fixture OWNER eshu169fixture' >/dev/null ||
  die 'cannot create migration fixture database'
docker exec -u postgres "${name}" psql -X -v ON_ERROR_STOP=1 -U postgres -d postgres \
  -c 'CREATE DATABASE eshu_7033_rollback' >/dev/null ||
  die 'cannot create rollback fixture database'

export ESHU_POSTGRES_MIGRATION169_TEST_DSN="postgres://eshu169fixture:${token}@127.0.0.1:${port}/eshu169fixture?sslmode=disable"
export ESHU_7033_ROLLBACK_TEST_DSN="postgres://postgres@127.0.0.1:${port}/eshu_7033_rollback?sslmode=disable"
export ESHU_TEST_POSTGRES_SYSTEM_IDENTIFIER="${system_id}"
events="${scratch}/events.jsonl"
start_seconds="${SECONDS}"
if (cd "${root}/go" && go test -json -count=1 -timeout=4m -tags integration \
  ./internal/storage/postgres -run "${pattern}") >"${events}" 2>"${scratch}/stderr" </dev/null; then
  test_status=0
else
  test_status=$?
fi
if python3 "${checker}" verify-events "${events}"; then
  event_status=0
else
  event_status=$?
fi
if [[ "${test_status}" -ne 0 ]]; then
  sed -n '1,30p' "${scratch}/stderr" >&2
  tail -n 30 "${events}" | cut -c 1-300 >&2
  die "go test exited ${test_status}"
fi
[[ "${event_status}" -eq 0 ]] || die 'migration proof events were incomplete'
printf 'live-postgres-migrations: suite_elapsed=%ss fixture=PostgreSQL-18.6 tests=7\n' \
  "$((SECONDS - start_seconds))"
