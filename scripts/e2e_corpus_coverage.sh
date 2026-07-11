#!/usr/bin/env bash
# Build the public-safe representative corpus coverage contract for Eshu E2E.
set -euo pipefail

input=""
output=""
mode="representative"
missing_reason="not observed in current private representative corpus"
issue_ref="#2641"
min_repository_count=20
max_repository_count=50

required_ecosystems='["npm","gomod","pypi","maven","composer","rubygems","cargo","nuget"]'
required_families='["terraform_iac","kubernetes_iac","image_sbom","deployment","relationship_evidence","vulnerability","observability","incident","work_item"]'
forbidden_keys='[
	"repository","repositories","repository_name","repository_id",
	"repo","repo_name","repo_id","package","packages","package_name",
	"package_id","provider_url","alert_url","installation",
	"provider_repository","url","host","hostname","ip","path","file",
	"token","payload","description","cve_description","transcript",
	"stdout","stderr","request","response","body","account_id","account"
]'
private_value_pattern='ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|/security/dependabot|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|(^|[[:space:]"'\''])[-A-Za-z0-9_.]+/[-A-Za-z0-9_.]+($|[[:space:]"'\'',])'

usage() {
	# Body lives in scripts/lib/ (not a heredoc): Homebrew bash >= 5.1 writes
	# the entire heredoc body to a pipe before forking the reader, and
	# macOS's 512-byte pipe buffer deadlocks on any body over that size
	# (#5074).
	cat "$(dirname "$0")/lib/e2e_corpus_coverage-usage.txt" >&2
}

die() {
	printf 'e2e-corpus-coverage: %s\n' "$*" >&2
	exit 1
}

require_tool() {
	command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}

require_non_negative_int_arg() {
	local name="$1" value="$2"
	[[ "${value}" =~ ^[0-9]+$ ]] || die "${name} must be a non-negative integer"
}

while (($# > 0)); do
	case "$1" in
		--input) input="${2:-}"; shift 2 ;;
		--output) output="${2:-}"; shift 2 ;;
		--mode) mode="${2:-}"; shift 2 ;;
		--missing-reason) missing_reason="${2:-}"; shift 2 ;;
		--issue-ref) issue_ref="${2:-}"; shift 2 ;;
		--min-repository-count) min_repository_count="${2:-}"; shift 2 ;;
		--max-repository-count) max_repository_count="${2:-}"; shift 2 ;;
		-h|--help) usage; exit 0 ;;
		*) die "unknown argument: $1" ;;
	esac
done

require_tool jq
require_tool rg

[[ -n "${input}" ]] || die "--input is required"
[[ -n "${output}" ]] || die "--output is required"
[[ -f "${input}" ]] || die "input file not found: ${input}"
case "${mode}" in
	smoke|representative|full) ;;
	*) die "--mode must be smoke, representative, or full" ;;
esac
[[ -n "${missing_reason}" ]] || die "--missing-reason must not be empty"
[[ "${issue_ref}" =~ ^#[0-9]+$ ]] || die "--issue-ref must look like #2641"
require_non_negative_int_arg "--min-repository-count" "${min_repository_count}"
require_non_negative_int_arg "--max-repository-count" "${max_repository_count}"

validate_public_safe_file() {
	local file="$1" label="$2"
	jq -e . "${file}" >/dev/null 2>&1 || die "${label} must be valid JSON"
	if ! jq -e --argjson forbidden "${forbidden_keys}" '
		[.. | objects | keys[]? | select(. as $key | $forbidden | index($key))]
		| length == 0
	' "${file}" >/dev/null; then
		die "${label} looks like private data; forbidden private-looking keys are not accepted"
	fi
	if jq -r '.. | strings' "${file}" | rg --quiet "${private_value_pattern}"; then
		die "${label} looks like private data; only aggregate counts, status enums, and public issue refs are accepted"
	fi
}

validate_input_contract() {
	local file="$1"
	jq -e '
		.repository_count | type == "number" and . >= 0 and . == floor
	' "${file}" >/dev/null || die "repository_count must be a non-negative integer"

	jq -e '
		def nonneg_count:
			(type == "number" and . >= 0) or
			(type == "object" and ((.count // 0) | type == "number" and . >= 0));
		((.schema_version // 1) == 1) and
		((.mode // $mode) == $mode) and
		(.ecosystems | type == "object") and
		(.evidence_families | type == "object") and
		all((.ecosystems // {} | to_entries[]); .value | nonneg_count) and
		all((.evidence_families // {} | to_entries[]); .value | nonneg_count)
	' --arg mode "${mode}" "${file}" >/dev/null || die "input shape is invalid"

	local repository_count
	repository_count="$(jq -r '.repository_count' "${file}")"
	if [[ "${mode}" == "representative" ]]; then
		if ((repository_count < min_repository_count || repository_count > max_repository_count)); then
			die "representative corpus requires repository_count between ${min_repository_count} and ${max_repository_count}; got ${repository_count}"
		fi
	fi
}

validate_public_safe_file "${input}" "input"
if printf '%s\n%s\n' "${missing_reason}" "${issue_ref}" | rg --quiet "${private_value_pattern}"; then
	die "missing reason or issue ref looks like private data"
fi
validate_input_contract "${input}"

tmp="$(mktemp "${TMPDIR:-/tmp}/eshu-corpus-coverage.XXXXXX")"
jq \
	--arg mode "${mode}" \
	--arg reason "${missing_reason}" \
	--arg issue_ref "${issue_ref}" \
	--argjson ecosystems "${required_ecosystems}" \
	--argjson families "${required_families}" '
	def count_for($section; $name):
		(.[$section][$name] // 0) as $value |
		if ($value | type) == "number" then $value
		elif ($value | type) == "object" then ($value.count // 0)
		else 0 end;
	def coverage_row($count):
		if $count > 0 then
			{status: "pass", count: $count}
		else
			{status: "fail", count: 0, reason: $reason, issue_refs: [$issue_ref]}
		end;
	. as $root |
	{
		schema_version: 1,
		mode: $mode,
		repository_count: $root.repository_count,
		ecosystems: (
			reduce $ecosystems[] as $name ({}; .[$name] = coverage_row($root | count_for("ecosystems"; $name)))
		),
		evidence_families: (
			reduce $families[] as $name ({}; .[$name] = coverage_row($root | count_for("evidence_families"; $name)))
		)
	}
' "${input}" >"${tmp}"

validate_public_safe_file "${tmp}" "output"
mkdir -p "$(dirname "${output}")"
mv "${tmp}" "${output}"
printf 'e2e-corpus-coverage: pass output=%s repository_count=%s\n' \
	"${output}" "$(jq -r '.repository_count' "${output}")"
