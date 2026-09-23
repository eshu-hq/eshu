#!/usr/bin/env bash
set -euo pipefail

# Self-test for the live Kubernetes two-team governance cross-scope denial proof
# verifier (#1910). It proves the verifier is well-formed, passes a good
# proof-artifact set, and fails closed on each tenant-isolation or cluster-posture
# regression: a leaked cross-scope repository, an open cross-scope selector, an
# API/MCP parity mismatch, an open unauthenticated read, a missing in-cluster
# NetworkPolicy, a non-kubernetes/unknown provenance, and leaked registry
# token-hash material. This runs locally with no cluster; the live run that
# produces real artifacts (scripts/run-k8s-two-team-governance-proof.sh) is the
# operator/CI gate.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-k8s-two-team-governance-proof.sh"
fixtures="${repo_root}/tests/fixtures/governance_k8s_two_team_proof"
expected_backend_image="ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1"
expected_index_image_id="docker-pullable://ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1"
expected_amd64_image_id="docker-pullable://ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:c4a2116e3c1547f750426d5c6c7fae618135f2fc1a3f7bf13fd7811a9c189915"
expected_arm64_image_id="docker-pullable://ghcr.io/eshu-hq/nornicdb-amd64-cpu@sha256:523137af6a6e26e7977c00ef8aeb96aef236a0414334253e63f6aae25fc466ce"

die() {
	printf 'test-verify-k8s-two-team-governance-proof: %s\n' "$*" >&2
	exit 1
}

# replace_first_literal copies source to destination with exactly the first
# literal occurrence of needle replaced. It fails closed when the fixture no
# longer contains the mutation target.
replace_first_literal() {
	local source="$1" destination="$2" needle="$3" replacement="$4"
	if ! awk -v needle="${needle}" -v replacement="${replacement}" '
		BEGIN { replaced = 0 }
		{
			if (!replaced) {
				position = index($0, needle)
				if (position != 0) {
					$0 = substr($0, 1, position - 1) replacement substr($0, position + length(needle))
					replaced = 1
				}
			}
			print
		}
		END { exit !replaced }
	' "${source}" >"${destination}"; then
		die "fixture mutation target is absent: ${needle}"
	fi
}

[[ -f "${verifier}" ]] || die "missing verifier: ${verifier}"
bash -n "${verifier}" || die "verifier failed bash syntax check"

for expected in \
	"\"backend_image\": \"${expected_backend_image}\"" \
	'"backend_platform": "linux/amd64"' \
	"\"backend_runtime_image_id\": \"${expected_amd64_image_id}\"" \
	'"backend_version": "NornicDB v1.3.3"'; do
	rg --fixed-strings --quiet "${expected}" "${fixtures}/good/provenance.json" \
		|| die "good provenance fixture missing exact backend identity: ${expected}"
done

# --list names every proof check without running anything.
list_log="$(bash "${verifier}" --list)"
for needle in "unauthenticated:" "admin:" "team-a allowed:" "team-a denied:" \
	"team-b allowed:" "team-b denied:" "parity:" "network policy:" "provenance:" \
	"redaction canary:"; do
	rg --fixed-strings --quiet "${needle}" < <(printf '%s\n' "${list_log}") \
		|| die "--list output missing ${needle}"
done

# Good artifacts prove the current handler's non-disclosing 404 selector result.
bash "${verifier}" --artifacts "${fixtures}/good" >/dev/null \
	|| die "verifier rejected the good proof artifacts"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

# A middleware-level 403 is also non-disclosing, but API and MCP must agree.
selector_403_dir="${tmp_dir}/selector-403"
cp -R "${fixtures}/good" "${selector_403_dir}"
for team in team-a team-b; do
	sed 's/_other_repo_selector_status": 404/_other_repo_selector_status": 403/g' \
		"${fixtures}/good/${team}.json" >"${selector_403_dir}/${team}.json"
done
bash "${verifier}" --artifacts "${selector_403_dir}" >/dev/null \
	|| die "verifier rejected non-disclosing 403 selector results"

mixed_selector_dir="${tmp_dir}/mixed-selector"
cp -R "${fixtures}/good" "${mixed_selector_dir}"
replace_first_literal "${fixtures}/good/team-a.json" "${mixed_selector_dir}/team-a.json" \
	'_other_repo_selector_status": 404' '_other_repo_selector_status": 403'
if bash "${verifier}" --artifacts "${mixed_selector_dir}" >/dev/null 2>&1; then
	die "verifier accepted API/MCP selector-status divergence"
fi

for surface in api mcp; do
	missing_own_status_dir="${tmp_dir}/missing-${surface}-own-status"
	cp -R "${fixtures}/good" "${missing_own_status_dir}"
	sed "/${surface}_own_repo_selector_status/d" \
		"${fixtures}/good/team-a.json" >"${missing_own_status_dir}/team-a.json"
	if bash "${verifier}" --artifacts "${missing_own_status_dir}" >/dev/null 2>&1; then
		die "verifier accepted missing ${surface} own-repository selector status"
	fi

	for status in 0 204 403 404 500; do
		bad_own_status_dir="${tmp_dir}/bad-${surface}-own-status-${status}"
		cp -R "${fixtures}/good" "${bad_own_status_dir}"
		replace_first_literal "${fixtures}/good/team-a.json" "${bad_own_status_dir}/team-a.json" \
			"${surface}_own_repo_selector_status\": 200" "${surface}_own_repo_selector_status\": ${status}"
		if bash "${verifier}" --artifacts "${bad_own_status_dir}" >/dev/null 2>&1; then
			die "verifier accepted ${surface} own-repository selector status ${status}"
		fi
	done
done

wrong_own_id_dir="${tmp_dir}/wrong-own-id"
cp -R "${fixtures}/good" "${wrong_own_id_dir}"
replace_first_literal "${fixtures}/good/team-a.json" "${wrong_own_id_dir}/team-a.json" \
	'api_own_repo_selector_repository_id": "repository:repoTeamAlpha"' \
	'api_own_repo_selector_repository_id": "repository:repoTeamBeta"'
if bash "${verifier}" --artifacts "${wrong_own_id_dir}" >/dev/null 2>&1; then
	die "verifier accepted the wrong own-repository selector identity"
fi

# Backend provenance accepts either the platform child or an exact index
# reported by the container runtime, and fails closed on every other identity.
arm64_dir="${tmp_dir}/arm64"
cp -R "${fixtures}/good" "${arm64_dir}"
sed \
	-e 's/"backend_platform": "linux\/amd64"/"backend_platform": "linux\/arm64"/' \
	-e "s#${expected_amd64_image_id}#${expected_arm64_image_id}#" \
	"${fixtures}/good/provenance.json" >"${arm64_dir}/provenance.json"
if bash "${verifier}" --artifacts "${arm64_dir}" >/dev/null 2>&1; then
	die "verifier accepted arm64 provenance without live backend proof"
fi

index_dir="${tmp_dir}/index-reporting-runtime"
cp -R "${fixtures}/good" "${index_dir}"
sed "s#${expected_amd64_image_id}#${expected_index_image_id}#" \
	"${fixtures}/good/provenance.json" >"${index_dir}/provenance.json"
bash "${verifier}" --artifacts "${index_dir}" >/dev/null \
	|| die "verifier rejected the exact index reported by the container runtime"

for mutation in wrong-index wrong-runtime-index wrong-runtime-repository wrong-platform-child wrong-version inferred-source-revision; do
	case "${mutation}" in
		wrong-index) replacement='s/fix-500-e022384c@sha256:74a8ed7b/v1.3.1@sha256:74a8ed7b/' ;;
		wrong-runtime-index) replacement='s/sha256:c4a2116e3c1547f750426d5c6c7fae618135f2fc1a3f7bf13fd7811a9c189915/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/' ;;
		wrong-runtime-repository) replacement='s#docker-pullable://ghcr.io/eshu-hq/nornicdb-amd64-cpu@#docker-pullable://example.invalid/nornicdb@#' ;;
		wrong-platform-child) replacement="s#${expected_amd64_image_id}#${expected_arm64_image_id}#" ;;
		wrong-version) replacement='s/NornicDB v1\.3\.3/NornicDB v1.3.0/' ;;
		inferred-source-revision) replacement='s/"backend_source_revision": "unavailable"/"backend_source_revision": "91289b0ed96e2bd23f3d96cb9b3f00fce30f6a0c"/' ;;
	esac
	mutation_dir="${tmp_dir}/${mutation}"
	cp -R "${fixtures}/good" "${mutation_dir}"
	sed "${replacement}" "${fixtures}/good/provenance.json" >"${mutation_dir}/provenance.json"
	if bash "${verifier}" --artifacts "${mutation_dir}" >/dev/null 2>&1; then
		die "verifier accepted invalid backend provenance: ${mutation}"
	fi
done

# Each bad artifact set must fail closed.
for bad in bad_cross_scope_leak bad_selector_open bad_parity bad_unauth_open \
	bad_netpol_absent bad_not_kubernetes bad_leak; do
	if bash "${verifier}" --artifacts "${fixtures}/${bad}" >/dev/null 2>&1; then
		die "verifier accepted bad artifacts: ${bad}"
	fi
done

printf 'live K8s two-team governance cross-scope denial proof verifier self-test passed\n'
