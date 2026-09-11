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
expected_backend_image="timothyswt/nornicdb-cpu-bge:v1.3.1@sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962"
expected_index_image_id="docker-pullable://timothyswt/nornicdb-cpu-bge@sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962"
expected_amd64_image_id="docker-pullable://timothyswt/nornicdb-cpu-bge@sha256:c0b5f73c55bd56a6764d1833665252b98a30b332248f0233f5f4eab4dc0d2ca1"
expected_arm64_image_id="docker-pullable://timothyswt/nornicdb-cpu-bge@sha256:d787abe61d92c67761bdbd21ae5224aad904f2a13283d46d13fdfb6269b86b7b"

die() {
	printf 'test-verify-k8s-two-team-governance-proof: %s\n' "$*" >&2
	exit 1
}

[[ -f "${verifier}" ]] || die "missing verifier: ${verifier}"
bash -n "${verifier}" || die "verifier failed bash syntax check"

for expected in \
	"\"backend_image\": \"${expected_backend_image}\"" \
	'"backend_platform": "linux/amd64"' \
	"\"backend_runtime_image_id\": \"${expected_amd64_image_id}\"" \
	'"backend_version": "NornicDB v1.3.1"'; do
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
	sed 's/_selector_status": 404/_selector_status": 403/g' \
		"${fixtures}/good/${team}.json" >"${selector_403_dir}/${team}.json"
done
bash "${verifier}" --artifacts "${selector_403_dir}" >/dev/null \
	|| die "verifier rejected non-disclosing 403 selector results"

mixed_selector_dir="${tmp_dir}/mixed-selector"
cp -R "${fixtures}/good" "${mixed_selector_dir}"
sed '0,/_selector_status": 404/s//_selector_status": 403/' \
	"${fixtures}/good/team-a.json" >"${mixed_selector_dir}/team-a.json"
if bash "${verifier}" --artifacts "${mixed_selector_dir}" >/dev/null 2>&1; then
	die "verifier accepted API/MCP selector-status divergence"
fi

# Backend provenance accepts either the platform child or an exact index
# reported by the container runtime, and fails closed on every other identity.
arm64_dir="${tmp_dir}/arm64"
cp -R "${fixtures}/good" "${arm64_dir}"
sed \
	-e 's/"backend_platform": "linux\/amd64"/"backend_platform": "linux\/arm64"/' \
	-e "s#${expected_amd64_image_id}#${expected_arm64_image_id}#" \
	"${fixtures}/good/provenance.json" >"${arm64_dir}/provenance.json"
bash "${verifier}" --artifacts "${arm64_dir}" >/dev/null \
	|| die "verifier rejected valid arm64 manifest provenance"

index_dir="${tmp_dir}/index-reporting-runtime"
cp -R "${fixtures}/good" "${index_dir}"
sed "s#${expected_amd64_image_id}#${expected_index_image_id}#" \
	"${fixtures}/good/provenance.json" >"${index_dir}/provenance.json"
bash "${verifier}" --artifacts "${index_dir}" >/dev/null \
	|| die "verifier rejected the exact index reported by the container runtime"

for mutation in wrong-index wrong-runtime-index wrong-runtime-repository wrong-platform-child wrong-version inferred-source-revision; do
	case "${mutation}" in
		wrong-index) replacement='s/v1\.3\.1@sha256:ac524899/v1.3.0@sha256:ac524899/' ;;
		wrong-runtime-index) replacement='s/sha256:c0b5f73c55bd56a6764d1833665252b98a30b332248f0233f5f4eab4dc0d2ca1/sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/' ;;
		wrong-runtime-repository) replacement='s#docker-pullable://timothyswt/nornicdb-cpu-bge@#docker-pullable://example.invalid/nornicdb@#' ;;
		wrong-platform-child) replacement="s#${expected_amd64_image_id}#${expected_arm64_image_id}#" ;;
		wrong-version) replacement='s/NornicDB v1\.3\.1/NornicDB v1.3.0/' ;;
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
