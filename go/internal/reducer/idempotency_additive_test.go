// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "sort"

// B-6 (#3799) additive-domain coverage for the idempotency replay suite.
//
// The base catalog (DefaultDomainDefinitions) is only part of what the
// production reducer registers. appendAdditiveDomainDefinitions registers a much
// larger set of adapter-gated domains when their explicit handler dependencies
// are wired by the reducer binary. The coverage guard MUST hold those additive
// domains to the same bar — a replay case or a documented exemption — or an
// adapter-gated production domain could ship with neither while the guard stays
// green (codex P2: "Guard additive reducer domains").
//
// registrableReducerDomains() enumerates the full production-registrable
// superset from knownDomains, which is the authoritative source: Registry.Register
// rejects (via Domain.Validate -> knownDomains membership) any domain not present
// there, so a future additive domain cannot register without appearing in this
// guard. This file holds the additive enumeration and exemptions so
// idempotency_cases_test.go stays under the repo's 500-line cap.

// idempotencyReservedNonRegistrableDomains lists knownDomains entries that are
// declared for parsing/validation but are NOT registered as reducer emit domains
// by implementedDefaultDomainDefinitions. They have no handler and therefore no
// emit path to replay, so they are excluded from the registrable superset rather
// than exempted. Each entry is verified below to be absent from the production
// registration path.
//
//   - data_lineage / ownership / governance: legacy reserved domain identifiers
//     declared in intent.go; no handler is wired in the default catalog or the
//     additive helper, so they never register and have nothing to replay.
var idempotencyReservedNonRegistrableDomains = map[Domain]struct{}{
	DomainDataLineage: {},
	DomainOwnership:   {},
	DomainGovernance:  {},
}

// registrableReducerDomains returns every reducer domain the production registry
// can register: knownDomains minus the shared/edge projection sinks (driven by
// projection runners, not registered emit handlers) and minus the reserved,
// unwired identifiers. The result is the exact set the coverage guard must hold
// to a replay case or an exemption.
//
// Because knownDomains is the gate Registry.Register enforces, any new additive
// domain MUST be added there to register at all, which automatically pulls it
// into this set and forces a case or exemption — the guard cannot silently miss
// it.
func registrableReducerDomains() []Domain {
	projection := make(map[Domain]struct{}, len(allProjectionDomains))
	for _, domain := range allProjectionDomains {
		projection[domain] = struct{}{}
	}

	out := make([]Domain, 0, len(knownDomains))
	for domain := range knownDomains {
		if _, ok := projection[domain]; ok {
			continue
		}
		if _, ok := idempotencyReservedNonRegistrableDomains[domain]; ok {
			continue
		}
		out = append(out, domain)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// idempotencyExemptDomain reports whether a domain is exempted by either the base
// or the additive exemption set, plus the recorded reason.
func idempotencyExemptDomain(domain Domain) (string, bool) {
	if reason, ok := idempotencyExemptDomains[domain]; ok {
		return reason, true
	}
	reason, ok := idempotencyAdditiveExemptDomains[domain]
	return reason, ok
}

// idempotencyAdditiveExemptDomains records the additive, adapter-gated reducer
// domains that the B-6 registry-driven unit-replay suite deliberately does not
// unit-replay here, each with a one-line reason citing the dedicated suite that
// already proves its reprojection idempotency. Keep this set honest: prefer
// adding a real replay case to idempotencyReplayCases() when a domain becomes
// drivable with a recording fake and a static fact fixture.
//
// Every entry shares the same structural reason: the domain is registered only
// when the reducer binary wires its explicit evidence loader / graph writer
// adapters (appendAdditiveDomainDefinitions), and its emit path fans in across
// sources and reads back committed graph state (readiness lookups, prior-
// generation checks, cross-source evidence joins) before it emits. That makes a
// single constructible unit fixture unrepresentative, so each domain's idempotency
// is proven by its own dedicated suite instead.
var idempotencyAdditiveExemptDomains = map[Domain]string{
	// Terraform/cloud drift + cloud-inventory admission: evidence-loader gated,
	// proven by their own suites.
	DomainConfigStateDrift:        "additive, gated on TerraformBackendResolver+DriftEvidenceLoader+DriftLogger; drift-replay idempotency proven by config_state_drift handler suites (terraform_config_state_drift_*_test.go)",
	DomainAWSCloudRuntimeDrift:    "additive, gated on AWSCloudRuntimeDrift evidence loader+writer; idempotency proven by aws_cloud_runtime_drift_test.go",
	DomainMultiCloudRuntimeDrift:  "additive, gated on MultiCloudRuntimeDrift evidence loader+writer; idempotency proven by multi_cloud_runtime_drift_*_test.go",
	DomainCloudInventoryAdmission: "additive, gated on CloudInventory evidence loader+admission writer with generation check; idempotency proven by cloud_inventory_admission_*_test.go",

	// Search/curation + package/code import correlation: fact-loader + dedicated
	// writer gated, proven by their own suites.
	DomainEshuSearchDocument:       "additive, gated on EshuSearchDocument source loader+writer; reprojection idempotency proven by eshu_search_document_*_test.go",
	DomainPackageSourceCorrelation: "additive, gated on FactLoader+PackageCorrelationWriter with admission-decision fan-in; idempotency proven by package_source_correlation_*_test.go",
	DomainCodeImportRepoEdge:       "additive, gated on FactLoader+package-ownership loader+RepoDependencyIntentWriter; idempotency proven by code_import_repo_edge_*_test.go",

	// Image identity + CI/CD + supply chain: cross-source digest/artifact fan-in,
	// proven by their own suites.
	DomainContainerImageIdentity:      "additive, gated on FactLoader+ContainerImageIdentityWriter; cross-source digest-keyed join, idempotency proven by container_image_identity_*_test.go",
	DomainCICDRunCorrelation:          "additive, gated on FactLoader+CICDRunCorrelationWriter; idempotency proven by cicd run-correlation suites (container_image_identity_cicd_test.go and defaults_cicd_test.go)",
	DomainSBOMAttestationAttachment:   "additive, gated on FactLoader+SBOMAttestationAttachmentWriter; digest-subject fan-in, idempotency proven by sbom_attestation_attachment_*_test.go",
	DomainSupplyChainImpact:           "additive, gated on FactLoader+SupplyChainImpactWriter; multi-signal fan-in, idempotency proven by supply_chain_impact_*_test.go",
	DomainSecurityAlertReconciliation: "additive, gated on FactLoader+SecurityAlertReconciliationWriter; idempotency proven by security_alert_reconciliation_*_test.go",

	// Service catalog + observability/kubernetes correlation: multi-loader
	// cross-source fan-in, proven by their own suites.
	DomainServiceCatalogCorrelation:        "additive, gated on FactLoader+ServiceCatalogCorrelationWriter plus deployment/runtime/docs/incident/vuln loaders; idempotency proven by service_catalog_correlation_*_test.go",
	DomainObservabilityCoverageCorrelation: "additive, gated on FactLoader+ObservabilityCoverageCorrelationWriter; cross-source coverage fan-in, idempotency proven by observability_coverage_correlation_*_test.go",
	DomainKubernetesCorrelation:            "additive, gated on FactLoader+KubernetesCorrelationWriter; live-vs-source fan-in, idempotency proven by kubernetes_correlation_*_test.go",

	// Secrets/IAM trust + graph projection: evidence/graph read-back gated, proven
	// by their own suites.
	DomainSecretsIAMTrustChain:      "additive, gated on SecretsIAMTrustChain evidence loader+writer; cross-source trust fan-in, idempotency proven by secrets_iam_trust_chain_*_test.go",
	DomainSecretsIAMGraphProjection: "additive, gated on FactLoader+SecretsIAMGraphWriter with presence lookup+prior-generation check; graph read-back, idempotency proven by secrets_iam_graph_projection_*_test.go",

	// Cloud resource node materializers: FactLoader+node writer gated, graph phase
	// publication, proven by their own suites.
	DomainAWSResourceMaterialization:              "additive, gated on FactLoader+CloudResourceNodeWriter with phase publication; idempotency proven by aws_resource_materialization_*_test.go",
	DomainGCPResourceMaterialization:              "additive, gated on FactLoader+GCP node writer; idempotency proven by gcp_resource_materialization_*_test.go",
	DomainAzureResourceMaterialization:            "additive, gated on FactLoader+Azure node writer; idempotency proven by azure_resource_materialization_test.go",
	DomainEC2InstanceNodeMaterialization:          "additive, gated on FactLoader+EC2InstanceNodeWriter with phase publication; idempotency proven by ec2_instance_node_materialization_*_test.go",
	DomainKubernetesWorkloadMaterialization:       "additive, gated on FactLoader+KubernetesWorkloadNodeWriter with phase publication+presence writer; idempotency proven by kubernetes_workload_materialization_*_test.go",
	DomainKubernetesNamespaceMaterialization:      "additive, gated on FactLoader+KubernetesNamespaceNodeWriter; MERGE on the collector-emitted object_id uid, and the writer routes a row to the no-environment or with-environment Cypher variant purely by that row's own environment value, so replay converges without a phase/readiness dependency; idempotency proven by kubernetes_namespace_materialization_test.go",
	DomainRDSPostureMaterialization:               "additive, gated on FactLoader+RDSPostureNodeWriter with readiness lookup; graph read-back, idempotency proven by rds_posture_materialization_*_test.go (defaults_rds_posture_test.go)",
	DomainEC2BlockDeviceKMSPostureMaterialization: "additive, gated on FactLoader+EC2BlockDeviceKMSPostureNodeWriter with readiness lookup; graph read-back, idempotency proven by ec2_block_device_kms_posture_materialization_*_test.go",
	DomainS3InternetExposureMaterialization:       "additive, gated on FactLoader+S3InternetExposureNodeWriter with readiness lookup; graph read-back, idempotency proven by s3_internet_exposure_materialization_*_test.go (defaults_s3_internet_exposure_test.go)",
	DomainEC2InternetExposureMaterialization:      "additive, gated on FactLoader+EC2InternetExposureNodeWriter with readiness lookup; graph read-back, idempotency proven by ec2_internet_exposure_materialization_*_test.go",

	// Cloud relationship/edge materializers: readiness-gated edge writes against
	// committed nodes, proven by their own suites.
	DomainAWSRelationshipMaterialization:           "additive, gated on FactLoader+CloudResourceEdgeWriter with readiness lookup; edges resolve against committed nodes, idempotency proven by aws_relationship_materialization_*_test.go",
	DomainGCPRelationshipMaterialization:           "additive, gated on FactLoader+GCPCloudResourceEdgeWriter with readiness lookup; idempotency proven by gcp_relationship_materialization_*_test.go",
	DomainAzureRelationshipMaterialization:         "additive, gated on FactLoader+AzureCloudResourceEdgeWriter with readiness lookup; idempotency proven by azure_relationship_materialization_test.go",
	DomainWorkloadCloudRelationshipMaterialization: "additive, gated on FactLoader+WorkloadCloudRelationshipEdgeWriter with readiness lookup; idempotency proven by workload_cloud_relationship_materialization_*_test.go",
	DomainObservabilityCoverageMaterialization:     "additive, gated on FactLoader+ObservabilityCoverageEdgeWriter with readiness lookup; idempotency proven by observability_coverage_materialization_*_test.go",
	DomainKubernetesCorrelationMaterialization:     "additive, gated on FactLoader+KubernetesCorrelationEdgeWriter with readiness lookup; idempotency proven by kubernetes_correlation_materialization_*_test.go",
	DomainCrossplaneSatisfiedByMaterialization:     "additive, gated on FactLoader+CrossplaneSatisfiedByEdgeWriter; MATCH-MATCH-MERGE edge write is idempotent by (claim_uid, SATISFIED_BY, xrd_uid) and rows are deduplicated before write, idempotency proven by crossplane_satisfied_by_edge_rows_test.go and crossplane_satisfied_by_edge_writer_test.go",
	DomainEC2UsesProfileMaterialization:            "additive, gated on FactLoader+EC2UsesProfileEdgeWriter with dual readiness lookup; idempotency proven by ec2_uses_profile_materialization_*_test.go",

	// Security group + IAM edge materializers: readiness-gated graph edges, proven
	// by their own suites.
	DomainSecurityGroupCidrMaterialization:         "additive, gated on FactLoader+security-group writers; idempotency proven by security_group_cidr_materialization_*_test.go",
	DomainSecurityGroupRuleMaterialization:         "additive, gated on FactLoader+security-group writers; idempotency proven by security_group_rule_materialization_*_test.go",
	DomainSecurityGroupReachabilityMaterialization: "additive, gated on FactLoader+security-group reachability writers; idempotency proven by security_group_reachability_materialization_*_test.go",
	DomainIAMCanAssumeMaterialization:              "additive, gated on FactLoader+IAMCanAssumeEdgeWriter with readiness lookup; idempotency proven by iam_can_assume_materialization_*_test.go",
	DomainIAMCanPerformMaterialization:             "additive, gated on FactLoader+IAMCanPerformEdgeWriter with readiness lookup; idempotency proven by iam_can_perform_materialization_*_test.go",
	DomainIAMEscalationMaterialization:             "additive, gated on FactLoader+IAMEscalationEdgeWriter with readiness lookup; idempotency proven by iam_escalation_materialization_*_test.go",
	DomainIAMInstanceProfileRoleMaterialization:    "additive, gated on FactLoader+IAMInstanceProfileRoleEdgeWriter with readiness lookup; idempotency proven by iam_instance_profile_role_materialization_*_test.go",
	DomainS3LogsToMaterialization:                  "additive, gated on FactLoader+S3LogsToEdgeWriter with readiness lookup; idempotency proven by s3_logs_to_materialization_*_test.go",
	DomainS3ExternalPrincipalGrantMaterialization:  "additive, gated on FactLoader+S3ExternalPrincipalGrantWriter with readiness lookup; idempotency proven by s3_external_principal_grant_*_test.go (defaults_s3_external_principal_grant_test.go)",

	// Code evidence materializers + incident correlation + deployable-unit
	// correlation: evidence-loader gated cross-source fan-in, proven by their own
	// suites.
	DomainCodeTaintEvidence:              "additive, gated on CodeTaint evidence loader+writer with prior-generation check; idempotency proven by code_taint_evidence_materialization_test.go",
	DomainCodeInterprocEvidence:          "additive, gated on CodeInterproc evidence loader+writer with prior-generation check; idempotency proven by code_interproc_evidence_materialization_test.go",
	DomainCodeFunctionSummary:            "additive, gated on CodeFunctionSummary loader+writer plus source/graph-id/value-flow writers; idempotency proven by code_function_summary_materialization_test.go",
	DomainIncidentRoutingMaterialization: "additive, gated on IncidentRouting evidence loader+writer with prior-generation check; idempotency proven by incident_routing_materialization_test.go",
	DomainIncidentRepositoryCorrelation:  "additive, gated on PagerDuty routing loader+repo resolver+IncidentRepositoryCorrelationWriter; cross-source fan-in, idempotency proven by incident_repository_correlation_test.go",
	DomainDeployableUnitCorrelation:      "additive, gated on DeployableUnitCorrelationHandler; cross-source/cross-scope candidate correlation with graph read-back, idempotency proven by deployable_unit_correlation_*_test.go",
}
