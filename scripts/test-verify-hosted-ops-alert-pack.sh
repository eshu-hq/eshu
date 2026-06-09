#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-ops-alert-pack.sh"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

expect_pass() {
	if ! "${verifier}" >"${tmp_dir}/pass.out" 2>"${tmp_dir}/pass.err"; then
		printf 'expected hosted ops alert pack verifier to pass\n' >&2
		sed -n '1,160p' "${tmp_dir}/pass.err" >&2
		exit 1
	fi
}

expect_fail() {
	local label="$1"
	local expected="$2"
	shift 2
	if "${verifier}" "$@" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	rg --fixed-strings --quiet -- "${expected}" "${tmp_dir}/${label}.err" \
		|| { printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2; sed -n '1,160p' "${tmp_dir}/${label}.err" >&2; exit 1; }
}

expect_pass

bad_dashboard="${tmp_dir}/bad-dashboard.json"
jq 'del(.panels)' "${repo_root}/deploy/grafana/dashboards/eshu-hosted-operations.json" >"${bad_dashboard}"
expect_fail bad_dashboard "dashboard must include panels" --dashboard "${bad_dashboard}"

bad_alerts="${tmp_dir}/bad-alerts.yaml"
cp "${repo_root}/deploy/observability/hosted-operations-alerts.yaml" "${bad_alerts}"
perl -0pi -e 's/EshuHostedDeadLettersPresent/EshuHostedDeadLettersRenamed/' "${bad_alerts}"
expect_fail bad_alerts "missing required alert" --alerts "${bad_alerts}"

bad_rule="${tmp_dir}/bad-prometheus-rule.yaml"
cp "${repo_root}/deploy/observability/hosted-operations-prometheus-rule.yaml" "${bad_rule}"
perl -0pi -e 's/runbook:/note:/' "${bad_rule}"
expect_fail bad_rule "every hosted alert needs a runbook annotation" --prometheus-rule "${bad_rule}"

printf 'hosted ops alert pack verifier tests passed\n'
