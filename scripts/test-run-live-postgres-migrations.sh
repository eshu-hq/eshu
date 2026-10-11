#!/usr/bin/env bash
# Fail-closed, disposable-resource checks for the migration-only CI runner.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runner="${root}/scripts/run-live-postgres-migrations.sh"
scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT
mkdir -p "${scratch}/bin"

fail() { printf 'test-live-postgres-migrations-runner: %s\n' "$*" >&2; exit 1; }

cat >"${scratch}/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${FAKE_CALLS}"
case "$1" in
  inspect)
    [[ -f "${FAKE_CONTAINER}" ]] || exit 1
    if [[ "$*" == *'.State.Running'* ]]; then printf 'true\n'; fi
    if [[ "$*" == *'{{.Id}}'* ]]; then printf '%064d\n' 0; fi
    if [[ "$*" == *'"eshu.fixture"'* ]]; then printf '%s\n' "${FAKE_DOCKER_OWNER:-live-postgres-migrations}"; fi
    if [[ "$*" == *'"eshu.fixture-id"'* ]]; then printf 'testfixture\n'; fi
    if [[ "$*" == *'"eshu.fixture-token"'* ]]; then
      printf '%s\n' "${FAKE_DOCKER_TOKEN_OVERRIDE:-$(<"${FAKE_CONTAINER_TOKEN}")}"
    fi
    ;;
  run)
    [[ "${FAKE_DOCKER_RUN_MODE:-pass}" != no_create_failure ]] || exit 7
    [[ ! -f "${FAKE_CONTAINER}" ]] || exit 1
    : >"${FAKE_CONTAINER}"
    for argument in "$@"; do
      if [[ "${argument}" == eshu.fixture-token=* ]]; then
        printf '%s\n' "${argument#eshu.fixture-token=}" >"${FAKE_CONTAINER_TOKEN}"
      fi
    done
    [[ "${FAKE_DOCKER_RUN_MODE:-pass}" != created_failure ]] || exit 7
    printf '%064d\n' 0
    ;;
  port) printf '127.0.0.1:15432\n' ;;
  exec)
    if [[ "$*" == *'pg_control_system()'* ]]; then printf '%s\n' "${FAKE_DOCKER_IDENTITY:-123456789|180006}"; fi
    ;;
  rm) rm -f "${FAKE_CONTAINER}" "${FAKE_CONTAINER_TOKEN}" ;;
  *) exit 9 ;;
esac
DOCKER
cat >"${scratch}/bin/go" <<'GO'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == *'-json'* && "$*" == *'-tags integration'* && "$*" == *'-count=1'* ]] || exit 11
[[ "$*" == *'./internal/storage/postgres'* ]] || exit 12
[[ "${ESHU_POSTGRES_MIGRATION169_TEST_DSN}" == 'postgres://eshu169fixture:0123456789abcdef0123456789abcdef@127.0.0.1:15432/eshu169fixture?sslmode=disable' ]] || exit 13
[[ "${ESHU_7033_ROLLBACK_TEST_DSN}" == 'postgres://postgres@127.0.0.1:15432/eshu_7033_rollback?sslmode=disable' ]] || exit 14
[[ "${ESHU_TEST_POSTGRES_SYSTEM_IDENTIFIER}" == 123456789 ]] || exit 15
if [[ "${FAKE_GO_MODE:-pass}" == block ]]; then
  : >"${FAKE_GO_STARTED}"
  sleep 10
fi
names=(
  TestMigration169InterruptedBackfillLive
  TestMigration169PhasedTrackedLive
  TestBootstrapInterruptedDefinitionRollsBackLive
  TestRollbackFixtureDSNGuard
  TestOwnedFixtureConnectorRejectsUnsafeInputBeforeDial
  TestOwnedFixtureConnectorRejectsInheritedPGEnvironment
  TestOwnedFixtureConnectorRejectsWrongServerAndReconnect
)
for name in "${names[@]}"; do
  [[ "$*" == *"${name}"* ]] || exit 16
  [[ "${FAKE_GO_MODE:-pass}" == omit && "${name}" == TestMigration169PhasedTrackedLive ]] && continue
  printf '{"Action":"run","Test":"%s"}\n' "${name}"
  action=pass
  [[ "${FAKE_GO_MODE:-pass}" == skip && "${name}" == TestMigration169PhasedTrackedLive ]] && action=skip
  [[ "${FAKE_GO_MODE:-pass}" == fail && "${name}" == TestMigration169PhasedTrackedLive ]] && action=fail
  printf '{"Action":"%s","Test":"%s"}\n' "${action}" "${name}"
done
printf '{"Action":"pass","Package":"github.com/eshu-hq/eshu/go/internal/storage/postgres"}\n'
GO
cat >"${scratch}/bin/openssl" <<'OPENSSL'
#!/usr/bin/env bash
[[ "$*" == 'rand -hex 16' ]] || exit 9
printf '0123456789abcdef0123456789abcdef\n'
OPENSSL
chmod +x "${scratch}/bin/docker" "${scratch}/bin/go" "${scratch}/bin/openssl"
export FAKE_CALLS="${scratch}/calls" FAKE_CONTAINER="${scratch}/container"
export FAKE_CONTAINER_TOKEN="${scratch}/container-token"
export FAKE_GO_STARTED="${scratch}/go-started"
export PATH="${scratch}/bin:${PATH}"
export ESHU_POSTGRES_MIGRATIONS_IMAGE='ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c'

run_case() {
  : >"${FAKE_CALLS}"
  output="$(bash "${runner}" testfixture 2>&1)"
}

export FAKE_GO_MODE=pass
run_case || fail "seven PASS events rejected: ${output}"
[[ "${output}" == *'7/7 PASS'* ]] || fail "PASS summary absent: ${output}"
[[ ! -f "${FAKE_CONTAINER}" ]] || fail 'success left the fixture container'
rg -q '^rm -f 0{64}$' "${FAKE_CALLS}" || fail 'success did not remove the owned container ID'

export FAKE_DOCKER_RUN_MODE=created_failure
run_case && fail 'Docker run failure after creation was accepted'
[[ "${output}" == *'cannot start owned PostgreSQL fixture'* ]] ||
  fail "Docker run failure not named: ${output}"
[[ ! -f "${FAKE_CONTAINER}" ]] || fail 'failed Docker run left its created fixture container'
rg -q '^rm -f 0{64}$' "${FAKE_CALLS}" || fail 'failed Docker run did not remove its created container ID'
unset FAKE_DOCKER_RUN_MODE

export FAKE_DOCKER_RUN_MODE=no_create_failure
run_case && fail 'Docker failure before creation was accepted'
[[ ! -f "${FAKE_CONTAINER}" ]] || fail 'failed Docker run created an unexpected fixture container'
unset FAKE_DOCKER_RUN_MODE

export FAKE_DOCKER_RUN_MODE=created_failure FAKE_DOCKER_TOKEN_OVERRIDE=other
run_case && fail 'Docker failure accepted a replaced container'
[[ "${output}" == *'refusing to remove unowned container'* ]] ||
  fail "replacement ownership guard not named: ${output}"
[[ -f "${FAKE_CONTAINER}" ]] || fail 'replacement container was removed'
rm -f "${FAKE_CONTAINER}" "${FAKE_CONTAINER_TOKEN}"
unset FAKE_DOCKER_RUN_MODE FAKE_DOCKER_TOKEN_OVERRIDE

for mode in skip omit fail; do
  export FAKE_GO_MODE="${mode}"
  run_case && fail "${mode} event was accepted"
  [[ "${output}" == *'TestMigration169PhasedTrackedLive'* ]] || fail "${mode} omitted failure detail: ${output}"
  [[ ! -f "${FAKE_CONTAINER}" ]] || fail "${mode} left the fixture container"
done

export FAKE_GO_MODE=pass FAKE_DOCKER_IDENTITY='123456789|170006'
run_case && fail 'PostgreSQL 17 identity was accepted'
[[ "${output}" == *'not PostgreSQL 18.6'* ]] || fail "version guard not named: ${output}"
[[ ! -f "${FAKE_CONTAINER}" ]] || fail 'wrong-version fixture was not removed'
unset FAKE_DOCKER_IDENTITY

export FAKE_DOCKER_OWNER=other
run_case && fail 'changed container owner was accepted at cleanup'
[[ "${output}" == *'refusing to remove unowned container'* ]] ||
  fail "ownership guard not named: ${output}"
[[ -f "${FAKE_CONTAINER}" ]] || fail 'unowned container was removed'
rm -f "${FAKE_CONTAINER}"
unset FAKE_DOCKER_OWNER

export FAKE_GO_MODE=block
out="$(timeout --signal=TERM --kill-after=3s 2s bash "${runner}" testfixture 2>&1)" &&
  fail 'cancelled runner exited successfully'
[[ -f "${FAKE_GO_STARTED}" ]] || fail 'cancellation did not reach go test'
[[ ! -f "${FAKE_CONTAINER}" ]] || fail 'cancellation left the fixture container'

export FAKE_GO_MODE=pass
: >"${FAKE_CONTAINER}"
run_case && fail 'preexisting container was accepted'
[[ -f "${FAKE_CONTAINER}" ]] || fail 'preexisting container was removed'
rg -q 'already exists|refus' <<<"${output}" || fail "preexisting container rejection not named: ${output}"
rm -f "${FAKE_CONTAINER}"

printf 'test-live-postgres-migrations-runner: PASS\n'
