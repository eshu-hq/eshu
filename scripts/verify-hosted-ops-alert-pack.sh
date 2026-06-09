#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
dashboard="${repo_root}/deploy/grafana/dashboards/eshu-hosted-operations.json"
alerts="${repo_root}/deploy/observability/hosted-operations-alerts.yaml"
prometheus_rule="${repo_root}/deploy/observability/hosted-operations-prometheus-rule.yaml"

required_alerts=(
	"EshuHostedMetricsMissing"
	"EshuHostedDeadLettersPresent"
	"EshuHostedQueueAgeSustained"
	"EshuHostedReadbackCompletenessDrift"
	"EshuHostedCollectorClaimStalled"
	"EshuHostedDependencyLatencyHigh"
	"EshuHostedRuntimeDependencyDegraded"
	"EshuHostedSchemaBootstrapFailed"
	"EshuHostedMCPToolErrors"
)

required_panels=(
	"Hosted Runtime Health"
	"Queue Terminal State"
	"Oldest Outstanding Work Age"
	"API and MCP Error Rate"
	"Dependency Latency p99"
	"Collector Claim Health"
	"Completeness Drift"
	"Schema Bootstrap Failure"
	"Debug Posture Checklist"
)

usage() {
	cat <<USAGE
Usage: $(basename "$0") [--dashboard <json>] [--alerts <yaml>] [--prometheus-rule <yaml>]

Validates the hosted operations dashboard, standalone Prometheus alert group,
PrometheusRule wrapper, and Helm ServiceMonitor render shape.
USAGE
}

die() {
	printf 'verify-hosted-ops-alert-pack: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--dashboard)
			dashboard="${2:-}"
			shift 2
			;;
		--alerts)
			alerts="${2:-}"
			shift 2
			;;
		--prometheus-rule)
			prometheus_rule="${2:-}"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			die "unknown option: $1"
			;;
	esac
done

command -v jq >/dev/null 2>&1 || die "jq is required"
command -v ruby >/dev/null 2>&1 || die "ruby is required"
command -v helm >/dev/null 2>&1 || die "helm is required"
command -v rg >/dev/null 2>&1 || die "rg is required"
[[ -f "${dashboard}" ]] || die "dashboard file not found: ${dashboard}"
[[ -f "${alerts}" ]] || die "standalone alert file not found: ${alerts}"
[[ -f "${prometheus_rule}" ]] || die "PrometheusRule file not found: ${prometheus_rule}"

jq -e . "${dashboard}" >/dev/null || die "dashboard must be valid JSON"

jq -e '
	(.title == "Eshu Hosted Operations") and
	(.uid == "eshu-hosted-operations") and
	((.panels // []) | type == "array" and length >= 9) and
	(any((.templating.list // [])[]?; .name == "service" and (.query | test("service_name"))))
' "${dashboard}" >/dev/null || die "dashboard must include panels, title, uid, and service variable"

for panel in "${required_panels[@]}"; do
	jq -e --arg panel "${panel}" 'any(.panels[]?; .title == $panel)' "${dashboard}" >/dev/null \
		|| die "dashboard missing required panel: ${panel}"
done

private_label_pattern='(repo(id|sitory)?|path|file|payload|token|delivery|work_item|account_id)[[:space:]]*(=|:|=~|!~)'

dashboard_exprs="$(jq -r '.. | objects | select(has("expr")) | .expr' "${dashboard}")"
if printf '%s\n' "${dashboard_exprs}" | rg --quiet "${private_label_pattern}"; then
	die "dashboard queries use high-cardinality or private-data-shaped labels"
fi

ruby -r yaml -e 'YAML.load_file(ARGV.fetch(0)); YAML.load_file(ARGV.fetch(1))' \
	"${alerts}" "${prometheus_rule}" >/dev/null \
	|| die "alert YAML files must parse"

standalone_alerts=()
while IFS= read -r alert; do
	standalone_alerts+=("${alert}")
done < <(ruby -r yaml -e '
	doc = YAML.load_file(ARGV.fetch(0))
	puts doc.fetch("groups").flat_map { |group| group.fetch("rules") }.map { |rule| rule.fetch("alert") }
' "${alerts}")
wrapped_alerts=()
while IFS= read -r alert; do
	wrapped_alerts+=("${alert}")
done < <(ruby -r yaml -e '
	doc = YAML.load_file(ARGV.fetch(0))
	puts doc.fetch("spec").fetch("groups").flat_map { |group| group.fetch("rules") }.map { |rule| rule.fetch("alert") }
' "${prometheus_rule}")

for alert in "${required_alerts[@]}"; do
	printf '%s\n' "${standalone_alerts[@]}" | rg --fixed-strings --quiet -- "${alert}" \
		|| die "missing required alert in standalone rules: ${alert}"
	printf '%s\n' "${wrapped_alerts[@]}" | rg --fixed-strings --quiet -- "${alert}" \
		|| die "missing required alert in PrometheusRule: ${alert}"
done

standalone_sorted="$(printf '%s\n' "${standalone_alerts[@]}" | sort)"
wrapped_sorted="$(printf '%s\n' "${wrapped_alerts[@]}" | sort)"
[[ "${standalone_sorted}" == "${wrapped_sorted}" ]] \
	|| die "standalone and PrometheusRule alert names must match"

ruby -r yaml -e '
	def rule_list(doc)
	  if doc.key?("spec")
	    doc.fetch("spec").fetch("groups").flat_map { |group| group.fetch("rules") }
	  else
	    doc.fetch("groups").flat_map { |group| group.fetch("rules") }
	  end
	end

	ARGV.each do |path|
	  rule_list(YAML.load_file(path)).each do |rule|
	    raise "#{rule["alert"]} missing expr" if rule["expr"].to_s.strip.empty?
	    raise "#{rule["alert"]} missing duration" if rule["for"].to_s.strip.empty?
	    annotations = rule.fetch("annotations")
	    raise "#{rule["alert"]}: every hosted alert needs a runbook annotation" if annotations["runbook"].to_s.strip.empty?
	    labels = rule.fetch("labels")
	    raise "#{rule["alert"]} missing severity" if labels["severity"].to_s.strip.empty?
	    raise "#{rule["alert"]} missing component" if labels["component"].to_s.strip.empty?
	    raise "#{rule["alert"]} missing runbook_section" if labels["runbook_section"].to_s.strip.empty?
	  end
	end
' "${alerts}" "${prometheus_rule}" >/dev/null \
	|| die "every hosted alert needs a runbook annotation, severity, component, and runbook_section"

if rg --quiet "${private_label_pattern}" "${alerts}" "${prometheus_rule}"; then
	die "alert pack contains high-cardinality or private-data-shaped label keys"
fi

rendered="$(mktemp)"
trap 'rm -f "${rendered}"' EXIT
helm template eshu "${repo_root}/deploy/helm/eshu" \
	--set observability.prometheus.enabled=true \
	--set observability.prometheus.serviceMonitor.enabled=true \
	>"${rendered}"

service_monitor_count="$(rg -c '^kind: ServiceMonitor$' "${rendered}" || true)"
if (( service_monitor_count < 4 )); then
	die "Helm render must include ServiceMonitor resources for hosted runtimes"
fi

for component in api mcp-server ingester resolution-engine; do
	rg --fixed-strings --quiet "app.kubernetes.io/component: ${component}" "${rendered}" \
		|| die "Helm ServiceMonitor render missing component: ${component}"
done

printf 'hosted ops alert pack verification passed\n'
