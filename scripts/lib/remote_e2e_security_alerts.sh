#!/usr/bin/env bash

target_story_provider_repository_match_count() {
	local file="$1"
	local expected="$2"
	jq -r --arg expected "${expected}" '
		(.data // .) as $body |
		($expected | ascii_downcase) as $expected_lower |
		def repository_anchor_matches($actual):
			$actual == $expected_lower
			or ($actual | endswith(":" + $expected_lower))
			or ($actual | endswith("/" + $expected_lower));
		def provider_repository_candidates($alert):
			[
				$alert.repository_id?,
				$alert.repository_slug?,
				$alert.repository_name?,
				$alert.repository_full_name?,
				$alert.provider_repository?,
				$alert.provider_repository_id?,
				$alert.provider_repository_name?,
				(($alert.provider_alert_id? // "") | capture("security-alert:[^:]+:(?<repository>[^:]+)")? | .repository)
			]
			| map(select(type == "string" and length > 0) | ascii_downcase);
		[
			$body.reconciliations[]?
			| provider_repository_candidates(.provider_alert // {}) as $anchors
			| select(any($anchors[]; repository_anchor_matches(.)))
		] | length
	' "${file}"
}

target_story_expected_security_alert_rows_count() {
	local expected_file="$1"
	jq -er '
		def expected_alerts:
			if type == "array" then .
			else (.alerts // [])
			end;
		expected_alerts | length
	' "${expected_file}"
}

target_story_validate_expected_security_alert_rows() {
	local expected_file="$1"
	local expected_count
	expected_count="$(target_story_expected_security_alert_rows_count "${expected_file}")"
	if ((expected_count > 200)); then
		echo "target security_alert_expected_rows cannot exceed bounded list limit 200" >&2
		return 1
	fi
	jq -e '
		def expected_alerts:
			if type == "array" then .
			else (.alerts // [])
			end;
		all(expected_alerts[]?;
			(((.provider_alert_id // "") | length) > 0 or (.provider_alert_number? != null))
		)
	' "${expected_file}" >/dev/null || {
		echo "target security_alert_expected_rows requires provider_alert_id or provider_alert_number" >&2
		return 1
	}
}

target_story_compare_security_alert_expected_rows() {
	local expected_file="$1"
	local actual_file="$2"
	target_story_validate_expected_security_alert_rows "${expected_file}"
	jq -nr --slurpfile expected "${expected_file}" --slurpfile actual "${actual_file}" '
		def expected_alerts:
			($expected[0] | if type == "array" then . else (.alerts // []) end);
		def actual_rows:
			(($actual[0].data // $actual[0]).reconciliations // []);
		def actual_fixed_version($row):
			($row.provider_alert.patched_version // $row.provider_alert.fixed_version // "");
		def actual_observed_version($row):
			($row.eshu_impact.observed_version // "");
		def actual_impact_status($row):
			($row.eshu_impact.impact_status // "");
		def expected_fixed_version($row):
			($row.fixed_version // $row.patched_version // "");
		def expected_observed_version($row):
			($row.installed_version // $row.observed_version // "");
		def matches_identifier($want; $row):
			((($want.provider_alert_id // "") | length) > 0 and
			 (($row.provider_alert.provider_alert_id // "") == $want.provider_alert_id))
			or
			(($want.provider_alert_number? != null) and
			 (($row.provider_alert.provider_alert_number // null | tostring) == ($want.provider_alert_number | tostring)));
		def field_mismatches($want; $row):
			[
				(if $want.provider? then (($row.provider_alert.provider // "") == $want.provider) else true end),
				(if $want.provider_state? then (($row.provider_alert.provider_state // "") == $want.provider_state) else true end),
				(if $want.ecosystem? then (($row.provider_alert.ecosystem // "") == $want.ecosystem) else true end),
				(if $want.package_name? then (($row.provider_alert.package_name // "") == $want.package_name) else true end),
				(if $want.manifest_path? then (($row.provider_alert.manifest_path // "") == $want.manifest_path) else true end),
				(if $want.vulnerable_range? then (($row.provider_alert.vulnerable_range // "") == $want.vulnerable_range) else true end),
				(if (expected_fixed_version($want) | length) > 0 then (actual_fixed_version($row) == expected_fixed_version($want)) else true end),
				(if (expected_observed_version($want) | length) > 0 then (actual_observed_version($row) == expected_observed_version($want)) else true end),
				(if $want.reconciliation_status? then (($row.reconciliation_status // "") == $want.reconciliation_status) else true end),
				(if $want.impact_status? then (actual_impact_status($row) == $want.impact_status) else true end)
			] | map(select(. == false)) | length;
		def has_evidence_or_reason($want; $row):
			if (($want.requires_evidence // true) == false) then true
			else
				((($row.evidence_fact_ids // []) | length) > 0)
				or ((($row.reason // "") | ascii_downcase) | test("missing|unavailable|unsupported|provider[-_]only|stale"))
				or ((($row.eshu_impact.missing_evidence // []) | length) > 0)
				or ((($row.missing_evidence_reason // "") | length) > 0)
			end;
		reduce expected_alerts[] as $want (
			{expected_count: 0, missing_count: 0, mismatch_count: 0, evidence_gap_count: 0};
			(actual_rows | map(select(matches_identifier($want; .))) | .[0]) as $match |
			.expected_count += 1 |
			if $match == null then
				.missing_count += 1
			else
				(if field_mismatches($want; $match) > 0 then .mismatch_count += 1 else . end) |
				(if has_evidence_or_reason($want; $match) then . else .evidence_gap_count += 1 end)
			end
		)
		| [.expected_count, .missing_count, .mismatch_count, .evidence_gap_count]
		| @tsv
	'
}
