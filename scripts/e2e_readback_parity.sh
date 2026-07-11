#!/usr/bin/env bash
# Build public-safe API/MCP/CLI readback-proof evidence from operator-local
# bounded readback summaries. Raw transcripts stay outside this repository.

set -euo pipefail

input=""
output=""

required_domains=(
	"repository"
	"package"
	"cloud"
	"deployment"
	"vulnerability"
	"sbom_image"
	"observability"
	"incident"
	"work_item"
	"service"
	"status"
)

forbidden_keys='["repository","repositories","repository_name","repository_id","repo","repo_name","repo_id","package","packages","package_name","package_id","provider_url","alert_url","installation","provider_repository","url","host","hostname","ip","path","file","token","payload","description","cve_description","transcript","stdout","stderr","request","response","body","account_id","account"]'

usage() {
	# printf (a builtin, no pipe) instead of a heredoc: this body is over 512
	# bytes and would deadlock under Homebrew bash >= 5.1's pipe-buffer
	# heredoc write (#5074).
	printf '%s\n' \
		"Usage: $(basename "$0") --input <summary.json> --output <readback-proof.json>" \
		"" \
		"The input is an operator-local aggregate summary with:" \
		"  schema_version: 1" \
		"  proof_id: public-safe id" \
		'  transcript_status: "captured"' \
		"  queue: {retrying, failed, dead_letters}" \
		"  checks[]: {" \
		"    domain, name, limit, timeout_seconds," \
		"    surfaces: {api, mcp, cli}" \
		"  }" \
		"" \
		"Each surface must include status, truth_level, truth_profile, readiness_state," \
		"count, truncated, missing_evidence, unsupported, and ambiguous. The runner" \
		"compares API/MCP/CLI for each check and writes the aggregate readback-proof JSON" \
		"accepted by security_intelligence_release_gate.sh --phases readback-proof."
}

die() {
	printf 'e2e-readback-parity: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--input)
			input="${2:-}"
			shift 2
			;;
		--output)
			output="${2:-}"
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
[[ -n "${output}" ]] || die "--output is required"
[[ -f "${input}" ]] || die "input file not found: ${input}"
command -v jq >/dev/null 2>&1 || die "jq is required"
command -v rg >/dev/null 2>&1 || die "rg is required"

jq -e . "${input}" >/dev/null 2>&1 || die "input must be valid JSON"

if ! jq -e --argjson forbidden "${forbidden_keys}" '
	[.. | objects | keys[]? | select(. as $key | $forbidden | index($key))]
	| length == 0
' "${input}" >/dev/null; then
	die "input looks like private data; only aggregate readback status and counts are accepted"
fi

if jq -r '.. | strings' "${input}" | rg --quiet \
	'ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|/security/dependabot|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|(^|[[:space:]"'\''])[-A-Za-z0-9_.]+/[-A-Za-z0-9_.]+($|[[:space:]"'\'',])'; then
	die "input looks like private data; raw transcripts and private identifiers are not accepted"
fi

if ! jq -e '
	def nonneg($value): (($value // 0) | type == "number" and . >= 0);
	.schema_version == 1 and
	(.proof_id | type == "string" and test("^[A-Za-z0-9._-]+$")) and
	(.transcript_status == "captured") and
	(.queue | type == "object") and
	nonneg(.queue.retrying) and
	nonneg(.queue.failed) and
	nonneg(.queue.dead_letters) and
	(.queue.retrying == 0) and
	(.queue.failed == 0) and
	(.queue.dead_letters == 0) and
	(.checks | type == "array" and length > 0)
' "${input}" >/dev/null; then
	die "input root shape must include proof id, captured transcripts, queue-zero counters, and checks"
fi

for domain in "${required_domains[@]}"; do
	jq -e --arg domain "${domain}" 'any(.checks[]?; .domain == $domain)' "${input}" >/dev/null \
		|| die "missing required readback domain: ${domain}"
done

shape_error="$(jq -r '
	def missing_positive_number($value): (($value // 0) | (type != "number" or . <= 0));
	.checks[]?
	| select(
		((.domain // "") | type != "string") or
		((.name // "") | type != "string") or
		missing_positive_number(.limit) or
		missing_positive_number(.timeout_seconds) or
		((.surfaces.api // null) == null) or
		((.surfaces.mcp // null) == null) or
		((.surfaces.cli // null) == null)
	)
	| "check \(.domain // "unknown")/\(.name // "unknown") is missing bounded limit, timeout, or API/MCP/CLI surface"
' "${input}" | sed -n '1p')"
[[ -z "${shape_error}" ]] || die "${shape_error}"

surface_shape_error="$(jq -r '
	def invalid_non_negative_number($value): (($value // -1) | (type != "number" or . < 0));
	def bad_surface($surface):
		(($surface.status // "") != "pass") or
		(($surface.truth_level // "") == "") or
		(($surface.truth_profile // "") == "") or
		(($surface.readiness_state // "") == "") or
		invalid_non_negative_number($surface.count) or
		($surface.truncated | type != "boolean") or
		invalid_non_negative_number($surface.missing_evidence) or
		invalid_non_negative_number($surface.unsupported) or
		invalid_non_negative_number($surface.ambiguous);
	.checks[]?
	| select(bad_surface(.surfaces.api) or bad_surface(.surfaces.mcp) or bad_surface(.surfaces.cli))
	| "check \(.domain)/\(.name) has an invalid surface summary"
' "${input}" | sed -n '1p')"
[[ -z "${surface_shape_error}" ]] || die "${surface_shape_error}"

empty_ready_error="$(jq -r '
	.checks[]? as $check
	| select(any(["api","mcp","cli"][]; . as $surface | ($check.surfaces[$surface].count == 0) and (($check.surfaces[$surface].readiness_state // "") | IN("ready", "empty_ready", "missing_evidence", "unsupported", "ambiguous") | not)))
	| "check \($check.domain)/\($check.name) returned empty results without a ready, missing-evidence, or unsupported state"
' "${input}" | sed -n '1p')"
[[ -z "${empty_ready_error}" ]] || die "${empty_ready_error}"

reason_error="$(jq -r '
	.checks[]? as $check
	| select(any(["api","mcp","cli"][]; . as $surface | ($check.surfaces[$surface].missing_evidence > 0 and (($check.surfaces[$surface].missing_reason // "") | length == 0)) or ($check.surfaces[$surface].unsupported > 0 and (($check.surfaces[$surface].unsupported_reason // "") | length == 0)) or ($check.surfaces[$surface].ambiguous > 0 and (($check.surfaces[$surface].ambiguity_reason // "") | length == 0))))
	| "check \($check.domain)/\($check.name) classified missing, unsupported, or ambiguous evidence without a reason"
' "${input}" | sed -n '1p')"
[[ -z "${reason_error}" ]] || die "${reason_error}"

parity_error="$(jq -r '
	def parity_tuple($surface): [
		$surface.truth_level,
		$surface.truth_profile,
		$surface.readiness_state,
		$surface.count,
		$surface.truncated,
		($surface.missing_evidence // 0),
		($surface.unsupported // 0),
		($surface.ambiguous // 0),
		($surface.missing_reason // ""),
		($surface.unsupported_reason // ""),
		($surface.ambiguity_reason // "")
	];
	.checks[]?
	| select((parity_tuple(.surfaces.api) != parity_tuple(.surfaces.mcp)) or (parity_tuple(.surfaces.api) != parity_tuple(.surfaces.cli)))
	| "check \(.domain)/\(.name) API/MCP/CLI parity mismatch"
' "${input}" | sed -n '1p')"
[[ -z "${parity_error}" ]] || die "${parity_error}"

tmp_output="${output}.tmp"
jq '{
	schema_version: 1,
	proof_id: .proof_id,
	surfaces: {
		api: {
			status: "pass",
			checked: (.checks | length),
			failed: 0,
			truncated: ([.checks[] | select(.surfaces.api.truncated == true)] | length),
			unsupported: ([.checks[].surfaces.api.unsupported] | add // 0),
			missing_evidence: ([.checks[].surfaces.api.missing_evidence] | add // 0),
			ambiguous: ([.checks[].surfaces.api.ambiguous] | add // 0)
		},
		mcp: {
			status: "pass",
			checked: (.checks | length),
			failed: 0,
			truncated: ([.checks[] | select(.surfaces.mcp.truncated == true)] | length),
			unsupported: ([.checks[].surfaces.mcp.unsupported] | add // 0),
			missing_evidence: ([.checks[].surfaces.mcp.missing_evidence] | add // 0),
			ambiguous: ([.checks[].surfaces.mcp.ambiguous] | add // 0)
		},
		cli: {
			status: "pass",
			checked: (.checks | length),
			failed: 0,
			truncated: ([.checks[] | select(.surfaces.cli.truncated == true)] | length),
			unsupported: ([.checks[].surfaces.cli.unsupported] | add // 0),
			missing_evidence: ([.checks[].surfaces.cli.missing_evidence] | add // 0),
			ambiguous: ([.checks[].surfaces.cli.ambiguous] | add // 0)
		}
	},
	queue: {
		retrying: .queue.retrying,
		failed: .queue.failed,
		dead_letters: .queue.dead_letters
	},
	transcript_status: .transcript_status
}' "${input}" >"${tmp_output}"
mv "${tmp_output}" "${output}"
printf 'e2e-readback-parity: wrote %s\n' "${output}"
