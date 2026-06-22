#!/usr/bin/env bash
# Verify a public-safe Okta OIDC live proof summary.

set -euo pipefail

input=""
output_json=""
output_markdown=""

usage() {
	cat <<USAGE
Usage: $(basename "$0") --input <okta-oidc-proof.json> --output-json <summary.json> --output-markdown <summary.md>

The input is an operator-local aggregate proof manifest. Raw Okta domains,
client ids, client secrets, users, groups, OIDC tokens, cookies, private
endpoints, and audit bodies are never copied to the summary output.
USAGE
}

die() {
	printf 'verify-okta-oidc-live-proof: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--input)
			input="${2:-}"
			shift 2
			;;
		--output-json)
			output_json="${2:-}"
			shift 2
			;;
		--output-markdown)
			output_markdown="${2:-}"
			shift 2
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			die "unknown option: $1"
			;;
	esac
done

[[ -n "${input}" ]] || die "--input is required"
[[ -n "${output_json}" ]] || die "--output-json is required"
[[ -n "${output_markdown}" ]] || die "--output-markdown is required"
[[ -f "${input}" ]] || die "input file not found: ${input}"
command -v jq >/dev/null 2>&1 || die "jq is required"
command -v ruby >/dev/null 2>&1 || die "ruby is required"

tmp_json="${output_json}.tmp"
tmp_markdown="${output_markdown}.tmp"
mkdir -p "$(dirname "${output_json}")" "$(dirname "${output_markdown}")"

ruby -r json -e '
input_path, output_path = ARGV
required_steps = %w[
  issuer_metadata
  jwks_validation
  client_redirect_config
  authorization_code_login
  state_nonce
  group_mapping
  tenant_workspace_selection
  bounded_session_refresh
  group_removal_denial
  user_revocation_denial
  expired_mapping_denial
  tombstoned_mapping_denial
  revoked_role_target_denial
  provider_unavailable_fail_closed
  duplicate_refresh_idempotent
  tenant_workspace_boundary
  no_raw_provider_persistence
]

def fail_with(message)
  warn message
  exit 1
end

def non_negative_integer?(value)
  value.is_a?(Integer) && value >= 0
end

def public_safe_token?(value, max_length = 96)
  value.is_a?(String) && value.match?(/\A[A-Za-z0-9._:-]{1,#{max_length}}\z/)
end

def exact_path?(value, expected)
  value.is_a?(String) && value == expected
end

private_pattern = Regexp.new(
  [
    "ghp_[A-Za-z0-9_]+",
    "github_pat_[A-Za-z0-9_]+",
    "glpat-[A-Za-z0-9_-]+",
    "AKIA[0-9A-Z]{16}",
    "ASIA[0-9A-Z]{16}",
    "xox[baprs]-[A-Za-z0-9-]+",
    "Bearer\\s+\\S+",
    "Authorization:\\s*\\S+",
    "Set-Cookie:\\s*\\S+",
    "-----BEGIN [A-Z ]+-----",
    "\\b(?:id_token|access_token|refresh_token|client_secret)\\b\\s*[:=]",
    "eyJ[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+",
    "https?://\\S+",
    "bolt://\\S+",
    "postgres(?:ql)?://\\S+",
    "arn:(?:aws|aws-us-gov|aws-cn):\\S+",
    "(?:^|[^0-9])[0-9]{12}(?:[^0-9]|$)",
    "(?:[0-9]{1,3}\\.){3}[0-9]{1,3}",
    "[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}",
    "/(?:Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/\\S*"
  ].join("|"),
  Regexp::IGNORECASE
)

manifest = JSON.parse(File.read(input_path))

unless manifest["schema_version"] == 1
  fail_with("manifest schema_version must be 1")
end

proof_id = manifest["proof_id"].to_s
if private_pattern.match?(proof_id)
  fail_with("private-shaped value leaked in proof_id")
end
unless proof_id.match?(/\Aokta-oidc-live-proof-[0-9]{8}(?:-[a-f0-9]{64})?\z/)
  fail_with("proof_id must be okta-oidc-live-proof-YYYYMMDD or okta-oidc-live-proof-YYYYMMDD-sha256digest")
end

generated_at = manifest["generated_at"].to_s
unless generated_at.match?(/\A[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z\z/)
  fail_with("generated_at must be an RFC3339 UTC second timestamp")
end

provider = manifest["provider"] || {}
if private_pattern.match?(JSON.generate(provider))
  fail_with("private-shaped value leaked in provider summary")
end
unless provider["provider_kind"] == "external_oidc"
  fail_with("provider.provider_kind must be external_oidc")
end
unless %w[operator_private_secret operator_private_url local_private_file].include?(provider["issuer_metadata_source_class"].to_s)
  fail_with("provider.issuer_metadata_source_class must name a private source class")
end
unless %w[operator_private_secret local_private_file].include?(provider["client_config_source_class"].to_s)
  fail_with("provider.client_config_source_class must name a private source class")
end
unless exact_path?(provider["login_path"].to_s, "/api/v0/auth/oidc/login")
  fail_with("provider.login_path must be /api/v0/auth/oidc/login")
end
unless exact_path?(provider["callback_path"].to_s, "/api/v0/auth/oidc/callback")
  fail_with("provider.callback_path must be /api/v0/auth/oidc/callback")
end
unless public_safe_token?(provider["subject_claim_class"].to_s, 64)
  fail_with("provider.subject_claim_class must be public-safe")
end
unless public_safe_token?(provider["group_claim_class"].to_s, 64)
  fail_with("provider.group_claim_class must be public-safe")
end
unless provider["role_mapping_revision"].to_s.match?(/\Asha256:[a-f0-9]{64}\z/)
  fail_with("provider.role_mapping_revision must be a sha256 digest")
end

revocation = manifest["revocation"] || {}
if private_pattern.match?(JSON.generate(revocation))
  fail_with("private-shaped value leaked in revocation summary")
end
window = revocation["external_group_refresh_window_seconds"]
unless window.is_a?(Numeric) && window > 0 && window <= 86_400
  fail_with("revocation.external_group_refresh_window_seconds must be between 1 and 86400")
end
unless public_safe_token?(revocation["external_group_refresh_window_source"].to_s)
  fail_with("revocation.external_group_refresh_window_source must be public-safe")
end

steps = manifest["proof_steps"]
unless steps.is_a?(Array)
  fail_with("proof_steps must be an array")
end

seen = {}
step_summaries = steps.map do |entry|
  if private_pattern.match?(JSON.generate(entry))
    fail_with("private-shaped value leaked in proof step entry")
  end
  step = entry["step"].to_s
  unless required_steps.include?(step)
    fail_with("unknown proof step")
  end
  if seen[step]
    fail_with("duplicate proof step: #{step}")
  end
  seen[step] = true
  unless entry["status"] == "pass"
    fail_with("proof step #{step} must pass")
  end
  count = entry["evidence_count"]
  unless non_negative_integer?(count)
    fail_with("evidence_count must be a non-negative integer for proof step #{step}")
  end
  unless count.positive?
    fail_with("evidence_count must be positive for proof step #{step}")
  end
  {
    "step" => step,
    "status" => "pass",
    "evidence_count" => count
  }
end

missing = required_steps - seen.keys
unless missing.empty?
  fail_with("missing required proof steps: #{missing.join(", ")}")
end

public_summary = manifest["public_summary"] || {}
login_count = public_summary["login_count"]
denied_count = public_summary["denied_count"]
refresh_attempt_count = public_summary["refresh_attempt_count"]
revoked_session_count = public_summary["revoked_session_count"]
unless non_negative_integer?(login_count)
  fail_with("public_summary.login_count must be a non-negative integer")
end
unless login_count.positive?
  fail_with("public_summary.login_count must be positive")
end
unless non_negative_integer?(denied_count)
  fail_with("public_summary.denied_count must be a non-negative integer")
end
unless denied_count.positive?
  fail_with("public_summary.denied_count must be positive")
end
unless non_negative_integer?(refresh_attempt_count)
  fail_with("public_summary.refresh_attempt_count must be a non-negative integer")
end
unless refresh_attempt_count.positive?
  fail_with("public_summary.refresh_attempt_count must be positive")
end
unless non_negative_integer?(revoked_session_count)
  fail_with("public_summary.revoked_session_count must be a non-negative integer")
end
unless revoked_session_count.positive?
  fail_with("public_summary.revoked_session_count must be positive")
end
role_names = public_summary["mapped_role_names"] || []
unless role_names.is_a?(Array) && role_names.all? { |role| public_safe_token?(role.to_s, 64) }
  fail_with("mapped role names must be public-safe")
end
decision_families = public_summary["decision_families"] || []
unless decision_families.is_a?(Array) && decision_families.all? { |family| %w[allowed denied fail_closed].include?(family.to_s) }
  fail_with("decision families must be allowed, denied, or fail_closed")
end
if private_pattern.match?(JSON.generate(public_summary))
  fail_with("private-shaped value leaked in public summary")
end

ordered_steps = step_summaries.sort_by { |entry| required_steps.index(entry["step"]) }
summary = {
  "status" => "pass",
  "schema_version" => 1,
  "proof_id" => proof_id,
  "generated_at" => generated_at,
  "provider" => {
    "provider_kind" => "external_oidc",
    "issuer_metadata_source_class" => provider["issuer_metadata_source_class"],
    "client_config_source_class" => provider["client_config_source_class"],
    "login_path" => provider["login_path"],
    "callback_path" => provider["callback_path"],
    "subject_claim_class" => provider["subject_claim_class"],
    "group_claim_class" => provider["group_claim_class"],
    "role_mapping_revision" => provider["role_mapping_revision"]
  },
  "revocation" => {
    "external_group_refresh_window_seconds" => window,
    "external_group_refresh_window_source" => revocation["external_group_refresh_window_source"]
  },
  "required_proof_steps" => required_steps,
  "proof_step_count" => ordered_steps.length,
  "total_evidence_count" => ordered_steps.sum { |entry| entry["evidence_count"].to_i },
  "proof_steps" => ordered_steps,
  "public_summary" => {
    "login_count" => login_count,
    "denied_count" => denied_count,
    "refresh_attempt_count" => refresh_attempt_count,
    "revoked_session_count" => revoked_session_count,
    "mapped_role_names" => role_names,
    "decision_families" => decision_families
  }
}

File.write(output_path, JSON.pretty_generate(summary) + "\n")
' "${input}" "${tmp_json}" || die "okta oidc live proof failed"

jq -r '
	[
		"# Okta OIDC live proof",
		"",
		"- Status: \(.status)",
		"- Proof ID: \(.proof_id)",
		"- Generated at: \(.generated_at)",
		"- Provider kind: \(.provider.provider_kind)",
		"- Issuer metadata source class: \(.provider.issuer_metadata_source_class)",
		"- Client config source class: \(.provider.client_config_source_class)",
		"- Login path: \(.provider.login_path)",
		"- Callback path: \(.provider.callback_path)",
		"- Proof steps: \(.proof_step_count)",
		"- Total evidence count: \(.total_evidence_count)",
		"- Login count: \(.public_summary.login_count)",
		"- Denied count: \(.public_summary.denied_count)",
		"- Refresh attempt count: \(.public_summary.refresh_attempt_count)",
		"- Revoked session count: \(.public_summary.revoked_session_count)",
		"- External group refresh window seconds: \(.revocation.external_group_refresh_window_seconds)",
		"",
		"## Proof Steps",
		"",
		(.proof_steps[] | "- \(.step): status=\(.status), evidence=\(.evidence_count)"),
		"",
		"Only aggregate counts, public-safe paths, source classes, role names, decision families, and timing classes are written to this summary."
	] | .[]
' "${tmp_json}" >"${tmp_markdown}"

mv "${tmp_json}" "${output_json}"
mv "${tmp_markdown}" "${output_markdown}"
printf 'verify-okta-oidc-live-proof: wrote %s and %s\n' "${output_json}" "${output_markdown}"
