#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the cloud_resource DIRECT-materialization
# family (#8358).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND, since #6309, the fault
# gate through scripts/lib/ifa_fault_injection_sources.sh — always right after
# scripts/lib/ifa_direct_family_live.sh, whose _ifa_direct_family_drive helper
# below reuses. The pair split once the shared file crossed the 500-line cap;
# the per-family REGISTRY ROWS stay separate, which is where the split that
# matters already is. Callers own strict mode and cleanup.
#
# The assert spells its `-domain cloud_resource` flag out literally rather
# than taking the domain as a parameter. That is deliberate: the domain is the
# one thing a shared helper must not abstract away, because it is what makes
# this family's assertion distinguishable from its siblings', and
# scripts/lib/test-ifa-determinism-family-cases.sh greps this family's own
# flag out of this file to prove the wiring exists. A parameterized call
# would let one family's coverage stand in for another's and satisfy that
# check with a single needle.

# ifa_cloud_resource_drive replays the cloud-resource cassette (three scope
# generations, one per provider writer, in one drive).
ifa_cloud_resource_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive cloud-resource \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_cloud_resource_assert pins the four-edge exact set through the
# predicate-scoped scan (#8358): two AWS verbs off one Lambda (duplicate,
# unresolved, and self-loop controls contribute nothing), one supported GCP
# relationship (partial/unresolved controls contribute nothing), one Azure
# managed_by (depends_on is screened out). Types are the writers' prefix
# consts + verb, NOT the AWS_/GCP_/AZURE_RELATIONSHIP metadata labels.
ifa_cloud_resource_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert cloud_resource materialized edges (four-edge exact set) ===\n' "${label}"
	# Scoped to the cassette's three scope ids (#8358): the shared cell
	# graph also holds ambient GCP edges with the same (prefix, stamp,
	# endpoints) shape, which the unscoped predicate scan counts as extras.
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain cloud_resource \
		-expected "${expected_edges}" \
		-scopes "aws:eshu-fixture-cloud-resource-account,gcp:project:eshu-fixture-project,azure:eshu-fixture-tenant:subscription:eshu-fixture-rg"
}
