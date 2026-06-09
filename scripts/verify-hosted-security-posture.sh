#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
chart="${repo_root}/deploy/helm/eshu"
allow_public_docs=false
values_args=()

usage() {
	cat <<USAGE
Usage: $(basename "$0") [--values <values.yaml>] [--chart <chart-dir>] [--allow-public-docs]

Renders the Eshu Helm chart and verifies hosted API/MCP auth, secret, pprof,
and public-docs posture without printing secret values.
USAGE
}

die() {
	printf 'verify-hosted-security-posture: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--values|-f)
			[[ $# -ge 2 ]] || die "--values requires a file"
			[[ -f "$2" ]] || die "values file not found: $2"
			values_args+=("-f" "$2")
			shift 2
			;;
		--chart)
			[[ $# -ge 2 ]] || die "--chart requires a directory"
			chart="$2"
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

command -v helm >/dev/null 2>&1 || die "helm is required"
command -v ruby >/dev/null 2>&1 || die "ruby is required"
command -v rg >/dev/null 2>&1 || die "rg is required"
[[ -d "${chart}" ]] || die "chart directory not found: ${chart}"

rendered="$(mktemp)"
trap 'rm -f "${rendered}"' EXIT

helm_cmd=(helm template eshu "${chart}")
if (( ${#values_args[@]} > 0 )); then
	helm_cmd+=("${values_args[@]}")
fi
"${helm_cmd[@]}" >"${rendered}"

if ! rg --quiet '^kind: (Deployment|StatefulSet|Job)$' "${rendered}"; then
	die "Helm render must include hosted workloads"
fi

ruby -r yaml -e '
  allow_public_docs = ARGV.fetch(0) == "true"
  manifest_path = ARGV.fetch(1)
  docs = YAML.load_stream(File.read(manifest_path)).compact
  failures = []

  credential_name = /
    (API_KEY|TOKEN|PASSWORD|PRIVATE_KEY|SECRET|CREDENTIAL|DSN|EMAIL|USERNAME)
  /x

  public_pprof = lambda do |value|
    text = value.to_s.strip
    text.start_with?("0.0.0.0:", "[::]:", "::") || text.start_with?(":")
  end

  docs.each do |doc|
    kind = doc["kind"]
    next unless ["Deployment", "StatefulSet", "Job"].include?(kind)

    metadata = doc.fetch("metadata", {})
    resource = "#{kind}/#{metadata.fetch("name", "unknown")}"
    spec = doc.fetch("spec", {})
    pod_spec = if kind == "Job"
      spec.fetch("template", {}).fetch("spec", {})
    else
      spec.fetch("template", {}).fetch("spec", {})
    end

    containers = []
    containers.concat(pod_spec.fetch("containers", []) || [])
    containers.concat(pod_spec.fetch("initContainers", []) || [])

    containers.each do |container|
      container.fetch("env", []).each do |env|
        name = env.fetch("name", "")
        value = env["value"]
        value_from = env["valueFrom"] || {}
        secret_ref = value_from["secretKeyRef"]

        if name == "ESHU_ENABLE_PUBLIC_DOCS" && value.to_s == "true" && !allow_public_docs
          failures << "#{resource} public API docs require explicit verifier opt-in"
        end

        if name == "ESHU_PPROF_ADDR" && value && public_pprof.call(value)
          failures << "#{resource} pprof must not bind publicly"
        end

        next unless name.match?(credential_name)

        if secret_ref
          if secret_ref["name"].to_s.strip.empty? || secret_ref["key"].to_s.strip.empty?
            if name == "ESHU_API_KEY"
              failures << "#{resource} missing API auth secret"
            else
              failures << "#{resource} credential secretKeyRef name must not be empty for #{name}"
            end
          end
        elsif value
          failures << "#{resource} credential env vars must use secretKeyRef for #{name}"
        else
          failures << "#{resource} credential env var must use secretKeyRef for #{name}"
        end
      end
    end
  end

  unless failures.empty?
    warn failures.uniq.join("\n")
    exit 1
  end
' "${allow_public_docs}" "${rendered}" \
	|| die "hosted security posture check failed"

printf 'hosted security posture verification passed\n'
