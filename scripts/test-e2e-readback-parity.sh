#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
runner="${repo_root}/scripts/e2e_readback_parity.sh"
release_gate="${repo_root}/scripts/security_intelligence_release_gate.sh"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

write_valid_input() {
	local path="$1"
	jq -n '
		def surface($count; $truncated; $missing; $unsupported; $ambiguous; $state): {
			status: "pass",
			truth_level: "canonical",
			truth_profile: "production",
			readiness_state: $state,
			count: $count,
			truncated: $truncated,
			missing_evidence: $missing,
			unsupported: $unsupported,
			ambiguous: $ambiguous,
			missing_reason: (if $missing > 0 then "source_not_configured" else "" end),
			unsupported_reason: (if $unsupported > 0 then "capability_not_supported" else "" end),
			ambiguity_reason: (if $ambiguous > 0 then "selector_matched_multiple" else "" end)
		};
		def check($domain; $name; $count; $truncated; $missing; $unsupported; $ambiguous; $state): {
			domain: $domain,
			name: $name,
			limit: 1,
			timeout_seconds: 30,
			surfaces: {
				api: surface($count; $truncated; $missing; $unsupported; $ambiguous; $state),
				mcp: surface($count; $truncated; $missing; $unsupported; $ambiguous; $state),
				cli: surface($count; $truncated; $missing; $unsupported; $ambiguous; $state)
			}
		};
		{
			schema_version: 1,
			proof_id: "readback-parity-test-v1",
			transcript_status: "captured",
			queue: {retrying: 0, failed: 0, dead_letters: 0},
			checks: [
				check("repository"; "summary"; 3; false; 0; 0; 0; "ready"),
				check("package"; "list"; 2; true; 0; 0; 0; "ready"),
				check("cloud"; "detail"; 1; false; 0; 0; 0; "ready"),
				check("deployment"; "coverage"; 1; false; 0; 0; 0; "ready"),
				check("vulnerability"; "missing-evidence"; 0; false; 1; 0; 0; "missing_evidence"),
				check("sbom_image"; "attachment"; 1; false; 0; 0; 0; "ready"),
				check("observability"; "unsupported"; 0; false; 0; 1; 0; "unsupported"),
				check("incident"; "context"; 1; false; 0; 0; 0; "ready"),
				check("work_item"; "context"; 1; false; 0; 0; 0; "ready"),
				check("service"; "ambiguous"; 0; false; 0; 0; 1; "ambiguous"),
				check("status"; "index"; 1; false; 0; 0; 0; "ready")
			]
		}
	' >"${path}"
}

expect_pass() {
	local label="$1"
	local input="$2"
	local output="${tmp_dir}/${label}-proof.json"
	if ! "${runner}" --input "${input}" --output "${output}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to pass\n' "${label}" >&2
		sed -n '1,120p' "${tmp_dir}/${label}.err" >&2
		exit 1
	fi
	printf '%s' "${output}"
}

expect_fail() {
	local label="$1"
	local input="$2"
	local expected="$3"
	local output="${tmp_dir}/${label}-proof.json"
	if "${runner}" --input "${input}" --output "${output}" >"${tmp_dir}/${label}.out" 2>"${tmp_dir}/${label}.err"; then
		printf 'expected %s to fail\n' "${label}" >&2
		exit 1
	fi
	rg --fixed-strings --quiet -- "${expected}" "${tmp_dir}/${label}.err" \
		|| { printf 'expected %s failure to include %s\n' "${label}" "${expected}" >&2; sed -n '1,120p' "${tmp_dir}/${label}.err" >&2; exit 1; }
}

valid="${tmp_dir}/valid.json"
write_valid_input "${valid}"
proof="$(expect_pass valid "${valid}")"
jq -e '
	.surfaces.api.checked == 11 and
	.surfaces.api.truncated == 1 and
	.surfaces.api.missing_evidence == 1 and
	.surfaces.api.unsupported == 1 and
	.surfaces.api.ambiguous == 1 and
	.queue.retrying == 0 and
	.transcript_status == "captured"
' "${proof}" >/dev/null || { printf 'aggregate proof output did not preserve expected counters\n' >&2; exit 1; }

gate_dir="${tmp_dir}/gate"
ESHU_RELEASE_GATE_SKIP_GO_TESTS=1 "${release_gate}" \
	--phases state,readback-proof \
	--out-dir "${gate_dir}" \
	--image-tag-candidate v0.0.3-test \
	--readback-proof "${proof}" >/dev/null
jq -e '
	.readback_proof.status == "pass" and
	.readback_proof.surfaces.api.truncated == 1 and
	.readback_proof.surfaces.cli.ambiguous == 1
' "${gate_dir}/evidence.json" >/dev/null \
	|| { printf 'release gate did not preserve extended readback counters\n' >&2; exit 1; }

missing_mcp="${tmp_dir}/missing-mcp.json"
jq 'del(.checks[0].surfaces.mcp)' "${valid}" >"${missing_mcp}"
expect_fail missing_mcp "${missing_mcp}" "missing bounded limit, timeout, or API/MCP/CLI surface"

cli_differs="${tmp_dir}/cli-differs.json"
jq '.checks[0].surfaces.cli.count = 99' "${valid}" >"${cli_differs}"
expect_fail cli_differs "${cli_differs}" "API/MCP/CLI parity mismatch"

missing_limit="${tmp_dir}/missing-limit.json"
jq 'del(.checks[0].limit)' "${valid}" >"${missing_limit}"
expect_fail missing_limit "${missing_limit}" "missing bounded limit"

empty_unknown="${tmp_dir}/empty-unknown.json"
jq '
	.checks[0].surfaces.api.count = 0 |
	.checks[0].surfaces.mcp.count = 0 |
	.checks[0].surfaces.cli.count = 0 |
	.checks[0].surfaces.api.readiness_state = "unknown" |
	.checks[0].surfaces.mcp.readiness_state = "unknown" |
	.checks[0].surfaces.cli.readiness_state = "unknown"
' "${valid}" >"${empty_unknown}"
expect_fail empty_unknown "${empty_unknown}" "returned empty results without a ready"

private_input="${tmp_dir}/private.json"
jq '.repository = "private-org/private-repo"' "${valid}" >"${private_input}"
expect_fail private_input "${private_input}" "looks like private data"

printf 'e2e readback parity tests passed\n'
