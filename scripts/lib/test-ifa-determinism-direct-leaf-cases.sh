#!/usr/bin/env bash
# shellcheck disable=SC2034,SC2154
# Direct-leaf static structural cases for
# scripts/test-verify-ifa-determinism.sh, split out of
# scripts/lib/test-ifa-determinism-family-cases.sh to keep that file below
# the repository's 500-line cap (mirroring that file's own extraction of
# test-ifa-determinism-maintenance-family-cases.sh). Covers the #6228 slice
# C/D live leaves -- built_from and cloud_resource_container_image -- whose
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
require_direct_family_lib "cloud-resource-container-image drive takes the labeled signature" 'local label="$1" bin_dir="$2" cassette="$3" workers="$4" log_dir="$5"'
}
