#!/usr/bin/env bash
set -euo pipefail

# scripts/verify-skill-roundtrip.sh gates the S3 CI step of the Eshu
# skillgen epic. It builds the `eshu skillgen` binary, runs its `check`
# subcommand against the committed skill-fragments/, expected/, and
# specs/surface-inventory.v1.yaml, and exits non-zero when the
# per-host roundtrip baseline drifts.
#
# The gate is hermetic (no live Postgres or NornicDB), fast (target
# under 60s wall time including the Go build), and cache-aware (the
# built binary is cached at <repo_root>/go/bin/skillgen and reused on
# subsequent runs).
#
# Env-var overrides (for tests and non-standard layouts):
#   ESHU_SKILL_ROUNDTRIP_REPO_ROOT  repo root (default: git rev-parse show-toplevel)
#   ESHU_SKILL_ROUNDTRIP_BIN        pre-built binary (default: builds into <repo_root>/go/bin/skillgen)
#   ESHU_SKILL_ROUNDTRIP_FRAGMENTS  fragments dir (default: <repo_root>/skill-fragments)
#   ESHU_SKILL_ROUNDTRIP_EXPECTED   expected dir (default: <repo_root>/expected)
#   ESHU_SKILL_ROUNDTRIP_CATALOG    catalog path (default: <repo_root>/specs/surface-inventory.v1.yaml)

repo_root="${ESHU_SKILL_ROUNDTRIP_REPO_ROOT:-}"
if [ -z "$repo_root" ]; then
  repo_root="$(git -C "$(dirname "$0")" rev-parse --show-toplevel 2>/dev/null \
    || (cd "$(dirname "$0")/.." && pwd))"
fi

fragments_dir="${ESHU_SKILL_ROUNDTRIP_FRAGMENTS:-${repo_root}/skill-fragments}"
expected_dir="${ESHU_SKILL_ROUNDTRIP_EXPECTED:-${repo_root}/expected}"
catalog_path="${ESHU_SKILL_ROUNDTRIP_CATALOG:-${repo_root}/specs/surface-inventory.v1.yaml}"

if [ ! -d "$fragments_dir" ]; then
  printf 'verify-skill-roundtrip: fragments dir %s does not exist\n' "$fragments_dir" >&2
  exit 1
fi
if [ ! -d "$expected_dir" ]; then
  printf 'verify-skill-roundtrip: expected dir %s does not exist\n' "$expected_dir" >&2
  exit 1
fi
if [ ! -f "$catalog_path" ]; then
  printf 'verify-skill-roundtrip: catalog %s does not exist\n' "$catalog_path" >&2
  exit 1
fi

bin_path="${ESHU_SKILL_ROUNDTRIP_BIN:-${repo_root}/go/bin/skillgen}"
if [ ! -x "$bin_path" ]; then
  go_module_dir="${repo_root}/go"
  if [ ! -d "$go_module_dir" ]; then
    printf 'verify-skill-roundtrip: go module dir %s does not exist\n' "$go_module_dir" >&2
    exit 1
  fi
  mkdir -p "$(dirname "$bin_path")"
  printf 'verify-skill-roundtrip: building %s\n' "$bin_path" >&2
  (cd "$go_module_dir" && go build -o "$bin_path" ./cmd/skillgen/...)
fi

if ! "$bin_path" check \
    -fragments "$fragments_dir" \
    -expected "$expected_dir" \
    -catalog "$catalog_path"; then
  exit 1
fi

printf 'verify-skill-roundtrip: skill-fragments/, expected/, and specs/surface-inventory.v1.yaml are in lockstep\n'
