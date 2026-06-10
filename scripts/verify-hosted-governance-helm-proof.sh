#!/usr/bin/env bash
# Build a public-safe hosted governance Helm proof artifact.

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
chart="${repo_root}/deploy/helm/eshu"
release="eshu"
namespace="eshu"
out_dir="${repo_root}/hosted-governance-helm-proof"
allow_public_docs=false
values_files=()

usage() {
	cat <<USAGE
Usage: $(basename "$0") --values <values.yaml> [options]

Options:
  --out-dir PATH
  --chart PATH
  --release NAME
  --namespace NAME
  -f, --values FILE
  --allow-public-docs

The gate composes the hosted Helm rollout proof, hosted security posture proof,
and hosted NetworkPolicy egress proof. It writes public-safe JSON and Markdown
summaries only; raw manifests, private values, endpoints, tokens, DSNs,
hostnames, and source payloads stay operator-local.
USAGE
}

die() {
	printf 'verify-hosted-governance-helm-proof: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--out-dir)
			out_dir="${2:-}"
			shift 2
			;;
		--chart)
			chart="${2:-}"
			shift 2
			;;
		--release)
			release="${2:-}"
			shift 2
			;;
		--namespace)
			namespace="${2:-}"
			shift 2
			;;
		-f|--values)
			values_files+=("${2:-}")
			shift 2
			;;
		--allow-public-docs)
			allow_public_docs=true
			shift
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

[[ -d "${chart}" ]] || die "chart directory not found: ${chart}"
(( ${#values_files[@]} > 0 )) || die "at least one --values file is required for hosted governance Helm proof"
for file in "${values_files[@]}"; do
	[[ -n "${file}" ]] || die "--values requires a file"
	[[ -f "${file}" ]] || die "values file not found: ${file}"
done

command -v helm >/dev/null 2>&1 || die "helm is required"
command -v jq >/dev/null 2>&1 || die "jq is required"
command -v ruby >/dev/null 2>&1 || die "ruby is required"
command -v rg >/dev/null 2>&1 || die "rg is required"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
mkdir -p "${out_dir}"

helm_values_args=()
for file in "${values_files[@]}"; do
	helm_values_args+=("-f" "${file}")
done

egress_mode="$(
	ruby -r yaml -e '
mode = "broad"
ARGV.each do |path|
  doc = YAML.load_file(path) || {}
  next unless doc.is_a?(Hash)
  value = doc.dig("networkPolicy", "egress", "mode")
  mode = value if value
end
puts mode
' "${values_files[@]}"
)"
[[ "${egress_mode}" == "restricted" ]] || die "networkPolicy.egress.mode must be restricted for hosted governance Helm proof"

rendered="${tmp_dir}/rendered.yaml"
helm template "${release}" "${chart}" --namespace "${namespace}" "${helm_values_args[@]}" >"${rendered}"

governance_env_json="${tmp_dir}/governance-env.json"
ruby -r yaml -r json -e '
manifest = ARGV.fetch(0)
docs = YAML.load_stream(File.read(manifest)).compact
required = {
  "ESHU_GOVERNANCE_MODE" => /\Ahosted_(single|multi)_tenant\z/,
  "ESHU_GOVERNANCE_STATE" => /\Aenforcing\z/,
  "ESHU_GOVERNANCE_SOURCE_KIND" => /\A(environment|kubernetes_secret|config_map|postgres_revision)\z/,
  "ESHU_GOVERNANCE_AUTH_MODE" => /\Ashared_token\z/,
  "ESHU_GOVERNANCE_EGRESS_MODE" => /\Arestricted\z/
}

def component(doc)
  labels = doc.dig("metadata", "labels") || {}
  labels["app.kubernetes.io/component"].to_s
end

def workload_name(doc)
  doc.dig("metadata", "name").to_s
end

def env_map(doc)
  containers = Array(doc.dig("spec", "template", "spec", "containers"))
  containers.each_with_object({}) do |container, acc|
    Array(container["env"]).each do |env|
      acc[env["name"]] = env["value"] if env.key?("value")
    end
  end
end

targets = {
  "api" => docs.find { |doc| doc["kind"] == "Deployment" && (component(doc) == "api" || workload_name(doc).end_with?("-api")) },
  "mcp" => docs.find { |doc| doc["kind"] == "Deployment" && (component(doc) == "mcp-server" || workload_name(doc).end_with?("-mcp-server")) }
}
failures = []
summary = {}
targets.each do |name, doc|
  unless doc
    failures << "#{name} workload not rendered"
    summary[name] = "missing"
    next
  end
  env = env_map(doc)
  missing = required.keys.select { |key| !env.key?(key) }
  invalid = required.select { |key, pattern| env.key?(key) && !pattern.match?(env[key].to_s) }.keys
  failures << "#{name} missing governance env: #{missing.join(", ")}" unless missing.empty?
  failures << "#{name} invalid governance env: #{invalid.join(", ")}" unless invalid.empty?
  summary[name] = missing.empty? && invalid.empty? ? "pass" : "fail"
end

unless failures.empty?
  warn failures.join("\n")
  exit 1
end

puts JSON.generate({
  "api" => summary.fetch("api"),
  "mcp" => summary.fetch("mcp"),
  "required_keys" => required.keys
})
' "${rendered}" >"${governance_env_json}" || die "governance status env proof failed"

rollout_dir="${tmp_dir}/rollout-proof"
rollout_stdout="${tmp_dir}/rollout.out"
rollout_stderr="${tmp_dir}/rollout.err"
if ! bash "${repo_root}/scripts/verify-hosted-helm-rollout-proof.sh" \
	--out-dir "${rollout_dir}" \
	--chart "${chart}" \
	--release "${release}" \
	--namespace "${namespace}" \
	"${helm_values_args[@]}" >"${rollout_stdout}" 2>"${rollout_stderr}"; then
	sed -n '1,120p' "${rollout_stderr}" >&2
	die "hosted Helm rollout proof failed"
fi

security_args=(--chart "${chart}")
if [[ "${allow_public_docs}" == "true" ]]; then
	security_args+=(--allow-public-docs)
fi
for file in "${values_files[@]}"; do
	security_args+=(--values "${file}")
done
security_stdout="${tmp_dir}/security.out"
security_stderr="${tmp_dir}/security.err"
if ! bash "${repo_root}/scripts/verify-hosted-security-posture.sh" \
	"${security_args[@]}" >"${security_stdout}" 2>"${security_stderr}"; then
	sed -n '1,120p' "${security_stderr}" >&2
	die "hosted security posture proof failed"
fi

egress_args=(--chart "${chart}")
for file in "${values_files[@]}"; do
	egress_args+=(--values "${file}")
done
egress_stdout="${tmp_dir}/egress.out"
egress_stderr="${tmp_dir}/egress.err"
if ! bash "${repo_root}/scripts/verify-hosted-network-policy-egress.sh" \
	"${egress_args[@]}" >"${egress_stdout}" 2>"${egress_stderr}"; then
	sed -n '1,120p' "${egress_stderr}" >&2
	die "hosted NetworkPolicy egress proof failed"
fi

rollout_artifact="${rollout_dir}/hosted-helm-rollout-proof.json"
[[ -f "${rollout_artifact}" ]] || die "hosted Helm rollout proof artifact missing"

artifact_tmp="${out_dir}/hosted-governance-helm-proof.json.tmp"
summary_tmp="${out_dir}/hosted-governance-helm-proof.md.tmp"

jq -n \
	--arg generated_at "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
	--arg release "${release}" \
	--arg namespace "${namespace}" \
	--slurpfile rollout "${rollout_artifact}" \
	--slurpfile governance_env "${governance_env_json}" \
	'{
		status: "pass",
		generated_at: $generated_at,
		release: $release,
		namespace: $namespace,
		chart: $rollout[0].chart,
		image: {repository: $rollout[0].image.repository, tag: $rollout[0].image.tag},
		values_digest: $rollout[0].install.values_digest,
		helm_rollout_status: "pass",
		security_posture_status: "pass",
		network_policy_status: "pass",
		governance_status_env: $governance_env[0],
		install: {
			required_workloads_present: $rollout[0].install.required_workloads_present,
			rendered_workload_count: $rollout[0].install.rendered_workload_count,
			schema_bootstrap: $rollout[0].install.schema_bootstrap
		},
		readback: {
			api_health: $rollout[0].readback.api_health,
			mcp_health: $rollout[0].readback.mcp_health,
			first_query_status: $rollout[0].readback.first_query.status
		},
		public_artifact_review: "pass"
	}' >"${artifact_tmp}"

if jq -r '.. | strings' "${artifact_tmp}" | rg --quiet --ignore-case \
	'ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|bolt://|postgres(ql)?://|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}'; then
	die "public artifact review failed; summary contains private-shaped data"
fi

jq -r '
	[
		"# Hosted governance Helm proof",
		"",
		"- Status: pass",
		"- Release: \(.release)",
		"- Namespace: \(.namespace)",
		"- Chart version: \(.chart.version)",
		"- App version: \(.chart.app_version)",
		"- Image: \(.image.repository):\(.image.tag)",
		"- Values digest: \(.values_digest)",
		"- Helm rollout proof: \(.helm_rollout_status)",
		"- Hosted security posture proof: \(.security_posture_status)",
		"- NetworkPolicy egress proof: \(.network_policy_status)",
		"- Governance status env: api=\(.governance_status_env.api), mcp=\(.governance_status_env.mcp)",
		"",
		"Raw manifests, private values, endpoints, tokens, DSNs, hostnames, source payloads, and cluster-specific details remain operator-local."
	] | .[]
' "${artifact_tmp}" >"${summary_tmp}"

mv "${artifact_tmp}" "${out_dir}/hosted-governance-helm-proof.json"
mv "${summary_tmp}" "${out_dir}/hosted-governance-helm-proof.md"
printf 'verify-hosted-governance-helm-proof: wrote %s\n' "${out_dir}/hosted-governance-helm-proof.json"
