#!/usr/bin/env bash

# public_collector_proof_assertions.sh
#
# Per-source readback assertion helpers for verify_local_public_collector_proof.sh.
# Sourced after the shared compose_verification_runtime_common.sh and after
# api_get / PROOF_* variables are set by the caller.
#
# Public-safe contract: every function prints only aggregate counts or boolean
# present/absent labels. No advisory keys, CVE ids, package names, scores, or
# raw provider locators appear in the output.

# assert_kev_evidence proves CISA KEV data landed by asserting the advisory
# catalog returns at least one row when filtered to kev=true. Prints an
# aggregate-only count line on success.
assert_kev_evidence() {
	local kev_file="${TMP_DIR}/kev-assert.json"
	if ! api_get "/supply-chain/advisories?limit=1&kev=true" "${kev_file}" 2>/dev/null; then
		echo "KEV source assertion: could not reach advisory catalog" >&2
		return 1
	fi
	local kev_count
	kev_count="$(jq -r '(.count // 0)' "${kev_file}")"
	if ! [[ "${kev_count}" =~ ^[1-9][0-9]*$ ]]; then
		echo "KEV source assertion failed: advisory catalog returned 0 KEV-flagged rows" >&2
		return 1
	fi
	echo "  KEV source: ${kev_count} advisory catalog row(s) with kev=true"
}

# assert_epss_evidence proves FIRST EPSS data landed for the configured CVE
# probe by looking up its advisory detail and asserting the epss array is
# non-empty. Prints an aggregate-only observation count on success.
assert_epss_evidence() {
	local detail_file="${TMP_DIR}/epss-assert.json"
	if ! api_get "/supply-chain/vulnerabilities/${PROOF_EPSS_CVE_ID}" "${detail_file}" 2>/dev/null; then
		echo "EPSS source assertion: advisory not found for the configured probe CVE" >&2
		return 1
	fi
	local epss_count
	epss_count="$(jq -r '(.epss // []) | length' "${detail_file}")"
	if ! [[ "${epss_count}" =~ ^[1-9][0-9]*$ ]]; then
		echo "EPSS source assertion failed: advisory detail has no EPSS observations for the probe CVE" >&2
		return 1
	fi
	echo "  EPSS source: ${epss_count} EPSS observation(s) on probe advisory"
}

# assert_osv_evidence proves OSV data landed by querying npm-ecosystem advisories
# and asserting at least one row carries the "osv" source label. Prints an
# aggregate-only matched-row count on success; no advisory keys or package names
# are printed.
assert_osv_evidence() {
	local osv_file="${TMP_DIR}/osv-assert.json"
	if ! api_get "/supply-chain/advisories?limit=5&ecosystem=npm" "${osv_file}" 2>/dev/null; then
		echo "OSV source assertion: could not reach advisory catalog (npm ecosystem)" >&2
		return 1
	fi
	local osv_count
	osv_count="$(jq -r '[.advisories[]? | select((.sources // []) | index("osv"))] | length' "${osv_file}")"
	if ! [[ "${osv_count}" =~ ^[1-9][0-9]*$ ]]; then
		echo "OSV source assertion failed: no npm advisory rows carry the osv source label" >&2
		return 1
	fi
	echo "  OSV source: ${osv_count} npm advisory row(s) with osv source label"
}
