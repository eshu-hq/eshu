#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT
mkdir -p "${fixture}/go/cmd/ifa" "${fixture}/go/cmd/probe" "${fixture}/go/internal"

cat >"${fixture}/go/cmd/ifa/drive.go" <<'GO'
package main
func open() { runtimecfg.OpenPostgres(ctx, getenv) }
GO
cat >"${fixture}/go/cmd/probe/main.go" <<'GO'
package main
func open() { runtimecfg.OpenPostgres(ctx, getenv) }
GO
if ESHU_ROLE_OPENER_SCAN_ROOT="${fixture}" bash "${repo_root}/scripts/dev/verify-postgres-role-openers.sh" >"${fixture}/red.log" 2>&1; then
  printf 'legacy production opener unexpectedly passed\n' >&2
  exit 1
fi
if ! rg -q 'go/cmd/probe/main.go' "${fixture}/red.log"; then
  printf 'guard failed for a reason other than the planted opener\n' >&2
  exit 1
fi

cat >"${fixture}/go/cmd/probe/main.go" <<'GO'
package main
func open() { runtimecfg.OpenPostgresForWorkload(ctx, getenv, runtimecfg.PostgresWorkloadRuntime) }
GO
ESHU_ROLE_OPENER_SCAN_ROOT="${fixture}" bash "${repo_root}/scripts/dev/verify-postgres-role-openers.sh"
printf 'Postgres opener guard seeded RED/GREEN: PASS\n'
