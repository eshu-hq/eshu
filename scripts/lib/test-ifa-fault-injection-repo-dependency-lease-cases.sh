#!/usr/bin/env bash
# Hermetic structural proof for repo_dependency's partition-lease mechanics
# (capture/require-subset/expire), split out of
# scripts/lib/test-ifa-fault-injection-repo-dependency-cases.sh to keep that
# file below the repository's 500-line cap. Sourced by
# scripts/test-verify-ifa-fault-injection.sh alongside its parent, and called
# from run_ifa_fault_injection_repo_dependency_cases the same way it always
# was, just now defined in this sibling file instead of inline.
run_ifa_repo_dependency_partition_lease_controls() (
	local dead_pid=987654 mode="lifecycle" pre_census="" post_census=""
	local lease_state_dir pg_marker census_marker update_marker zero_marker updated_marker
	local domain partition_id partition_count owner
	local FAULT_COMPOSE_PROJECT=test use_compose=0 ESHU_POSTGRES_DSN=test compose_file=test
	local owner_prefix="repo-dependency-projection-runner:test-host:${dead_pid}:0123456789abcdef0123456789abcdef"
	local first="repo_dependency|0|4|${owner_prefix}/worker-0-of-4"
	local second="repo_dependency|1|4|${owner_prefix}/worker-1-of-4"
	local third="repo_dependency|2|4|${owner_prefix}/worker-2-of-4"
	local fourth="repo_dependency|3|4|${owner_prefix}/worker-3-of-4"
	local expected_pre="${first}"$'\n'"${second}"
	local expected_post="${expected_pre}"$'\n'"${third}"$'\n'"${fourth}"
	lease_state_dir="$(mktemp -d)" || return 1
	trap 'rm -rf "${lease_state_dir}"' EXIT
	pg_marker="${lease_state_dir}/preserved"
	census_marker="${lease_state_dir}/census"
	update_marker="${lease_state_dir}/updates"
	zero_marker="${lease_state_dir}/zero-checks"
	updated_marker="${lease_state_dir}/updated-snapshot"
	printf '0' >"${census_marker}"
	printf '0' >"${update_marker}"
	printf '0' >"${zero_marker}"

	ifa_det_pg() {
		local sql="$4"
		if [[ "${sql}" == "SELECT count(*) FROM shared_projection_partition_leases"* ]]; then
			printf '%s' "$(( $(<"${zero_marker}") + 1 ))" >"${zero_marker}"
			[[ "${sql}" == *"projection_domain = 'repo_dependency'"* ]] || return 1
			[[ "${sql}" == *":${dead_pid}:[0-9a-f]{32}/worker-"* ]] || return 1
			[[ "${mode}" == "remaining" || "${mode}" == "pre-only-mutation" ]] && printf '2' || printf '0'
			return 0
		fi
		if [[ "${sql}" == *"SELECT COALESCE(string_agg("* ]]; then
			printf '%s' "$(( $(<"${census_marker}") + 1 ))" >"${census_marker}"
			[[ "${sql}" == *"projection_domain = 'repo_dependency'"* ]] || return 1
			[[ "${sql}" == *"lease_expires_at > CURRENT_TIMESTAMP"* ]] || return 1
			[[ "${sql}" == *":${dead_pid}:[0-9a-f]{32}/worker-"* ]] || return 1
			case "${mode}" in
			zero) return 0 ;;
			wrong-owner)
				printf 'repo_dependency|0|4|repo-dependency-projection-runner:test-host:987655:0123456789abcdef0123456789abcdef/worker-0-of-4'
				return 0
				;;
			wrong-domain)
				printf 'documentation_materialization|0|4|%s/worker-0-of-4' "${owner_prefix}"
				return 0
				;;
			pre-only-mutation)
				printf '%s' "${expected_pre}"
				return 0
				;;
			esac
			[[ "$(<"${census_marker}")" -eq 1 ]] && printf '%s' "${expected_pre}" || printf '%s' "${expected_post}"
			return 0
		fi
		[[ "${sql}" == *"UPDATE shared_projection_partition_leases AS lease"* ]] || return 1
		printf '%s' "$(( $(<"${update_marker}") + 1 ))" >"${update_marker}"
		[[ "${sql}" == *"lease.projection_domain = captured.projection_domain"* ]] || return 1
		[[ "${sql}" == *"lease.partition_id = captured.partition_id"* ]] || return 1
		[[ "${sql}" == *"lease.partition_count = captured.partition_count"* ]] || return 1
		[[ "${sql}" == *"lease.lease_owner = captured.lease_owner"* ]] || return 1
		[[ "${sql}" != *"documentation_materialization"* && "${sql}" != *"unrelated-owner"* ]] || return 1
		printf '%s\n' \
			'documentation_materialization|same-owner|future' \
			'repo_dependency|unrelated-owner|future' >"${pg_marker}"
		printf '%s' "${expected_post}" >"${updated_marker}"
		if [[ "${mode}" == "pre-only-mutation" ]]; then
			printf '%s' "${expected_pre}" >"${updated_marker}"
			printf '2\n%s' "${expected_pre}"
			return 0
		fi
		while IFS='|' read -r domain partition_id partition_count owner; do
			[[ "${sql}" == *"('${domain}',${partition_id},${partition_count},'${owner}')"* ]] || return 1
		done <<<"${expected_post}"
		if [[ "${mode}" == "partial" ]]; then
			printf '3\n%s\n%s\n%s' "${first}" "${second}" "${third}"
		else
			printf '4\n%s' "${expected_post}"
		fi
	}

	ifa_repo_dependency_fault_capture_partition_leases test_cell "${dead_pid}" pre_census || return 1
	[[ "${pre_census}" == "${expected_pre}" ]] || return 1
	ifa_repo_dependency_fault_capture_partition_leases test_cell "${dead_pid}" post_census || return 1
	[[ "${post_census}" == "${expected_post}" ]] || return 1
	ifa_repo_dependency_fault_require_partition_lease_subset "${dead_pid}" "${pre_census}" "${post_census}" || return 1
	! ifa_repo_dependency_fault_require_partition_lease_subset "${dead_pid}" "${post_census}" "${pre_census}" || return 1
	ifa_repo_dependency_fault_expire_partition_leases test_cell "${dead_pid}" "${post_census}" || return 1
	[[ "$(<"${updated_marker}")" == "${expected_post}" && "$(<"${zero_marker}")" -eq 1 ]] || return 1
	[[ "$(<"${pg_marker}")" == $'documentation_materialization|same-owner|future\nrepo_dependency|unrelated-owner|future' ]] || return 1

	mode="zero"
	! ifa_repo_dependency_fault_capture_partition_leases test_cell "${dead_pid}" post_census || return 1
	mode="wrong-owner"
	! ifa_repo_dependency_fault_capture_partition_leases test_cell "${dead_pid}" post_census || return 1
	mode="wrong-domain"
	! ifa_repo_dependency_fault_capture_partition_leases test_cell "${dead_pid}" post_census || return 1
	mode="partial"
	! ifa_repo_dependency_fault_expire_partition_leases test_cell "${dead_pid}" "${expected_post}" || return 1
	mode="remaining"
	! ifa_repo_dependency_fault_expire_partition_leases test_cell "${dead_pid}" "${expected_post}" || return 1

	mode="pre-only-mutation"
	printf '0' >"${census_marker}"
	ifa_repo_dependency_fault_capture_partition_leases test_cell "${dead_pid}" pre_census || return 1
	! {
		ifa_repo_dependency_fault_expire_partition_leases test_cell "${dead_pid}" "${pre_census}" &&
			[[ "$(<"${updated_marker}")" == "${expected_post}" ]]
	} || return 1

	mode="lifecycle"
	rm -f "${pg_marker}"
	local other_domain="documentation_materialization|0|4|${owner_prefix}/worker-0-of-4"
	local duplicate="${first}"$'\n'"${first}"
	local malformed="repo_dependency|0|4|${owner_prefix}/worker-x-of-4"
	local quote_owner="repo_dependency|0|4|repo-dependency-projection-runner:test'host:${dead_pid}:0123456789abcdef0123456789abcdef/worker-0-of-4"
	local update_calls_before
	update_calls_before="$(<"${update_marker}")"
	! ifa_repo_dependency_fault_expire_partition_leases test_cell "${dead_pid}" "${other_domain}" || return 1
	! ifa_repo_dependency_fault_expire_partition_leases test_cell "${dead_pid}" "${duplicate}" || return 1
	! ifa_repo_dependency_fault_expire_partition_leases test_cell "${dead_pid}" "${malformed}" || return 1
	! ifa_repo_dependency_fault_expire_partition_leases test_cell "${dead_pid}" "${quote_owner}" || return 1
	[[ "$(<"${update_marker}")" -eq "${update_calls_before}" ]] || return 1
	[[ ! -e "${pg_marker}" ]] || return 1
	local live_owner="repo_dependency|0|4|repo-dependency-projection-runner:test-host:$$:0123456789abcdef0123456789abcdef/worker-0-of-4"
	! ifa_repo_dependency_fault_expire_partition_leases test_cell "$$" "${live_owner}" || return 1
	[[ "$(<"${update_marker}")" -eq "${update_calls_before}" ]] || return 1
	[[ ! -e "${pg_marker}" ]] || return 1
	local multi_owner="repo-dependency-projection-runner:test-host:${dead_pid}:0123456789abcdef0123456789abcdef"
	ifa_repo_dependency_fault_validate_partition_leases "${dead_pid}" \
		"repo_dependency|2|12|${multi_owner}/worker-2-of-12"$'\n'"repo_dependency|10|12|${multi_owner}/worker-10-of-12" || return 1
)
