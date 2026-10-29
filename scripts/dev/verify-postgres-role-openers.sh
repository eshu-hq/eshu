#!/usr/bin/env bash
set -euo pipefail

repo_root="${ESHU_ROLE_OPENER_SCAN_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
cd "${repo_root}"

# The generic opener refuses role-bound mode. Keep every production caller on
# a compiled workload route so no new binary loses physical attestation.
if rg -n --glob '*.go' --glob '!**/*_test.go' --glob '!go/cmd/ifa/drive.go' \
  'runtimecfg\.OpenPostgres(WithSession(Hooks)?)?\(' go/cmd go/internal; then
  printf 'production Postgres callers must use a workload-aware opener\n' >&2
  exit 1
fi

# Ifa's offline re-drain CLI opens two independently chosen databases from
# command flags. Its legacy opener refuses role-contract JSON; a future
# workload-aware two-contract design must cover source and target separately.
if [[ "$(rg -c 'runtimecfg\.OpenPostgres\(ctx, getenv\)' go/cmd/ifa/drive.go || true)" != 1 ]]; then
  printf 'review the Ifa two-database legacy-opener waiver\n' >&2
  exit 1
fi

printf 'Postgres workload-aware opener census: PASS\n'
