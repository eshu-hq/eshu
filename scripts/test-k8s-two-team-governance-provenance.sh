#!/usr/bin/env bash
set -euo pipefail

# Behavioral tests for the Kubernetes governance provenance capture helper.
# The harness fakes cluster reads but exercises the production shell function.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${repo_root}/scripts/lib/k8s-two-team-governance-provenance.sh"
driver="${repo_root}/scripts/run-k8s-two-team-governance-proof.sh"
expected_image="timothyswt/nornicdb-cpu-bge:v1.3.1@sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962"
expected_image_id="docker-pullable://timothyswt/nornicdb-cpu-bge@sha256:c0b5f73c55bd56a6764d1833665252b98a30b332248f0233f5f4eab4dc0d2ca1"

die() {
	printf 'test-k8s-two-team-governance-provenance: %s\n' "$*" >&2
	exit 1
}

[[ -f "${helper}" ]] || die "missing helper: ${helper}"
rg --quiet --fixed-strings 'command -v jq >/dev/null 2>&1 || die "jq is required"' "${driver}" \
	|| die "live driver does not require jq before provenance capture"
# shellcheck source=scripts/lib/k8s-two-team-governance-provenance.sh
. "${helper}"

tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

run_capture() (
	set -euo pipefail
	local configured_image="$1"
	local artifacts_dir="$2"
	local requested_image="${3:-${expected_image}}"
	local fail_publish="${4:-false}"

	die() {
		printf 'fake cluster: %s\n' "$*" >&2
		exit 1
	}
	kc() {
		local joined="$*"
		case "${joined}" in
			'get pods -l app.kubernetes.io/component=nornicdb '*) printf 'nornicdb-pod' ;;
			*'.spec.nodeName}'*) printf 'worker-a' ;;
			*'.spec.containers[?(@.name=="nornicdb")].image}'*) printf '%s' "${configured_image}" ;;
			*'.status.containerStatuses[?(@.name=="nornicdb")].imageID}'*) printf '%s' "${expected_image_id}" ;;
			'exec nornicdb-pod -c nornicdb -- /app/nornicdb version') printf 'NornicDB v1.3.1\n' ;;
			*) return 1 ;;
		esac
	}
	kubectl() {
		local joined="$*"
		case "${joined}" in
			'get nodes '*) printf 'v1.34.8' ;;
			'get node worker-a '*) printf 'amd64' ;;
			*) return 1 ;;
		esac
	}
	mv() {
		if [[ "${fail_publish}" == true ]]; then
			return 1
		fi
		command mv "$@"
	}

	mkdir -p "${artifacts_dir}"
	capture_k8s_governance_provenance "${repo_root}" "${artifacts_dir}" "${requested_image}"
)

good_dir="${tmp_root}/good"
run_capture "${expected_image}" "${good_dir}" || die "matching Pod image was rejected"
rg --fixed-strings --quiet "\"backend_image\": \"${expected_image}\"" "${good_dir}/provenance.json" \
	|| die "provenance did not record the observed Pod image"

escaped_image=$'registry.example/nornicdb:quote"-slash\\-line\ntwo'
escaped_dir="${tmp_root}/escaped"
run_capture "${escaped_image}" "${escaped_dir}" "${escaped_image}" \
	|| die "capture rejected an escapable configured image"
jq -e --arg expected "${escaped_image}" '.backend_image == $expected' \
	"${escaped_dir}/provenance.json" >/dev/null \
	|| die "provenance did not JSON-escape the configured image"

publish_failure_dir="${tmp_root}/publish-failure"
mkdir -p "${publish_failure_dir}"
printf '%s\n' sentinel >"${publish_failure_dir}/provenance.json"
if run_capture "${expected_image}" "${publish_failure_dir}" "${expected_image}" true \
	>/dev/null 2>&1; then
	die "capture accepted a failed atomic publish"
fi
[[ "$(cat "${publish_failure_dir}/provenance.json")" == sentinel ]] \
	|| die "failed publish replaced the existing provenance artifact"
shopt -s nullglob
publish_temps=("${publish_failure_dir}"/.provenance.*)
shopt -u nullglob
((${#publish_temps[@]} == 0)) || die "failed publish left a hidden provenance temp file"

for case_name in tag-only wrong-digest missing-container; do
	case "${case_name}" in
		tag-only) configured_image='timothyswt/nornicdb-cpu-bge:v1.3.1' ;;
		wrong-digest) configured_image='timothyswt/nornicdb-cpu-bge:v1.3.1@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' ;;
		missing-container) configured_image='' ;;
	esac
	case_dir="${tmp_root}/${case_name}"
	if run_capture "${configured_image}" "${case_dir}" >/dev/null 2>&1; then
		die "capture accepted ${case_name} Pod image"
	fi
	[[ ! -f "${case_dir}/provenance.json" ]] \
		|| die "capture wrote accepted provenance for ${case_name} Pod image"
done

printf 'Kubernetes governance provenance capture self-test passed\n'
