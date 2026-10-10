#!/usr/bin/env bash
# Seeded failure checks for the disposable physical-standby CI fixture.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${repo_root}/scripts/ci/live-postgres-standby-fixture.sh"
workflow="${repo_root}/.github/workflows/live-postgres-readiness.yml"
scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT

fail() { printf 'test-live-postgres-standby-fixture: %s\n' "$*" >&2; exit 1; }

command -v rg >/dev/null 2>&1 || fail 'ripgrep (rg) is required to inspect the workflow'
[[ -f "${helper}" ]] || fail 'fixture helper missing'
install_step="$(rg -n -m1 '^[[:space:]]+run: scripts/ci/install-apt-packages\.sh ripgrep$' "${workflow}" || true)"
fixture_step="$(rg -n -m1 -F 'name: Check physical-standby fixture fail-closed behavior' "${workflow}" || true)"
start_step="$(rg -n -m1 -F 'name: Start disposable physical standby' "${workflow}" || true)"
[[ -n "${install_step}" && -n "${fixture_step}" && -n "${start_step}" ]] ||
  fail 'workflow does not install ripgrep before the standby fixture steps'
[[ "${install_step%%:*}" -lt "${fixture_step%%:*}" && "${install_step%%:*}" -lt "${start_step%%:*}" ]] ||
  fail 'workflow installs ripgrep after a standby fixture step'
rg -q 'job.services.postgres.id' "${workflow}" || fail 'workflow does not pass its exact primary container ID'
rg -q 'ESHU_TEST_CONTENT_INDEX_POSTGRES_READ_DSN:' "${workflow}" || fail 'workflow does not pass direct standby DSN'
rg -q 'ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE:' "${workflow}" || fail 'workflow does not opt into disposable DBs'
rg -q 'if: always\(\)' "${workflow}" || fail 'workflow lacks always-on teardown'

mkdir -p "${scratch}/bin"
cat >"${scratch}/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"${ESHU_FIXTURE_DOCKER_CALLS}"
exit 1
EOF
chmod +x "${scratch}/bin/docker"
export ESHU_FIXTURE_DOCKER_CALLS="${scratch}/calls"
out="$(PATH="${scratch}/bin:${PATH}" bash "${helper}" start nonexistent-primary seeded 2>&1)" &&
  fail 'missing primary was accepted'
[[ "${out}" == *'primary'* ]] || fail "missing primary error was not named: ${out}"
[[ ! -s "${scratch}/calls" || "$(wc -l <"${scratch}/calls")" -le 2 ]] ||
  fail 'missing primary triggered fixture creation'

out="$(bash "${helper}" start '/bad' seeded 2>&1)" && fail 'invalid primary ID was accepted'
[[ "${out}" == *'primary'* ]] || fail "invalid primary ID was not named: ${out}"
out="$(bash "${helper}" stop '/bad' 2>&1)" && fail 'invalid fixture ID was accepted'
[[ "${out}" == *'fixture'* ]] || fail "invalid fixture ID was not named: ${out}"

printf 'test-live-postgres-standby-fixture: PASS\n'
