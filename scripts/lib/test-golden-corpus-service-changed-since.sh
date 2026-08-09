#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/golden-corpus-service-changed-since.sh
source "${repo_root}/scripts/lib/golden-corpus-service-changed-since.sh"

fail() { printf 'test-golden-corpus-service-changed-since: %s\n' "$*" >&2; exit 1; }
die() { fail "$@"; }

case_dir="$(mktemp -d -t golden-service-changed-since.XXXXXX)"
trap 'rm -rf "${case_dir}"' EXIT
corpus_dir="${case_dir}/corpus"
fixture_repo="${corpus_dir}/deployable-config"
mkdir -p "${fixture_repo}"
cp "${repo_root}/tests/fixtures/ecosystems/deployable-config/catalog-info.yaml" "${fixture_repo}/catalog-info.yaml"
git -C "${fixture_repo}" -c init.defaultBranch=main init >/dev/null
git -C "${fixture_repo}" config user.email "gate@eshu.local"
git -C "${fixture_repo}" config user.name "Golden Gate"
git -C "${fixture_repo}" add -- catalog-info.yaml
git -C "${fixture_repo}" update-index --add --cacheinfo \
	160000,5420542054205420542054205420542054205420,vendor/deployable-source
git -C "${fixture_repo}" commit -m initial >/dev/null
[[ "$(git -C "${fixture_repo}" status --short --untracked-files=no)" == " D vendor/deployable-source" ]] ||
	fail "test fixture must mirror the intentionally unpopulated deployable-source gitlink"

mock_active_generation="service-gen:prior"
mock_deployment_counts="0|0|3|0|0"
pg() {
	local sql="$1"
	if [[ "${sql}" == *"WITH prior_keys AS"* && "${sql}" == *"evidence_family = 'deployment'"* ]]; then
		printf '%s\n' "${mock_deployment_counts}"
		return
	fi
	if [[ "${sql}" == *"WITH adjacent_differences AS"* ]]; then
		printf '2|1|1|superseded|1|0|0|1|0\n'
		return
	fi
	if [[ "${sql}" == *"SELECT generation_id"* && "${sql}" == *"status = 'active'"* ]]; then
		printf '%s\n' "${mock_active_generation}"
		return
	fi
	fail "unexpected SQL: ${sql}"
}

golden_service_changed_since_capture_prior
[[ "${golden_service_changed_since_prior_generation}" == "service-gen:prior" ]] || fail "prior generation was not captured"
golden_service_changed_since_mutate_owner
rg -q '^  owner: group:default/runtime-platform$' "${fixture_repo}/catalog-info.yaml" || fail "owner mutation missing"
[[ "$(git -C "${fixture_repo}" rev-list --count HEAD)" == "2" ]] || fail "owner mutation did not create exactly one new commit"
[[ "$(git -C "${fixture_repo}" status --short --untracked-files=no)" == " D vendor/deployable-source" ]] ||
	fail "owner mutation changed the pre-existing gitlink status"

mock_active_generation="service-gen:current"
golden_service_changed_since_validate_current
[[ "${golden_service_changed_since_current_generation}" == "service-gen:current" ]] || fail "current generation was not captured"
[[ "${golden_service_changed_since_deployment_added_count}" == "0" ]] || fail "deployment added count was not captured"
[[ "${golden_service_changed_since_deployment_updated_count}" == "0" ]] || fail "deployment updated count was not captured"
[[ "${golden_service_changed_since_deployment_unchanged_count}" == "3" ]] || fail "deployment unchanged count was not captured"
[[ "${golden_service_changed_since_deployment_retired_count}" == "0" ]] || fail "deployment retired count was not captured"
[[ "${golden_service_changed_since_deployment_superseded_count}" == "0" ]] || fail "deployment superseded count was not captured"

for invalid_counts in "0|bad|3|0|0" "0|0|3|0" "0|0|-1|0|0" "0|0|3|0|0|9"; do
	if (
		mock_deployment_counts="${invalid_counts}"
		golden_service_changed_since_capture_deployment_counts \
			"service-gen:prior" "service-gen:current"
	) >/dev/null 2>&1; then
		fail "deployment count capture accepted invalid result: ${invalid_counts}"
	fi
done

input_snapshot="${case_dir}/suppression-composed.json"
output_snapshot="${case_dir}/service-changed-composed.json"
jq \
	--arg added "${golden_service_changed_since_deployment_added_sentinel}" \
	--arg updated "${golden_service_changed_since_deployment_updated_sentinel}" \
	--arg unchanged "${golden_service_changed_since_deployment_unchanged_sentinel}" \
	--arg retired "${golden_service_changed_since_deployment_retired_sentinel}" \
	--arg superseded "${golden_service_changed_since_deployment_superseded_sentinel}" \
	'.composition_marker = "preserved"
	 | (.query_shapes.mcp.get_service_changed_since.required_json_object_matches["categories[]"][]
	    | select(.category == "deployment").counts) = {
	       added: $added,
	       updated: $updated,
	       unchanged: $unchanged,
	       retired: $retired,
	       superseded: $superseded
	   }' \
	"${repo_root}/testdata/golden/e2e-20repo-snapshot.json" >"${input_snapshot}"
golden_service_changed_since_compose_snapshot "${input_snapshot}" "${output_snapshot}"
jq -e '
  .composition_marker == "preserved"
  and .query_shapes.mcp.get_service_changed_since.arguments.since_generation_id == "service-gen:prior"
  and .query_shapes.mcp.get_service_changed_since.required_json_values.since_generation_id == "service-gen:prior"
  and .query_shapes.mcp.get_service_changed_since.required_json_values.current_active_generation_id == "service-gen:current"
  and (.query_shapes.mcp.get_service_changed_since.required_json_object_matches["categories[]"][]
       | select(.category == "deployment").counts) == {
         added: 0,
         updated: 0,
         unchanged: 3,
         retired: 0,
         superseded: 0
       }
' "${output_snapshot}" >/dev/null || fail "runtime snapshot composition lost a prior transform or generation ID"

missing_sentinels_snapshot="${case_dir}/missing-sentinels-input.json"
jq '(.query_shapes.mcp.get_service_changed_since.required_json_object_matches["categories[]"][]
     | select(.category == "deployment").counts.updated) = 0' \
	"${repo_root}/testdata/golden/e2e-20repo-snapshot.json" >"${missing_sentinels_snapshot}"
if (golden_service_changed_since_compose_snapshot \
	"${missing_sentinels_snapshot}" \
	"${case_dir}/missing-sentinels.json") >/dev/null 2>&1; then
	fail "composition accepted a snapshot without deployment-count sentinels"
fi

printf 'PASS: golden service changed-since leaf helper\n'
