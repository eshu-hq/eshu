#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
chart="${repo_root}/deploy/helm/eshu"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

usage() {
	cat <<'USAGE'
Usage: scripts/verify-hosted-network-policy-egress.sh [options]

Options:
  -f, --values PATH  Render the chart with an operator values file.
      --chart PATH   Helm chart path. Defaults to deploy/helm/eshu.
  -h, --help         Show this help.
USAGE
}

die() {
	printf 'verify-hosted-network-policy-egress: %s\n' "$*" >&2
	exit 1
}

values_files=()
while [[ $# -gt 0 ]]; do
	case "$1" in
		-f|--values)
			[[ $# -ge 2 ]] || die "$1 requires a path"
			values_files+=("$2")
			shift 2
			;;
		--chart)
			[[ $# -ge 2 ]] || die "$1 requires a path"
			chart="$2"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			die "unknown argument: $1"
			;;
	esac
done

command -v helm >/dev/null 2>&1 || die "helm is required"
command -v ruby >/dev/null 2>&1 || die "ruby is required"

render_chart() {
	local output="$1"
	shift
	local helm_cmd=(helm template eshu "${chart}")
	while [[ $# -gt 0 ]]; do
		helm_cmd+=(-f "$1")
		shift
	done
	"${helm_cmd[@]}" >"${output}"
}

values_mode() {
	ruby -r yaml -e '
mode = "broad"
ARGV.each do |path|
  doc = YAML.load_file(path) || {}
  next unless doc.is_a?(Hash)
  egress = doc.dig("networkPolicy", "egress")
  next unless egress.is_a?(Hash) && egress.key?("mode")
  mode = egress["mode"]
end
unless ["broad", "restricted"].include?(mode)
  warn "networkPolicy.egress.mode must be broad or restricted"
  exit 2
end
puts mode
' "$@"
}

validate_manifest() {
	local manifest="$1"
	local mode="$2"
	local expectation="${3:-}"
	ruby -r yaml -e '
manifest_path, mode, expectation = ARGV
docs = YAML.load_stream(File.read(manifest_path)).compact
policies = docs.select { |doc| doc.is_a?(Hash) && doc["kind"] == "NetworkPolicy" }
abort "no NetworkPolicy resources rendered" if policies.empty?

def component(policy)
  labels = policy.dig("metadata", "labels") || {}
  labels["app.kubernetes.io/component"]
end

def unrestricted?(policy)
  Array(policy.dig("spec", "egress")).any? { |rule| rule == {} }
end

def marker?(policy, value)
  Array(policy.dig("spec", "egress")).any? do |rule|
    Array(rule["to"]).any? do |peer|
      ["namespaceSelector", "podSelector"].any? do |selector|
        labels = peer.dig(selector, "matchLabels") || {}
        labels["egress.eshu.io/class"] == value
      end
    end
  end
end

if mode == "broad"
  abort "broad egress mode must render an unrestricted egress rule" unless policies.any? { |policy| unrestricted?(policy) }
  puts "broad egress mode is a hosted governance risk; set networkPolicy.egress.mode=restricted for enforced least privilege"
  exit 0
end

offenders = policies.select { |policy| unrestricted?(policy) }.map { |policy| component(policy) || policy.dig("metadata", "name") }
abort "restricted egress rendered unrestricted policies: #{offenders.join(", ")}" unless offenders.empty?

empty = policies.select { |policy| Array(policy.dig("spec", "egress")).empty? }.map { |policy| component(policy) || policy.dig("metadata", "name") }
abort "restricted egress rendered empty egress for: #{empty.join(", ")}" unless empty.empty?

case expectation
when "collector-provider"
  collector = policies.find { |policy| component(policy) == "confluence-collector" }
  abort "collector-provider case did not render confluence-collector" unless collector
  abort "collector-provider egress missing from confluence-collector" unless marker?(collector, "collector-provider")
  api = policies.find { |policy| component(policy) == "api" }
  abort "collector-provider egress leaked onto api" if api && marker?(api, "collector-provider")
  puts "verified restricted collector-provider egress"
when "semantic-provider"
  ["api", "mcp-server", "resolution-engine"].each do |name|
    policy = policies.find { |candidate| component(candidate) == name }
    abort "semantic-provider case did not render #{name}" unless policy
    abort "semantic-provider egress missing from #{name}" unless marker?(policy, "semantic-provider")
  end
  puts "verified restricted semantic-provider egress"
when "extension"
  policy = policies.find { |candidate| component(candidate) == "workflow-coordinator" }
  abort "extension case did not render workflow-coordinator" unless policy
  abort "extension egress missing from workflow-coordinator" unless marker?(policy, "extension")
  puts "verified restricted extension egress"
else
  puts "verified restricted NetworkPolicy egress"
end
' "${manifest}" "${mode}" "${expectation}"
}

run_values_case() {
	local name="$1"
	local mode="$2"
	local expectation="$3"
	local values="$4"
	local manifest="${tmp_dir}/${name}-render.yaml"
	render_chart "${manifest}" "${values}"
	validate_manifest "${manifest}" "${mode}" "${expectation}"
}

if [[ ${#values_files[@]} -gt 0 ]]; then
	mode="$(values_mode "${values_files[@]}")"
	manifest="${tmp_dir}/operator.yaml"
	render_chart "${manifest}" "${values_files[@]}"
	validate_manifest "${manifest}" "${mode}"
	exit 0
fi

default_manifest="${tmp_dir}/default.yaml"
render_chart "${default_manifest}"
validate_manifest "${default_manifest}" "broad"

broad_values="${tmp_dir}/broad.yaml"
cat >"${broad_values}" <<'YAML'
networkPolicy:
  egress:
    mode: broad
YAML
run_values_case "broad-explicit" "broad" "" "${broad_values}"

restricted_values="${tmp_dir}/restricted.yaml"
cat >"${restricted_values}" <<'YAML'
schemaBootstrap:
  useHelmHooks: false
nornicdb:
  enabled: true
networkPolicy:
  egress:
    mode: restricted
    datastores:
      to:
        - podSelector:
            matchLabels:
              egress.eshu.io/class: datastore
YAML
run_values_case "restricted" "restricted" "" "${restricted_values}"

collector_values="${tmp_dir}/collector.yaml"
cat >"${collector_values}" <<'YAML'
confluenceCollector:
  enabled: true
  baseUrl: https://confluence.example.com
  spaceId: DOCS
  credentials:
    secretName: confluence-credentials
    bearerTokenKey: token
networkPolicy:
  egress:
    mode: restricted
    classes:
      collectorProviders:
        to:
          - namespaceSelector:
              matchLabels:
                egress.eshu.io/class: collector-provider
YAML
run_values_case "collector" "restricted" "collector-provider" "${collector_values}"

semantic_values="${tmp_dir}/semantic.yaml"
cat >"${semantic_values}" <<'YAML'
networkPolicy:
  egress:
    mode: restricted
    classes:
      semanticProviders:
        to:
          - namespaceSelector:
              matchLabels:
                egress.eshu.io/class: semantic-provider
YAML
run_values_case "semantic" "restricted" "semantic-provider" "${semantic_values}"

extension_values="${tmp_dir}/extension.yaml"
cat >"${extension_values}" <<'YAML'
workflowCoordinator:
  enabled: true
networkPolicy:
  egress:
    mode: restricted
    classes:
      extensions:
        to:
          - namespaceSelector:
              matchLabels:
                egress.eshu.io/class: extension
YAML
run_values_case "extension" "restricted" "extension" "${extension_values}"
