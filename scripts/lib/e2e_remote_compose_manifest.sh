#!/usr/bin/env bash
# Helpers for the remote Compose E2E evidence manifest.

json_from_tsv() {
	local input="$1" output="$2" c1="$3" c2="$4"
	jq -R -s --arg c1 "${c1}" --arg c2 "${c2}" '
		split("\n")
		| map(select(length > 0) | split("\t"))
		| map({($c1): .[0], ($c2): .[1], count: (.[2] | tonumber)})
	' "${input}" >"${output}"
}

json_reducer_counts_from_tsv() {
	local input="$1" output="$2"
	jq -R -s '
		split("\n")
		| map(select(length > 0) | split("\t"))
		| map({
			reducer: .[0],
			source_facts: (.[1] | tonumber),
			reducer_facts: (.[2] | tonumber)
		})
	' "${input}" >"${output}"
}

json_array_from_lines() {
	local input="$1" output="$2"
	jq -R -s 'split("\n") | map(select(length > 0))' "${input}" >"${output}"
}

write_missing_readback_proof() {
	local output="$1"
	jq -n '{
		schema_version: 1,
		proof_id: "remote-compose-readback-proof-missing",
		surfaces: {
			api: {status: "fail", checked: 0, failed: 0, truncated: 0, unsupported: 0, missing_evidence: 0, ambiguous: 0, reason: "readback proof not provided"},
			mcp: {status: "fail", checked: 0, failed: 0, truncated: 0, unsupported: 0, missing_evidence: 0, ambiguous: 0, reason: "readback proof not provided"},
			cli: {status: "fail", checked: 0, failed: 0, truncated: 0, unsupported: 0, missing_evidence: 0, ambiguous: 0, reason: "readback proof not provided"}
		},
		queue: {retrying: 0, failed: 0, dead_letter: 0}
	}' >"${output}"
}

validate_readback_proof() {
	local proof="$1"
	[[ -f "${proof}" ]] || die "readback-proof file not found"
	jq -e '
		def nonneg($value): ($value | type == "number" and . >= 0);
		. as $root |
		.schema_version == 1 and
		all(["api","mcp","cli"][]; . as $surface |
			($root.surfaces[$surface].status | IN("pass", "fail", "skipped", "unsupported")) and
			nonneg($root.surfaces[$surface].checked) and
			nonneg($root.surfaces[$surface].failed) and
			nonneg($root.surfaces[$surface].truncated // 0) and
			nonneg($root.surfaces[$surface].unsupported // 0) and
			nonneg($root.surfaces[$surface].missing_evidence // 0) and
			nonneg($root.surfaces[$surface].ambiguous // 0))
	' "${proof}" >/dev/null || die "readback-proof shape is invalid"
	if jq -r '.. | strings' "${proof}" | rg --quiet \
		'ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|/security/dependabot|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}'; then
		die "readback-proof looks like private data"
	fi
}

build_manifest() {
	local facts_json="$1" workflow_json="$2" reducer_counts_json="$3" index_status="$4"
	local services_json="$5" stats_file="$6" readback_json="$7" output="$8"
	local commit
	if [[ -n "${commit_override}" ]]; then
		commit="${commit_override}"
	else
		commit="$(git -C "${REPO_ROOT}" rev-parse --short=12 HEAD)"
	fi
	jq -n \
		--slurpfile facts "${facts_json}" \
		--slurpfile workflow "${workflow_json}" \
		--slurpfile reducer_counts "${reducer_counts_json}" \
		--slurpfile index "${index_status}" \
		--slurpfile services "${services_json}" \
		--slurpfile coverage "${corpus_coverage}" \
		--slurpfile volume "${runtime_volume_proof}" \
		--slurpfile readback_proof "${readback_json}" \
		--arg run_kind "${run_kind}" \
		--arg commit "${commit}" \
		--arg image "${image_tag_candidate}" \
		--arg backend "${backend_kind}" \
		--arg corpus_mode "${corpus_mode}" \
		--arg unsupported_hosted_collectors "${unsupported_hosted_collectors}" \
		--arg unsupported_reducers "${unsupported_reducers}" \
		--argjson repository_count "${repository_count}" '
		($facts[0] // []) as $fact_rows |
		($workflow[0] // []) as $workflow_rows |
		($reducer_counts[0] // []) as $reducer_count_rows |
		($services[0] // []) as $service_rows |
		($unsupported_hosted_collectors | split(",") | map(gsub("^\\s+|\\s+$"; "")) | map(select(length > 0))) as $unsupported_hosted_rows |
		($unsupported_reducers | split(",") | map(gsub("^\\s+|\\s+$"; "")) | map(select(length > 0))) as $unsupported_reducer_rows |
		def sum_source($names): [$fact_rows[] | select(.source_system as $s | $names | index($s)) | .count] | add // 0;
		def sum_kind($pattern): [$fact_rows[] | select(.fact_kind | test($pattern)) | .count] | add // 0;
		def reducer_count($name; $field): [$reducer_count_rows[] | select(.reducer == $name) | .[$field]] | add // 0;
		def service_enabled($name): $service_rows | index($name) != null;
		def explicitly_unsupported_hosted($name): $unsupported_hosted_rows | index($name) != null;
		def explicitly_unsupported_reducer($name): $unsupported_reducer_rows | index($name) != null;
		def surface_row($surface):
			($readback_proof[0].surfaces[$surface] // {
				status: "fail", checked: 0, failed: 0, truncated: 0,
				unsupported: 0, missing_evidence: 0, ambiguous: 0,
				reason: "readback proof not provided"
			}) as $s |
			{
				status: ($s.status // "fail"),
				checked: ($s.checked // 0),
				failed: ($s.failed // 0),
				truncated: ($s.truncated // 0),
				unsupported: ($s.unsupported // 0),
				missing_evidence: ($s.missing_evidence // 0),
				ambiguous: ($s.ambiguous // 0)
			} + (if (($s.reason // "") | length) > 0 then {reason: $s.reason} else {} end);
		def surface_ok($surface):
			(surface_row($surface).status == "pass") and
			(surface_row($surface).checked > 0) and
			((surface_row($surface).failed // 0) == 0);
		def readback_ok: surface_ok("api") and surface_ok("mcp");
		def row_readback: {api: surface_row("api"), mcp: surface_row("mcp")};
		def collector_row($n):
			if $n > 0 then {status: "pass", facts: $n}
			else {status: "fail", facts: 0, reason: "no source facts observed"} end;
		def hosted_collector_row($name; $service; $n):
			if $n > 0 then {status: "pass", facts: $n}
			elif explicitly_unsupported_hosted($name) then {status: "unsupported", facts: 0, reason: "collector explicitly unsupported in remote Compose profile"}
			elif service_enabled($service) then {status: "fail", facts: 0, reason: "no source facts observed for enabled collector service"}
			else {status: "skipped", facts: 0, reason: "collector service disabled in remote Compose profile"} end;
		def reducer_row($name; $source; $reducer):
			{source_facts: $source, reducer_facts: $reducer, count: $reducer, readback: row_readback} as $row |
			if $source > 0 and $reducer > 0 and readback_ok then $row + {status: "pass"}
			elif explicitly_unsupported_reducer($name) then $row + {status: "unsupported", reason: "reducer path explicitly unsupported in remote Compose profile"}
			elif $source <= 0 then $row + {status: "fail", reason: "no source facts observed for reducer evidence path"}
			elif $reducer <= 0 then $row + {status: "fail", reason: "no reducer evidence observed"}
			else $row + {status: "fail", reason: "API/MCP readback proof missing or failed"} end;
		def incident_work_item_reducer_row($source; $reducer):
			{source_facts: $source, reducer_facts: $reducer, count: $reducer, readback: row_readback} as $row |
			if $source > 0 and $reducer > 0 and readback_ok then $row + {status: "pass"}
			elif explicitly_unsupported_reducer("incident_work_item_correlation") then $row + {status: "unsupported", reason: "reducer path explicitly unsupported in remote Compose profile"}
			elif $source <= 0 then $row + {status: "fail", reason: "no source facts observed for reducer evidence path"}
			elif $reducer <= 0 and readback_ok then $row + {
				status: "unsupported",
				reason: "incident and work-item evidence is source and API read-model evidence today; no reducer-owned incident work-item correlation fact is implemented",
				issue_refs: ["#1249"]
			}
			elif $reducer <= 0 then $row + {status: "fail", reason: "no reducer evidence observed"}
			else $row + {status: "fail", reason: "API/MCP readback proof missing or failed"} end;
		def workflow_completed($collector):
			[$workflow_rows[] | select(.collector_kind == $collector and .status == "completed") | .count] | add // 0;
		def workflow_row($collector): {completed: workflow_completed($collector)};
		def queue_num($name): ($index[0].queue[$name] // 0);
		{
			schema_version: 1,
			status: "pass",
			run: {
				id: ("remote-compose-" + $run_kind + "-" + $commit),
				kind: $run_kind,
				commit: $commit,
				image_tag_candidate: $image,
				backend: {kind: $backend}
			},
			corpus: {mode: $corpus_mode, repository_count: $repository_count, coverage: ($coverage[0] // {})},
			runtimes: {
				schema_bootstrap: {status: "pass"},
				api: {status: "pass"},
				mcp_server: {status: "pass"},
				ingester: {status: "pass"},
				resolution_engine: {status: "pass"},
				workflow_coordinator: {status: "pass"},
				hosted_collectors: {status: "pass"},
				scanner_worker: {status: "pass"}
			},
			collectors: {
				git: collector_row(sum_source(["git"])),
				terraform_state: collector_row(sum_source(["terraform_state"])),
				aws_cloud: collector_row(sum_source(["aws"])),
				oci_registry: collector_row(sum_source(["oci_registry"])),
				package_registry: collector_row(sum_source(["package_registry"])),
				sbom_document: collector_row(sum_source(["sbom_document"])),
				provider_security_alerts: collector_row(sum_source(["security_alert","security_alerts"])),
				vulnerability_intelligence: collector_row(sum_source(["vulnerability_intelligence"])),
				scanner_worker: collector_row(sum_source(["scanner_worker"])),
				confluence: collector_row(sum_source(["confluence"])),
				pagerduty: hosted_collector_row("pagerduty"; "collector-pagerduty"; sum_source(["pagerduty"])),
				jira: hosted_collector_row("jira"; "collector-jira"; sum_source(["jira"])),
				grafana: hosted_collector_row("grafana"; "collector-grafana"; sum_source(["grafana"])),
				prometheus_mimir: hosted_collector_row("prometheus_mimir"; "collector-prometheus-mimir"; sum_source(["prometheus_mimir"])),
				loki: hosted_collector_row("loki"; "collector-loki"; sum_source(["loki"])),
				tempo: hosted_collector_row("tempo"; "collector-tempo"; sum_source(["tempo"]))
			},
			reducers: {
				repository_dependencies: reducer_row("repository_dependencies"; sum_source(["git","package_registry"]); sum_kind("reducer_package_(ownership|consumption|publication)_correlation|reducer_package_correlation")),
				terraform_iac_relationships: reducer_row("terraform_iac_relationships"; reducer_count("terraform_iac_relationships"; "source_facts"); reducer_count("terraform_iac_relationships"; "reducer_facts")),
				aws_cloud_relationships: reducer_row("aws_cloud_relationships"; sum_source(["aws"]); sum_kind("reducer_aws|aws_relationship")),
				oci_image_identity: reducer_row("oci_image_identity"; sum_source(["oci_registry","scanner_worker"]); sum_kind("reducer_container_image_identity")),
				sbom_attachment: reducer_row("sbom_attachment"; sum_source(["sbom_document","scanner_worker"]); sum_kind("reducer_sbom_attestation_attachment")),
				vulnerability_matching: reducer_row("vulnerability_matching"; sum_source(["vulnerability_intelligence","scanner_worker","package_registry","security_alert"]); sum_kind("reducer_vulnerability_match|reducer_supply_chain_impact_finding")),
				provider_alert_reconciliation: reducer_row("provider_alert_reconciliation"; sum_source(["security_alert","security_alerts"]); sum_kind("reducer_security_alert_reconciliation")),
				supply_chain_impact: reducer_row("supply_chain_impact"; sum_source(["vulnerability_intelligence","scanner_worker","package_registry","security_alert"]); sum_kind("reducer_supply_chain_impact_finding")),
				deployment_correlation: reducer_row("deployment_correlation"; sum_source(["git","terraform_state","aws","oci_registry"]); sum_kind("reducer_(deployment|kubernetes|workload|ci_cd_run|service_catalog)_correlation|reducer_workload_identity")),
				observability_correlation: reducer_row("observability_correlation"; sum_kind("^(aws_resource|aws_relationship|observability\\.)"); sum_kind("reducer_observability(_coverage)?_correlation")),
				incident_work_item_correlation: incident_work_item_reducer_row(sum_kind("incident\\.|change\\.record|work_item\\.|incident_routing\\."); sum_kind("reducer_incident_work_item_correlation"))
			},
			readback: {api: surface_row("api"), mcp: surface_row("mcp"), cli: surface_row("cli")},
			queue: {
				pending: queue_num("pending"),
				in_flight: queue_num("in_flight"),
				retrying: queue_num("retrying"),
				failed: queue_num("failed"),
				dead_letter: queue_num("dead_letter")
			},
			workflow: {
				collector_claims: {
					git: workflow_row("git"),
					terraform_state: workflow_row("terraform_state"),
					aws: workflow_row("aws"),
					oci_registry: workflow_row("oci_registry"),
					package_registry: workflow_row("package_registry"),
					sbom_attestation: workflow_row("sbom_attestation"),
					security_alert: workflow_row("security_alert"),
					vulnerability_intelligence: workflow_row("vulnerability_intelligence"),
					scanner_worker: workflow_row("scanner_worker"),
					confluence: workflow_row("confluence"),
					pagerduty: workflow_row("pagerduty"),
					jira: workflow_row("jira"),
					grafana: workflow_row("grafana"),
					prometheus_mimir: workflow_row("prometheus_mimir"),
					loki: workflow_row("loki"),
					tempo: workflow_row("tempo")
				}
			},
			observability: {
				pprof_status: "reachable",
				logs_status: "captured",
				resource_snapshot_status: "captured",
				resource_snapshot_count: ([inputs] | length)
			},
			runtime_volume_proof: ($volume[0] // {}),
			privacy: {status: "pass"},
			follow_up_issues: [],
			preserved_restart: {
				duplicate_guard_status: "not_applicable",
				current_totals: {
					facts: ([$fact_rows[].count] | add // 0),
					claims: ([$workflow_rows[].count] | add // 0),
					findings: (sum_kind("reducer_supply_chain_impact_finding"))
				}
			}
		}
		| .status = (
			[
				(.collectors // {} | to_entries[] | .value.status),
				(.reducers // {} | to_entries[] | .value.status),
				(.readback // {} | to_entries[] | .value.status),
				(.corpus.coverage.ecosystems // {} | to_entries[] | .value.status),
				(.corpus.coverage.evidence_families // {} | to_entries[] | .value.status)
			] as $required_statuses |
			(((.queue.retrying // 0) > 0) or ((.queue.failed // 0) > 0) or ((.queue.dead_letter // 0) > 0)) as $queue_failed |
			if (($required_statuses | all(. == "pass")) and ($queue_failed | not)) then "pass"
			elif (($required_statuses | any(. == "fail")) or $queue_failed) then "fail"
			else "partial" end
		)
	' "${stats_file}" >"${output}"
}

print_nonpass_reasons() {
	jq -r '
		(.collectors // {} | to_entries[] | select(.value.status != "pass") |
			if (.value.reason // "") == "no source facts observed" then
				"collector \(.key) has no source facts"
			else
				"collector \(.key): \(.value.reason // "missing source facts")"
			end),
		(.reducers // {} | to_entries[] | select(.value.status != "pass") |
			"reducer \(.key): \(.value.reason // "missing evidence")"),
		(.readback // {} | to_entries[] | select(.value.status != "pass") |
			"readback \(.key): \(.value.reason // "missing readback proof")"),
		(.corpus.coverage.ecosystems // {} | to_entries[] | select(.value.status != "pass") |
			"ecosystem \(.key): \(.value.reason // "missing coverage")"),
		(.corpus.coverage.evidence_families // {} | to_entries[] | select(.value.status != "pass") |
			"evidence family \(.key): \(.value.reason // "missing coverage")")
	' "${manifest}" >&2
}
