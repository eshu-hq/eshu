#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/golden-corpus-changed-since.sh
source "${repo_root}/scripts/lib/golden-corpus-changed-since.sh"

fail() { printf 'test-golden-corpus-changed-since: %s\n' "$*" >&2; exit 1; }
die() { fail "$@"; }

case_root="$(mktemp -d -t golden-changed-since.XXXXXX)"
trap 'rm -rf "${case_root}"' EXIT
case_dir="${case_root}/hostile path [fixture]"
corpus_dir="${case_dir}/corpus"
fixture_dir="${corpus_dir}/supply-chain-demo-db/config"
mkdir -p "${fixture_dir}"
cp "${repo_root}/tests/fixtures/ecosystems/supply-chain-demo-db/config/freshness.cfg" \
	"${fixture_dir}/freshness.cfg"

mock_active_generation="generation:prior-1"
sql_log="${case_dir}/sql.log"
pg() {
	local sql="$1"
	printf '%s\n-- statement boundary --\n' "${sql}" >>"${sql_log}"
	if [[ "${sql}" == *"WITH prior_keys AS"* ]]; then
		printf '2|1|1|superseded|1|0\n'
		return
	fi
	if [[ "${sql}" == *"SELECT active_generation_id"* ]]; then
		printf '%s\n' "${mock_active_generation}"
		return
	fi
	fail "unexpected SQL: ${sql}"
}

golden_changed_since_capture_prior
[[ "${golden_changed_since_prior_generation}" == "generation:prior-1" ]] ||
	fail "prior generation was not captured"
golden_changed_since_mutate_fixture
rg -q '^release_marker = "current"$' "${fixture_dir}/freshness.cfg" ||
	fail "staged fixture mutation is missing"

mock_active_generation="generation:current-2"
golden_changed_since_validate_current
[[ "${golden_changed_since_current_generation}" == "generation:current-2" ]] ||
	fail "current generation was not captured"

input_snapshot="${case_dir}/prior transform.json"
output_snapshot="${case_dir}/changed since.json"
second_output="${case_dir}/changed since second.json"
jq '
  .composition_marker = "preserved"
  | .query_shapes.mcp.get_changed_since = {
      arguments: {
        scope_id: "git-repository-scope:repository:r_b11b6e25",
        since_generation_id: "__runtime_changed_since_prior_generation__",
        sample_limit: 10
      },
      required_json_values: {
        since_generation_id: "__runtime_changed_since_prior_generation__",
        current_active_generation_id: "__runtime_changed_since_current_generation__"
      }
    }
' "${repo_root}/testdata/golden/e2e-20repo-snapshot.json" >"${input_snapshot}"
golden_changed_since_compose_snapshot "${input_snapshot}" "${output_snapshot}"
golden_changed_since_compose_snapshot "${input_snapshot}" "${second_output}"
cmp -s "${output_snapshot}" "${second_output}" || fail "runtime composition is not deterministic"
jq -e '
  .composition_marker == "preserved"
  and .query_shapes.mcp.get_changed_since.arguments.since_generation_id == "generation:prior-1"
  and .query_shapes.mcp.get_changed_since.required_json_values.since_generation_id == "generation:prior-1"
  and .query_shapes.mcp.get_changed_since.required_json_values.current_active_generation_id == "generation:current-2"
' "${output_snapshot}" >/dev/null || fail "runtime composition lost a prior transform or generation ID"

if rg -ni '\b(insert|update|delete|merge|truncate|create|alter|drop)\b' "${sql_log}" >/dev/null; then
	fail "leaf helper issued mutating SQL"
fi

hostile_multi_row_prior() {
	pg() { printf 'generation:a\ngeneration:b\n'; }
	golden_changed_since_capture_prior
}

hostile_unsafe_generation_id() {
	pg() { printf '%s\n' "generation:'unsafe"; }
	golden_changed_since_capture_prior
}

hostile_repeat_mutation() {
	golden_changed_since_mutate_fixture
}

hostile_missing_sentinel() {
	local missing_snapshot="${case_dir}/missing.json"
	golden_changed_since_prior_generation="prior"
	golden_changed_since_current_generation="current"
	jq 'del(.query_shapes.mcp.get_changed_since.arguments.since_generation_id)' \
		"${input_snapshot}" >"${missing_snapshot}"
	golden_changed_since_compose_snapshot "${missing_snapshot}" "${case_dir}/unused.json"
}

assert_subprocess_fails() {
	local name="$1" case_function="$2"
	if ("${case_function}"); then
		fail "hostile case unexpectedly passed: ${name}"
	fi
}

assert_subprocess_fails "multi-row prior" hostile_multi_row_prior
assert_subprocess_fails "unsafe generation id" hostile_unsafe_generation_id
assert_subprocess_fails "repeat mutation" hostile_repeat_mutation
assert_subprocess_fails "missing sentinel" hostile_missing_sentinel

printf 'PASS: golden repository changed-since leaf helper\n'
