#!/usr/bin/env bash
set -euo pipefail

# Verifier for the LIVE Kubernetes/Helm two-team hosted governance cross-scope
# denial proof (#1910). It is the cluster sibling of
# scripts/verify-two-team-governance-proof.sh (Compose) and asserts the same
# tenant-isolation invariants plus two cluster-specific ones:
#
#   - unauthenticated reads are rejected (401),
#   - an admin (all-scopes) token sees every seeded repository,
#   - each team's scoped token reads ONLY its own repository (allowed in-scope),
#   - each team's scoped token CANNOT see the other team's repository, and the
#     other team's single-repository selector fails closed without disclosing
#     existence (403 or 404),
#   - API and MCP readbacks agree per team (parity),
#   - the rendered NetworkPolicies are actually applied in-cluster (api + mcp
#     present, restricted egress) (cluster-specific), and
#   - provenance records platform=kubernetes with a non-empty kubernetes_version
#     (cluster-specific), plus eshu_commit, backend, and registry token count.
#
# The verifier operates on a recorded artifacts directory so it is deterministic
# and self-testable. The live run that produces those artifacts from a deployed
# cluster (scripts/run-k8s-two-team-governance-proof.sh) is the operator/CI gate.
# Artifacts carry counts and HTTP states only, never response bodies.

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || (cd "$(dirname "$0")/.." && pwd))"
list_only=false
artifacts_dir=""
readonly expected_backend_image="timothyswt/nornicdb-cpu-bge:v1.3.1@sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962"
readonly expected_index_digest="sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962"
readonly expected_amd64_digest="sha256:c0b5f73c55bd56a6764d1833665252b98a30b332248f0233f5f4eab4dc0d2ca1"
readonly expected_arm64_digest="sha256:d787abe61d92c67761bdbd21ae5224aad904f2a13283d46d13fdfb6269b86b7b"

usage() {
	# printf, not a heredoc: Homebrew bash >= 5.1 writes an entire heredoc
	# body to a pipe before forking the reader, and macOS's 512-byte pipe
	# buffer deadlocks on any body over that size (#5074). This body expands
	# "$(basename "$0")", so it cannot move to a static scripts/lib/ data
	# file; each literal line is single-quoted and the one expanding line is
	# double-quoted to preserve the original heredoc's expansion behavior.
	printf '%s\n' \
		"Usage: $(basename "$0") --artifacts <dir> [--list]" \
		'' \
		'Verifies recorded LIVE Kubernetes two-team governance cross-scope denial proof' \
		'artifacts:' \
		'  admin.json           admin (all-scopes) repository enumeration' \
		'  team-a.json          team-A scoped allowed/denied reads (API + MCP)' \
		'  team-b.json          team-B scoped allowed/denied reads (API + MCP)' \
		'  unauth.json          unauthenticated rejection states' \
		'  network-policy.json  in-cluster NetworkPolicy applied state' \
		'  provenance.json      eshu commit, exact backend identity, platform, kubernetes version' \
		'' \
		'The artifacts directory is produced by running the live K8s governance driver' \
		'(scripts/run-k8s-two-team-governance-proof.sh) against a deployed Eshu Helm' \
		'release.' \
		'' \
		'  --list   print the proof checks without running them'
}

die() {
	printf 'verify-k8s-two-team-governance-proof: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--list) list_only=true; shift ;;
		--artifacts) artifacts_dir="${2:-}"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown option: $1" ;;
	esac
done

command -v rg >/dev/null 2>&1 || die "rg is required"

# Forbidden material that must never appear in any proof artifact.
readonly forbidden_patterns=(
	'/Users/'
	'/home/'
	'BEGIN [A-Z ]*PRIVATE KEY'
	'[Bb]earer [A-Za-z0-9._-]{8,}'
	'token_sha256'
	'postgres(ql)?://'
	'([0-9]{1,3}\.){3}[0-9]{1,3}'
)

print_checks() {
	# printf, not a heredoc: see usage() above for the #5074 pipe-deadlock
	# rationale. This body has no variable expansion but its 880-byte source
	# exceeds the 512-byte heredoc-budget threshold, so it moves to printf
	# too. Two lines carry a literal apostrophe ("team-B's" / "team-A's");
	# those are double-quoted only to avoid escaping the apostrophe inside a
	# single-quoted string — neither line expands anything.
	printf '%s\n' \
		'live K8s two-team governance cross-scope denial proof checks:' \
		'  1. unauthenticated: API and MCP repository reads return 401' \
		'  2. admin: all-scopes token enumerates at least two repositories' \
		'  3. team-a allowed: team-A scoped token API+MCP list includes only its own repo (count==1)' \
			"  4. team-a denied: team-A list excludes team-B's repo; selector for it returns a non-disclosing 403 or 404" \
		'  5. team-b allowed: team-B scoped token API+MCP list includes only its own repo (count==1)' \
			"  6. team-b denied: team-B list excludes team-A's repo; selector for it returns a non-disclosing 403 or 404" \
		'  7. parity: API and MCP scoped readbacks agree per team' \
		'  8. network policy: api + mcp NetworkPolicies applied in-cluster with restricted egress' \
		'  9. provenance: platform=kubernetes, exact NornicDB index/platform digest and version, eshu_commit, token count' \
		' 10. redaction canary: no bearer tokens, token hashes, host paths, DSNs, keys, or raw IPs'
}

if [[ "${list_only}" == true ]]; then
	print_checks
	exit 0
fi

[[ -n "${artifacts_dir}" ]] || die "--artifacts <dir> is required (or use --list)"
[[ -d "${artifacts_dir}" ]] || die "artifacts directory not found: ${artifacts_dir}"

admin="${artifacts_dir}/admin.json"
team_a="${artifacts_dir}/team-a.json"
team_b="${artifacts_dir}/team-b.json"
unauth="${artifacts_dir}/unauth.json"
netpol="${artifacts_dir}/network-policy.json"
provenance="${artifacts_dir}/provenance.json"
for required in "${admin}" "${team_a}" "${team_b}" "${unauth}" "${netpol}" "${provenance}"; do
	[[ -f "${required}" ]] || die "missing required artifact: ${required}"
done

json_str() {
	rg -o "\"$2\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" "$1" | rg -o ':[[:space:]]*"[^"]*"' | rg -o '"[^"]*"$' | tr -d '"' | head -1
}
json_num() {
	rg -o "\"$2\"[[:space:]]*:[[:space:]]*[0-9]+" "$1" | rg -o '[0-9]+$' | head -1
}
require_eq() {
	local got="$1" want="$2" what="$3"
	[[ "${got}" == "${want}" ]] || die "${what}: got '${got}', want '${want}'"
}
require_non_disclosing_selector_status() {
	local got="$1" what="$2"
	case "${got}" in
		403|404) ;;
		*) die "${what}: got '${got}', want non-disclosing 403 or 404" ;;
	esac
}

# 1. Unauthenticated rejection.
require_eq "$(json_num "${unauth}" api_status)" "401" "unauth API repository read status"
require_eq "$(json_num "${unauth}" mcp_status)" "401" "unauth MCP repository read status"

# 2. Admin enumerates at least two repositories.
admin_count="$(json_num "${admin}" repository_count)"
[[ -n "${admin_count}" ]] || die "admin artifact missing repository_count"
[[ "${admin_count}" -ge 2 ]] || die "admin enumerated ${admin_count} repositories; need at least 2 distinct tenants"

# 3-7. Per-team allowed/denied/parity.
check_team() {
	local file="$1" label="$2"
	for surface in api mcp; do
		local count own other sel
		count="$(json_num "${file}" "${surface}_repository_count")"
		own="$(json_str "${file}" "${surface}_own_repo_present")"
		other="$(json_str "${file}" "${surface}_other_repo_present")"
		sel="$(json_num "${file}" "${surface}_other_repo_selector_status")"
		require_eq "${own}" "true" "${label} ${surface} own repository present"
		require_eq "${count}" "1" "${label} ${surface} scoped repository count"
		require_eq "${other}" "false" "${label} ${surface} cross-scope repository leaked"
		require_non_disclosing_selector_status "${sel}" "${label} ${surface} cross-scope selector status"
	done
	require_eq "$(json_num "${file}" api_repository_count)" "$(json_num "${file}" mcp_repository_count)" "${label} API/MCP count parity"
	require_eq "$(json_str "${file}" api_own_repo_present)" "$(json_str "${file}" mcp_own_repo_present)" "${label} API/MCP own-repo parity"
	require_eq "$(json_str "${file}" api_other_repo_present)" "$(json_str "${file}" mcp_other_repo_present)" "${label} API/MCP cross-scope parity"
	require_eq "$(json_num "${file}" api_other_repo_selector_status)" "$(json_num "${file}" mcp_other_repo_selector_status)" "${label} API/MCP selector parity"
}
check_team "${team_a}" "team-a"
check_team "${team_b}" "team-b"

# 8. NetworkPolicy applied in-cluster.
require_eq "$(json_str "${netpol}" api_policy_applied)" "true" "API NetworkPolicy applied in-cluster"
require_eq "$(json_str "${netpol}" mcp_policy_applied)" "true" "MCP NetworkPolicy applied in-cluster"
require_eq "$(json_str "${netpol}" egress_mode)" "restricted" "NetworkPolicy egress mode"
np_count="$(json_num "${netpol}" applied_count)"
[[ -n "${np_count}" && "${np_count}" -ge 2 ]] || die "network-policy applied_count=${np_count:-missing}; expected >=2"

# 9. Provenance: cluster-specific fields.
for field in eshu_commit backend backend_image backend_platform backend_runtime_image_id \
	backend_version backend_source_revision platform kubernetes_version metrics_handle; do
	rg --quiet "\"${field}\"[[:space:]]*:[[:space:]]*\"[^\"]+\"" "${provenance}" \
		|| die "provenance missing or empty field: ${field}"
done
require_eq "$(json_str "${provenance}" platform)" "kubernetes" "provenance platform"
require_eq "$(json_str "${provenance}" backend)" "nornicdb" "provenance backend"
require_eq "$(json_str "${provenance}" backend_image)" "${expected_backend_image}" "provenance configured backend image"
require_eq "$(json_str "${provenance}" backend_version)" "NornicDB v1.3.1" "provenance backend version"
require_eq "$(json_str "${provenance}" backend_source_revision)" "unavailable" "official image source revision status"

backend_platform="$(json_str "${provenance}" backend_platform)"
case "${backend_platform}" in
	linux/amd64) expected_platform_digest="${expected_amd64_digest}" ;;
	linux/arm64) expected_platform_digest="${expected_arm64_digest}" ;;
	*) die "provenance backend_platform '${backend_platform}' is not a published v1.3.1 platform" ;;
esac
backend_runtime_image_id="$(json_str "${provenance}" backend_runtime_image_id)"
[[ "${backend_runtime_image_id}" == *"timothyswt/nornicdb-cpu-bge@"* ]] \
	|| die "provenance backend_runtime_image_id '${backend_runtime_image_id}' is not the expected repository"
case "${backend_runtime_image_id}" in
	*@"${expected_platform_digest}"|*@"${expected_index_digest}") ;;
	*) die "provenance backend_runtime_image_id '${backend_runtime_image_id}' matches neither the immutable v1.3.1 index nor ${backend_platform} digest ${expected_platform_digest}" ;;
esac
rg --quiet '"eshu_commit"[[:space:]]*:[[:space:]]*"unknown"' "${provenance}" \
	&& die "provenance eshu_commit is unknown (capture ran outside a checkout)"
rg --quiet '"kubernetes_version"[[:space:]]*:[[:space:]]*"unknown"' "${provenance}" \
	&& die "provenance kubernetes_version is unknown (capture ran without a live cluster)"
reg_count="$(json_num "${provenance}" registry_token_count)"
[[ -n "${reg_count}" && "${reg_count}" -ge 3 ]] || die "provenance registry_token_count=${reg_count:-missing}; expected admin + 2 teams"

# 10. Redaction canary across every artifact.
for artifact in "${admin}" "${team_a}" "${team_b}" "${unauth}" "${netpol}" "${provenance}"; do
	for pattern in "${forbidden_patterns[@]}"; do
		if rg --quiet "${pattern}" "${artifact}"; then
			die "forbidden material matched /${pattern}/ in $(basename "${artifact}")"
		fi
	done
done

printf 'live K8s two-team governance cross-scope denial proof artifacts verified (admin repos=%s, network policies=%s, backend=%s, platform=%s)\n' \
	"${admin_count}" "${np_count}" "${expected_backend_image}" "${backend_platform}"
