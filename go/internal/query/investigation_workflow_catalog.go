package query

func vulnerableDependencyWorkflow() InvestigationWorkflow {
	return InvestigationWorkflow{
		ID:          "guided_vulnerable_dependency",
		Name:        "Guided vulnerable-dependency investigation",
		Version:     "1.0.0",
		Domain:      "supply_chain",
		Description: "Starts from a CVE, advisory, or dependency and guides callers through advisory, package, SBOM, image, workload, service, and owner evidence.",
		RequiredInputs: []PlaybookInput{
			{Name: "subject", Type: PlaybookInputString, Required: true, Description: "CVE, advisory ID, or dependency/package selector."},
			{Name: "repo_id", Type: PlaybookInputIdentifier, Description: "Optional repository scope for package, SBOM, and impact reads."},
			{Name: "service_id", Type: PlaybookInputIdentifier, Description: "Optional service selector for service-story drilldown."},
		},
		RequiredEvidence: []WorkflowEvidence{
			{Key: "advisory", Name: "Advisory source truth", Description: "Source advisory identity, affected range, severity, freshness, and source disagreement.", SourceFamilies: []string{"vulnerability_source", "advisory_catalog"}},
			{Key: "package", Name: "Owned package evidence", Description: "Repository-owned package/version evidence rather than provider-alert-only claims.", SourceFamilies: []string{"package_registry", "dependency_inventory"}},
			{Key: "impact", Name: "Reducer-owned impact finding", Description: "Canonical vulnerable package or image impact evidence with readiness classification.", SourceFamilies: []string{"supply_chain_impact"}},
		},
		OptionalEvidence: []WorkflowEvidence{
			{Key: "sbom", Name: "SBOM and attestation path", Description: "Image or package SBOM attachment evidence.", SourceFamilies: []string{"sbom", "attestation", "container_image"}},
			{Key: "workload", Name: "Workload and service context", Description: "Service, workload, owner, and deployment evidence for the vulnerable component.", SourceFamilies: []string{"service_story", "deployment", "ownership"}},
			{Key: "freshness", Name: "Generation freshness", Description: "Current or stale generation state for the answer scope.", SourceFamilies: []string{"freshness", "status"}},
		},
		OutputPacket: WorkflowOutputPacket{
			Schema:      "guided-vulnerable-dependency.v1",
			TruthLabels: []string{"exact", "derived", "unsupported", "stale", "permission_hidden"},
			Sections:    []string{"subject", "advisory", "package", "impact", "sbom", "workload", "missing_evidence", "recommended_next_calls"},
		},
		ToolGroups: []WorkflowToolGroup{
			{Name: "supply_chain", Tools: []string{"list_supply_chain_impact_findings", "explain_supply_chain_impact", "list_advisory_evidence", "get_vulnerability_scanner_read_contract"}},
			{Name: "package_and_image", Tools: []string{"list_package_registry_correlations", "list_sbom_attestation_attachments", "list_container_image_identities"}},
			{Name: "service_context", Tools: []string{"get_service_story", "list_service_catalog_correlations", "get_generation_lifecycle"}},
		},
		StarterPrompts: []string{
			"Investigate whether subject affects repo_id, and show the missing supply-chain hops before recommending remediation.",
			"Start from this CVE or dependency and return advisory, package, SBOM, image, workload, and owner evidence with next calls for gaps.",
		},
		MissingEvidenceRoutes: []WorkflowMissingEvidenceRoute{
			{
				EvidenceKey: "advisory",
				Calls: []WorkflowNextCall{{
					ID:               "advisory_sources",
					Tool:             "list_advisory_evidence",
					Reason:           "hydrate source advisory truth before inferring owned impact",
					Params:           []PlaybookParam{inputParam("advisory_id", "subject"), limitParam("limit", 10)},
					ExpectedEvidence: "bounded advisory source rows with freshness and disagreement metadata",
				}},
			},
			{
				EvidenceKey: "package",
				Calls: []WorkflowNextCall{{
					ID:               "package_correlation",
					Tool:             "list_package_registry_correlations",
					Reason:           "check whether the package evidence is owned by the scoped repository",
					Params:           []PlaybookParam{inputParam("repository_id", "repo_id"), limitParam("limit", 10)},
					ExpectedEvidence: "bounded package correlation rows or explicit missing package evidence",
				}},
			},
			{
				EvidenceKey: "sbom",
				Calls: []WorkflowNextCall{{
					ID:               "sbom_attachment",
					Tool:             "list_sbom_attestation_attachments",
					Reason:           "check image or package SBOM attachment evidence before claiming image impact",
					Params:           []PlaybookParam{inputParam("repository_id", "repo_id"), limitParam("limit", 10)},
					ExpectedEvidence: "bounded SBOM attachment rows, missing-image evidence, or missing SBOM evidence",
				}},
			},
			{
				EvidenceKey: "workload",
				Calls: []WorkflowNextCall{{
					ID:               "impact_findings",
					Tool:             "list_supply_chain_impact_findings",
					Reason:           "read reducer-owned finding rows that connect the advisory to repository, service, workload, or image scope",
					Params:           []PlaybookParam{inputParam("advisory_id", "subject"), inputParam("repository_id", "repo_id"), limitParam("limit", 10)},
					ExpectedEvidence: "bounded reducer-owned impact findings with readiness and missing-evidence metadata",
				}},
			},
			commonFreshnessRoute(),
			commonPermissionRoute(),
		},
	}
}

func deployableDriftWorkflow() InvestigationWorkflow {
	return InvestigationWorkflow{
		ID:          "guided_deployable_drift",
		Name:        "Guided deployable drift investigation",
		Version:     "1.0.0",
		Domain:      "deployable_drift",
		Description: "Explains why a deployable unit is accepted, stale, drifted, unmanaged, rejected, or ambiguous.",
		RequiredInputs: []PlaybookInput{
			{Name: "deployable_unit_id", Type: PlaybookInputIdentifier, Required: true, Description: "Deployable unit, workload, or resource identity to investigate."},
			{Name: "generation_id", Type: PlaybookInputIdentifier, Required: true, Description: "Scope generation ID that bounds reducer admission evidence."},
			{Name: "scope_id", Type: PlaybookInputIdentifier, Required: true, Description: "Collector or ingestion scope ID that bounds admission and runtime drift evidence."},
			{Name: "account_id", Type: PlaybookInputIdentifier, Description: "Optional AWS account ID to further bound runtime drift reads."},
			{Name: "repo_id", Type: PlaybookInputIdentifier, Description: "Optional repository scope for deployment config evidence."},
			{Name: "region", Type: PlaybookInputString, Description: "Optional AWS region when account_id is supplied."},
			{Name: "environment", Type: PlaybookInputString, Description: "Optional runtime environment selector."},
		},
		RequiredEvidence: []WorkflowEvidence{
			{Key: "admission", Name: "Reducer admission decision", Description: "Accepted, rejected, ambiguous, stale, or permission-hidden reducer decision.", SourceFamilies: []string{"admission_decisions"}},
			{Key: "deployment_config", Name: "Deployment configuration evidence", Description: "IaC, Helm, Kustomize, Argo, or source deployment evidence.", SourceFamilies: []string{"iac", "deployment_config"}},
			{Key: "runtime", Name: "Runtime or cloud state", Description: "Observed cloud/runtime drift or unmanaged-resource evidence.", SourceFamilies: []string{"cloud", "runtime", "terraform_state"}},
		},
		OptionalEvidence: []WorkflowEvidence{
			{Key: "service", Name: "Service/workload story", Description: "Service context and ownership story for the deployable unit.", SourceFamilies: []string{"service_story"}},
			{Key: "freshness", Name: "Freshness and generation state", Description: "Generation lifecycle and service changed-since evidence.", SourceFamilies: []string{"freshness"}},
		},
		OutputPacket: WorkflowOutputPacket{
			Schema:      "guided-deployable-drift.v1",
			TruthLabels: []string{"exact", "derived", "ambiguous", "rejected", "stale", "permission_hidden"},
			Sections:    []string{"deployable_unit", "admission", "deployment_config", "runtime_state", "drift", "missing_evidence", "recommended_next_calls"},
		},
		ToolGroups: []WorkflowToolGroup{
			{Name: "admission", Tools: []string{"list_admission_decisions", "get_relationship_evidence"}},
			{Name: "deployment_config", Tools: []string{"investigate_deployment_config", "explain_iac_management_status"}},
			{Name: "runtime_drift", Tools: []string{"find_unmanaged_resources", "list_aws_runtime_drift_findings"}},
		},
		StarterPrompts: []string{
			"Explain why deployable_unit_id is accepted, drifted, unmanaged, rejected, or ambiguous, and name the next evidence calls.",
			"Start from this deployable unit and compare source, deployment config, reducer decision, runtime state, and freshness.",
		},
		MissingEvidenceRoutes: []WorkflowMissingEvidenceRoute{
			{
				EvidenceKey: "admission",
				Calls: []WorkflowNextCall{{
					ID:               "admission_decision",
					Tool:             "list_admission_decisions",
					Reason:           "read reducer-owned admission state before treating a deployable candidate as canonical truth",
					Params:           []PlaybookParam{constStringParam("domain", "deployable_unit"), inputParam("scope_id", "scope_id"), inputParam("generation_id", "generation_id"), constStringParam("anchor_kind", "workload"), inputParam("anchor_id", "deployable_unit_id"), limitParam("limit", 10)},
					ExpectedEvidence: "bounded admission rows with admitted, rejected, ambiguous, stale, or missing-evidence reasons",
				}},
			},
			{
				EvidenceKey: "deployment_config",
				Calls: []WorkflowNextCall{{
					ID:               "deployment_config_influence",
					Tool:             "investigate_deployment_config",
					Reason:           "trace source deployment config before interpreting runtime drift",
					Params:           []PlaybookParam{inputParam("workload_id", "deployable_unit_id"), inputParam("repo_id", "repo_id"), limitParam("limit", 10)},
					ExpectedEvidence: "bounded source deployment config and influence evidence",
				}},
			},
			{
				EvidenceKey: "runtime",
				Calls: []WorkflowNextCall{{
					ID:               "runtime_drift",
					Tool:             "list_aws_runtime_drift_findings",
					Reason:           "check observed runtime drift or unmanaged-resource evidence for the deployable unit",
					Params:           []PlaybookParam{inputParam("scope_id", "scope_id"), inputParam("account_id", "account_id"), inputParam("region", "region"), limitParam("limit", 10)},
					ExpectedEvidence: "bounded runtime drift rows with freshness and missing-source state",
				}},
			},
			commonFreshnessRoute(),
			commonPermissionRoute(),
		},
	}
}

func incidentContextWorkflow() InvestigationWorkflow {
	return InvestigationWorkflow{
		ID:          "guided_incident_context",
		Name:        "Guided incident-context investigation",
		Version:     "1.0.0",
		Domain:      "incident_context",
		Description: "Turns an incident, service, or environment into incident, service, deployment, runtime, observability, alert, and recent-change context.",
		RequiredInputs: []PlaybookInput{
			{Name: "incident_id", Type: PlaybookInputIdentifier, Required: true, Description: "Incident identifier to investigate."},
			{Name: "repo_id", Type: PlaybookInputIdentifier, Description: "Optional repository scope for recent CI/CD change evidence."},
			{Name: "service_id", Type: PlaybookInputIdentifier, Description: "Optional service or workload selector for service-story and freshness drilldowns."},
			{Name: "environment", Type: PlaybookInputString, Description: "Optional environment selector."},
		},
		RequiredEvidence: []WorkflowEvidence{
			{Key: "incident", Name: "Incident context", Description: "PagerDuty or incident-source context and routing evidence.", SourceFamilies: []string{"pagerduty", "incident_context"}},
			{Key: "service", Name: "Service/workload context", Description: "Service story, workload, deployment, and ownership handles.", SourceFamilies: []string{"service_story"}},
			{Key: "changes", Name: "Recent changes", Description: "CI/CD, commit, PR, deployment, or service changed-since evidence.", SourceFamilies: []string{"ci_cd", "freshness"}},
		},
		OptionalEvidence: []WorkflowEvidence{
			{Key: "observability", Name: "Observability and alert context", Description: "Observability coverage and alert-source correlation evidence.", SourceFamilies: []string{"observability", "alerts"}},
			{Key: "runtime", Name: "Runtime/deployment chain", Description: "Deployment and runtime chain evidence for the service.", SourceFamilies: []string{"deployment", "runtime"}},
		},
		OutputPacket: WorkflowOutputPacket{
			Schema:      "guided-incident-context.v1",
			TruthLabels: []string{"exact", "derived", "unsupported", "stale", "permission_hidden"},
			Sections:    []string{"incident", "service", "deployment", "runtime", "observability", "recent_changes", "missing_evidence", "recommended_next_calls"},
		},
		ToolGroups: []WorkflowToolGroup{
			{Name: "incident", Tools: []string{"get_incident_context", "list_work_item_evidence"}},
			{Name: "service_and_runtime", Tools: []string{"get_service_story", "trace_deployment_chain"}},
			{Name: "observability_and_changes", Tools: []string{"list_observability_coverage_correlations", "list_ci_cd_run_correlations"}},
		},
		StarterPrompts: []string{
			"Investigate incident_id and return incident, service, observability, runtime, and recent-change context with missing evidence called out.",
			"Start from this incident and name which source families are present, stale, or missing before recommending deeper tool calls.",
		},
		MissingEvidenceRoutes: []WorkflowMissingEvidenceRoute{
			{
				EvidenceKey: "incident",
				Calls: []WorkflowNextCall{{
					ID:               "incident_context",
					Tool:             "get_incident_context",
					Reason:           "read the bounded incident context before composing service or change evidence",
					Params:           []PlaybookParam{inputParam("incident_id", "incident_id")},
					ExpectedEvidence: "incident context with routing, timeline, service, runtime, review, and work-item handles",
				}},
			},
			{
				EvidenceKey: "observability",
				Calls: []WorkflowNextCall{{
					ID:               "observability_coverage",
					Tool:             "list_observability_coverage_correlations",
					Reason:           "check whether observability and alert coverage exists before declaring observability absent",
					Params:           []PlaybookParam{inputParam("service_id", "service_id"), inputParam("environment", "environment"), limitParam("limit", 10)},
					ExpectedEvidence: "bounded observability correlation rows or explicit missing observability coverage",
				}},
			},
			{
				EvidenceKey: "changes",
				Calls: []WorkflowNextCall{{
					ID:               "service_changes",
					Tool:             "list_ci_cd_run_correlations",
					Reason:           "check recent service materialization changes before linking incident context to change evidence",
					Params:           []PlaybookParam{inputParam("repository_id", "repo_id"), inputParam("environment", "environment"), limitParam("limit", 10)},
					ExpectedEvidence: "bounded CI/CD correlation rows or explicit missing recent-change evidence",
				}},
			},
			commonFreshnessRoute(),
			commonPermissionRoute(),
		},
	}
}

func commonFreshnessRoute() WorkflowMissingEvidenceRoute {
	return WorkflowMissingEvidenceRoute{
		EvidenceKey: "freshness",
		States:      []string{"stale", "building"},
		Calls: []WorkflowNextCall{{
			ID:               "generation_lifecycle",
			Tool:             "get_generation_lifecycle",
			Reason:           "classify stale or building freshness before treating missing evidence as absence",
			Params:           []PlaybookParam{limitParam("limit", 10)},
			ExpectedEvidence: "bounded generation lifecycle rows with freshness state and next check",
		}},
	}
}

func commonPermissionRoute() WorkflowMissingEvidenceRoute {
	return WorkflowMissingEvidenceRoute{
		EvidenceKey: "permission_hidden",
		States:      []string{"permission_hidden"},
		Calls: []WorkflowNextCall{{
			ID:               "governance_status",
			Tool:             "get_hosted_governance_status",
			Reason:           "classify permission-hidden state without suggesting tenant-data reads that may disclose hidden scope",
			ExpectedEvidence: "redacted governance status and scoped-token posture",
		}},
	}
}
