#!/usr/bin/env bash

verify_tfstate_warning_summary() {
	local max_state_missing="${ESHU_REMOTE_E2E_TFSTATE_STATE_MISSING_MAX:-0}"
	require_non_negative_integer ESHU_REMOTE_E2E_TFSTATE_STATE_MISSING_MAX "${max_state_missing}"

	echo "Checking Terraform-state warning summary..."
	local status_file="${TMP_DIR}/status-index.json"
	if ! api_get "/status/index" "${status_file}"; then
		echo "remote E2E Terraform-state warning check could not read ${API_BASE_URL}/status/index" >&2
		return 1
	fi
	if ! jq -e '(.terraform_state.warning_summary // null) | type == "array"' "${status_file}" >/dev/null; then
		echo "remote E2E Terraform-state warning summary missing from status readback" >&2
		return 1
	fi

	local summary_count
	summary_count="$(jq -r '[.terraform_state.warning_summary[]?] | length' "${status_file}")"
	if ((summary_count == 0)); then
		echo "remote E2E Terraform-state warning summary: no warnings reported"
		echo "remote E2E Terraform-state state_missing warning threshold verified: count=0 max=${max_state_missing}"
		return 0
	fi

	jq -r '
		.terraform_state.warning_summary[]?
		| "remote E2E Terraform-state warning summary: warning_kind=\(.warning_kind // "unknown") reason=\(.reason // "unknown") scope_class=\(.scope_class // "unknown") count=\(.count // 0)"
	' "${status_file}"
	jq -r '
		.terraform_state.recent_warnings[]?
		| select((.warning_kind // "") == "state_missing")
		| "remote E2E Terraform-state warning detail: warning_kind=\(.warning_kind // "unknown") reason=\(.reason // "unknown") source=\(.source // "unknown") source_handle=\(.source_handle // "unknown") safe_locator_hash=\(.safe_locator_hash // "unknown")"
	' "${status_file}"

	local state_missing_count
	state_missing_count="$(jq -r '
		[
			.terraform_state.warning_summary[]?
			| select((.warning_kind // "") == "state_missing")
			| (.count // 0)
		] | add // 0
	' "${status_file}")"
	if ((state_missing_count > max_state_missing)); then
		printf 'remote E2E Terraform-state state_missing warnings exceeded: count=%s max=%s\n' \
			"${state_missing_count}" "${max_state_missing}" >&2
		return 1
	fi
	printf 'remote E2E Terraform-state state_missing warning threshold verified: count=%s max=%s\n' \
		"${state_missing_count}" "${max_state_missing}"
}
