#!/usr/bin/env bash
set -euo pipefail

# Self-test for the go-reachability remote e2e proof verifier (#10031). It
# proves the verifier is well-formed, that it passes a good proof-artifact
# set, and that it fails closed on every single-fault mutation: an unpinned
# seed image, an untrusted component, a retrying or failed work item, a
# fact count that drifts from expected.json, a flipped reachability verdict
# on either surface, a dropped or rewritten missing_evidence entry, an
# incomplete provenance record, and leaked host-local material. It also
# anchors the committed expected.json to the golden snapshot pins, so the
# file the verifier diffs against cannot drift from the real contract.
# This runs locally with no Compose stack and no live backend; the live run
# that produces real artifacts is the operator/CI gate.
#
# Seeded-value note: the 9229 wire values (7.5, cvss_v3_high, high label)
# are the golden snapshot pins. The 9301/9401 cvss 5.3 + medium label mirror
# the committed cassette advisory vectors
# (CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N, CVSS 3.1 derivation); the
# verifier asserts their presence, not their magnitude, per the snapshot.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-remote-e2e-go-reachability.sh"
fixtures="${repo_root}/tests/fixtures/go_reachability_proof"
snapshot="${repo_root}/testdata/golden/e2e-20repo-snapshot.json"

die() {
	printf 'test-verify-remote-e2e-go-reachability: %s\n' "$*" >&2
	exit 1
}

[[ -f "${verifier}" ]] || die "missing verifier: ${verifier}"
bash -n "${verifier}" || die "verifier failed bash syntax check"
command -v jq >/dev/null 2>&1 || die "jq is required"

# --list names every proof check without running anything.
list_log="$(bash "${verifier}" --list)"
for needle in "inventory:" "deployed API/MCP:" "workflow:" "facts:" "provenance:" "redaction canary:"; do
	rg --fixed-strings --quiet "${needle}" < <(printf '%s\n' "${list_log}") \
		|| die "--list output missing ${needle}"
done

# The committed expectation must exist and stay anchored to the golden
# snapshot pins it mirrors: same per-CVE verdicts and the same
# missing_evidence entry, read from the snapshot itself.
[[ -f "${fixtures}/expected.json" ]] || die "missing expected.json"
for cve_id in CVE-2026-9229 CVE-2026-9301 CVE-2026-9401; do
	shape="GET /api/v0/supply-chain/impact/findings?limit=10&cve_id=${cve_id}&profile=comprehensive"
	snap_verdict="$(jq -r --arg s "${shape}" '.query_shapes.http[$s].required_json_values["findings[].runtime_reachability"]' "${snapshot}")"
	exp_verdict="$(jq -r --arg c "${cve_id}" '.findings[] | select(.cve_id == $c) | .required_values["findings[].runtime_reachability"]' "${fixtures}/expected.json")"
	[[ -n "${snap_verdict}" && "${snap_verdict}" != "null" ]] || die "snapshot has no pin for ${cve_id}"
	[[ "${snap_verdict}" == "${exp_verdict}" ]] || die "expected.json verdict for ${cve_id} (${exp_verdict}) drifted from snapshot (${snap_verdict})"
done
snap_gap="$(jq -r '.query_shapes.http["GET /api/v0/supply-chain/impact/findings?limit=10&cve_id=CVE-2026-9401&profile=comprehensive"].required_json_values["findings[].reachability.missing_evidence[]"]' "${snapshot}")"
exp_gap="$(jq -r '.warning.missing_evidence' "${fixtures}/expected.json")"
[[ "${snap_gap}" == "${exp_gap}" ]] || die "expected.json missing_evidence (${exp_gap}) drifted from snapshot (${snap_gap})"

# Good artifacts pass.
bash "${verifier}" --artifacts "${fixtures}/good" >/dev/null \
	|| die "verifier rejected the good proof artifacts"

# Every bad artifact set is a single-fault mutation of good/ built with jq
# here, so each RED proves exactly one check is load-bearing.
mutate() {
	local name="$1"
	local filter="$2"
	local file="$3"
	local dir
	dir="$(mktemp -d "${TMPDIR:-/tmp}/go-reachability-bad.XXXXXX")"
	cp "${fixtures}/good/"*.json "${dir}/"
	jq "${filter}" "${dir}/${file}" >"${dir}/${file}.tmp"
	mv "${dir}/${file}.tmp" "${dir}/${file}"
	printf '%s' "${dir}"
}

expect_red() {
	local dir="$1"
	local why="$2"
	if bash "${verifier}" --artifacts "${dir}" >/dev/null 2>&1; then
		rm -rf "${dir}"
		die "verifier accepted ${why}"
	fi
	rm -rf "${dir}"
}

# A tag-pinned seed image (no digest) must fail the inventory digest pin.
expect_red "$(mutate bad_digest '.seed_image = "ghcr.io/eshu-hq/scanner-go-reachability:latest" | .seed_digest = "unknown"' inventory.json)" "a tag-pinned seed image"
# An untrusted component must fail the inventory trust pin.
expect_red "$(mutate bad_trust '.trusted = false' inventory.json)" "an untrusted component"
# A retrying work item must fail the workflow terminal pin.
expect_red "$(mutate bad_retry '.items[0].state = "retrying"' workflow-items.json)" "a retrying work item"
# A failed work item must fail the workflow terminal pin.
expect_red "$(mutate bad_failed '.items[0].state = "failed"' workflow-items.json)" "a failed work item"
# A drifted fact count must fail the facts-vs-expected pin.
expect_red "$(mutate bad_facts '.["vulnerability.go_call_reachability"] = 1' facts.json)" "a drifted fact count"
# A flipped verdict on either surface must fail that surface's finding pin.
expect_red "$(mutate bad_api '.data.findings[0].runtime_reachability = "not_called"' api-findings-cve_2026_9229.json)" "a flipped API verdict"
expect_red "$(mutate bad_mcp '.data.findings[0].runtime_reachability = "symbol_reachable"' mcp-findings-cve_2026_9301.json)" "a flipped MCP verdict"
# A rewritten missing_evidence state must fail the #10043 mapping pin.
expect_red "$(mutate bad_state '.data.findings[0].reachability.state = "reachable"' api-findings-cve_2026_9401.json)" "a rewritten missing_evidence state"
# A dropped missing_evidence entry must fail the #10043 mapping pin.
expect_red "$(mutate bad_gap 'del(.data.findings[0].reachability.missing_evidence)' mcp-findings-cve_2026_9401.json)" "a dropped missing_evidence entry"
# A pre-projection empty findings page with the readiness signal must
# pass: #10031 verifies collection truth, projected findings belong to
# #10032 (arbiter verdict b). The empty page keeps its envelope, fields,
# and the readiness_unavailable missing_evidence entry.
empty_dir="$(mktemp -d "${TMPDIR:-/tmp}/go-reachability-empty.XXXXXX")"
cp "${fixtures}/good/"*.json "${empty_dir}/"
for f in "${empty_dir}"/api-findings-*.json "${empty_dir}"/mcp-findings-*.json; do
	jq '.data.findings = [] | .data.count = 0 | .data.readiness = {readiness_state: "readiness_unavailable", missing_evidence: ["readiness_unavailable"]}' \
		"${f}" >"${f}.tmp"
	mv "${f}.tmp" "${f}"
done
bash "${verifier}" --artifacts "${empty_dir}" >/dev/null \
	|| die "verifier rejected the pre-projection empty-findings page"
rm -rf "${empty_dir}"
# An empty findings page WITHOUT the readiness signal must fail: silence
# without the missing_evidence entry proves nothing.
expect_red "$(mutate bad_silent '.data.findings = [] | .data.count = 0 | del(.data.readiness)' api-findings-cve_2026_9229.json)" "a silent empty findings page"
# An unknown commit must fail the provenance pin.
expect_red "$(mutate bad_provenance '.eshu_commit = "unknown"' provenance.json)" "an unknown provenance commit"
# Leaked host-local material must trip the redaction canary.
expect_red "$(mutate bad_leak '.metrics_handle = "/home/operator/.eshu/metrics"' provenance.json)" "leaked host-local material"

printf 'go-reachability proof verifier self-test passed\n'
