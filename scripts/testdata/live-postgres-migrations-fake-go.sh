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
