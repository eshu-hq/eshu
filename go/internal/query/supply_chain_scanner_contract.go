package query

import "net/http"

type vulnerabilityScannerFilterContract struct {
	Name       string   `json:"name"`
	Support    string   `json:"support"`
	Semantics  []string `json:"semantics"`
	Parameters []string `json:"parameters"`
	Routes     []string `json:"routes"`
	Backing    string   `json:"backing"`
	Notes      string   `json:"notes,omitempty"`
}

type vulnerabilityScannerRouteContract struct {
	Name             string   `json:"name"`
	Path             string   `json:"path"`
	Tool             string   `json:"tool,omitempty"`
	TruthCapability  string   `json:"truth_capability"`
	LimitRequired    bool     `json:"limit_required"`
	Timeout          string   `json:"timeout"`
	Ordering         string   `json:"ordering"`
	TruncatedField   string   `json:"truncated_field"`
	MissingEvidence  string   `json:"missing_evidence"`
	UnsupportedQuery []string `json:"unsupported_query,omitempty"`
}

type remediationPacketContract struct {
	SchemaVersion  string                     `json:"schema_version"`
	Summary        string                     `json:"summary"`
	Sections       []remediationPacketSection `json:"sections"`
	MissingStates  []remediationPacketState   `json:"missing_states"`
	Surfaces       []remediationPacketSurface `json:"surfaces"`
	SecurityReview remediationPacketSecurity  `json:"security_review"`
}

type remediationPacketSection struct {
	Name                     string   `json:"name"`
	Representation           string   `json:"representation"`
	Evidence                 []string `json:"evidence"`
	MissingEvidence          string   `json:"missing_evidence"`
	DeterministicEvidence    []string `json:"deterministic_evidence,omitempty"`
	OptionalSemanticRequired bool     `json:"optional_semantic_required"`
}

type remediationPacketState struct {
	Name           string `json:"name"`
	Meaning        string `json:"meaning"`
	Representation string `json:"representation"`
}

type remediationPacketSurface struct {
	Name           string `json:"name"`
	Representation string `json:"representation"`
	TruthContract  string `json:"truth_contract"`
}

type remediationPacketSecurity struct {
	FalsePositiveControls []string `json:"false_positive_controls"`
	LeakageControls       []string `json:"leakage_controls"`
}

func (h *SupplyChainHandler) getVulnerabilityScannerReadContract(w http.ResponseWriter, r *http.Request) {
	route := QueryParam(r, "route")
	contract := vulnerabilityScannerReadContract()
	if route != "" {
		filtered := contractForScannerRoute(contract, route)
		if len(filtered) == 0 {
			WriteError(w, http.StatusBadRequest, "route must be one of impact_findings, impact_count, impact_inventory, impact_explain, security_alert_reconciliations, security_alert_count, security_alert_inventory, scanner_report")
			return
		}
		contract["routes"] = filtered
	}
	WriteSuccess(w, r, http.StatusOK, contract, BuildTruthEnvelope(
		h.profile(),
		vulnerabilityScannerReadContractCapability,
		TruthBasisContentIndex,
		"static API/MCP scanner read contract; route handlers own runtime evidence and missing-evidence envelopes",
	))
}

func vulnerabilityScannerReadContract() map[string]any {
	return map[string]any{
		"schema_version": "eshu.vulnerability_scanner_read_contract.v1",
		"filters": []vulnerabilityScannerFilterContract{
			{Name: "repository", Support: "derived", Semantics: []string{"exact", "derived", "provider-only"}, Parameters: []string{"repository_id"}, Routes: []string{"impact_findings", "impact_count", "impact_inventory", "impact_explain", "security_alert_reconciliations", "security_alert_count", "security_alert_inventory", "scanner_report"}, Backing: "repository selector plus reducer/Postgres read models; provider-only security alerts include provider repository scopes", Notes: "human selectors fail closed on unknown or ambiguous matches"},
			{Name: "package", Support: "exact", Semantics: []string{"exact"}, Parameters: []string{"package_id"}, Routes: []string{"impact_findings", "impact_count", "impact_inventory", "impact_explain", "security_alert_reconciliations", "security_alert_count", "security_alert_inventory"}, Backing: "payload package_id predicate over active reducer facts"},
			{Name: "advisory", Support: "exact", Semantics: []string{"exact", "provider-only"}, Parameters: []string{"cve_id", "advisory_id", "ghsa_id", "osv_id"}, Routes: []string{"impact_findings", "impact_count", "impact_inventory", "impact_explain", "security_alert_reconciliations", "security_alert_count", "security_alert_inventory"}, Backing: "payload CVE/advisory/GHSA predicates over active reducer and provider reconciliation facts", Notes: "provider alert routes support cve_id and ghsa_id only; advisory_id and osv_id are impact-route aliases"},
			{Name: "image_digest", Support: "exact", Semantics: []string{"exact", "missing-evidence driven"}, Parameters: []string{"subject_digest", "digest", "image_ref"}, Routes: []string{"impact_findings", "impact_count", "impact_inventory", "impact_explain", "scanner_report"}, Backing: "subject_digest and image_ref predicates over impact, SBOM, and container-image read models"},
			{Name: "workload", Support: "derived", Semantics: []string{"derived", "missing-evidence driven"}, Parameters: []string{"workload_id"}, Routes: []string{"impact_findings", "impact_count", "impact_inventory", "impact_explain"}, Backing: "reducer impact workload_ids array; missing runtime mapping remains missing evidence"},
			{Name: "service", Support: "derived", Semantics: []string{"derived", "missing-evidence driven"}, Parameters: []string{"service_id"}, Routes: []string{"impact_findings", "impact_count", "impact_inventory", "impact_explain"}, Backing: "reducer impact service_ids array; service comes only from admitted service/workload evidence"},
			{Name: "environment", Support: "derived", Semantics: []string{"derived", "missing-evidence driven"}, Parameters: []string{"environment"}, Routes: []string{"impact_findings", "impact_count", "impact_inventory"}, Backing: "reducer impact environments array; environment aliases are not guessed by the read layer"},
			{Name: "ecosystem", Support: "exact", Semantics: []string{"exact", "unsupported"}, Parameters: []string{"ecosystem"}, Routes: []string{"impact_findings", "impact_count", "impact_inventory"}, Backing: "payload ecosystem predicate plus readiness unsupported_targets for unsupported ecosystems"},
			{Name: "language", Support: "unsupported", Semantics: []string{"unsupported"}, Parameters: []string{"language"}, Routes: []string{}, Backing: "no scanner read model maps source language to vulnerability impact truth", Notes: "use ecosystem or package_id; language query params fail cheaply"},
			{Name: "severity", Support: "derived", Semantics: []string{"derived"}, Parameters: []string{"severity"}, Routes: []string{"impact_findings", "impact_count", "impact_inventory"}, Backing: "impact severity derives from selected CVSS score; provider severity stays inside provider alert rows and is not a scanner filter"},
			{Name: "status", Support: "exact", Semantics: []string{"exact", "provider-only"}, Parameters: []string{"impact_status", "reconciliation_status"}, Routes: []string{"impact_findings", "impact_count", "impact_inventory", "security_alert_reconciliations", "security_alert_count", "security_alert_inventory", "scanner_report"}, Backing: "impact_status is Eshu reducer truth; reconciliation_status compares provider alert state with Eshu impact truth"},
			{Name: "readiness", Support: "missing-evidence driven", Semantics: []string{"missing-evidence driven"}, Parameters: []string{"readiness"}, Routes: []string{"impact_findings", "impact_explain", "scanner_report"}, Backing: "readiness envelope built from source/read-model counts; it is not a row filter"},
			{Name: "provider_state", Support: "provider-only", Semantics: []string{"provider-only"}, Parameters: []string{"provider_state", "provider"}, Routes: []string{"security_alert_reconciliations", "security_alert_count", "security_alert_inventory"}, Backing: "provider alert reconciliation read model; never changes Eshu impact_status"},
		},
		"routes":             vulnerabilityScannerRouteContracts(),
		"remediation_packet": remediationPacketReadContract(),
	}
}

func remediationPacketReadContract() remediationPacketContract {
	return remediationPacketContract{
		SchemaVersion: "eshu.supply_chain_remediation_packet.v1",
		Summary:       "Deterministic CVE/package-to-owner packet assembled from reducer-owned impact explanation evidence; optional semantic output may summarize it but is never required to populate contract fields.",
		Sections: []remediationPacketSection{
			{Name: "vulnerability_fact", Representation: "advisory identity, aliases, severity, source, CVSS/EPSS/KEV/CWE, withdrawn state", Evidence: []string{"vulnerability.cve", "vulnerability.reference", "supply_chain_impact.explanation.advisory"}, MissingEvidence: "missing_advisory_evidence"},
			{Name: "package_fact", Representation: "ecosystem, normalized package id, installed version, vulnerable range, fixed versions, manifest path, dependency role", Evidence: []string{"vulnerability.affected_package", "package.consumption", "supply_chain_impact.package"}, MissingEvidence: "missing_package_evidence"},
			{Name: "sbom_subject", Representation: "SBOM document id/digest, subject digest, attachment status, warning summaries", Evidence: []string{"sbom.attestation", "reducer_sbom_attestation_attachment"}, MissingEvidence: "missing_sbom_subject"},
			{Name: "image_digest", Representation: "digest-first image identity, image reference, OCI repository, source repository bridge, stale-image warning", Evidence: []string{"oci.image_identity", "container_image_identity", "supply_chain_impact.image"}, MissingEvidence: "missing_image_digest"},
			{Name: "workload", Representation: "reducer-admitted workload id, environment, runtime/image anchors, missing runtime mapping state", Evidence: []string{"service_catalog.correlation", "kubernetes/workload evidence", "supply_chain_impact.workload"}, MissingEvidence: "missing_workload"},
			{Name: "service", Representation: "service id/name, catalog entity refs, repository/service story anchors", Evidence: []string{"service_catalog.correlation", "service_story.supply_chain"}, MissingEvidence: "missing_service"},
			{Name: "owner", Representation: "owner/team/contact handle from service catalog or repository ownership evidence", Evidence: []string{"service_catalog.owner", "repository.owner", "workload.owner"}, MissingEvidence: "missing_owner"},
			{Name: "exposure", Representation: "reachable/runtime/exposed-surface summary with confidence and missing-evidence reason", Evidence: []string{"reachability", "runtime evidence", "cloud/posture evidence"}, MissingEvidence: "missing_exposure_evidence"},
			{Name: "remediation_recommendation", Representation: "advisory-only upgrade recommendation, next action, confidence, blocker reason, and evidence handles", Evidence: []string{"supply_chain_impact.remediation"}, MissingEvidence: "missing_remediation_evidence", DeterministicEvidence: []string{"installed_version", "vulnerable_range", "fixed_versions", "manifest_range", "direct_or_transitive", "parent_package", "evidence_fact_ids"}, OptionalSemanticRequired: false},
		},
		MissingStates: []remediationPacketState{
			{Name: "missing_owner", Meaning: "owned impact exists, but no admitted service, workload, repository, or catalog owner evidence is available", Representation: "missing_evidence.owner"},
			{Name: "missing_workload", Meaning: "package/image impact exists, but no reducer-admitted workload/runtime anchor connects the finding to running compute", Representation: "missing_evidence.workload"},
			{Name: "stale_image", Meaning: "image evidence exists but freshness or source-state indicates the digest/reference may not describe the current deployed workload", Representation: "missing_evidence.image_freshness or freshness.state=stale"},
			{Name: "permission_hidden", Meaning: "the source collector reports an origin ACL/permission-hidden state; this is source evidence state, not caller authorization", Representation: "missing_evidence.permission_hidden"},
		},
		Surfaces: []remediationPacketSurface{
			{Name: "api", Representation: "/api/v0/supply-chain/impact/explain response payload", TruthContract: "application/eshu.envelope+json with supply_chain.impact.explain truth"},
			{Name: "mcp", Representation: "explain_supply_chain_impact envelope resource", TruthContract: "MCP resource block with application/eshu.envelope+json"},
			{Name: "console", Representation: "same packet sections rendered as CVE/package, evidence chain, owner/exposure, gaps, and next action", TruthContract: "Console must preserve API truth labels and missing-evidence state without relabeling gaps as clean"},
		},
		SecurityReview: remediationPacketSecurity{
			FalsePositiveControls: []string{
				"impact requires owned package/image/workload evidence; advisory-only facts stay source evidence",
				"ambiguous joins return missing/ambiguous evidence instead of selecting an owner or workload",
				"recommendations cite deterministic fixed-version and manifest evidence and do not require optional semantic output",
			},
			LeakageControls: []string{
				"permission-hidden source state stays explicit and distinct from caller permission_denied",
				"payloads use evidence handles and normalized ids; raw private provider payloads, secrets, local paths, and endpoint details stay out",
				"console and MCP render the same envelope truth and missing-evidence states as the API",
			},
		},
	}
}

func vulnerabilityScannerRouteContracts() []vulnerabilityScannerRouteContract {
	return []vulnerabilityScannerRouteContract{
		{Name: "impact_findings", Path: "/api/v0/supply-chain/impact/findings", Tool: "list_supply_chain_impact_findings", TruthCapability: supplyChainImpactFindingsCapability, LimitRequired: true, Timeout: "request context; bounded Postgres active-fact predicates only", Ordering: "finding_id asc by default; priority sorts tie-break by finding_id", TruncatedField: "truncated plus next_cursor.after_finding_id", MissingEvidence: "readiness.missing_evidence and finding.missing_evidence"},
		{Name: "impact_count", Path: "/api/v0/supply-chain/impact/findings/count", Tool: "count_supply_chain_impact_findings", TruthCapability: supplyChainImpactAggregateCapability, LimitRequired: false, Timeout: "request context; aggregate read model predicates only", Ordering: "not applicable", TruncatedField: "not applicable", MissingEvidence: "same scope semantics as impact_findings; use list/explain for readiness detail"},
		{Name: "impact_inventory", Path: "/api/v0/supply-chain/impact/inventory", Tool: "get_supply_chain_impact_inventory", TruthCapability: supplyChainImpactAggregateCapability, LimitRequired: false, Timeout: "request context; aggregate read model predicates only", Ordering: "count desc then bucket asc", TruncatedField: "truncated plus next_offset", MissingEvidence: "same scope semantics as impact_findings; use list/explain for readiness detail"},
		{Name: "impact_explain", Path: "/api/v0/supply-chain/impact/explain", Tool: "explain_supply_chain_impact", TruthCapability: supplyChainImpactExplanationCapability, LimitRequired: false, Timeout: "request context; one finding or one bounded advisory/package/repository/image/workload/service path", Ordering: "one row only; ambiguous scopes return conflict", TruncatedField: "not applicable", MissingEvidence: "explanation.missing_evidence and readiness.missing_evidence"},
		{Name: "security_alert_reconciliations", Path: "/api/v0/supply-chain/security-alerts/reconciliations", Tool: "list_security_alert_reconciliations", TruthCapability: securityAlertReconciliationsCapability, LimitRequired: true, Timeout: "request context; provider reconciliation read model predicates only", Ordering: "reconciliation_id asc after current-alert de-duplication", TruncatedField: "truncated plus next_cursor.after_reconciliation_id", MissingEvidence: "coverage.state target_incomplete for partial provider collection"},
		{Name: "security_alert_count", Path: "/api/v0/supply-chain/security-alerts/reconciliations/count", Tool: "count_security_alert_reconciliations", TruthCapability: securityAlertReconciliationAggregateCapability, LimitRequired: false, Timeout: "request context; aggregate read model predicates only", Ordering: "not applicable", TruncatedField: "not applicable", MissingEvidence: "coverage.state target_incomplete for partial provider collection"},
		{Name: "security_alert_inventory", Path: "/api/v0/supply-chain/security-alerts/reconciliations/inventory", Tool: "get_security_alert_reconciliation_inventory", TruthCapability: securityAlertReconciliationAggregateCapability, LimitRequired: false, Timeout: "request context; aggregate read model predicates only", Ordering: "count desc then bucket asc", TruncatedField: "truncated plus next_offset", MissingEvidence: "coverage.state target_incomplete for partial provider collection"},
		{Name: "scanner_report", Path: "eshu vuln-scan repo --json", TruthCapability: supplyChainImpactFindingsCapability, LimitRequired: true, Timeout: "CLI/API request context; report is assembled from impact_findings readiness", Ordering: "same as impact_findings", TruncatedField: "report.summary.truncated mirrors API truncated", MissingEvidence: "report.readiness and report.scope_plan missing_evidence"},
	}
}

func contractForScannerRoute(contract map[string]any, route string) []vulnerabilityScannerRouteContract {
	routes, _ := contract["routes"].([]vulnerabilityScannerRouteContract)
	for _, candidate := range routes {
		if candidate.Name == route {
			return []vulnerabilityScannerRouteContract{candidate}
		}
	}
	return nil
}
