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

# The private-label guard had no negative case, and it was blind: it pipes the
# dashboard expressions into `rg` and asks "did anything match". With --quiet,
# rg exits on the first match while printf is still writing, printf takes
# SIGPIPE, and pipefail makes the pipeline non-zero -- so `if match; then die`
# read a REAL match as "no match" and passed. Measured blind at the live
# dashboard size (1433 bytes; macOS pipes buffer 512).
leaky_dashboard="${tmp_dir}/leaky-dashboard.json"
jq '.panels[0].targets[0].expr = "sum(rate(eshu_probe_total{path=\"/etc/secret\"}[5m]))"' \
	"${repo_root}/deploy/grafana/dashboards/eshu-hosted-operations.json" >"${leaky_dashboard}"
expect_fail leaky_dashboard "private-data-shaped labels" --dashboard "${leaky_dashboard}"

# `repo_id` is the label name this guard exists to keep out, and the pattern
# missed it: `repo(id|sitory)?` matches repo/repoid/repository, and the
# underscore in repo_id stops the match dead. Found while fixing the SIGPIPE
# blindness above -- the first fixture used repo_id and "passed" for this
# reason rather than the one under test.
repo_id_dashboard="${tmp_dir}/repo-id-dashboard.json"
jq '.panels[0].targets[0].expr = "sum(rate(eshu_probe_total{repo_id=\"leak\"}[5m]))"' \
	"${repo_root}/deploy/grafana/dashboards/eshu-hosted-operations.json" >"${repo_id_dashboard}"
expect_fail repo_id_dashboard "private-data-shaped labels" --dashboard "${repo_id_dashboard}"

bad_rule="${tmp_dir}/bad-prometheus-rule.yaml"
cp "${repo_root}/deploy/observability/hosted-operations-prometheus-rule.yaml" "${bad_rule}"
perl -0pi -e 's/runbook:/note:/' "${bad_rule}"
expect_fail bad_rule "every hosted alert needs a runbook annotation" --prometheus-rule "${bad_rule}"

printf 'hosted ops alert pack verifier tests passed\n'
