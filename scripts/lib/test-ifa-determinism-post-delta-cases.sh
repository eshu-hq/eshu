#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# fail() and the ${script} path variable are defined by
# scripts/test-verify-ifa-determinism.sh before it sources this file through
# the family-cases module; shellcheck cannot see that from this file alone.
# Post-delta exact-set ordering cases for scripts/test-verify-ifa-determinism.sh,
# sourced so the family-cases module stays below the repository's 500-line cap
# (the same split the maintenance-backed families took into
# scripts/lib/test-ifa-determinism-maintenance-family-cases.sh once the
# family-cases file crossed the cap on its own). This module's
# run_ifa_determinism_post_delta_cases is called from inside
# run_ifa_determinism_family_cases, in the same narrative position after the
# per-family count pins.
#
# Takes the gate script path, the delta-drain call line, and the post-delta
# dump line as $1/$2/$3 -- all located by the caller, never re-derived here,
# so a locator regression reds in exactly one place.
run_ifa_determinism_post_delta_cases() {
	local script="$1" delta_call_line="$2" post_delta_dump_line="$3"
	# The nine DIRECT families (#6228/#6309) take the same single-post-delta-call
	# shape as the three above. All nine were asserted pre-delta ONLY when they
	# landed, and the matrix cannot see that gap: it compares one canonicalized
	# digest per N, so a generation-2 regression that retracts or mutates these
	# edges identically at N=1, 2 and 4 keeps every digest equal and the gate
	# green while the graph no longer matches the expected set. A count pin alone
	# would be satisfied by the pre-delta call moving out of the loop, so the
	# ordering check below is what says WHERE the surviving call has to be.
	for direct_assert_fn in ifa_kubernetes_namespace_environment_assert ifa_iam_instance_profile_role_assert ifa_iam_can_assume_assert ifa_iam_can_perform_assert ifa_workload_cloud_relationship_assert ifa_iam_escalation_assert ifa_ec2_uses_profile_assert ifa_s3_logs_to_assert ifa_kubernetes_correlation_assert; do
		# `|| true`, and the shape check that follows it, are load-bearing under
		# `set -e`: rg exits 1 on ZERO matches, so a bare command substitution
		# would abort this whole mirror with status 1 and print nothing at all --
		# the case this pin exists to report is exactly the one it would report
		# least. Proven by deleting the post-delta call and re-running.
		direct_assert_count="$(rg --count --fixed-strings -- "${direct_assert_fn} \"" "${script}" || true)"
		[[ "${direct_assert_count}" =~ ^[0-9]+$ ]] || direct_assert_count=0
		[[ "${direct_assert_count}" -eq 1 ]] \
			|| fail "expected exactly 1 occurrence of ${direct_assert_fn} (the post-delta re-assertion) outside the registry loop; found ${direct_assert_count} -- a second occurrence would double-assert this DIRECT family in every N-loop cell, and zero would leave generation 2 unchecked for it"
	done
	post_delta_ns_line="$(rg -n --fixed-strings -- 'ifa_kubernetes_namespace_environment_assert "post-delta N=${n}"' "${script}" | cut -d: -f1 || true)"
	post_delta_iam_line="$(rg -n --fixed-strings -- 'ifa_iam_instance_profile_role_assert "post-delta N=${n}"' "${script}" | cut -d: -f1 || true)"
	post_delta_can_line="$(rg -n --fixed-strings -- 'ifa_iam_can_assume_assert "post-delta N=${n}"' "${script}" | cut -d: -f1 || true)"
	post_delta_perform_line="$(rg -n --fixed-strings -- 'ifa_iam_can_perform_assert "post-delta N=${n}"' "${script}" | cut -d: -f1 || true)"
	post_delta_workload_line="$(rg -n --fixed-strings -- 'ifa_workload_cloud_relationship_assert "post-delta N=${n}"' "${script}" | cut -d: -f1 || true)"
	post_delta_escalation_line="$(rg -n --fixed-strings -- 'ifa_iam_escalation_assert "post-delta N=${n}"' "${script}" | cut -d: -f1 || true)"
	post_delta_ec2_line="$(rg -n --fixed-strings -- 'ifa_ec2_uses_profile_assert "post-delta N=${n}"' "${script}" | cut -d: -f1 || true)"
	post_delta_s3_line="$(rg -n --fixed-strings -- 'ifa_s3_logs_to_assert "post-delta N=${n}"' "${script}" | cut -d: -f1 || true)"
	post_delta_k8s_line="$(rg -n --fixed-strings -- 'ifa_kubernetes_correlation_assert "post-delta N=${n}"' "${script}" | cut -d: -f1 || true)"
	[[ "${post_delta_ns_line}" =~ ^[0-9]+$ && "${post_delta_iam_line}" =~ ^[0-9]+$ && "${post_delta_can_line}" =~ ^[0-9]+$ && "${post_delta_perform_line}" =~ ^[0-9]+$ && "${post_delta_workload_line}" =~ ^[0-9]+$ && "${post_delta_escalation_line}" =~ ^[0-9]+$ && "${post_delta_ec2_line}" =~ ^[0-9]+$ && "${post_delta_s3_line}" =~ ^[0-9]+$ && "${post_delta_k8s_line}" =~ ^[0-9]+$ \
		&& "${delta_call_line}" -lt "${post_delta_ns_line}" \
		&& "${post_delta_ns_line}" -lt "${post_delta_dump_line}" \
		&& "${delta_call_line}" -lt "${post_delta_iam_line}" \
		&& "${post_delta_iam_line}" -lt "${post_delta_dump_line}" \
		&& "${delta_call_line}" -lt "${post_delta_can_line}" \
		&& "${post_delta_can_line}" -lt "${post_delta_dump_line}" \
		&& "${delta_call_line}" -lt "${post_delta_perform_line}" \
		&& "${post_delta_perform_line}" -lt "${post_delta_dump_line}" \
		&& "${delta_call_line}" -lt "${post_delta_workload_line}" \
		&& "${post_delta_workload_line}" -lt "${post_delta_dump_line}" \
		&& "${delta_call_line}" -lt "${post_delta_escalation_line}" \
		&& "${post_delta_escalation_line}" -lt "${post_delta_dump_line}" \
		&& "${delta_call_line}" -lt "${post_delta_ec2_line}" \
		&& "${post_delta_ec2_line}" -lt "${post_delta_dump_line}" \
		&& "${delta_call_line}" -lt "${post_delta_s3_line}" \
		&& "${post_delta_s3_line}" -lt "${post_delta_dump_line}" \
		&& "${delta_call_line}" -lt "${post_delta_k8s_line}" \
		&& "${post_delta_k8s_line}" -lt "${post_delta_dump_line}" ]] \
		|| fail "every N cell must exact-assert all nine DIRECT families (kubernetes_namespace_environment, iam_instance_profile_role, iam_can_assume, iam_can_perform, workload_cloud_relationship, iam_escalation, ec2_uses_profile, s3_logs_to, kubernetes_correlation) AFTER the shared delta drain and BEFORE its graph dump -- asserted only pre-delta, an identical-across-N generation-2 mutation leaves every digest equal and the matrix green"
}
