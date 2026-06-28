#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

mkdir -p "$tmp_root/specs"
cp "$repo_root/specs/fact-kind-registry.v1.yaml" "$tmp_root/specs/fact-kind-registry.v1.yaml"

ESHU_FACT_KIND_REGISTRY_REPO_ROOT="$tmp_root" \
  ESHU_FACT_KIND_REGISTRY_GO_DIR="${repo_root}/go" \
  "$repo_root/scripts/generate-fact-kind-registry.sh" >/tmp/eshu-fact-kind-registry-generate.out

gen_go="$tmp_root/go/internal/facts/fact_kind_registry.generated.go"
gen_doc="$tmp_root/go/internal/facts/FACT_KIND_REGISTRIES.md"
for path in "$gen_go" "$gen_doc"; do
  if [ ! -f "$path" ]; then
    printf 'fact-kind-registry: expected generated file %s not found\n' "$path" >&2
    exit 1
  fi
done

if ! rg -q 'optional_semantic' "$gen_go"; then
  printf 'generated Go missing optional semantic truth profile\n' >&2
  exit 1
fi
if ! rg -q 'semantic.documentation_observation' "$gen_doc"; then
  printf 'generated docs missing semantic documentation observation row\n' >&2
  exit 1
fi
if ! rg -q 'No-provider' "$gen_doc"; then
  printf 'generated docs missing no-provider column\n' >&2
  exit 1
fi

cp "$gen_go" "${gen_go}.bak"
cp "$gen_doc" "${gen_doc}.bak"
ESHU_FACT_KIND_REGISTRY_REPO_ROOT="$tmp_root" \
  ESHU_FACT_KIND_REGISTRY_GO_DIR="${repo_root}/go" \
  "$repo_root/scripts/generate-fact-kind-registry.sh" >/tmp/eshu-fact-kind-registry-generate-2.out

if ! cmp -s "$gen_go" "${gen_go}.bak" || ! cmp -s "$gen_doc" "${gen_doc}.bak"; then
  printf 'generate-fact-kind-registry is not idempotent on a clean re-run\n' >&2
  exit 1
fi

ESHU_FACT_KIND_REGISTRY_REPO_ROOT="$tmp_root" \
  ESHU_FACT_KIND_REGISTRY_GO_DIR="${repo_root}/go" \
  "$repo_root/scripts/verify-fact-kind-registry.sh" >/tmp/eshu-fact-kind-registry-verify.out

printf 'package facts\n' >"$gen_go"
if ESHU_FACT_KIND_REGISTRY_REPO_ROOT="$tmp_root" \
  ESHU_FACT_KIND_REGISTRY_GO_DIR="${repo_root}/go" \
  "$repo_root/scripts/verify-fact-kind-registry.sh" >/tmp/eshu-fact-kind-registry-stale.out 2>/tmp/eshu-fact-kind-registry-stale.err; then
  printf 'expected stale fact-kind registry check to fail\n' >&2
  exit 1
fi
if ! rg -q 'stale' /tmp/eshu-fact-kind-registry-stale.err; then
  printf 'expected stale generated file error, got:\n' >&2
  head -n 20 /tmp/eshu-fact-kind-registry-stale.err >&2
  exit 1
fi

cp "$repo_root/specs/fact-kind-registry.v1.yaml" "$tmp_root/specs/fact-kind-registry.v1.yaml"
perl -0pi -e 's/\n    policy_gate: semanticpolicy//' "$tmp_root/specs/fact-kind-registry.v1.yaml"
if ESHU_FACT_KIND_REGISTRY_REPO_ROOT="$tmp_root" \
  ESHU_FACT_KIND_REGISTRY_GO_DIR="${repo_root}/go" \
  "$repo_root/scripts/verify-fact-kind-registry.sh" >/tmp/eshu-fact-kind-registry-policy.out 2>/tmp/eshu-fact-kind-registry-policy.err; then
  printf 'expected missing semantic policy gate check to fail\n' >&2
  exit 1
fi
if ! rg -q 'policy_gate' /tmp/eshu-fact-kind-registry-policy.err; then
  printf 'expected policy_gate validation error, got:\n' >&2
  head -n 20 /tmp/eshu-fact-kind-registry-policy.err >&2
  exit 1
fi

printf 'test-verify-fact-kind-registry tests passed\n'
