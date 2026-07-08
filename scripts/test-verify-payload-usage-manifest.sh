#!/usr/bin/env bash
# Test mirror for scripts/verify-payload-usage-manifest.sh. Drives the real
# verify script and the real CLI (not a re-implementation of their logic)
# against the checked-in schema (temporarily mutated and restored), proving
# the issue #4573 fixture cases:
#   (a) forward-safe: the real repo state passes today -> pass
#   (b) failing-first: dropping a declared field a real handler still reads
#       -> fail, naming the handler file, fact kind, and field
#   (c) manifest generation is idempotent (re-running -> no diff)
#   (d) CI/workflow triggers watch every payload-usage surface
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

pass=0
fail=0

check() {
  local desc="$1"
  local status="$2"
  if [ "$status" -eq 0 ]; then
    printf 'PASS: %s\n' "$desc"
    pass=$((pass + 1))
  else
    printf 'FAIL: %s\n' "$desc"
    fail=$((fail + 1))
  fi
}

# run_gate runs the real verify script with stdin closed, capturing combined
# output to $1 and the exit code into the global rc variable, without ever
# combining a command substitution and a pipe/redirect in one compound
# expression (kept deliberately simple and sequential; a compound `cmd1 | rg
# -q ... && cmd2` chain was found to hang intermittently in this harness).
rc=0
run_gate() {
  local out_file="$1"
  rc=0
  ESHU_PAYLOAD_USAGE_MANIFEST_REPO_ROOT="$repo_root" \
    bash "$repo_root/scripts/verify-payload-usage-manifest.sh" </dev/null >"$out_file" 2>&1 || rc=$?
}

# --- (a) forward-safe: the real repository state passes the gate today ---
out_a_file="${tmp_root}/gate_output_a.txt"
run_gate "$out_a_file"
if [ "$rc" -eq 0 ]; then
  check "real repository state passes the gate (forward-safe case)" 0
else
  cat "$out_a_file" >&2
  check "real repository state passes the gate (forward-safe case)" 1
fi

# --- (b) failing-first: drop a declared field a real handler still reads ---
# aws_resource.v1.schema.json declares "resource_type" as required; every
# migrated aws_resource consumer reads resource.ResourceType (see
# go/internal/reducer/aws_resource_materialization.go's cloudResourceNodeRow
# and go/internal/reducer/aws_relationship_join.go's resource-uid helper).
# Dropping it from the schema's properties+required arrays must fail the
# gate and name the field, the fact kind, and at least one real handler file.
schema_rel="sdk/go/factschema/schema/aws_resource.v1.schema.json"
schema_path="${repo_root}/${schema_rel}"
backup_path="${tmp_root}/aws_resource.v1.schema.json.bak"
cp "$schema_path" "$backup_path"
restore_schema() { cp "$backup_path" "$schema_path"; }
trap 'restore_schema; rm -rf "$tmp_root"' EXIT

broken_schema="${tmp_root}/aws_resource.v1.schema.broken.json"
jq 'del(.properties.resource_type) | .required |= map(select(. != "resource_type"))' \
  <"$schema_path" >"$broken_schema"
cp "$broken_schema" "$schema_path"

out_b_file="${tmp_root}/gate_output_b.txt"
run_gate "$out_b_file"
restore_schema

b_ok=1
if [ "$rc" -eq 0 ]; then
  b_ok=0
fi
if ! rg -q 'ResourceType' "$out_b_file"; then
  b_ok=0
fi
if ! rg -q 'FactKindAWSResource' "$out_b_file"; then
  b_ok=0
fi
if ! rg -q 'aws_resource_materialization\.go' "$out_b_file"; then
  b_ok=0
fi
if [ "$b_ok" -eq 1 ]; then
  check "dropping a field a real handler reads fails and names handler + kind + field" 0
else
  cat "$out_b_file" >&2
  check "dropping a field a real handler reads fails and names handler + kind + field" 1
fi

# Restore is idempotent-safe: confirm the gate is green again after restore,
# so this test does not leave the working tree in a broken state.
out_b2_file="${tmp_root}/gate_output_b2.txt"
run_gate "$out_b2_file"
if [ "$rc" -eq 0 ]; then
  check "schema restore leaves the gate green again" 0
else
  cat "$out_b2_file" >&2
  check "schema restore leaves the gate green again" 1
fi

# --- (c) manifest generation is idempotent ---
gen_dir="${tmp_root}/gen"
mkdir -p "$gen_dir"
(
  cd "${repo_root}/go"
  go run ./cmd/payload-usage-manifest -repo-root "$repo_root" -mode generate -out "${gen_dir}/manifest.json" </dev/null
)
cp "${gen_dir}/manifest.json" "${gen_dir}/manifest.json.bak"
(
  cd "${repo_root}/go"
  go run ./cmd/payload-usage-manifest -repo-root "$repo_root" -mode generate -out "${gen_dir}/manifest.json" </dev/null
)
if cmp -s "${gen_dir}/manifest.json" "${gen_dir}/manifest.json.bak"; then
  check "generator is idempotent on a clean re-run" 0
else
  check "generator is idempotent on a clean re-run" 1
fi

# --- (d) trigger coverage: every checked source surface wakes the gate ---
workflow="${repo_root}/.github/workflows/payload-usage-manifest.yml"
registry="${repo_root}/specs/ci-gates.v1.yaml"
triggers_ok=0
for watched_path in \
  'go/internal/reducer/**' \
  'go/internal/projector/**' \
  'go/internal/query/**' \
  'go/internal/storage/postgres/**' \
  'go/internal/relationships/**' \
  'go/internal/replay/**' \
  'sdk/go/factschema/**' \
  'go/internal/payloadusage/**' \
  'go/cmd/payload-usage-manifest/**'; do
  if ! rg -F -q "\"${watched_path}\"" "$workflow"; then
    printf 'missing workflow trigger: %s\n' "$watched_path" >&2
    triggers_ok=1
  fi
  if ! rg -F -q "\"${watched_path}\"" "$registry"; then
    printf 'missing registry trigger: %s\n' "$watched_path" >&2
    triggers_ok=1
  fi
done
check "workflow and ci-gate registry watch every payload-usage surface" "$triggers_ok"

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
