#!/usr/bin/env bash
# Verify public-safe hosted governance artifacts do not leak private values.

set -euo pipefail

manifest=""
output_json=""
output_markdown=""

usage() {
	cat <<USAGE
Usage: $(basename "$0") --manifest <proof.json> --output-json <summary.json> --output-markdown <summary.md>

The manifest and referenced artifacts are operator-local proof inputs. Raw
artifact bodies, canary values, private locators, endpoints, token values, DSNs,
and source payloads are never copied to the summary output.
USAGE
}

die() {
	printf 'verify-hosted-governance-negative-leakage-proof: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--manifest)
			manifest="${2:-}"
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

[[ -n "${manifest}" ]] || die "--manifest is required"
[[ -n "${output_json}" ]] || die "--output-json is required"
[[ -n "${output_markdown}" ]] || die "--output-markdown is required"
[[ -f "${manifest}" ]] || die "manifest file not found: ${manifest}"
command -v jq >/dev/null 2>&1 || die "jq is required"
command -v ruby >/dev/null 2>&1 || die "ruby is required"

tmp_json="${output_json}.tmp"
tmp_markdown="${output_markdown}.tmp"
mkdir -p "$(dirname "${output_json}")" "$(dirname "${output_markdown}")"

ruby -r json -r digest -r pathname -e '
manifest_path, output_path = ARGV
required_surfaces = %w[
  facts
  logs
  metric_labels
  status_errors
  graph_properties
  api_bodies
  mcp_bodies
  console_surfaces
  audit_events
  generated_docs
  onboarding_artifacts
]

def fail_with(message)
  warn message
  exit 1
end

manifest = JSON.parse(File.read(manifest_path))
base_dir = File.expand_path(File.dirname(manifest_path))

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

canaries = manifest["canaries"]
unless canaries.is_a?(Array) && !canaries.empty? && canaries.all? { |value| value.is_a?(String) && !value.empty? }
  fail_with("canaries must be a non-empty string array")
end

surfaces = manifest["surfaces"]
unless surfaces.is_a?(Array)
  fail_with("surfaces must be an array")
end

names = surfaces.map { |entry| entry["surface"].to_s }
unknown = names.uniq - required_surfaces
unless unknown.empty?
  fail_with("unknown surfaces: #{unknown.sort.join(", ")}")
end

duplicates = names.group_by(&:itself).select { |_name, values| values.length > 1 }.keys
unless duplicates.empty?
  fail_with("duplicate surfaces: #{duplicates.sort.join(", ")}")
end

missing = required_surfaces - names
unless missing.empty?
  fail_with("missing required surfaces: #{missing.join(", ")}")
end

private_pattern = Regexp.new(
  [
    "ghp_[A-Za-z0-9_]+",
    "github_pat_[A-Za-z0-9_]+",
    "glpat-[A-Za-z0-9_-]+",
    "AKIA[0-9A-Z]{16}",
    "ASIA[0-9A-Z]{16}",
    "xox[baprs]-[A-Za-z0-9-]+",
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

surface_summaries = surfaces.sort_by { |entry| required_surfaces.index(entry["surface"]) }.map do |entry|
  surface = entry["surface"].to_s
  artifact = entry["artifact"].to_s
  if artifact.empty? || artifact.start_with?("/", "~") || artifact.split(File::SEPARATOR).include?("..")
    fail_with("artifact paths must stay relative to the manifest directory")
  end

  full_path = File.expand_path(artifact, base_dir)
  unless full_path.start_with?(base_dir + File::SEPARATOR)
    fail_with("artifact paths must stay relative to the manifest directory")
  end
  unless File.file?(full_path)
    fail_with("artifact not found for surface #{surface}")
  end

  body = File.binread(full_path)
  canaries.each do |canary|
    if body.include?(canary)
      fail_with("declared canary leaked in surface #{surface}")
    end
  end
  if private_pattern.match?(body)
    fail_with("private-shaped value leaked in surface #{surface}")
  end

  record_count = entry["record_count"]
  unless record_count.is_a?(Numeric) && record_count >= 0
    fail_with("record_count must be non-negative for surface #{surface}")
  end

  {
    "surface" => surface,
    "record_count" => record_count,
    "byte_count" => body.bytesize,
    "line_count" => body.lines.count,
    "artifact_sha256" => Digest::SHA256.hexdigest(body)
  }
end

summary = {
  "status" => "pass",
  "schema_version" => 1,
  "proof_id" => proof_id,
  "generated_at" => generated_at,
  "required_surfaces" => required_surfaces,
  "surface_count" => surface_summaries.length,
  "canary_count" => canaries.length,
  "surfaces" => surface_summaries,
  "public_artifact_review" => "pass"
}

File.write(output_path, JSON.pretty_generate(summary) + "\n")
' "${manifest}" "${tmp_json}" || die "negative leakage proof failed"

jq -r '
	[
		"# Hosted governance negative leakage proof",
		"",
		"- Status: \(.status)",
		"- Proof ID: \(.proof_id)",
		"- Generated at: \(.generated_at)",
		"- Required surfaces: \(.surface_count)",
		"- Canary count: \(.canary_count)",
		"- Public artifact review: \(.public_artifact_review)",
		"",
		"## Surfaces",
		"",
		(.surfaces[] | "- \(.surface): records=\(.record_count), bytes=\(.byte_count), lines=\(.line_count), sha256=\(.artifact_sha256)"),
		"",
		"Raw artifact bodies, declared canary values, private locators, endpoints, token values, DSNs, and source payloads remain operator-local."
	] | .[]
' "${tmp_json}" >"${tmp_markdown}"

mv "${tmp_json}" "${output_json}"
mv "${tmp_markdown}" "${output_markdown}"
printf 'verify-hosted-governance-negative-leakage-proof: wrote %s and %s\n' "${output_json}" "${output_markdown}"
