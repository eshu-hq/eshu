#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# Direct-leaf static structural cases for
# scripts/test-verify-ifa-determinism.sh, split out of
# scripts/lib/test-ifa-determinism-family-cases.sh to keep that file below
# the repository's 500-line cap (mirroring that file's own extraction of
# test-ifa-determinism-maintenance-family-cases.sh). Covers the #6228 slice
# C/D/crossplane/derived_from/code-interproc/code-taint/observability-coverage/incident-routing live leaves -- built_from, cloud_resource_container_image, crossplane_satisfied_by, derived_from, code_interproc_evidence, code_taint_evidence, observability_coverage and incident_routing_evidence -- whose
# drive/assert callbacks live in their own split live libs under the tooling
# line cap. run_ifa_determinism_direct_leaf_cases is called from inside
# run_ifa_determinism_family_cases's body, in the same position this code
# used to occupy inline, so the slice C/D narrative still reads in
# family-lifecycle order between uses_vault_policy and the vault-family
# helper definition immediately after it.
run_ifa_determinism_direct_leaf_cases() {
require_direct_family_lib "built-from assert-edges domain" "-domain built_from"
require_direct_family_lib "built-from drive takes the labeled signature" 'local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"'
require_direct_family_lib "cloud-resource-container-image assert-edges domain" "-domain cloud_resource_container_image"
require_direct_family_lib "crossplane-satisfied-by assert-edges domain" "-domain crossplane_satisfied_by"
require_direct_family_lib "code-interproc-evidence assert-edges domain" "-domain code_interproc_evidence"
require_direct_family_lib "cloud-resource-container-image drive takes the labeled signature" 'local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"'
require_direct_family_lib "crossplane-satisfied-by drive takes the labeled signature" 'local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"'
require_direct_family_lib "derived-from assert-edges domain" "-domain derived_from"
require_direct_family_lib "derived-from drive takes the labeled signature" 'local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"'
require_direct_family_lib "observability-coverage assert-edges domain" "-domain observability_coverage"
require_direct_family_lib "observability-coverage drive takes the labeled signature" 'local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"'
require_direct_family_lib "code-interproc-evidence drive takes the labeled signature" 'local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"'
require_direct_family_lib "code-taint-evidence assert-edges domain" "-domain code_taint_evidence"
require_direct_family_lib "code-taint-evidence drive takes the labeled signature" 'local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"'
require_direct_family_lib "incident-routing-evidence assert-edges domain" "-domain incident_routing_evidence"
require_direct_family_lib "incident-routing-evidence drive takes the labeled signature" 'local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"'
}
