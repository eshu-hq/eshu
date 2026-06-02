#!/usr/bin/env bash

remote_compose_validate_unsupported_hosted_collectors() {
	local raw="$1"
	local invalid
	invalid="$(jq -nr --arg raw "${raw}" '
		def trim: gsub("^\\s+|\\s+$"; "");
		["pagerduty", "jira", "grafana", "prometheus_mimir", "loki", "tempo"] as $allowed |
		$raw
		| split(",")
		| map(trim)
		| map(select(length > 0))
		| map(. as $name | select(($allowed | index($name)) | not))
		| .[0] // ""
	')"
	[[ -z "${invalid}" ]] || die "unsupported hosted collector row is invalid: ${invalid}"
}

remote_compose_json_array_from_lines() {
	local input="$1" output="$2"
	jq -R -s 'split("\n") | map(select(length > 0))' "${input}" >"${output}"
}
