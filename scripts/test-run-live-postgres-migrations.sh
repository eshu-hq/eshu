#!/usr/bin/env bash
# Fail-closed, disposable-resource checks for the migration-only CI runner.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runner="${root}/scripts/run-live-postgres-migrations.sh"
scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT
mkdir -p "${scratch}/bin"

fail() { printf 'test-live-postgres-migrations-runner: %s\n' "$*" >&2; exit 1; }

cp "${root}/scripts/testdata/live-postgres-migrations-fake-docker.sh" "${scratch}/bin/docker"
cp "${root}/scripts/testdata/live-postgres-migrations-fake-go.sh" "${scratch}/bin/go"
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
