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
			{Name: "advisory_id", Type: PlaybookInputIdentifier, Description: "Optional source advisory identifier such as GHSA, OSV, GLAD, vendor advisory, or CVE id."},
			{Name: "cve_id", Type: PlaybookInputIdentifier, Description: "Optional CVE identifier when the subject is known to be a CVE."},
			{Name: "finding_id", Type: PlaybookInputIdentifier, Description: "Optional reducer-owned supply-chain finding identifier for explain drilldowns."},
			{Name: "image_ref", Type: PlaybookInputIdentifier, Description: "Optional image reference for reducer-owned image identity and impact reads."},
			{Name: "owner_ref", Type: PlaybookInputIdentifier, Description: "Optional service-catalog owner reference for ownership drilldowns."},
			{Name: "package_id", Type: PlaybookInputIdentifier, Description: "Optional normalized package identity such as pkg:npm/example."},
			{Name: "repo_id", Type: PlaybookInputIdentifier, Description: "Optional repository scope for package, SBOM, and impact reads."},
			{Name: "service_id", Type: PlaybookInputIdentifier, Description: "Optional service selector for service-story drilldown."},
			{Name: "subject_digest", Type: PlaybookInputIdentifier, Description: "Optional image or artifact digest from SBOM/runtime evidence."},
			{Name: "workload_id", Type: PlaybookInputIdentifier, Description: "Optional workload selector for impact and ownership drilldowns."},
		},
		RequiredEvidence: []WorkflowEvidence{
			{Key: "advisory", Name: "Advisory source truth", Description: "Source advisory identity, affected range, severity, freshness, and source disagreement.", SourceFamilies: []string{"vulnerability_source", "advisory_catalog"}},
			{Key: "package", Name: "Owned package evidence", Description: "Repository-owned package/version evidence rather than provider-alert-only claims.", SourceFamilies: []string{"package_registry", "dependency_inventory"}},
			{Key: "impact", Name: "Reducer-owned impact finding", Description: "Canonical vulnerable package or image impact evidence with readiness classification.", SourceFamilies: []string{"supply_chain_impact"}},
		},
		OptionalEvidence: []WorkflowEvidence{
			{Key: "scanner", Name: "Scanner route/readiness contract", Description: "Vulnerability scanner route support, unsupported filters, and missing-evidence semantics.", SourceFamilies: []string{"scanner_contract", "readiness"}},
			{Key: "sbom", Name: "SBOM and attestation path", Description: "Image or package SBOM attachment evidence.", SourceFamilies: []string{"sbom", "attestation", "container_image"}},
			{Key: "image", Name: "Container image identity", Description: "Reducer-owned image identity and source bridge evidence.", SourceFamilies: []string{"container_image", "oci_registry"}},
			{Key: "workload", Name: "Workload context", Description: "Reducer-owned workload or deployment evidence for the vulnerable component.", SourceFamilies: []string{"workload", "deployment"}},
			{Key: "service", Name: "Service story", Description: "Service dossier and deployment lanes for the vulnerable component.", SourceFamilies: []string{"service_story", "deployment"}},
			{Key: "owner", Name: "Owner and catalog correlation", Description: "Service catalog, owner, repository, or workload ownership candidates.", SourceFamilies: []string{"service_catalog", "ownership"}},
			{Key: "freshness", Name: "Generation freshness", Description: "Current or stale generation state for the answer scope.", SourceFamilies: []string{"freshness", "status"}},
		},
		OutputPacket: WorkflowOutputPacket{
			Schema:      "guided-vulnerable-dependency.v1",
			TruthLabels: []string{"exact", "derived", "unsupported", "stale", "permission_hidden"},
			Sections:    []string{"subject", "scanner", "advisory", "package", "impact", "sbom", "image", "workload", "service", "owner", "refusal_reasons", "missing_evidence", "recommended_next_calls"},
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
		FailureModes: []WorkflowFailureMode{
			{
				Condition:         "scanner_absent",
				Meaning:           "Scanner support, precise-version coverage, or route support is unavailable for the requested subject or ecosystem.",
				RecommendedAction: "Read the scanner contract and return unsupported or missing-evidence state instead of claiming no vulnerability.",
				RelatedEvidence:   []string{"scanner", "package", "impact"},
			},
			{
				Condition:         "sbom_absent",
				Meaning:           "SBOM or attestation evidence is missing, so image/package impact cannot be promoted through an SBOM path.",
				RecommendedAction: "Read bounded SBOM attachment rows and keep missing SBOM evidence distinct from no vulnerable package.",
				RelatedEvidence:   []string{"sbom", "image", "impact"},
			},
			{
				Condition:         "stale_generation",
				Meaning:           "Generation freshness is stale or still building, so absence cannot be treated as current truth.",
				RecommendedAction: "Read generation lifecycle before interpreting empty impact, SBOM, image, service, or owner evidence.",
				RelatedEvidence:   []string{"freshness"},
			},
			{
				Condition:         "permission_hidden",
				Meaning:           "Scoped authorization may hide repositories, services, or owner evidence from the caller.",
				RecommendedAction: "Return permission-hidden state and governance posture instead of suggesting out-of-scope tenant reads.",
				RelatedEvidence:   []string{"permission_hidden", "owner", "service"},
			},
		},
		MissingEvidenceRoutes: []WorkflowMissingEvidenceRoute{
			{
				EvidenceKey: "scanner",
				States:      []string{"unsupported", "missing_evidence"},
				Calls: []WorkflowNextCall{{
					ID:               "scanner_contract",
					Tool:             "get_vulnerability_scanner_read_contract",
					Reason:           "read scanner support and missing-evidence semantics before treating absent scanner output as no impact",
					Params:           []PlaybookParam{constStringParam("route", "impact_findings")},
					ExpectedEvidence: "scanner route contract with supported filters, unsupported filters, and missing-evidence semantics",
				}},
			},
			{
				EvidenceKey: "advisory",
				Calls: []WorkflowNextCall{{
					ID:                "advisory_sources",
					Tool:              "list_advisory_evidence",
					Reason:            "hydrate source advisory truth before inferring owned impact",
					Params:            []PlaybookParam{inputParam("advisory_id", "advisory_id"), inputParam("cve_id", "cve_id"), inputParam("package_id", "package_id"), inputParam("repository_id", "repo_id"), inputParam("service_id", "service_id"), inputParam("workload_id", "workload_id"), limitParam("limit", 10)},
					RequiredInputsAny: []string{"advisory_id", "cve_id", "package_id", "repo_id", "service_id", "workload_id"},
					ExpectedEvidence:  "bounded advisory source rows with freshness and disagreement metadata",
				}},
			},
			{
				EvidenceKey: "package",
				Calls: []WorkflowNextCall{{
					ID:                "package_correlation",
					Tool:              "list_package_registry_correlations",
					Reason:            "check whether the package evidence is owned by the scoped repository",
					Params:            []PlaybookParam{inputParam("repository_id", "repo_id"), inputParam("package_id", "package_id"), constStringParam("relationship_kind", "consumption"), limitParam("limit", 10)},
					RequiredInputsAny: []string{"repo_id", "package_id"},
					ExpectedEvidence:  "bounded package correlation rows or explicit missing package evidence",
				}},
			},
			{
				EvidenceKey: "sbom",
				Calls: []WorkflowNextCall{{
					ID:                "sbom_attachment",
					Tool:              "list_sbom_attestation_attachments",
					Reason:            "check image or package SBOM attachment evidence before claiming image impact",
					Params:            []PlaybookParam{inputParam("repository_id", "repo_id"), inputParam("service_id", "service_id"), inputParam("subject_digest", "subject_digest"), inputParam("workload_id", "workload_id"), limitParam("limit", 10)},
					RequiredInputsAny: []string{"repo_id", "service_id", "subject_digest", "workload_id"},
					ExpectedEvidence:  "bounded SBOM attachment rows, missing-image evidence, or missing SBOM evidence",
				}},
			},
			{
				EvidenceKey: "impact",
				Calls: []WorkflowNextCall{{
					ID:                "impact_explanation",
					Tool:              "explain_supply_chain_impact",
					Reason:            "explain one reducer-owned impact path before presenting impact as exact, derived, unsupported, stale, or hidden",
					Params:            []PlaybookParam{inputParam("advisory_id", "advisory_id"), inputParam("cve_id", "cve_id"), inputParam("finding_id", "finding_id"), inputParam("package_id", "package_id"), inputParam("repository_id", "repo_id"), inputParam("subject_digest", "subject_digest")},
					RequiredInputsAny: []string{"advisory_id", "cve_id", "finding_id"},
					ExpectedEvidence:  "bounded impact explanation with source path, readiness, missing evidence, and recommended next calls",
				}},
			},
			{
				EvidenceKey: "image",
				Calls: []WorkflowNextCall{{
					ID:                "image_identity",
					Tool:              "list_container_image_identities",
					Reason:            "read reducer-owned image identities before linking a package or SBOM to runtime image impact",
					Params:            []PlaybookParam{inputParam("image_ref", "image_ref"), inputParam("digest", "subject_digest"), inputParam("source_repository_id", "repo_id"), limitParam("limit", 10)},
					RequiredInputsAny: []string{"image_ref", "subject_digest", "repo_id"},
					ExpectedEvidence:  "bounded image identity rows with source repository bridge and reducer outcome",
				}},
			},
			{
				EvidenceKey: "workload",
				Calls: []WorkflowNextCall{{
					ID:                "impact_findings",
					Tool:              "list_supply_chain_impact_findings",
					Reason:            "read reducer-owned finding rows that connect the advisory to repository, service, workload, or image scope",
					Params:            []PlaybookParam{inputParam("advisory_id", "advisory_id"), inputParam("cve_id", "cve_id"), inputParam("package_id", "package_id"), inputParam("repository_id", "repo_id"), inputParam("service_id", "service_id"), inputParam("subject_digest", "subject_digest"), inputParam("workload_id", "workload_id"), inputParam("image_ref", "image_ref"), limitParam("limit", 10)},
					RequiredInputsAny: []string{"advisory_id", "cve_id", "package_id", "repo_id", "service_id", "subject_digest", "workload_id", "image_ref"},
					ExpectedEvidence:  "bounded reducer-owned impact findings with readiness and missing-evidence metadata",
				}},
			},
			{
				EvidenceKey: "service",
				Calls: []WorkflowNextCall{{
					ID:                "service_story",
					Tool:              "get_service_story",
					Reason:            "read the service dossier before asserting the vulnerable component has no service or deployment context",
					Params:            []PlaybookParam{inputParam("service_name", "service_id"), inputParam("workload_id", "workload_id"), inputParam("repo_id", "repo_id")},
					RequiredInputsAny: []string{"service_id", "workload_id"},
					ExpectedEvidence:  "service story with deployment lanes, dependencies, consumers, and evidence handles",
				}},
			},
			{
				EvidenceKey: "owner",
				Calls: []WorkflowNextCall{{
					ID:                "owner_correlation",
					Tool:              "list_service_catalog_correlations",
					Reason:            "read service catalog and ownership correlations before recommending an owner or escalation path",
					Params:            []PlaybookParam{inputParam("owner_ref", "owner_ref"), inputParam("repository_id", "repo_id"), inputParam("service_id", "service_id"), inputParam("workload_id", "workload_id"), limitParam("limit", 10)},
					RequiredInputsAny: []string{"owner_ref", "repo_id", "service_id", "workload_id"},
					ExpectedEvidence:  "bounded service catalog ownership or explicit missing-owner evidence",
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
			{Name: "account_id", Type: PlaybookInputIdentifier, Description: "Optional AWS account ID alias to further bound provider-neutral runtime drift reads."},
			{Name: "environment", Type: PlaybookInputString, Description: "Optional runtime environment selector."},
			{Name: "project_id", Type: PlaybookInputIdentifier, Description: "Optional GCP project ID alias to further bound provider-neutral runtime drift reads."},
			{Name: "provider", Type: PlaybookInputString, Description: "Optional runtime provider filter such as aws, gcp, or azure."},
			{Name: "repo_id", Type: PlaybookInputIdentifier, Description: "Optional repository anchor for reducer admission and deployment config evidence."},
			{Name: "subscription_id", Type: PlaybookInputIdentifier, Description: "Optional Azure subscription ID alias to further bound provider-neutral runtime drift reads."},
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
			Sections:    []string{"deployable_unit", "admission", "deployment_config", "runtime_state", "drift", "service", "freshness", "missing_evidence", "recommended_next_calls"},
		},
		ToolGroups: []WorkflowToolGroup{
			{Name: "admission", Tools: []string{"list_admission_decisions", "get_relationship_evidence"}},
			{Name: "deployment_config", Tools: []string{"investigate_deployment_config", "explain_iac_management_status"}},
			{Name: "runtime_drift", Tools: []string{"find_unmanaged_resources", "list_cloud_runtime_drift_findings"}},
			{Name: "service_context", Tools: []string{"get_workload_story", "get_generation_lifecycle"}},
		},
		StarterPrompts: []string{
			"Explain why deployable_unit_id is accepted, drifted, unmanaged, rejected, or ambiguous, and name the next evidence calls.",
			"Start from this deployable unit and compare source, deployment config, reducer decision, runtime state, and freshness.",
		},
		MissingEvidenceRoutes: []WorkflowMissingEvidenceRoute{
			{
				EvidenceKey: "admission",
				Calls: []WorkflowNextCall{{
					ID:                "admission_decision",
					Tool:              "list_admission_decisions",
					Reason:            "read reducer-owned admission state before treating a deployable candidate as canonical truth",
					Params:            []PlaybookParam{constStringParam("domain", "deployable_unit_correlation"), inputParam("scope_id", "scope_id"), inputParam("generation_id", "generation_id"), constStringParam("anchor_kind", "repository"), inputParam("anchor_id", "repo_id"), boolParam("include_evidence", true), limitParam("limit", 10)},
					RequiredInputsAny: []string{"repo_id"},
					ExpectedEvidence:  "bounded admission rows with admitted, rejected, ambiguous, stale, or missing-evidence reasons plus source handles",
				}},
			},
			{
				EvidenceKey: "deployment_config",
				Calls: []WorkflowNextCall{{
					ID:               "deployment_config_influence",
					Tool:             "investigate_deployment_config",
					Reason:           "trace source deployment config before interpreting runtime drift",
					Params:           []PlaybookParam{inputParam("workload_id", "deployable_unit_id"), inputParam("environment", "environment"), limitParam("limit", 10)},
					ExpectedEvidence: "bounded source deployment config and influence evidence",
				}},
			},
			{
				EvidenceKey: "runtime",
				Calls: []WorkflowNextCall{{
					ID:               "runtime_drift",
					Tool:             "list_cloud_runtime_drift_findings",
					Reason:           "check observed provider-neutral runtime drift or unmanaged-resource evidence for the deployable unit scope",
					Params:           []PlaybookParam{inputParam("scope_id", "scope_id"), inputParam("account_id", "account_id"), inputParam("project_id", "project_id"), inputParam("subscription_id", "subscription_id"), inputParam("provider", "provider"), limitParam("limit", 10)},
					ExpectedEvidence: "bounded runtime drift rows with freshness and missing-source state",
				}},
			},
			{
				EvidenceKey: "service",
				Calls: []WorkflowNextCall{{
					ID:               "workload_story",
					Tool:             "get_workload_story",
					Reason:           "read service or workload story evidence before claiming the deployable unit has no service context",
					Params:           []PlaybookParam{inputParam("workload_id", "deployable_unit_id"), inputParam("environment", "environment")},
					ExpectedEvidence: "bounded workload story with service, deployment, ownership, dependency, and evidence handles",
				}},
			},
			{
				EvidenceKey: "freshness",
				States:      []string{"stale", "building"},
				Calls: []WorkflowNextCall{{
					ID:               "generation_lifecycle",
					Tool:             "get_generation_lifecycle",
					Reason:           "classify the scoped generation as stale, building, failed, or current before treating missing drift evidence as absence",
					Params:           []PlaybookParam{inputParam("scope_id", "scope_id"), inputParam("generation_id", "generation_id"), limitParam("limit", 10)},
					ExpectedEvidence: "bounded generation lifecycle rows for the requested scope and generation",
				}},
			},
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
			{Name: "incident_id", Type: PlaybookInputIdentifier, Description: "Optional incident identifier to investigate."},
			{Name: "environment", Type: PlaybookInputString, Description: "Optional environment selector."},
			{Name: "provider", Type: PlaybookInputString, Description: "Optional incident provider, such as pagerduty."},
			{Name: "provider_service_id", Type: PlaybookInputIdentifier, Description: "Optional provider-native incident service identifier for incident-source drilldowns."},
			{Name: "repo_id", Type: PlaybookInputIdentifier, Description: "Optional repository scope for recent CI/CD change and freshness evidence."},
			{Name: "scope_id", Type: PlaybookInputIdentifier, Description: "Optional provider or ingestion scope that bounds incident, observability, change, and freshness evidence."},
			{Name: "service_id", Type: PlaybookInputIdentifier, Description: "Optional service or workload selector for service-story, runtime, and observability drilldowns."},
			{Name: "since", Type: PlaybookInputString, Description: "Optional RFC3339 lower bound for incident fallback change candidates."},
			{Name: "until", Type: PlaybookInputString, Description: "Optional RFC3339 upper bound for incident fallback change candidates."},
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
					ID:                "incident_context",
					Tool:              "get_incident_context",
					Reason:            "read the bounded incident context before composing service or change evidence",
					Params:            []PlaybookParam{inputParam("provider_incident_id", "incident_id"), inputParam("provider", "provider"), inputParam("scope_id", "scope_id"), inputParam("service_id", "provider_service_id"), inputParam("since", "since"), inputParam("until", "until"), limitParam("limit", 25)},
					RequiredInputsAny: []string{"incident_id"},
					ExpectedEvidence:  "incident context with routing, timeline, service, runtime, review, and work-item handles",
				}},
			},
			{
				EvidenceKey: "service",
				Calls: []WorkflowNextCall{{
					ID:                "service_story",
					Tool:              "get_service_story",
					Reason:            "read service or workload story evidence before claiming the incident has no service context",
					Params:            []PlaybookParam{inputParam("workload_id", "service_id"), inputParam("repo_id", "repo_id"), inputParam("environment", "environment")},
					RequiredInputsAny: []string{"service_id"},
					ExpectedEvidence:  "bounded service story with deployment lanes, ownership, dependencies, consumers, and evidence handles",
				}},
			},
			{
				EvidenceKey: "observability",
				Calls: []WorkflowNextCall{{
					ID:                "observability_coverage",
					Tool:              "list_observability_coverage_correlations",
					Reason:            "check whether observability and alert coverage exists before declaring observability absent",
					Params:            []PlaybookParam{inputParam("scope_id", "scope_id"), inputParam("target_service_ref", "service_id"), limitParam("limit", 10)},
					RequiredInputsAny: []string{"service_id", "scope_id"},
					ExpectedEvidence:  "bounded observability correlation rows or explicit missing observability coverage",
				}},
			},
			{
				EvidenceKey: "changes",
				Calls: []WorkflowNextCall{{
					ID:                "service_changes",
					Tool:              "list_ci_cd_run_correlations",
					Reason:            "check recent service materialization changes before linking incident context to change evidence",
					Params:            []PlaybookParam{inputParam("scope_id", "scope_id"), inputParam("repository_id", "repo_id"), inputParam("environment", "environment"), limitParam("limit", 10)},
					RequiredInputsAny: []string{"repo_id", "scope_id", "environment"},
					ExpectedEvidence:  "bounded CI/CD correlation rows or explicit missing recent-change evidence",
				}},
			},
			{
				EvidenceKey: "runtime",
				Calls: []WorkflowNextCall{{
					ID:                "deployment_chain",
					Tool:              "trace_deployment_chain",
					Reason:            "trace deployment chain evidence before claiming the incident has no runtime or deployment context",
					Params:            []PlaybookParam{inputParam("service_name", "service_id"), boolParam("direct_only", true), limitParam("max_depth", 4)},
					RequiredInputsAny: []string{"service_id"},
					ExpectedEvidence:  "bounded deployment chain for the incident service with missing runtime hops called out",
				}},
			},
			{
				EvidenceKey: "freshness",
				States:      []string{"stale", "building"},
				Calls: []WorkflowNextCall{{
					ID:                "generation_lifecycle",
					Tool:              "get_generation_lifecycle",
					Reason:            "classify scoped generation freshness before treating missing incident, service, runtime, observability, or change evidence as absence",
					Params:            []PlaybookParam{inputParam("scope_id", "scope_id"), inputParam("repository", "repo_id"), limitParam("limit", 10)},
					RequiredInputsAny: []string{"scope_id", "repo_id"},
					ExpectedEvidence:  "bounded generation lifecycle rows for the incident scope or repository",
				}},
			},
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
