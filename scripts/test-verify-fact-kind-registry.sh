#!/usr/bin/env bash
# Test mirror for scripts/verify-fact-kind-registry.sh (issue #6629).
#
# The generator's source of truth is now the typed contract
# declarations in the Go tree, and its registry cross-checks compile
# against the live fact-family helpers, so this mirror runs against
# the real tree instead of a copied fixture root. Every mutation it
# makes is restored by regeneration (see the EXIT trap), leaving the
# tree exactly as it found it when the generated artifacts started
# current. Seeded declaration violations (duplicate ownership and
# friends) are covered exhaustively by the Go guard tests in
# go/cmd/fact-kind-registry/contract_guards_test.go; this mirror
# proves the shell-level generate/verify contract: positive content,
# byte-identical regeneration, and stale/missing/extra detection for
# every generated output class.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

generate() { bash scripts/generate-fact-kind-registry.sh >/tmp/eshu-fact-kind-registry-generate.out; }
verify_ok() { bash scripts/verify-fact-kind-registry.sh >/tmp/eshu-fact-kind-registry-verify.out; }

# Always leave the tree regenerated, even on failure.
trap 'bash scripts/generate-fact-kind-registry.sh >/dev/null 2>&1 || true; rm -f go/internal/facts/cloud/zz_duplicate_family_fixture.go specs/fact-kind-registry-contracts.d/families/zz-extra.yaml' EXIT

generate
verify_ok

gen_go="go/internal/facts/fact_kind_registry.generated.go"
gen_doc="go/internal/facts/FACT_KIND_REGISTRIES.md"
gen_yaml="specs/fact-kind-registry.v1.yaml"
shard="specs/fact-kind-registry-contracts.d/kinds/aws/resource.yaml"
for path in "$gen_go" "$gen_doc" "$gen_yaml" "$shard"; do
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
if ! rg -q 'PayloadSchema: "sdk/go/factschema/schema/aws_resource.v1.schema.json"' "$gen_go"; then
  printf 'generated Go missing wired aws_resource payload_schema\n' >&2
  exit 1
fi
if ! rg -q 'Payload schema' "$gen_doc"; then
  printf 'generated docs missing payload schema column\n' >&2
  exit 1
fi
if ! rg -q 'reducer_bindings:' "$shard"; then
  printf 'kind shard missing reducer bindings\n' >&2
  exit 1
fi
if ! rg -q 'version: 1.1.0' "$gen_yaml"; then
  printf 'generated registry YAML missing v1.1 version\n' >&2
  exit 1
fi

# Repeated generation must be byte-identical.
snapshot="$(mktemp -d)"
cp "$gen_go" "$gen_doc" "$gen_yaml" "$snapshot/"
cp -r specs/fact-kind-registry-contracts.d "$snapshot/contracts.d"
generate
if ! cmp -s "$gen_go" "$snapshot/fact_kind_registry.generated.go" \
  || ! cmp -s "$gen_doc" "$snapshot/FACT_KIND_REGISTRIES.md" \
  || ! cmp -s "$gen_yaml" "$snapshot/fact-kind-registry.v1.yaml" \
  || ! diff -r "$snapshot/contracts.d" specs/fact-kind-registry-contracts.d >/dev/null; then
  printf 'generate-fact-kind-registry is not idempotent on a clean re-run\n' >&2
  exit 1
fi
rm -rf "$snapshot"

# Stale generated Markdown must fail the verify script. (The generated
# Go file cannot be the stale probe in the real tree: the generator
# itself imports the facts package it generates.)
printf 'stale\n' >"$gen_doc"
if bash scripts/verify-fact-kind-registry.sh >/tmp/eshu-fact-kind-registry-stale.out 2>/tmp/eshu-fact-kind-registry-stale.err; then
  printf 'expected stale fact-kind registry check to fail\n' >&2
  exit 1
fi
if ! rg -q 'stale' /tmp/eshu-fact-kind-registry-stale.err; then
  printf 'expected stale generated file error, got:\n' >&2
  head -n 20 /tmp/eshu-fact-kind-registry-stale.err >&2
  exit 1
fi
generate

# A missing shard must fail the verify script.
rm "$shard"
if bash scripts/verify-fact-kind-registry.sh >/tmp/eshu-fact-kind-registry-missing.out 2>/tmp/eshu-fact-kind-registry-missing.err; then
  printf 'expected missing shard check to fail\n' >&2
  exit 1
fi
if ! rg -q 'missing' /tmp/eshu-fact-kind-registry-missing.err; then
  printf 'expected missing generated file error, got:\n' >&2
  head -n 20 /tmp/eshu-fact-kind-registry-missing.err >&2
  exit 1
fi
generate

# An extra file in the shard tree must fail the verify script.
printf 'stale: true\n' >specs/fact-kind-registry-contracts.d/families/zz-extra.yaml
if bash scripts/verify-fact-kind-registry.sh >/tmp/eshu-fact-kind-registry-extra.out 2>/tmp/eshu-fact-kind-registry-extra.err; then
  printf 'expected extra shard check to fail\n' >&2
  exit 1
fi
if ! rg -q 'extra' /tmp/eshu-fact-kind-registry-extra.err; then
  printf 'expected extra generated file error, got:\n' >&2
  head -n 20 /tmp/eshu-fact-kind-registry-extra.err >&2
  exit 1
fi
generate

# A seeded duplicate family declaration must fail with a file:line
# diagnostic naming the conflicting ownership. The fixture stays
# minimal (ID only): the duplicate-ownership guard fires in the
# ownership index before metadata checks run, and the heredoc budget
# caps inline fixture bodies at 512 bytes.
cat >go/internal/facts/cloud/zz_duplicate_family_fixture.go <<'EOF'
// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

var zzDuplicateAWSFamily = contract.Declare(contract.Family{ID: "aws"})
EOF
if bash scripts/verify-fact-kind-registry.sh >/tmp/eshu-fact-kind-registry-dupe.out 2>/tmp/eshu-fact-kind-registry-dupe.err; then
  printf 'expected duplicate family check to fail\n' >&2
  exit 1
fi
if ! rg -q 'duplicate family "aws"' /tmp/eshu-fact-kind-registry-dupe.err \
  || ! rg -q 'zz_duplicate_family_fixture.go:' /tmp/eshu-fact-kind-registry-dupe.err; then
  printf 'expected duplicate family file:line error, got:\n' >&2
  head -n 20 /tmp/eshu-fact-kind-registry-dupe.err >&2
  exit 1
fi
rm go/internal/facts/cloud/zz_duplicate_family_fixture.go
generate
verify_ok

printf 'test-verify-fact-kind-registry tests passed\n'
