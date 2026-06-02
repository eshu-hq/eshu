#!/usr/bin/env bash
# Shared validation for public-safe Eshu E2E evidence manifests.

e2e_manifest_required_paths=(
	"run.id"
	"run.kind"
	"run.commit"
	"run.image_tag_candidate"
	"run.backend.kind"
	"corpus.mode"
	"corpus.repository_count"
	"corpus.coverage.ecosystems.npm"
	"corpus.coverage.ecosystems.gomod"
	"corpus.coverage.ecosystems.pypi"
	"corpus.coverage.ecosystems.maven"
	"corpus.coverage.ecosystems.composer"
	"corpus.coverage.ecosystems.rubygems"
	"corpus.coverage.ecosystems.cargo"
	"corpus.coverage.ecosystems.nuget"
	"corpus.coverage.evidence_families.terraform_iac"
	"corpus.coverage.evidence_families.kubernetes_iac"
	"corpus.coverage.evidence_families.image_sbom"
	"corpus.coverage.evidence_families.deployment"
	"corpus.coverage.evidence_families.vulnerability"
	"corpus.coverage.evidence_families.observability"
	"corpus.coverage.evidence_families.incident"
	"corpus.coverage.evidence_families.work_item"
	"runtimes.schema_bootstrap"
	"runtimes.api"
	"runtimes.mcp_server"
	"runtimes.ingester"
	"runtimes.resolution_engine"
	"runtimes.workflow_coordinator"
	"runtimes.hosted_collectors"
	"runtimes.scanner_worker"
	"collectors.git"
	"collectors.terraform_state"
	"collectors.aws_cloud"
	"collectors.oci_registry"
	"collectors.package_registry"
	"collectors.sbom_document"
	"collectors.provider_security_alerts"
	"collectors.vulnerability_intelligence"
	"collectors.scanner_worker"
	"collectors.confluence"
	"collectors.pagerduty"
	"collectors.jira"
	"collectors.grafana"
	"collectors.prometheus_mimir"
	"collectors.loki"
	"collectors.tempo"
	"reducers.repository_dependencies"
	"reducers.terraform_iac_relationships"
	"reducers.aws_cloud_relationships"
	"reducers.oci_image_identity"
	"reducers.sbom_attachment"
	"reducers.vulnerability_matching"
	"reducers.provider_alert_reconciliation"
	"reducers.supply_chain_impact"
	"reducers.deployment_correlation"
	"reducers.observability_correlation"
	"reducers.incident_work_item_correlation"
	"readback.api"
	"readback.mcp"
	"readback.cli"
	"queue.pending"
	"queue.in_flight"
	"queue.retrying"
	"queue.failed"
	"queue.dead_letter"
	"observability.pprof_status"
	"observability.logs_status"
	"observability.resource_snapshot_status"
	"privacy.status"
)

e2e_manifest_forbidden_keys='[
	"repository","repositories","repository_name","repository_id",
	"repo","repo_name","repo_id","package","packages","package_name",
	"package_id","provider_url","alert_url","installation",
	"provider_repository","url","host","hostname","ip","path","file",
	"token","payload","description","cve_description","transcript",
	"stdout","stderr","request","response","body","account_id","account"
]'

e2e_manifest_die() {
	printf 'e2e-evidence-manifest: %s\n' "$*" >&2
	return 1
}

e2e_manifest_require_tools() {
	command -v jq >/dev/null 2>&1 || { e2e_manifest_die "jq is required"; return 1; }
	command -v rg >/dev/null 2>&1 || { e2e_manifest_die "rg is required"; return 1; }
}

e2e_manifest_jq_path() {
	local dotted="$1"
	local jq_path=""
	local part
	local -a parts
	IFS='.' read -r -a parts <<<"${dotted}"
	for part in "${parts[@]}"; do
		jq_path="${jq_path}.${part}"
	done
	printf '%s' "${jq_path}"
}

e2e_manifest_require_path() {
	local file="$1"
	local dotted="$2"
	local jq_path
	jq_path="$(e2e_manifest_jq_path "${dotted}")"
	jq -e "${jq_path} != null" "${file}" >/dev/null \
		|| e2e_manifest_die "missing required evidence: ${dotted}"
}

e2e_manifest_validate_privacy() {
	local file="$1"
	if ! jq -e --argjson forbidden "${e2e_manifest_forbidden_keys}" '
		[.. | objects | keys[]? | select(. as $key | $forbidden | index($key))]
		| length == 0
	' "${file}" >/dev/null; then
		e2e_manifest_die "manifest looks like private data; forbidden private-looking keys are not accepted"
		return 1
	fi

	if jq -r '.. | strings' "${file}" | rg --quiet \
		'ghp_|github_pat_|glpat-|AKIA|ASIA|xox[baprs]-|https?://|/security/dependabot|arn:(aws|aws-us-gov|aws-cn):|/(Users|home|private|var|tmp|Volumes|workspace|workspaces|repos|personal-repos)/|(^|[^0-9])[0-9]{12}([^0-9]|$)|([0-9]{1,3}\.){3}[0-9]{1,3}|[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|(^|[[:space:]"'\''])[-A-Za-z0-9_.]+/[-A-Za-z0-9_.]+($|[[:space:]"'\'',])'; then
		e2e_manifest_die "manifest looks like private data; only aggregate counts, status enums, and public issue refs are accepted"
		return 1
	fi
}

e2e_manifest_validate_shape() {
	local file="$1"
	jq -e '
		.schema_version == 1 and
		(.status | IN("pass", "partial", "fail")) and
		(.run.kind | IN("clean", "preserved")) and
		(.corpus.mode | IN("smoke", "representative", "full")) and
		(.corpus.repository_count | type == "number" and . >= 0) and
		(.privacy.status | IN("pass", "fail"))
	' "${file}" >/dev/null || e2e_manifest_die "manifest root shape is invalid"
}

e2e_manifest_validate_component_statuses() {
	local file="$1"
	jq -e '
		def allowed($status): ["pass", "fail", "skipped", "unsupported"] | index($status);
		[
			(.runtimes // {} | to_entries[]),
			(.collectors // {} | to_entries[]),
			(.reducers // {} | to_entries[]),
			(.readback // {} | to_entries[]),
			(.corpus.coverage.ecosystems // {} | to_entries[]),
			(.corpus.coverage.evidence_families // {} | to_entries[])
		]
		| map(select((.value.status // "") as $status | allowed($status) | not))
		| length == 0
	' "${file}" >/dev/null || e2e_manifest_die "component status must be pass, fail, skipped, or unsupported"

	local classified
	classified="$(jq -r '
		[
			(.runtimes // {} | to_entries[] | {path: ("runtimes." + .key), value: .value}),
			(.collectors // {} | to_entries[] | {path: ("collectors." + .key), value: .value}),
			(.reducers // {} | to_entries[] | {path: ("reducers." + .key), value: .value}),
			(.readback // {} | to_entries[] | {path: ("readback." + .key), value: .value}),
			(.corpus.coverage.ecosystems // {} | to_entries[] | {path: ("corpus.coverage.ecosystems." + .key), value: .value}),
			(.corpus.coverage.evidence_families // {} | to_entries[] | {path: ("corpus.coverage.evidence_families." + .key), value: .value})
		][]
		| select((.value.status // "") | IN("fail", "skipped", "unsupported"))
		| select(((.value.reason // "") | length) == 0)
		| "\(.path) classified as \(.value.status) without reason"
	' "${file}" | sed -n '1p')"
	if [[ -n "${classified}" ]]; then
		e2e_manifest_die "${classified}"
		return 1
	fi

	if jq -e '.status == "pass"' "${file}" >/dev/null; then
		local non_pass
		non_pass="$(jq -r '
			[
				(.runtimes // {} | to_entries[] | {path: ("runtimes." + .key), status: (.value.status // "")}),
				(.collectors // {} | to_entries[] | {path: ("collectors." + .key), status: (.value.status // "")}),
				(.reducers // {} | to_entries[] | {path: ("reducers." + .key), status: (.value.status // "")}),
				(.readback // {} | to_entries[] | {path: ("readback." + .key), status: (.value.status // "")}),
				(.corpus.coverage.ecosystems // {} | to_entries[] | {path: ("corpus.coverage.ecosystems." + .key), status: (.value.status // "")}),
				(.corpus.coverage.evidence_families // {} | to_entries[] | {path: ("corpus.coverage.evidence_families." + .key), status: (.value.status // "")})
			]
			| map(select(.status != "pass"))
			| .[0].status // ""
		' "${file}")"
		if [[ -n "${non_pass}" ]]; then
			e2e_manifest_die "top-level status pass cannot include ${non_pass} evidence"
			return 1
		fi
	fi
}

e2e_manifest_validate_source_contracts() {
	local file="$1"
	if ! jq -e '.collectors.sbom_attestation == null' "${file}" >/dev/null; then
		e2e_manifest_die "collectors.sbom_attestation is ambiguous; use collectors.sbom_document for SBOM source facts and reducers.sbom_attachment for attachment truth"
		return 1
	fi

	jq -e '
		def positive($name):
			(.[$name] // 0) as $value
			| ($value | type == "number" and $value > 0);
		.collectors.sbom_document as $row
		| if (($row.status // "") == "pass") then
			($row | positive("source_facts") or positive("facts"))
		  else
			true
		  end
	' "${file}" >/dev/null \
		|| { e2e_manifest_die "collectors.sbom_document pass requires source_facts > 0 or facts > 0"; return 1; }

	jq -e '
		def positive($name):
			(.[$name] // 0) as $value
			| ($value | type == "number" and $value > 0);
		.collectors.scanner_worker as $row
		| if (($row.status // "") == "pass") then
			($row | positive("facts") or positive("source_facts") or positive("warnings"))
		  else
			true
		  end
	' "${file}" >/dev/null \
		|| { e2e_manifest_die "collectors.scanner_worker pass requires facts > 0, source_facts > 0, or warnings > 0"; return 1; }
}

e2e_manifest_validate_numeric_contracts() {
	local file="$1"
	local queue_name
	for queue_name in pending in_flight retrying failed dead_letter; do
		jq -e --arg name "${queue_name}" '.queue[$name] | type == "number" and . >= 0' "${file}" >/dev/null \
			|| { e2e_manifest_die "queue.${queue_name} must be a non-negative number"; return 1; }
	done
	if jq -e '.status == "pass"' "${file}" >/dev/null; then
		for queue_name in retrying failed dead_letter; do
			jq -e --arg name "${queue_name}" '.queue[$name] == 0' "${file}" >/dev/null \
				|| { e2e_manifest_die "queue.${queue_name} must be 0"; return 1; }
		done
		local surface
		for surface in api mcp cli; do
			jq -e --arg surface "${surface}" '
				(.readback[$surface].checked | type == "number" and . > 0) and
				((.readback[$surface].failed // 0) == 0)
			' "${file}" >/dev/null || { e2e_manifest_die "readback.${surface} must have checked > 0 and failed == 0"; return 1; }
		done
	fi
}

e2e_manifest_validate_reducer_rows() {
	local file="$1"
	local invalid
	invalid="$(jq -r '
		def readback_pass($surface):
			((.readback[$surface].status // "") == "pass") and
			((.readback[$surface].checked // 0) > 0) and
			((.readback[$surface].failed // 0) == 0);
		.reducers // {}
		| to_entries[]
		| select(.value.status == "pass")
		| select((.value.source_facts // 0) <= 0
			or (.value.reducer_facts // 0) <= 0
			or ((.value | readback_pass("api")) | not)
			or ((.value | readback_pass("mcp")) | not))
		| "reducers.\(.key) must include source_facts > 0, reducer_facts > 0, and API/MCP readback pass"
	' "${file}" | sed -n '1p')"
	if [[ -n "${invalid}" ]]; then
		e2e_manifest_die "${invalid}"
		return 1
	fi
}

e2e_manifest_validate_observability() {
	local file="$1"
	jq -e '
		(.observability.pprof_status // "") == "reachable" and
		(.observability.logs_status // "") == "captured" and
		(.observability.resource_snapshot_status // "") == "captured"
	' "${file}" >/dev/null || e2e_manifest_die "observability.pprof_status must be reachable and logs/resource snapshots must be captured"
}

validate_e2e_evidence_manifest() {
	local file="${1:-}"
	[[ -n "${file}" ]] || { e2e_manifest_die "manifest path is required"; return 1; }
	[[ -f "${file}" ]] || { e2e_manifest_die "manifest file not found: ${file}"; return 1; }
	e2e_manifest_require_tools || return 1
	jq -e . "${file}" >/dev/null 2>&1 || { e2e_manifest_die "manifest must be valid JSON"; return 1; }
	e2e_manifest_validate_privacy "${file}" || return 1
	e2e_manifest_validate_shape "${file}" || return 1
	local required_path
	for required_path in "${e2e_manifest_required_paths[@]}"; do
		e2e_manifest_require_path "${file}" "${required_path}" || return 1
	done
	e2e_manifest_validate_source_contracts "${file}" || return 1
	e2e_manifest_validate_component_statuses "${file}" || return 1
	e2e_manifest_validate_reducer_rows "${file}" || return 1
	e2e_manifest_validate_numeric_contracts "${file}" || return 1
	e2e_manifest_validate_observability "${file}" || return 1
	printf 'e2e-evidence-manifest: pass\n'
}
