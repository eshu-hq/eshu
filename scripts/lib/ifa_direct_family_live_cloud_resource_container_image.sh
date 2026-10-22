#!/usr/bin/env bash
# shellcheck shell=bash
# Live-gate drive/assert callbacks for the cloud_resource_container_image
# DIRECT-materialization family (#6228 slice D live leaf).
#
# SOURCED BY scripts/verify-ifa-determinism.sh AND, since #6309, the fault
# gate through scripts/lib/ifa_fault_injection_sources.sh -- always right
# after scripts/lib/ifa_direct_family_live_built_from.sh, whose
# _ifa_direct_family_drive helper below reuses. Split out rather than
# appended there because that file sits at the tooling line cap; the
# per-family REGISTRY ROWS stay separate, which is where the split that
# matters already is. Callers own strict mode and cleanup.
#
# The assert spells its `-domain cloud_resource_container_image` flag out
# literally rather than taking the domain as a parameter. That is
# deliberate: the domain is the one thing a shared helper must not abstract
# away, because it is what makes this family's assertion distinguishable
# from its siblings'. A parameterized call would let one family's coverage
# stand in for another's.

# ifa_cloud_resource_container_image_drive replays the image cassette.
ifa_cloud_resource_container_image_drive() {
	local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"
	_ifa_direct_family_drive cloud-resource-container-image \
		"${label}" "${bin_dir}" "${cassette}" "${workers}" "${log_dir}"
}

# ifa_cloud_resource_container_image_assert pins the two-edge exact set.
# Called twice per cell for the reason recorded on the namespace assert in
# ifa_direct_family_live.sh: post-delta is the only place an
# identical-across-N generation-2 mutation shows up.
#
# The two edges are the exact-digest resolutions: fn-demo and fn-worker
# resolve the same registry+repository@digest reference (two Lambda
# functions, one shared image -- the dedup key is (source, target) so one
# shared image fans out to one edge per function). A regression that
# promoted every resolved reference to an edge regardless of digest
# resolvability would still produce "some edges" and fail only against an
# exact set. The fixture's other five relationships -- the tag-only ECS
# task-definition reference (EXACT-ONLY policy: stays Postgres-only), the
# zip-packaged function with no resolved digest, the tag-only resolved
# reference unparseable as a digest ref, the unscanned ghost function that
# resolves no source endpoint, and the foreign uses_kms_key verb outside
# this domain's closed vocabulary -- must contribute nothing; the writer
# screens each row against its closed single-member vocabulary and drops a
# row whose source or target node is absent rather than inventing an edge,
# and this set is what holds it to that.
#
# The relationship type is AWS_lambda_function_uses_image, read off the
# image writer's upsert template filled per row from the closed
# single-member vocabulary. It is NOT derived from the port or family name,
# and NOT AWS_LAMBDA_FUNCTION_USES_IMAGE, the writer's statement metadata.
ifa_cloud_resource_container_image_assert() {
	local label="$1" bin_dir="$2" expected_edges="$3"
	printf '\n=== %s: assert cloud_resource_container_image materialized edges (two-edge exact set) ===\n' "${label}"
	"${bin_dir}/eshu-ifa" assert-edges \
		-domain cloud_resource_container_image \
		-expected "${expected_edges}"
}
