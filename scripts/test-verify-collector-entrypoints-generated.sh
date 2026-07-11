#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

manifest="${tmp_root}/collector_entrypoints.yaml"
# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes the
# entire heredoc body to a pipe before forking the reader, and macOS's
# 512-byte pipe buffer deadlocks on any body over that size (#5074).
cat "${repo_root}/scripts/lib/test-verify-collector-entrypoints-generated-manifest.yaml" >"$manifest"

ESHU_COLLECTOR_ENTRYPOINTS_REPO_ROOT="$tmp_root" \
  ESHU_COLLECTOR_ENTRYPOINTS_GO_DIR="${repo_root}/go" \
  ESHU_COLLECTOR_ENTRYPOINTS_MANIFEST="$manifest" \
  "${repo_root}/scripts/generate-collector-entrypoints.sh" >/tmp/eshu-entrypoints-generate.out

ESHU_COLLECTOR_ENTRYPOINTS_REPO_ROOT="$tmp_root" \
  ESHU_COLLECTOR_ENTRYPOINTS_GO_DIR="${repo_root}/go" \
  ESHU_COLLECTOR_ENTRYPOINTS_MANIFEST="$manifest" \
  "${repo_root}/scripts/verify-collector-entrypoints-generated.sh" >/tmp/eshu-entrypoints-verify.out

printf 'package main\n' >"${tmp_root}/go/cmd/collector-demo/main.go"
if ESHU_COLLECTOR_ENTRYPOINTS_REPO_ROOT="$tmp_root" \
  ESHU_COLLECTOR_ENTRYPOINTS_GO_DIR="${repo_root}/go" \
  ESHU_COLLECTOR_ENTRYPOINTS_MANIFEST="$manifest" \
  "${repo_root}/scripts/verify-collector-entrypoints-generated.sh" >/tmp/eshu-entrypoints-stale.out 2>/tmp/eshu-entrypoints-stale.err; then
  printf 'expected stale generated collector entrypoint check to fail\n' >&2
  exit 1
fi

if ! rg -q 'generated file .* is stale' /tmp/eshu-entrypoints-stale.err; then
  printf 'expected stale generated file error, got:\n' >&2
  sed -n '1,120p' /tmp/eshu-entrypoints-stale.err >&2
  exit 1
fi

printf 'verify-collector-entrypoints-generated tests passed\n'
