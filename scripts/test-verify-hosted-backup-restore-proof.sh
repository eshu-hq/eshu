#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-hosted-backup-restore-proof.sh"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

write_valid_proof() {
	local path="$1"
	jq -n --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
		{
			schema_version: 1,
			proof_id: "hosted-backup-restore-test",
			generated_at: $generated_at,
			mode: "clean_restore",
			backup: {
				artifact_handle: "backup-generation-20260609",
				age_seconds: 600,
				checksum_present: true,
				encrypted: true
			},
			restore: {
				status: "succeeded",
				duration_seconds: 92,
				failure_class: "none",
				target_scope_class: "isolated_restore_environment"
			},
			graph_rebuild: {
				status: "succeeded",
				postgres_preserved: true,
				schema_bootstrap_rerun: true,
				projection_replayed: true,
				full_recollection_explicit: false
			},
			parity: {
				status: "match",
				drift_count: 0
			},
			queue: {
				pending: 0,
				retrying: 0,
				failed: 0,
				dead_letter: 0
			},
			readback: {
				api_status: "pass",
				mcp_status: "pass",
				first_query_status: "pass"
			},
			security: {
				artifact_contents_platform_owned: true,
				secret_scan: "passed",
				private_locator_scan: "passed"
			}
		}
	' >"${path}"
}

expect_pass() {
	local label="$1"
	local input="$2"
	local out_json="${tmp_dir}/${label}-summary.json"
	local out_md="${tmp_dir}/${label}-summary.md"
	if ! "${verifier}" --input "${input}" --output-json "${out_json}" --output-markdown "${out_md}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to pass\n' "${label}" >&2
		sed -n '1,120p' "${tmp_dir}/${label}.err" >&2
		exit 1
	fi
	jq -e '
		.status == "pass" and
		.proof_id == "hosted-backup-restore-test" and
		.backup.age_seconds == 600 and
		.restore.duration_seconds == 92 and
		.queue.dead_letter == 0 and
		.readback.api_status == "pass"
	' "${out_json}" >/dev/null || {
		printf 'expected %s output to preserve public-safe proof fields\n' "${label}" >&2
		jq . "${out_json}" >&2
		exit 1
	}
	rg --fixed-strings --quiet 'Hosted backup and restore proof' "${out_md}" \
		|| { printf 'expected markdown summary for %s\n' "${label}" >&2; exit 1; }
}

expect_fail() {
	local label="$1"
	local input="$2"
	local expected="$3"
	local out_json="${tmp_dir}/${label}-summary.json"
	local out_md="${tmp_dir}/${label}-summary.md"
	if "${verifier}" --input "${input}" --output-json "${out_json}" --output-markdown "${out_md}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	rg --fixed-strings --quiet -- "${expected}" "${tmp_dir}/${label}.err" \
		|| { printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2; sed -n '1,120p' "${tmp_dir}/${label}.err" >&2; exit 1; }
}

valid="${tmp_dir}/valid.json"
write_valid_proof "${valid}"
expect_pass valid "${valid}"

explicit_recollection="${tmp_dir}/explicit-recollection.json"
jq '.graph_rebuild.postgres_preserved = false | .graph_rebuild.full_recollection_explicit = true' "${valid}" >"${explicit_recollection}"
expect_pass explicit_recollection "${explicit_recollection}"

stale="${tmp_dir}/stale.json"
jq '.backup.age_seconds = 864001' "${valid}" >"${stale}"
expect_fail stale "${stale}" "backup artifact is stale"

missing_artifact="${tmp_dir}/missing-artifact.json"
jq 'del(.backup.artifact_handle)' "${valid}" >"${missing_artifact}"
expect_fail missing_artifact "${missing_artifact}" "backup artifact handle is required"

corrupt_artifact="${tmp_dir}/corrupt-artifact.json"
printf '{not-json\n' >"${corrupt_artifact}"
expect_fail corrupt_artifact "${corrupt_artifact}" "input must be valid JSON"

partial_restore="${tmp_dir}/partial-restore.json"
jq '.restore.status = "failed" | .restore.failure_class = "partial_restore"' "${valid}" >"${partial_restore}"
expect_fail partial_restore "${partial_restore}" "restore did not succeed"

graph_loss="${tmp_dir}/graph-loss.json"
jq '.mode = "graph_only_loss" | .graph_rebuild.postgres_preserved = false' "${valid}" >"${graph_loss}"
expect_fail graph_loss "${graph_loss}" "graph rebuild must preserve Postgres"

queue_backlog="${tmp_dir}/queue-backlog.json"
jq '.queue.retrying = 1' "${valid}" >"${queue_backlog}"
expect_fail queue_backlog "${queue_backlog}" "queue terminal state is not zero"

parity_drift="${tmp_dir}/parity-drift.json"
jq '.parity.status = "drift" | .parity.drift_count = 2' "${valid}" >"${parity_drift}"
expect_fail parity_drift "${parity_drift}" "restore parity drift is not acceptable"

missing_readback="${tmp_dir}/missing-readback.json"
jq '.readback.mcp_status = "missing"' "${valid}" >"${missing_readback}"
expect_fail missing_readback "${missing_readback}" "API and MCP readback must pass"

private_locator="${tmp_dir}/private-locator.json"
jq '.restore.url = "redacted"' "${valid}" >"${private_locator}"
expect_fail private_locator "${private_locator}" "input looks like private data"

printf 'hosted backup/restore proof verifier tests passed\n'
