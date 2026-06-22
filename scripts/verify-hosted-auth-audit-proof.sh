#!/usr/bin/env bash
# Verify a public-safe auth audit and revocation proof summary.

set -euo pipefail

input=""
output_json=""
output_markdown=""

usage() {
	cat <<USAGE
Usage: $(basename "$0") --input <auth-audit-proof.json> --output-json <summary.json> --output-markdown <summary.md>

The input is an operator-local proof manifest that names only public-safe
auth-audit event families, aggregate counts, and revocation timing classes.
Raw principals, tokens, claims, provider assertions, private locators, and
audit event bodies are never copied to the summary output.
USAGE
}

die() {
	printf 'verify-hosted-auth-audit-proof: %s\n' "$*" >&2
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
required_event_types = %w[
  api_mcp_authentication
  identity_authentication
  mfa_lifecycle
  session_lifecycle
  token_lifecycle
  idp_config_change
  role_grant_change
  read_authorization
  tenant_switch
  sensitive_data_access
  ask_search_run
  export
  bootstrap
  break_glass
  audit_read
]

def fail_with(message)
  warn message
  exit 1
end

def non_negative_integer?(value)
  value.is_a?(Integer) && value >= 0
end

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

events = manifest["audit_events"]
unless events.is_a?(Array)
  fail_with("audit_events must be an array")
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

seen = {}
event_summaries = events.map do |entry|
  event_type = entry["event_type"].to_s
  unless required_event_types.include?(event_type)
    fail_with("unknown audit event type: #{event_type}")
  end
  if seen[event_type]
    fail_with("duplicate audit event type: #{event_type}")
  end
  seen[event_type] = true

  record_count = entry["record_count"]
  unless non_negative_integer?(record_count)
    fail_with("record_count must be a non-negative integer for event #{event_type}")
  end
  decision_counts = entry["decision_counts"] || {}
  unless decision_counts.is_a?(Hash)
    fail_with("decision_counts must be an object for event #{event_type}")
  end
  decision_counts.each do |decision, count|
    unless %w[allowed denied unavailable].include?(decision)
      fail_with("unknown decision #{decision} for event #{event_type}")
    end
    unless non_negative_integer?(count)
      fail_with("decision count must be a non-negative integer for event #{event_type}")
    end
  end
  unless decision_counts.values.sum == record_count
    fail_with("decision counts must sum to record_count for event #{event_type}")
  end
  if private_pattern.match?(JSON.generate(entry))
    fail_with("private-shaped value leaked in audit event summary #{event_type}")
  end
  {
    "event_type" => event_type,
    "record_count" => record_count,
    "decision_counts" => decision_counts
  }
end

missing = required_event_types - seen.keys
unless missing.empty?
  fail_with("missing required audit event types: #{missing.join(", ")}")
end

ordinary_reads = manifest["ordinary_reads"] || {}
unless ordinary_reads["structured_telemetry_only"] == true
  fail_with("ordinary_reads.structured_telemetry_only must be true")
end

revocation = manifest["revocation"] || {}
unless revocation["eshu_owned_sessions"] == "immediate"
  fail_with("revocation.eshu_owned_sessions must be immediate")
end
unless revocation["eshu_owned_tokens"] == "immediate"
  fail_with("revocation.eshu_owned_tokens must be immediate")
end
window = revocation["external_group_refresh_window_seconds"]
unless window.is_a?(Numeric) && window > 0 && window <= 86_400
  fail_with("revocation.external_group_refresh_window_seconds must be between 1 and 86400")
end
source = revocation["external_group_refresh_window_source"].to_s
unless source.match?(/\A[A-Za-z0-9._:-]{1,96}\z/)
  fail_with("revocation.external_group_refresh_window_source must be public-safe")
end
if private_pattern.match?(JSON.generate(revocation))
  fail_with("private-shaped value leaked in revocation summary")
end

summary = {
  "status" => "pass",
  "schema_version" => 1,
  "proof_id" => proof_id,
  "generated_at" => generated_at,
  "required_audit_event_types" => required_event_types,
  "audit_event_type_count" => event_summaries.length,
  "total_audit_records" => event_summaries.sum { |entry| entry["record_count"].to_i },
  "ordinary_reads" => {
    "structured_telemetry_only" => true
  },
  "revocation" => {
    "eshu_owned_sessions" => "immediate",
    "eshu_owned_tokens" => "immediate",
    "external_group_refresh_window_seconds" => window,
    "external_group_refresh_window_source" => source
  },
  "audit_events" => event_summaries
}

File.write(output_path, JSON.pretty_generate(summary) + "\n")
' "${input}" "${tmp_json}" || die "auth audit proof failed"

jq -r '
	[
		"# Hosted auth audit and revocation proof",
		"",
		"- Status: \(.status)",
		"- Proof ID: \(.proof_id)",
		"- Generated at: \(.generated_at)",
		"- Audit event families: \(.audit_event_type_count)",
		"- Total audit records: \(.total_audit_records)",
		"- Eshu-owned session revocation: \(.revocation.eshu_owned_sessions)",
		"- Eshu-owned token revocation: \(.revocation.eshu_owned_tokens)",
		"- External group refresh window seconds: \(.revocation.external_group_refresh_window_seconds)",
		"",
		"## Audit Event Families",
		"",
		(.audit_events[] | "- \(.event_type): records=\(.record_count)"),
		"",
		"Only aggregate counts and public-safe timing classes are written to this summary."
	] | .[]
' "${tmp_json}" >"${tmp_markdown}"

mv "${tmp_json}" "${output_json}"
mv "${tmp_markdown}" "${output_markdown}"
printf 'verify-hosted-auth-audit-proof: wrote %s and %s\n' "${output_json}" "${output_markdown}"
