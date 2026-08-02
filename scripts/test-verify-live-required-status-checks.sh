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
	printf '{"id":19745843,"name":"main protection","target":"branch","enforcement":"%s","bypass_actors":%s,"conditions":{"ref_name":{"include":%s,"exclude":[]}},"rules":[{"type":"deletion"},{"type":"non_fast_forward"},{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":true,"required_status_checks":[{"context":"go-core-complete","integration_id":15368},{"context":"required-gates-complete","integration_id":15368}]}}]}\n' \
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

jq '(.rules[] | select(.type == "required_status_checks").parameters.strict_required_status_checks_policy) = false' \
	"${ruleset}" >"${tmp_dir}/ruleset-with-non-strict-owner.json"
mv "${tmp_dir}/ruleset-with-non-strict-owner.json" "${ruleset}"
if run_verifier >/dev/null 2>&1; then
	echo "expected a non-strict owning required-status rule to fail" >&2
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
if run_verifier >/dev/null 2>&1; then
	echo "expected a non-strict required-status rule to fail" >&2
	exit 1
fi

write_effective true '[{"context":"extra","integration_id":15368},{"context":"go-core-complete","integration_id":15368},{"context":"required-gates-complete","integration_id":15368}]'
if run_verifier >/dev/null 2>&1; then
	echo "expected an unregistered required context to fail" >&2
	exit 1
fi

write_effective true '[{"context":"go-core-complete","integration_id":15368},{"context":"required-gates-complete","integration_id":15368}]'
jq '. + [{"type":"merge_queue","parameters":{}}]' "${effective}" >"${tmp_dir}/merge-queue.json"
mv "${tmp_dir}/merge-queue.json" "${effective}"
if run_verifier >/dev/null 2>&1; then
	echo "expected an unsupported merge-queue rule to fail" >&2
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

echo "test-verify-live-required-status-checks: pass"
