#!/usr/bin/env bash
# Runs the actual section seed against disposable PostgreSQL; no serving access.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
container="eshu-documentation-reseed-${$}"
image="${ESHU_RESEED_TEST_POSTGRES_IMAGE:-postgres:18}"
cleanup() { docker rm -f "${container}" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker run --detach --name "${container}" --network none \
  -e POSTGRES_PASSWORD=fixture-password "${image}" >/dev/null
ready=false
for _ in $(seq 1 30); do
  if docker exec "${container}" pg_isready -U postgres >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
[[ "${ready}" == true ]] || { echo 'PostgreSQL readiness timed out' >&2; exit 1; }
pg() { docker exec "${container}" psql -XAt -v ON_ERROR_STOP=1 -U postgres -c "$1"; }
die() { echo "$*" >&2; exit 1; }
assert_state() {
  local expected="$1" visible="${2:-1}" actual
  actual="$(pg "SELECT scope_id || '|' || generation_id || '|' ||
    (payload->>'heading_text') FROM fact_records
    WHERE fact_id = 'golden-documentation-section';")"
  [[ "${actual}" == "${expected}" ]] || die "seed state: ${actual}; want ${expected}"
  [[ "$(pg 'SELECT count(*) FROM fact_records;')" == 1 ]] || die 'duplicate section fact'
  actual="$(pg "SELECT count(*) FROM fact_records fact
    JOIN ingestion_scopes scope ON scope.scope_id = fact.scope_id
      AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations generation ON generation.generation_id = fact.generation_id
    WHERE generation.status = 'active' AND fact.fact_kind = 'documentation_section';")"
  [[ "${actual}" == "${visible}" ]] || die "active section count: ${actual}; want ${visible}"
}
docker exec -i "${container}" psql -X -v ON_ERROR_STOP=1 -U postgres \
  < "${repo_root}/scripts/fixtures/golden-documentation-reseed-schema.sql" >/dev/null
# shellcheck source=scripts/lib/golden-corpus-documentation-fixture.sh
source "${repo_root}/scripts/lib/golden-corpus-documentation-fixture.sh"
pg 'SELECT version();'
seed_golden_documentation_section_read_model
assert_state 'scope-one|generation-one|Runtime readiness'
pg "INSERT INTO scope_generations VALUES ('generation-two', 'active');
  UPDATE ingestion_scopes SET active_generation_id = 'generation-two';
  UPDATE fact_records SET payload = '{\"heading_text\":\"old payload\"}';" >/dev/null
seed_golden_documentation_section_read_model
assert_state 'scope-one|generation-two|Runtime readiness'
pg "UPDATE scope_generations SET status = 'retired';
  INSERT INTO scope_generations VALUES ('generation-three', 'active');
  INSERT INTO ingestion_scopes VALUES ('scope-two', 'repository',
    'generation-three', '{\"name\":\"orders-api\"}');" >/dev/null
seed_golden_documentation_section_read_model
assert_state 'scope-two|generation-three|Runtime readiness'
seed_golden_documentation_section_read_model
assert_state 'scope-two|generation-three|Runtime readiness'
pg "UPDATE scope_generations SET status = 'retired';" >/dev/null
if (seed_golden_documentation_section_read_model); then
  die 'missing active generation must fail closed'
fi
assert_state 'scope-two|generation-three|Runtime readiness' 0
echo 'PASS: actual seed refreshes payload, scope and generation; replay is unique; missing active generation fails closed'
