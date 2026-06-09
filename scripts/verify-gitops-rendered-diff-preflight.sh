#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
CHART="${REPO_ROOT}/deploy/helm/eshu"

overlay="deploy/argocd/base"
extra_values=()

usage() {
	cat <<'USAGE'
Usage: scripts/verify-gitops-rendered-diff-preflight.sh [--overlay PATH] [--values FILE ...]

Renders the Eshu Helm chart with Argo CD value files and fails before GitOps
sync when the rendered shape contains placeholder values, unpinned images, or
chart-invalid runtime combinations. Output is an operator summary and does not
print raw DSNs, bearer tokens, or provider credentials.
USAGE
}

die() {
	printf 'gitops-rendered-diff-preflight: %s\n' "$*" >&2
	exit 1
}

require_tool() {
	local tool="$1"
	command -v "${tool}" >/dev/null 2>&1 || die "${tool} is required"
}

relative_path() {
	local path="$1"
	case "${path}" in
		"${REPO_ROOT}"/*) printf '%s\n' "${path#"${REPO_ROOT}/"}" ;;
		*) printf '%s\n' "${path}" ;;
	esac
}

add_values_arg() {
	local file="$1"
	[[ -f "${file}" ]] || die "values file not found: $(relative_path "${file}")"
	values_files+=("${file}")
}

resolve_overlay_values() {
	local overlay_path="$1"
	local absolute="${overlay_path}"
	if [[ "${absolute}" != /* ]]; then
		absolute="${REPO_ROOT}/${overlay_path}"
	fi
	[[ -d "${absolute}" ]] || die "overlay directory not found: $(relative_path "${absolute}")"

	case "$(relative_path "${absolute}")" in
		deploy/argocd/base)
			add_values_arg "${absolute}/values.yaml"
			;;
		deploy/argocd/overlays/*)
			add_values_arg "${REPO_ROOT}/deploy/argocd/base/values.yaml"
			add_values_arg "${absolute}/values.yaml"
			;;
		*)
			add_values_arg "${absolute}/values.yaml"
			;;
	esac
}

while (($# > 0)); do
	case "$1" in
		--overlay)
			(($# >= 2)) || die "--overlay requires a path"
			overlay="$2"
			shift 2
			;;
		--values|-f)
			(($# >= 2)) || die "$1 requires a values file"
			extra_values+=("$2")
			shift 2
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			die "unknown argument: $1"
			;;
	esac
done

require_tool helm
require_tool rg

values_files=()
resolve_overlay_values "${overlay}"
if ((${#extra_values[@]} > 0)); then
	for value_file in "${extra_values[@]}"; do
		if [[ "${value_file}" != /* ]]; then
			value_file="${REPO_ROOT}/${value_file}"
		fi
		add_values_arg "${value_file}"
	done
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

rendered="${tmp_dir}/rendered.yaml"
render_err="${tmp_dir}/render.err"

helm_args=(template eshu "${CHART}" --namespace eshu)
for value_file in "${values_files[@]}"; do
	helm_args+=(-f "${value_file}")
done

if ! helm "${helm_args[@]}" >"${rendered}" 2>"${render_err}"; then
	cat "${render_err}" >&2
	exit 1
fi

fail_if_rendered() {
	local pattern="$1" message="$2"
	if rg -q "${pattern}" "${rendered}"; then
		die "${message}"
	fi
}

fail_if_rendered 'replace-me' 'placeholder value rendered: replace-me'
fail_if_rendered 'your-[a-z0-9-]+' 'placeholder value rendered: your-*'
fail_if_rendered 'example\.com' 'placeholder value rendered: example.com'
fail_if_rendered '123456789012' 'placeholder value rendered: example account id'
fail_if_rendered '<[^>]+>' 'placeholder value rendered: angle-bracket placeholder'

if rg -q 'image: .+:(latest|main|master|dev)(@|["[:space:]]*$)' "${rendered}"; then
	if rg -q 'image: .+:latest(@|["[:space:]]*$)' "${rendered}"; then
		die "unpinned image tag latest"
	fi
	die "unpinned image tag"
fi

resources="${tmp_dir}/resources.txt"
awk '
	/^kind: / { kind=$2; next }
	/^metadata:/ { in_metadata=1; next }
	in_metadata && /^  name: / {
		name=$2
		gsub(/"/, "", name)
		if (kind != "" && name != "") {
			print kind "/" name
		}
		kind=""
		in_metadata=0
	}
' "${rendered}" | sort -u >"${resources}"

require_resource() {
	local resource="$1"
	if ! rg -q "^${resource}$" "${resources}"; then
		die "rendered resource missing: ${resource}"
	fi
}

require_resource 'Deployment/eshu-api'
require_resource 'Deployment/eshu-mcp-server'
require_resource 'StatefulSet/eshu'
require_resource 'Deployment/eshu-resolution-engine'
require_resource 'Job/eshu-schema-bootstrap'

if rg -q 'kind: ServiceMonitor' "${rendered}"; then
	service_monitor_state="configured"
else
	service_monitor_state="not_configured"
fi

if rg -q 'name: ESHU_POSTGRES_DSN' "${rendered}"; then
	postgres_state="configured"
else
	postgres_state="missing"
fi

if rg -q 'name: NEO4J_URI' "${rendered}"; then
	graph_state="configured"
else
	graph_state="missing"
fi

if [[ "${postgres_state}" != "configured" ]]; then
	die "rendered workload missing Postgres DSN environment wiring"
fi
if [[ "${graph_state}" != "configured" ]]; then
	die "rendered workload missing graph URI environment wiring"
fi

chart_version="$(awk '/^version:/ {print $2; exit}' "${CHART}/Chart.yaml")"
app_version="$(awk '/^appVersion:/ {gsub(/"/, "", $2); print $2; exit}' "${CHART}/Chart.yaml")"

image_refs="${tmp_dir}/images.txt"
rg 'image: ' "${rendered}" \
	| sed -E 's/^[[:space:]]*image:[[:space:]]*"?([^"]+)"?/\1/' \
	| sort -u >"${image_refs}"

printf 'gitops rendered-diff preflight passed\n'
printf 'overlay=%s\n' "$(relative_path "${overlay}")"
printf 'values_files:\n'
for value_file in "${values_files[@]}"; do
	printf '  - %s\n' "$(relative_path "${value_file}")"
done
printf 'chart_version=%s\n' "${chart_version}"
printf 'app_version=%s\n' "${app_version}"
printf 'external_dependencies:\n'
printf '  postgres=%s\n' "${postgres_state}"
printf '  graph=%s\n' "${graph_state}"
printf '  service_monitors=%s\n' "${service_monitor_state}"
printf 'image_refs:\n'
while IFS= read -r image_ref; do
	[[ -n "${image_ref}" ]] || continue
	printf '  - %s\n' "${image_ref}"
done <"${image_refs}"
printf 'resources:\n'
while IFS= read -r resource; do
	[[ -n "${resource}" ]] || continue
	printf '  - %s\n' "${resource}"
done <"${resources}"
printf 'next_steps:\n'
printf '  - review the rendered resource set before Argo CD sync\n'
printf '  - verify API and MCP health, /admin/status, and /api/v0/index-status after sync\n'
