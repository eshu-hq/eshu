#!/usr/bin/env bash
# Hermetic cases for the live effective-rules verifier used by required-gates.yml.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-live-required-status-checks.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

expected="${tmp_dir}/expected.json"
effective="${tmp_dir}/effective.json"
ruleset="${tmp_dir}/ruleset.json"

printf '%s\n' '[{"context":"go-core-complete","integration_id":15368},{"context":"required-gates-complete","integration_id":15368}]' >"${expected}"

# main merges by pull request with required status checks (#8246): the merge
# queue was removed from the live ruleset, so the base fixtures carry no
# merge_queue rule. A queue is not asserted; the cases below prove both shapes
# (with and without a queue) pass.
merge_queue_rule='{"type":"merge_queue","parameters":{"check_response_timeout_minutes":120,"grouping_strategy":"ALLGREEN","max_entries_to_build":4,"max_entries_to_merge":4,"merge_method":"SQUASH","min_entries_to_merge":1,"min_entries_to_merge_wait_minutes":5}}'

write_effective() {
	local strict="$1"
	local checks="$2"
	printf '[{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":%s,"required_status_checks":%s}}]\n' \
		"${strict}" "${checks}" >"${effective}"
}

run_verifier() {
	ESHU_EXPECTED_REQUIRED_STATUS_JSON="${expected}" \
		ESHU_EFFECTIVE_RULES_JSON="${effective}" \
		ESHU_RULESET_JSON="${ruleset}" \
		"${verifier}"
}

run_unprivileged_verifier() {
	ESHU_EXPECTED_REQUIRED_STATUS_JSON="${expected}" \
		ESHU_EFFECTIVE_RULES_JSON="${effective}" \
		ESHU_RULESET_JSON="${ruleset}" \
		ESHU_REQUIRE_VISIBLE_BYPASS_ACTORS=false \
		"${verifier}"
}

write_ruleset() {
	local enforcement="$1"
	local bypass_actors="$2"
	local includes="$3"
	printf '{"id":19745843,"name":"main protection","target":"branch","enforcement":"%s","bypass_actors":%s,"conditions":{"ref_name":{"include":%s,"exclude":[]}},"rules":[{"type":"deletion"},{"type":"non_fast_forward"},{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":false,"required_status_checks":[{"context":"go-core-complete","integration_id":15368},{"context":"required-gates-complete","integration_id":15368}]}}]}\n' \
		"${enforcement}" "${bypass_actors}" "${includes}" >"${ruleset}"
}

write_ruleset active '[]' '["~DEFAULT_BRANCH"]'

write_effective true '[{"context":"go-core-complete","integration_id":15368},{"context":"required-gates-complete","integration_id":15368}]'
run_verifier >/dev/null

jq 'del(.bypass_actors)' "${ruleset}" >"${tmp_dir}/ruleset-without-bypass-actors.json"
mv "${tmp_dir}/ruleset-without-bypass-actors.json" "${ruleset}"
if run_verifier >/dev/null 2>&1; then
	echo "expected a privileged audit with hidden bypass actors to fail" >&2
	exit 1
fi
run_unprivileged_verifier >/dev/null
write_ruleset active '[]' '["~DEFAULT_BRANCH"]'

jq '(.rules[] | select(.type == "required_status_checks").parameters.required_status_checks) = [{"context":"go-core-complete","integration_id":15368}]' \
	"${ruleset}" >"${tmp_dir}/ruleset-with-wrong-owner-contexts.json"
mv "${tmp_dir}/ruleset-with-wrong-owner-contexts.json" "${ruleset}"
if run_verifier >/dev/null 2>&1; then
	echo "expected effective contexts supplied by a different owning ruleset to fail" >&2
	exit 1
fi
write_ruleset active '[]' '["~DEFAULT_BRANCH"]'

# strict_required_status_checks_policy is an owner preference, not policy.
jq '(.rules[] | select(.type == "required_status_checks").parameters.strict_required_status_checks_policy) = true' \
	"${ruleset}" >"${tmp_dir}/ruleset-with-strict-owner.json"
mv "${tmp_dir}/ruleset-with-strict-owner.json" "${ruleset}"
run_verifier >/dev/null
write_ruleset active '[]' '["~DEFAULT_BRANCH"]'

# Rules only ever add restrictions, so an extra one (the live ruleset carries
# copilot_code_review) must not fail the audit; the 30/30 scheduled reds
# before this change were that exact-set comparison, not a policy gap.
jq '.rules += [{"type":"copilot_code_review","parameters":{"review_on_push":true}}]' \
	"${ruleset}" >"${tmp_dir}/ruleset-with-extra-rule.json"
mv "${tmp_dir}/ruleset-with-extra-rule.json" "${ruleset}"
run_verifier >/dev/null
write_ruleset active '[]' '["~DEFAULT_BRANCH"]'

# A queue is neither required nor forbidden: a ruleset and effective rules that
# still carry a merge_queue rule pass, whatever its timeout.
jq --argjson q "${merge_queue_rule}" '.rules += [$q]' \
	"${ruleset}" >"${tmp_dir}/ruleset-with-merge-queue.json"
mv "${tmp_dir}/ruleset-with-merge-queue.json" "${ruleset}"
jq --argjson q "${merge_queue_rule}" '. += [$q]' \
	"${effective}" >"${tmp_dir}/effective-with-merge-queue.json"
mv "${tmp_dir}/effective-with-merge-queue.json" "${effective}"
run_verifier >/dev/null
jq '(.rules[] | select(.type == "merge_queue").parameters.check_response_timeout_minutes) = 1' \
	"${ruleset}" >"${tmp_dir}/ruleset-with-short-queue-timeout.json"
mv "${tmp_dir}/ruleset-with-short-queue-timeout.json" "${ruleset}"
run_verifier >/dev/null
write_ruleset active '[]' '["~DEFAULT_BRANCH"]'
write_effective true '[{"context":"go-core-complete","integration_id":15368},{"context":"required-gates-complete","integration_id":15368}]'

jq '.rules |= map(select(.type != "required_status_checks"))' \
	"${ruleset}" >"${tmp_dir}/ruleset-without-required-status.json"
mv "${tmp_dir}/ruleset-without-required-status.json" "${ruleset}"
if run_verifier >/dev/null 2>&1; then
	echo "expected an owning ruleset without a required_status_checks rule to fail" >&2
	exit 1
fi
write_ruleset active '[]' '["~DEFAULT_BRANCH"]'

jq '.rules |= map(select(.type != "deletion"))' \
	"${ruleset}" >"${tmp_dir}/ruleset-without-deletion.json"
mv "${tmp_dir}/ruleset-without-deletion.json" "${ruleset}"
if run_verifier >/dev/null 2>&1; then
	echo "expected an owning ruleset without the deletion rule to fail" >&2
	exit 1
fi
write_ruleset active '[]' '["~DEFAULT_BRANCH"]'

write_effective true '[{"context":"go-core-complete","integration_id":15368}]'
if run_verifier >/dev/null 2>&1; then
	echo "expected a missing required context to fail" >&2
	exit 1
fi

write_effective true '[{"context":"go-core-complete","integration_id":15368},{"context":"required-gates-complete","integration_id":1}]'
if run_verifier >/dev/null 2>&1; then
	echo "expected a wrong integration id to fail" >&2
	exit 1
fi

write_effective false '[{"context":"go-core-complete","integration_id":15368},{"context":"required-gates-complete","integration_id":15368}]'
run_verifier >/dev/null

write_effective true '[{"context":"extra","integration_id":15368},{"context":"go-core-complete","integration_id":15368},{"context":"required-gates-complete","integration_id":15368}]'
if run_verifier >/dev/null 2>&1; then
	echo "expected an unregistered required context to fail" >&2
	exit 1
fi

write_effective true '[{"context":"go-core-complete","integration_id":15368},{"context":"required-gates-complete","integration_id":15368}]'
write_ruleset evaluate '[]' '["~DEFAULT_BRANCH"]'
if run_verifier >/dev/null 2>&1; then
	echo "expected a non-active owning ruleset to fail" >&2
	exit 1
fi

write_ruleset active '[{"actor_id":1,"actor_type":"RepositoryRole","bypass_mode":"always"}]' '["~DEFAULT_BRANCH"]'
if run_verifier >/dev/null 2>&1; then
	echo "expected an unapproved ruleset bypass actor to fail" >&2
	exit 1
fi

write_ruleset active '[]' '["refs/heads/main"]'
if run_verifier >/dev/null 2>&1; then
	echo "expected a non-default-branch ruleset condition to fail" >&2
	exit 1
fi

# #9089: the live ruleset requires only the two Go umbrellas; the aggregate
# publisher is a registry entry the ruleset does not require. These cases go
# through the real `ci-gates contexts --ruleset-only` path (no expected-JSON
# override) against a fixture registry.
write_fixture_registry() {
	local aggregate_ruleset_required="$1"
	cat >"${tmp_dir}/fixture-registry.yaml" <<YAML
version: v1
gates: []
required_status_checks:
  - context: go-core-complete
    workflow: test.yml
    job: go-core-complete
    integration_id: 15368
  - context: go-race-complete
    workflow: test.yml
    job: go-race-complete
    integration_id: 15368
  - context: required-gates-complete
    workflow: required-gates.yml
    job: aggregate
    source_workflow: Build Test
    integration_id: 15368
    aggregates_blocking_gates: true
    ruleset_required: ${aggregate_ruleset_required}
YAML
}

run_registry_verifier() {
	ESHU_CI_GATES_REGISTRY="${tmp_dir}/fixture-registry.yaml" \
		ESHU_EFFECTIVE_RULES_JSON="${effective}" \
		ESHU_RULESET_JSON="${ruleset}" \
		"${verifier}"
}

two_context_checks='[{"context":"go-core-complete","integration_id":15368},{"context":"go-race-complete","integration_id":15368}]'
write_ruleset active '[]' '["~DEFAULT_BRANCH"]'
jq --argjson checks "${two_context_checks}" \
	'(.rules[] | select(.type == "required_status_checks").parameters.required_status_checks) = $checks' \
	"${ruleset}" >"${tmp_dir}/ruleset-two-contexts.json"
mv "${tmp_dir}/ruleset-two-contexts.json" "${ruleset}"
write_effective true "${two_context_checks}"

write_fixture_registry false
run_registry_verifier >/dev/null

write_fixture_registry true
if run_registry_verifier >/dev/null 2>&1; then
	echo "expected a registry that ruleset-requires the aggregate to fail against a two-context ruleset" >&2
	exit 1
fi

echo "test-verify-live-required-status-checks: pass"
