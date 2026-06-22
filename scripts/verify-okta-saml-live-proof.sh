#!/usr/bin/env bash
# Verify a public-safe Okta SAML live proof summary.

set -euo pipefail

input=""
output_json=""
output_markdown=""

usage() {
	cat <<USAGE
Usage: $(basename "$0") --input <okta-saml-proof.json> --output-json <summary.json> --output-markdown <summary.md>

The input is an operator-local aggregate proof manifest. Raw Okta domains,
metadata XML, users, groups, SAML assertions, SAML attributes, cookies, private
endpoints, and audit bodies are never copied to the summary output.
USAGE
}

die() {
	printf 'verify-okta-saml-live-proof: %s\n' "$*" >&2
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
  sp_metadata
  saml_login
  group_mapping
  tenant_workspace_selection
  revocation_refresh
  denied_access
  missing_group_claims
  replay
  clock_skew
  disabled_provider
  disabled_user
  disabled_membership
  disabled_grant
  metadata_certificate_failure
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

def public_safe_path?(value, suffix)
  value.is_a?(String) &&
    value.match?(
      %r{\A/api/v0/auth/saml/providers/(?:\{provider_id\}|[A-Za-z0-9._-]{1,96})/#{suffix}\z}
    )
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
    "-----BEGIN [A-Z ]+-----",
    "SAMLResponse\\s*=",
    "<(?:EntityDescriptor|md:EntityDescriptor)",
    "<(?:Assertion|saml:Assertion)",
    "<(?:Response|samlp:Response)",
    "<(?:saml|samlp):",
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
unless proof_id.match?(/\A[A-Za-z0-9._-]+\z/)
  fail_with("proof_id must be public-safe")
end

generated_at = manifest["generated_at"].to_s
unless generated_at.match?(/\A[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z\z/)
  fail_with("generated_at must be an RFC3339 UTC second timestamp")
end

provider = manifest["provider"] || {}
if private_pattern.match?(JSON.generate(provider))
  fail_with("private-shaped value leaked in provider summary")
end
unless provider["provider_kind"] == "external_saml"
  fail_with("provider.provider_kind must be external_saml")
end
unless %w[operator_private_secret operator_private_url local_private_file].include?(provider["metadata_source_class"].to_s)
  fail_with("provider.metadata_source_class must name a private source class")
end
unless public_safe_path?(provider["sp_metadata_path"].to_s, "metadata")
  fail_with("provider.sp_metadata_path must be a public-safe SAML metadata path")
end
unless public_safe_path?(provider["acs_path"].to_s, "acs")
  fail_with("provider.acs_path must be a public-safe SAML ACS path")
end
unless public_safe_token?(provider["name_id_policy_class"].to_s, 64)
  fail_with("provider.name_id_policy_class must be public-safe")
end
unless public_safe_token?(provider["group_attribute_class"].to_s, 64)
  fail_with("provider.group_attribute_class must be public-safe")
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
  step = entry["step"].to_s
  unless required_steps.include?(step)
    fail_with("unknown proof step: #{step}")
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
  if private_pattern.match?(JSON.generate(entry))
    fail_with("private-shaped value leaked in proof step #{step}")
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
unless non_negative_integer?(login_count)
  fail_with("public_summary.login_count must be a non-negative integer")
end
unless non_negative_integer?(denied_count)
  fail_with("public_summary.denied_count must be a non-negative integer")
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
    "provider_kind" => "external_saml",
    "metadata_source_class" => provider["metadata_source_class"],
    "sp_metadata_path" => provider["sp_metadata_path"],
    "acs_path" => provider["acs_path"],
    "name_id_policy_class" => provider["name_id_policy_class"],
    "group_attribute_class" => provider["group_attribute_class"],
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
    "mapped_role_names" => role_names,
    "decision_families" => decision_families
  }
}

File.write(output_path, JSON.pretty_generate(summary) + "\n")
' "${input}" "${tmp_json}" || die "okta saml live proof failed"

jq -r '
	[
		"# Okta SAML live proof",
		"",
		"- Status: \(.status)",
		"- Proof ID: \(.proof_id)",
		"- Generated at: \(.generated_at)",
		"- Provider kind: \(.provider.provider_kind)",
		"- Metadata source class: \(.provider.metadata_source_class)",
		"- SP metadata path: \(.provider.sp_metadata_path)",
		"- ACS path: \(.provider.acs_path)",
		"- Proof steps: \(.proof_step_count)",
		"- Total evidence count: \(.total_evidence_count)",
		"- Login count: \(.public_summary.login_count)",
		"- Denied count: \(.public_summary.denied_count)",
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
printf 'verify-okta-saml-live-proof: wrote %s and %s\n' "${output_json}" "${output_markdown}"
