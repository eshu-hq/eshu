#!/usr/bin/env bash
# Compares the registry's required contexts with GitHub's effective branch rules.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
expected_source="${ESHU_EXPECTED_REQUIRED_STATUS_JSON:-}"
effective_source="${ESHU_EFFECTIVE_RULES_JSON:-}"
ruleset_source="${ESHU_RULESET_JSON:-}"
ruleset_id="${ESHU_RULESET_ID:-19745843}"
ruleset_name="${ESHU_RULESET_NAME:-main protection}"
require_visible_bypass_actors="${ESHU_REQUIRE_VISIBLE_BYPASS_ACTORS:-true}"
branch="${ESHU_RULESET_BRANCH:-main}"
repo="${GITHUB_REPOSITORY:-eshu-hq/eshu}"

if [[ "${require_visible_bypass_actors}" != "true" && "${require_visible_bypass_actors}" != "false" ]]; then
	echo "ESHU_REQUIRE_VISIBLE_BYPASS_ACTORS must be true or false" >&2
	exit 1
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

if [[ -n "${expected_source}" ]]; then
	cp "${expected_source}" "${tmp_dir}/expected-source.json"
else
	(
		cd "${repo_root}/go"
		go run ./cmd/ci-gates contexts --registry ../specs/ci-gates.v1.yaml --json
	) >"${tmp_dir}/expected-source.json"
fi

jq -S 'sort_by(.context, .integration_id)' \
	"${tmp_dir}/expected-source.json" >"${tmp_dir}/expected.json"

if [[ -n "${effective_source}" ]]; then
	cp "${effective_source}" "${tmp_dir}/effective-source.json"
else
	gh api "repos/${repo}/rules/branches/${branch}" >"${tmp_dir}/effective-source.json"
fi

if [[ -n "${ruleset_source}" ]]; then
	cp "${ruleset_source}" "${tmp_dir}/ruleset-source.json"
else
	gh api "repos/${repo}/rulesets/${ruleset_id}" >"${tmp_dir}/ruleset-source.json"
fi

if [[ "${require_visible_bypass_actors}" == "true" ]] && \
	! jq -e 'has("bypass_actors") and (.bypass_actors | type == "array")' \
		"${tmp_dir}/ruleset-source.json" >/dev/null; then
	echo "ruleset ${ruleset_id} did not expose bypass_actors; rerun with ruleset write access or explicitly disable the privileged bypass audit" >&2
	exit 1
fi

if ! jq -e \
	--argjson ruleset_id "${ruleset_id}" \
	--arg ruleset_name "${ruleset_name}" '
  .id == $ruleset_id
    and .name == $ruleset_name
    and .target == "branch"
    and .enforcement == "active"
    and ((.bypass_actors // []) == [])
    and .conditions.ref_name.include == ["~DEFAULT_BRANCH"]
    and .conditions.ref_name.exclude == []
    and ([.rules[].type] | sort) == ["deletion", "non_fast_forward", "required_status_checks"]
' "${tmp_dir}/ruleset-source.json" >/dev/null; then
	echo "ruleset ${ruleset_id} (${ruleset_name}) does not match the expected active default-branch required-status policy" >&2
	exit 1
fi

if ! jq -e '
  [.rules[] | select(.type == "required_status_checks")] as $rules
  | ($rules | length) == 1
    and $rules[0].parameters.strict_required_status_checks_policy == true
' "${tmp_dir}/ruleset-source.json" >/dev/null; then
	echo "ruleset ${ruleset_id} (${ruleset_name}) does not own one strict required-status rule" >&2
	exit 1
fi

jq -S '[
  .rules[]
  | select(.type == "required_status_checks")
  | .parameters.required_status_checks[]
  | {context, integration_id}
] | sort_by(.context, .integration_id)' \
	"${tmp_dir}/ruleset-source.json" >"${tmp_dir}/owner-actual.json"

if ! cmp -s "${tmp_dir}/expected.json" "${tmp_dir}/owner-actual.json"; then
	echo "ruleset ${ruleset_id} (${ruleset_name}) does not own the required contexts from specs/ci-gates.v1.yaml:" >&2
	diff -u "${tmp_dir}/expected.json" "${tmp_dir}/owner-actual.json" >&2 || true
	exit 1
fi

if jq -e 'any(.[]; .type == "merge_queue")' "${tmp_dir}/effective-source.json" >/dev/null; then
	echo "effective rules for ${branch} enable merge queue, but required-gates-complete does not yet evaluate merge_group heads" >&2
	exit 1
fi

if ! jq -e '
  [.[] | select(.type == "required_status_checks")] as $rules
  | ($rules | length) > 0
    and ($rules | all(.parameters.strict_required_status_checks_policy == true))
' "${tmp_dir}/effective-source.json" >/dev/null; then
	echo "effective rules for ${branch} do not have strict required-status enforcement" >&2
	exit 1
fi

jq -S '[
  .[]
  | select(.type == "required_status_checks")
  | .parameters.required_status_checks[]
  | {context, integration_id}
] | sort_by(.context, .integration_id)' \
	"${tmp_dir}/effective-source.json" >"${tmp_dir}/actual.json"

if ! cmp -s "${tmp_dir}/expected.json" "${tmp_dir}/actual.json"; then
	echo "required status checks differ from specs/ci-gates.v1.yaml:" >&2
	diff -u "${tmp_dir}/expected.json" "${tmp_dir}/actual.json" >&2 || true
	exit 1
fi

echo "verify-live-required-status-checks: pass (${branch})"
