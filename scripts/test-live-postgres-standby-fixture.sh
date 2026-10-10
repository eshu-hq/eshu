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
# Bind the condition and stop command to the teardown step itself. Queue
# selection may wrap always(), but only by ANDing it with this job's selector.
standby_teardown_guard() {
  local step condition command normalized expected_queue expected_run
  step="$(awk '
    /^      - name: Stop disposable physical standby$/ { inside=1; print; next }
    inside && /^      - / { exit }
    inside { print }
  ' "$1")"
  condition="$(printf '%s\n' "${step}" | rg '^        if: ' || true)"
  command="$(printf '%s\n' "${step}" | rg '^        run: ' || true)"
  normalized="$(printf '%s' "${condition}" | tr -d "[:space:]'\"")"
  expected_queue='if:${{(github.event_name!=merge_group||(contains(fromJSON(needs.queue-selection.outputs.jobs||[]),live-postgres-readiness(postgres18))))&&(always())}}'
  expected_run="        run: bash scripts/ci/live-postgres-standby-fixture.sh stop '\${{ github.run_id }}-\${{ github.run_attempt }}'"
  [[ "${normalized}" == 'if:always()' || "${normalized}" == "${expected_queue}" ]] || return 1
  [[ "${command}" == "${expected_run}" ]]
}
standby_teardown_guard "${workflow}" || fail 'workflow lacks always-on teardown'

# The original unconditional stop step remains valid outside queue selection.
sed '/- name: Stop disposable physical standby/{n;s/.*/        if: always()/;}' \
  "${workflow}" >"${scratch}/teardown-legacy.yml"
standby_teardown_guard "${scratch}/teardown-legacy.yml" ||
  fail 'teardown guard rejects an unconditional always-on stop'

# A sibling always() cannot cover this step, and OR would bypass the selector.
sed '/- name: Stop disposable physical standby/{n;s/.*/        if: false/;}' \
  "${workflow}" >"${scratch}/teardown-skipped.yml"
rg --fixed-strings --quiet -- '        if: false' "${scratch}/teardown-skipped.yml" ||
  fail 'skipped-teardown fixture was not planted'
if standby_teardown_guard "${scratch}/teardown-skipped.yml"; then
  fail 'teardown guard accepts a skipped stop step'
fi
sed '/- name: Stop disposable physical standby/{n;s/always()/true || always()/;}' \
  "${workflow}" >"${scratch}/teardown-bypassed.yml"
rg --fixed-strings --quiet -- 'true || always()' "${scratch}/teardown-bypassed.yml" ||
  fail 'bypassed-teardown fixture was not planted'
if standby_teardown_guard "${scratch}/teardown-bypassed.yml"; then
  fail 'teardown guard accepts an OR bypass'
fi
sed '/- name: Stop disposable physical standby/{n;n;s/fixture.sh stop/fixture.sh start/;}' \
  "${workflow}" >"${scratch}/teardown-wrong-command.yml"
rg --fixed-strings --quiet -- 'fixture.sh start' "${scratch}/teardown-wrong-command.yml" ||
  fail 'wrong-command fixture was not planted'
if standby_teardown_guard "${scratch}/teardown-wrong-command.yml"; then
  fail 'teardown guard accepts a non-stop command'
fi

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
