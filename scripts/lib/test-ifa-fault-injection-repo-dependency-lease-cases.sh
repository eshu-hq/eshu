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
	# The SQL owner pattern must accept both shapes the product emits: the
	# serial owner (no suffix) and the sharded owner (/worker-N-of-M).
	local owner_sql_re=":${dead_pid}:[0-9a-f]{32}(/worker-([0-9]+)-of-([1-9][0-9]*))?\$'"
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
			[[ "${sql}" == *"${owner_sql_re}"* ]] || return 1
			[[ "${mode}" == "remaining" || "${mode}" == "pre-only-mutation" ]] && printf '2' || printf '0'
			return 0
		fi
		if [[ "${sql}" == *"SELECT COALESCE(string_agg("* ]]; then
			printf '%s' "$(( $(<"${census_marker}") + 1 ))" >"${census_marker}"
			[[ "${sql}" == *"projection_domain = 'repo_dependency'"* ]] || return 1
			[[ "${sql}" == *"lease_expires_at > CURRENT_TIMESTAMP"* ]] || return 1
			[[ "${sql}" == *"${owner_sql_re}"* ]] || return 1
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
		done < <(printf '%s\n' "${expected_post}")  # no <<<: deadlocks on bash >= 5.3
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

# Neo4j runs the repo_dependency projection with one worker by default, so its
# lease owner has no /worker-N-of-M suffix and the row is partition 0 of 1. The
# harness must accept that serial shape and the sharded shape, tie each shape
# to its partition columns, and refuse a snapshot that mixes the two.
run_ifa_repo_dependency_lease_owner_shapes() (
	local pid=987654 other_pid=987655
	local prefix="repo-dependency-projection-runner:test-host:${pid}:0123456789abcdef0123456789abcdef"
	local other_prefix="repo-dependency-projection-runner:test-host:${other_pid}:0123456789abcdef0123456789abcdef"
	local serial="repo_dependency|0|1|${prefix}"
	local sharded0="repo_dependency|0|4|${prefix}/worker-0-of-4"
	local sharded1="repo_dependency|1|4|${prefix}/worker-1-of-4"
	local sharded3="repo_dependency|3|4|${prefix}/worker-3-of-4"
	local fail_count=0

	expect_valid() {
		ifa_repo_dependency_fault_validate_partition_leases "${pid}" "$2" || {
			printf 'lease-shape case %s: want accepted, got rejected\n' "$1" >&2
			fail_count=$((fail_count + 1))
		}
	}
	expect_invalid() {
		if ifa_repo_dependency_fault_validate_partition_leases "${3:-${pid}}" "$2"; then
			printf 'lease-shape case %s: want rejected, got accepted\n' "$1" >&2
			fail_count=$((fail_count + 1))
		fi
	}

	expect_valid serial-0-of-1 "${serial}"
	expect_valid sharded-2-workers "${sharded0}"$'\n'"${sharded1}"
	expect_valid sharded-4-workers "${sharded0}"$'\n'"${sharded1}"$'\n'"repo_dependency|2|4|${prefix}/worker-2-of-4"$'\n'"${sharded3}"
	expect_valid sharded-single-row "${sharded3}"
	expect_invalid mixed-serial-and-sharded "${serial}"$'\n'"${sharded0}"
	expect_invalid mixed-sharded-then-serial "${sharded1}"$'\n'"${serial}"
	expect_invalid serial-partition-1-of-1 "repo_dependency|1|1|${prefix}"
	expect_invalid serial-partition-0-of-4 "repo_dependency|0|4|${prefix}"
	expect_invalid serial-partition-0-of-2 "repo_dependency|0|2|${prefix}"
	expect_invalid sharded-id-disagrees-with-suffix "repo_dependency|1|4|${prefix}/worker-0-of-4"
	expect_invalid sharded-count-disagrees-with-suffix "repo_dependency|0|2|${prefix}/worker-0-of-4"
	expect_invalid sharded-worker-out-of-range "repo_dependency|4|4|${prefix}/worker-4-of-4"
	expect_invalid sharded-zero-workers "repo_dependency|0|0|${prefix}/worker-0-of-0"
	expect_invalid sharded-worker-counts-differ "${sharded0}"$'\n'"repo_dependency|1|2|${prefix}/worker-1-of-2"
	expect_invalid duplicate-serial "${serial}"$'\n'"${serial}"
	expect_invalid duplicate-sharded "${sharded0}"$'\n'"${sharded0}"
	expect_invalid wrong-pid-serial "repo_dependency|0|1|${other_prefix}"
	expect_invalid wrong-pid-sharded "repo_dependency|0|4|${other_prefix}/worker-0-of-4"
	expect_invalid wrong-domain-serial "documentation_materialization|0|1|${prefix}"
	expect_invalid wrong-prefix "repo_dependency|0|1|other-runner:test-host:${pid}:0123456789abcdef0123456789abcdef"
	expect_invalid short-nonce "repo_dependency|0|1|repo-dependency-projection-runner:test-host:${pid}:0123456789abcdef"
	expect_invalid trailing-junk-after-serial "repo_dependency|0|1|${prefix}/extra"
	expect_invalid malformed-suffix "repo_dependency|0|4|${prefix}/worker-x-of-4"
	expect_invalid empty-snapshot ""
	expect_invalid zero-pid "${serial}" 0

	# End to end on the serial shape: capture, census subset and expiry must
	# match the exact row a one-worker reducer holds, quarantine included (the
	# quarantine keeps the same owner row, it never rewrites it).
	# shellcheck disable=SC2034 # read by the sourced capture/expire helpers
	local FAULT_COMPOSE_PROJECT=test use_compose=0 ESHU_POSTGRES_DSN=test compose_file=test
	local sql_re=":${pid}:[0-9a-f]{32}(/worker-([0-9]+)-of-([1-9][0-9]*))?\$'"
	local captured="" census_remaining=0 update_marker
	update_marker="$(mktemp)" || return 1
	trap 'rm -f "${update_marker}"' EXIT
	: >"${update_marker}"
	ifa_det_pg() {
		local sql="$4"
		if [[ "${sql}" == "SELECT count(*) FROM shared_projection_partition_leases"* ]]; then
			[[ "${sql}" == *"projection_domain = 'repo_dependency'"* && "${sql}" == *"${sql_re}"* ]] || return 1
			printf '%s' "${census_remaining}"
			return 0
		fi
		if [[ "${sql}" == *"SELECT COALESCE(string_agg("* ]]; then
			[[ "${sql}" == *"projection_domain = 'repo_dependency'"* && "${sql}" == *"${sql_re}"* ]] || return 1
			printf '%s' "${serial}"
			return 0
		fi
		[[ "${sql}" == *"UPDATE shared_projection_partition_leases AS lease"* ]] || return 1
		[[ "${sql}" == *"('repo_dependency',0,1,'${prefix}')"* ]] || return 1
		printf 'x' >>"${update_marker}"
		printf '1\n%s' "${serial}"
	}
	ifa_repo_dependency_fault_capture_partition_leases serial_cell "${pid}" captured || fail_count=$((fail_count + 1))
	[[ "${captured}" == "${serial}" ]] || { printf 'lease-shape case serial-capture: got %q\n' "${captured}" >&2; fail_count=$((fail_count + 1)); }
	ifa_repo_dependency_fault_require_partition_lease_subset "${pid}" "${captured}" "${captured}" || fail_count=$((fail_count + 1))
	ifa_repo_dependency_fault_expire_partition_leases serial_cell "${pid}" "${captured}" || {
		printf 'lease-shape case serial-expire: rejected\n' >&2
		fail_count=$((fail_count + 1))
	}
	[[ "$(<"${update_marker}")" == "x" ]] || { printf 'lease-shape case serial-expire: no UPDATE issued\n' >&2; fail_count=$((fail_count + 1)); }
	census_remaining=1
	if ifa_repo_dependency_fault_assert_no_active_partition_leases serial_cell "${pid}" 2>/dev/null; then
		printf 'lease-shape case serial-census-remaining: want rejected, got accepted\n' >&2
		fail_count=$((fail_count + 1))
	fi
	[[ "${fail_count}" -eq 0 ]]
)
