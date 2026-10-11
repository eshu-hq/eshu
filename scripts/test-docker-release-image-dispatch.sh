#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TEMP_DIR}"' EXIT

EXPECTED_SHA="$(git rev-parse HEAD)"
WORKFLOW_REF='eshu-hq/eshu/.github/workflows/docker-publish.yml@refs/heads/release/7934-qa-metrics-jira'
DISPATCH_REF='refs/heads/release/7934-qa-metrics-jira'

run_dispatch_selector() {
	local expected_sha="$1"
	local ref="$2"
	local repository="$3"
	local workflow_ref="$4"
	local event_name="$5"
	local github_sha="${6:-${EXPECTED_SHA}}"
	local output_file="${TEMP_DIR}/selector.out"
	: > "${output_file}"

	env \
		GITHUB_OUTPUT="${output_file}" \
		GITHUB_EVENT_NAME="${event_name}" \
		GITHUB_REPOSITORY="${repository}" \
		GITHUB_WORKFLOW_REF="${workflow_ref}" \
		GITHUB_REF="${ref}" \
		GITHUB_SHA="${github_sha}" \
		EXPECTED_SHA="${expected_sha}" \
		bash scripts/select-docker-publication-lanes.sh
}

expect_dispatch_rejected() {
	local label="$1"
	shift
	if run_dispatch_selector "$@" > "${TEMP_DIR}/reject.out" 2>&1; then
		printf 'expected dispatch guard to reject %s\n' "${label}" >&2
		exit 1
	fi
	if [[ -s "${TEMP_DIR}/selector.out" ]]; then
		printf 'rejected dispatch emitted publication lanes for %s\n' "${label}" >&2
		exit 1
	fi
}

run_dispatch_selector "${EXPECTED_SHA}" "${DISPATCH_REF}" 'eshu-hq/eshu' \
	"${WORKFLOW_REF}" workflow_dispatch "${EXPECTED_SHA}"
[[ "$(cat "${TEMP_DIR}/selector.out")" == $'image=true\nchart=false\napkfloors=false' ]]

expect_dispatch_rejected 'short expected SHA' \
	"${EXPECTED_SHA:0:39}" "${DISPATCH_REF}" 'eshu-hq/eshu' "${WORKFLOW_REF}" \
	workflow_dispatch "${EXPECTED_SHA}"
expect_dispatch_rejected 'different expected SHA' \
	0000000000000000000000000000000000000000 "${DISPATCH_REF}" 'eshu-hq/eshu' \
	"${WORKFLOW_REF}" workflow_dispatch "${EXPECTED_SHA}"
expect_dispatch_rejected 'wrong ref' \
	"${EXPECTED_SHA}" refs/heads/main 'eshu-hq/eshu' "${WORKFLOW_REF}" \
	workflow_dispatch "${EXPECTED_SHA}"
expect_dispatch_rejected 'wrong repository' \
	"${EXPECTED_SHA}" "${DISPATCH_REF}" 'other/repo' "${WORKFLOW_REF}" \
	workflow_dispatch "${EXPECTED_SHA}"
expect_dispatch_rejected 'wrong workflow ref' \
	"${EXPECTED_SHA}" "${DISPATCH_REF}" 'eshu-hq/eshu' \
	'eshu-hq/eshu/.github/workflows/other.yml@refs/heads/release/7934-qa-metrics-jira' \
	workflow_dispatch "${EXPECTED_SHA}"
expect_dispatch_rejected 'wrong event' \
	"${EXPECTED_SHA}" "${DISPATCH_REF}" 'eshu-hq/eshu' "${WORKFLOW_REF}" schedule \
	"${EXPECTED_SHA}"
expect_dispatch_rejected 'checkout SHA mismatch' \
	0000000000000000000000000000000000000000 "${DISPATCH_REF}" 'eshu-hq/eshu' \
	"${WORKFLOW_REF}" workflow_dispatch 0000000000000000000000000000000000000000

output_file="${TEMP_DIR}/tag.out"
GITHUB_OUTPUT="${output_file}" GITHUB_EVENT_NAME=push GITHUB_REF_TYPE=tag \
	GITHUB_REF=refs/tags/v1.2.3 bash scripts/select-docker-publication-lanes.sh
[[ "$(cat "${output_file}")" == $'image=true\nchart=true\napkfloors=true' ]]

mkdir -p "${TEMP_DIR}/selector-bin"
REAL_GIT="$(command -v git)"
cat > "${TEMP_DIR}/selector-bin/git" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
	diff) printf '%s\n' "${MOCK_CHANGED_PATHS}" ;;
	merge-base) printf '%s\n' "${MOCK_MERGE_BASE}" ;;
	*) exec "${REAL_GIT}" "$@" ;;
esac
EOF
chmod +x "${TEMP_DIR}/selector-bin/git"

run_regular_selector() {
	local event_name="$1"
	local changed_paths="$2"
	local output_file="${TEMP_DIR}/regular-selector.out"
	: > "${output_file}"
	env PATH="${TEMP_DIR}/selector-bin:${PATH}" \
		REAL_GIT="${REAL_GIT}" GITHUB_OUTPUT="${output_file}" \
		GITHUB_EVENT_NAME="${event_name}" GITHUB_REF_TYPE=branch \
		GITHUB_SHA="${EXPECTED_SHA}" PUSH_BEFORE_SHA="${EXPECTED_SHA}" \
		PULL_REQUEST_BASE_SHA="${EXPECTED_SHA}" PULL_REQUEST_HEAD_SHA="${EXPECTED_SHA}" \
		MOCK_CHANGED_PATHS="${changed_paths}" MOCK_MERGE_BASE="${EXPECTED_SHA}" \
		bash scripts/select-docker-publication-lanes.sh
	cat "${output_file}"
}

[[ "$(run_regular_selector push 'go/example.go')" == $'image=true\nchart=false\napkfloors=false' ]]
[[ "$(run_regular_selector pull_request Dockerfile)" == $'image=true\nchart=false\napkfloors=true' ]]
[[ "$(run_regular_selector pull_request deploy/helm/eshu/Chart.yaml)" == $'image=false\nchart=true\napkfloors=false' ]]
[[ "$(run_regular_selector merge_group 'go/example.go')" == $'image=false\nchart=false\napkfloors=false' ]]
[[ "$(run_regular_selector merge_group Dockerfile)" == $'image=true\nchart=false\napkfloors=true' ]]

assert_release_tags() {
	local tags="$1"
	local rc=0
	RELEASE_IMAGE_TAGS="${tags}" EXPECTED_SHA="${EXPECTED_SHA}" \
		bash scripts/assert-docker-release-image-tags.sh > "${TEMP_DIR}/tags.out" 2>&1 || rc=$?
	return "${rc}"
}

assert_release_tags "ghcr.io/eshu-hq/eshu:sha-${EXPECTED_SHA}"
if assert_release_tags "ghcr.io/eshu-hq/eshu:sha-${EXPECTED_SHA}"$'\n'"ghcr.io/eshu-hq/eshu:sha-${EXPECTED_SHA}"; then
	printf '%s\n' 'expected duplicate image tag to be rejected' >&2
	exit 1
fi
if assert_release_tags "ghcr.io/eshu-hq/eshu:sha-${EXPECTED_SHA}"$'\n'latest; then
	printf '%s\n' 'expected extra image tag to be rejected' >&2
	exit 1
fi
if assert_release_tags 'sha-1234567'; then
	printf '%s\n' 'expected non-full-SHA image tag to be rejected' >&2
	exit 1
fi

registry_curl_fixture() {
	local url=""
	local basic_auth=false
	local bearer_auth=false
	local arg
	for arg in "$@"; do
		case "${arg}" in
			https://*) url="${arg}" ;;
			x-access-token:fake-token) basic_auth=true ;;
			'Authorization: Bearer fixture-registry-token') bearer_auth=true ;;
		esac
	done
	if [[ "${url}" == 'https://ghcr.io/token' ]]; then
		[[ "${basic_auth}" == true ]]
		[[ "$*" == *'scope=repository:eshu-hq/eshu:pull'* ]]
		case "${FAKE_TOKEN_MODE:-valid}" in
			denied) exit 22 ;;
			empty) printf '{"token":""}' ;;
			malformed) printf 'not-json' ;;
			access_token) printf '{"access_token":"fixture-registry-token"}' ;;
			*) printf '{"token":"fixture-registry-token"}' ;;
		esac
		exit 0
	fi
	[[ "${url}" == "https://ghcr.io/v2/eshu-hq/eshu/manifests/${EXPECTED_TAG}" ]]
	case "${FAKE_REGISTRY_MODE:-absent}" in
		absent) printf 'HTTP/2 404\r\n\r\n' ;;
		exists) printf 'HTTP/2 200\r\ndocker-content-digest: sha256:1111111111111111111111111111111111111111111111111111111111111111\r\n\r\n' ;;
		401) printf 'HTTP/2 401\r\ndocker-content-digest: sha256:1111\r\n\r\n' ;;
		403) printf 'HTTP/2 403\r\n\r\n' ;;
		500) printf 'HTTP/2 500\r\n\r\n' ;;
		malformed) printf 'HTTP/2 200\r\n\r\n' ;;
		network) exit 28 ;;
		*) exit 2 ;;
	esac
	[[ "${bearer_auth}" == true ]]
}

mkdir -p "${TEMP_DIR}/registry-bin"
{
	printf '#!/usr/bin/env bash\nset -euo pipefail\n'
	declare -f registry_curl_fixture
	printf 'registry_curl_fixture "$@"\n'
} > "${TEMP_DIR}/registry-bin/curl"
chmod +x "${TEMP_DIR}/registry-bin/curl"

check_registry_tag() {
	local token_mode="$1"
	local registry_mode="$2"
	local rc=0
	env PATH="${TEMP_DIR}/registry-bin:${PATH}" GITHUB_TOKEN=fake-token \
		GITHUB_REPOSITORY=eshu-hq/eshu EXPECTED_TAG="sha-${EXPECTED_SHA}" \
		FAKE_TOKEN_MODE="${token_mode}" FAKE_REGISTRY_MODE="${registry_mode}" \
		bash scripts/assert-ghcr-image-tag-absent.sh \
		> "${TEMP_DIR}/registry.out" 2>&1 || rc=$?
	return "${rc}"
}

check_registry_tag valid absent
for pair in 'valid exists' 'valid 401' 'valid 403' 'valid 500' 'valid malformed' 'valid network' 'denied absent' 'empty absent' 'malformed absent'; do
	read -r token_mode registry_mode <<< "${pair}"
	if check_registry_tag "${token_mode}" "${registry_mode}"; then
		printf 'expected registry result token=%s response=%s to fail closed\n' \
			"${token_mode}" "${registry_mode}" >&2
		exit 1
	fi
done

if env PATH="${TEMP_DIR}/registry-bin:${PATH}" GITHUB_TOKEN=fake-token \
	GITHUB_REPOSITORY=other/repo EXPECTED_TAG="sha-${EXPECTED_SHA}" \
	FAKE_TOKEN_MODE=valid FAKE_REGISTRY_MODE=absent \
	bash scripts/assert-ghcr-image-tag-absent.sh \
	> "${TEMP_DIR}/registry.out" 2>&1; then
	printf '%s\n' 'expected GHCR guard to reject a different repository' >&2
	exit 1
fi
if env PATH="${TEMP_DIR}/registry-bin:${PATH}" GITHUB_TOKEN=fake-token \
	GITHUB_REPOSITORY=eshu-hq/eshu EXPECTED_TAG=sha-1234567 \
	FAKE_TOKEN_MODE=valid FAKE_REGISTRY_MODE=absent \
	bash scripts/assert-ghcr-image-tag-absent.sh \
	> "${TEMP_DIR}/registry.out" 2>&1; then
	printf '%s\n' 'expected GHCR guard to reject a non-full-SHA tag' >&2
	exit 1
fi
if rg -F -e 'fake-token' -e 'fixture-registry-token' "${TEMP_DIR}/registry.out"; then
	printf '%s\n' 'registry failure output leaked a credential marker' >&2
	exit 1
fi

workflow='.github/workflows/docker-publish.yml'
for required in \
	'expected_sha:' \
	'required: true' \
	'scripts/select-docker-publication-lanes.sh' \
	'scripts/assert-docker-release-image-tags.sh' \
	'scripts/assert-ghcr-image-tag-absent.sh' \
	'type=sha,format=long' \
	'latest=false'; do
	if ! rg -F -q -- "${required}" "${workflow}"; then
		printf 'workflow is missing release image guard: %s\n' "${required}" >&2
		exit 1
	fi
done

line_number() {
	rg -n -F -m 1 -- "$1" "${workflow}" | cut -d: -f1
}
checkout_guard_line="$(line_number 'name: Revalidate release checkout before credentials')"
existing_tag_line="$(line_number 'name: Refuse an existing release image tag')"
registry_login_line="$(line_number 'name: Log in to Container Registry')"
build_line="$(line_number 'name: Build and push Docker image')"
[[ "${checkout_guard_line}" -lt "${existing_tag_line}" ]]
[[ "${existing_tag_line}" -lt "${registry_login_line}" ]]
[[ "${existing_tag_line}" -lt "${build_line}" ]]
if ! rg -F -q "if: github.event_name != 'workflow_dispatch' && env.LANE_APPLIES == 'true'" "${workflow}"; then
	printf '%s\n' 'normal image metadata must remain separate from release dispatch metadata' >&2
	exit 1
fi

printf '%s\n' 'docker release image dispatch guards passed'
