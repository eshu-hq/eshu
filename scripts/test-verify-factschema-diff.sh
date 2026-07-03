#!/usr/bin/env bash
# Test mirror for scripts/verify-factschema-diff.sh. Builds a throwaway git
# fixture repo with a baseline commit on "main" and a feature branch, then
# drives the real verify script (not a re-implementation of its logic)
# through the fixtures issue #4569 requires plus the added break classes:
#   (a) removed required field -> fail
#   (b) renamed field -> fail
#   (c) narrowed type -> fail
#   (d) additive optional field -> pass
#   (f) removed optional field (fail-closed) -> fail
#   (g) added new required field -> fail
#   plus a new-schema-passes case and a help-text content check.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

baseline_schema='{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "resource_id", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}'

schema_rel="sdk/go/factschema/schema/aws_resource.v1.schema.json"

git_env=(
  "GIT_AUTHOR_NAME=test" "GIT_AUTHOR_EMAIL=test@example.com"
  "GIT_COMMITTER_NAME=test" "GIT_COMMITTER_EMAIL=test@example.com"
)

setup_fixture_repo() {
  local dir="$1"
  mkdir -p "$dir/$(dirname "$schema_rel")"
  ( cd "$dir" && git init -q -b main )
  printf '%s\n' "$baseline_schema" >"$dir/$schema_rel"
  ( cd "$dir" && git add -A && env "${git_env[@]}" git commit -q -m "baseline schema" )
  ( cd "$dir" && git checkout -q -b feature )
}

run_gate() {
  local dir="$1"
  ESHU_FACTSCHEMA_DIFF_REPO_ROOT="$dir" \
    ESHU_FACTSCHEMA_DIFF_GO_DIR="${repo_root}/go" \
    "$repo_root/scripts/verify-factschema-diff.sh" -base-ref main
}

commit_schema() {
  local dir="$1"
  local contents="$2"
  local message="$3"
  printf '%s\n' "$contents" >"$dir/$schema_rel"
  ( cd "$dir" && git add -A && env "${git_env[@]}" git commit -q -m "$message" )
}

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

# --- Fixture (a): removed required field -> gate fails, names field + kind ---
dir_a="$tmp_root/a-removed-field"
setup_fixture_repo "$dir_a"
removed_field_schema='{
  "properties": {
    "account_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}'
commit_schema "$dir_a" "$removed_field_schema" "remove resource_id"
out_a="$(run_gate "$dir_a" 2>&1)" && rc_a=0 || rc_a=$?
if [ "$rc_a" -ne 0 ] && printf '%s' "$out_a" | rg -q 'resource_id' && printf '%s' "$out_a" | rg -q 'removed_required_field'; then
  check "removed required field fails and names field + violation type" 0
else
  printf '%s\n' "$out_a" >&2
  check "removed required field fails and names field + violation type" 1
fi

# --- Fixture (b): renamed field -> gate fails (surfaces as removed) ---
dir_b="$tmp_root/b-renamed-field"
setup_fixture_repo "$dir_b"
renamed_field_schema='{
  "properties": {
    "account_id": {"type": "string"},
    "resource_identifier": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "resource_identifier", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}'
commit_schema "$dir_b" "$renamed_field_schema" "rename resource_id to resource_identifier"
out_b="$(run_gate "$dir_b" 2>&1)" && rc_b=0 || rc_b=$?
if [ "$rc_b" -ne 0 ] && printf '%s' "$out_b" | rg -q 'resource_id'; then
  check "renamed field fails and names the old field" 0
else
  printf '%s\n' "$out_b" >&2
  check "renamed field fails and names the old field" 1
fi

# --- Fixture (c): narrowed type -> gate fails, names field + kind ---
dir_c="$tmp_root/c-narrowed-type"
setup_fixture_repo "$dir_c"
narrowed_type_schema='{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string", "enum": ["us-east-1", "us-west-2"]},
    "resource_type": {"type": "string"},
    "name": {"type": "string"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "resource_id", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}'
commit_schema "$dir_c" "$narrowed_type_schema" "narrow region to an enum"
out_c="$(run_gate "$dir_c" 2>&1)" && rc_c=0 || rc_c=$?
if [ "$rc_c" -ne 0 ] && printf '%s' "$out_c" | rg -q 'region' && printf '%s' "$out_c" | rg -q 'narrowed_type'; then
  check "narrowed type fails and names field + violation type" 0
else
  printf '%s\n' "$out_c" >&2
  check "narrowed type fails and names field + violation type" 1
fi

# --- Fixture (d): additive optional field -> gate passes ---
dir_d="$tmp_root/d-additive-optional"
setup_fixture_repo "$dir_d"
additive_schema='{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "name": {"type": "string"},
    "availability_zone": {"type": "string"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "resource_id", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}'
commit_schema "$dir_d" "$additive_schema" "add optional availability_zone"
out_d="$(run_gate "$dir_d" 2>&1)" && rc_d=0 || rc_d=$?
if [ "$rc_d" -eq 0 ]; then
  check "additive optional field passes" 0
else
  printf '%s\n' "$out_d" >&2
  check "additive optional field passes" 1
fi

# --- Fixture (f): removed OPTIONAL field (fail-closed) -> gate fails ---
# additionalProperties:false makes dropping an optional field a real break.
dir_f="$tmp_root/f-removed-optional"
setup_fixture_repo "$dir_f"
removed_optional_schema='{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "resource_id", "region", "resource_type"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}'
commit_schema "$dir_f" "$removed_optional_schema" "remove optional field name"
out_f="$(run_gate "$dir_f" 2>&1)" && rc_f=0 || rc_f=$?
if [ "$rc_f" -ne 0 ] && printf '%s' "$out_f" | rg -q 'name' && printf '%s' "$out_f" | rg -q 'removed_field'; then
  check "removed optional field fails and names field + violation type" 0
else
  printf '%s\n' "$out_f" >&2
  check "removed optional field fails and names field + violation type" 1
fi

# --- Fixture (g): added new REQUIRED field -> gate fails ---
dir_g="$tmp_root/g-added-required"
setup_fixture_repo "$dir_g"
added_required_schema='{
  "properties": {
    "account_id": {"type": "string"},
    "resource_id": {"type": "string"},
    "region": {"type": "string"},
    "resource_type": {"type": "string"},
    "partition": {"type": "string"},
    "name": {"type": "string"}
  },
  "additionalProperties": false,
  "type": "object",
  "required": ["account_id", "resource_id", "region", "resource_type", "partition"],
  "title": "Eshu aws.resource Payload (schema version 1)"
}'
commit_schema "$dir_g" "$added_required_schema" "add new required field partition"
out_g="$(run_gate "$dir_g" 2>&1)" && rc_g=0 || rc_g=$?
if [ "$rc_g" -ne 0 ] && printf '%s' "$out_g" | rg -q 'partition' && printf '%s' "$out_g" | rg -q 'added_required_field'; then
  check "added required field fails and names field + violation type" 0
else
  printf '%s\n' "$out_g" >&2
  check "added required field fails and names field + violation type" 1
fi

# --- New schema with no baseline counterpart -> gate passes ---
dir_e="$tmp_root/e-new-schema"
setup_fixture_repo "$dir_e"
new_kind_schema='{
  "properties": {"kind": {"type": "string"}},
  "additionalProperties": false,
  "type": "object",
  "required": ["kind"],
  "title": "Eshu incident.event Payload (schema version 1)"
}'
printf '%s\n' "$new_kind_schema" >"$dir_e/sdk/go/factschema/schema/incident_event.v1.schema.json"
( cd "$dir_e" && git add -A && env "${git_env[@]}" git commit -q -m "add new fact kind schema" )
out_e="$(run_gate "$dir_e" 2>&1)" && rc_e=0 || rc_e=$?
if [ "$rc_e" -eq 0 ]; then
  check "new schema with no baseline counterpart passes" 0
else
  printf '%s\n' "$out_e" >&2
  check "new schema with no baseline counterpart passes" 1
fi

# --- --help documents baseline behavior ---
help_out="$(cd "${repo_root}/go" && go run ./cmd/factschema-diff -help 2>&1)" || true
if printf '%s' "$help_out" | rg -qi 'base-ref' && printf '%s' "$help_out" | rg -qi 'merge-base' && printf '%s' "$help_out" | rg -Uqi 'NOT\s+a\s*\n?\s*break'; then
  check "--help documents base-ref, merge-base default, and new-schema-is-not-a-break" 0
else
  printf '%s\n' "$help_out" >&2
  check "--help documents base-ref, merge-base default, and new-schema-is-not-a-break" 1
fi

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
