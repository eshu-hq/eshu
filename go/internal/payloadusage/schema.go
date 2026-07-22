// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// factKindSchemaFile maps a factschema.FactKind* constant identifier to its
// checked-in JSON Schema file base name under sdk/go/factschema/schema/. This
// mirrors the file-naming convention every schema in that directory follows
// (fact kind name + ".v1.schema.json"); it is intentionally a lookup this
// file owns (not derived from the fact-kind string via string manipulation
// alone) so a future schema file rename is a one-line change here rather than
// a silent lookup miss.
var factKindSchemaFile = map[string]string{ // #nosec G101 -- fact-kind identifier to JSON-schema-filename lookup (e.g. vault_secret_engine_mount, k8s_service_account_token_posture); these are schema file names, not credentials.
	"FactKindAWSResource":                 "aws_resource.v1.schema.json",
	"FactKindAWSRelationship":             "aws_relationship.v1.schema.json",
	"FactKindAWSSecurityGroupRule":        "aws_security_group_rule.v1.schema.json",
	"FactKindEC2InstancePosture":          "ec2_instance_posture.v1.schema.json",
	"FactKindS3BucketPosture":             "s3_bucket_posture.v1.schema.json",
	"FactKindRDSInstancePosture":          "rds_instance_posture.v1.schema.json",
	"FactKindS3ExternalPrincipalGrant":    "s3_external_principal_grant.v1.schema.json",
	"FactKindAWSIAMPermission":            "aws_iam_permission.v1.schema.json",
	"FactKindAWSResourcePolicyPermission": "aws_resource_policy_permission.v1.schema.json",
	"FactKindAWSIAMPrincipal":             "aws_iam_principal.v1.schema.json",
	// Cross-provider image_reference family (#4685): the three cloud image
	// reference kinds the shared container-image-identity reducer decodes
	// through the typed seam (factschema_decode_imagereference.go). The OCI and
	// CI/CD image kinds this same domain reads are already mapped under the
	// oci_registry and ci_cd_run families below, so they are not repeated here.
	"FactKindAWSImageReference":   "aws_image_reference.v1.schema.json",
	"FactKindAzureImageReference": "azure_image_reference.v1.schema.json",
	"FactKindGCPImageReference":   "gcp_image_reference.v1.schema.json",
	// Incident family: the kinds a reducer OR query-layer decode seam wrapper
	// actually decodes are mapped. incident.record, incident_routing.applied_
	// pagerduty_resource, incident_routing.observed_pagerduty_service, and
	// incident_routing.coverage_warning are decoded by the reducer
	// (factschema_decode_incident.go); incident.lifecycle_event and
	// change.record have no reducer decode call but ARE decoded by the
	// query-layer incident-context read model
	// (go/internal/query/factschema_decode_incident.go, #4794 W2a), so they are
	// mapped too. applied_alert_route and observed_pagerduty_integration carry
	// a schema but no decode call from either layer, so they remain
	// intentionally absent here — mapping them would assert a gate contract
	// for a kind no handler reads.
	"FactKindIncidentRecord":                          "incident.record.v1.schema.json",
	"FactKindIncidentLifecycleEvent":                  "incident.lifecycle_event.v1.schema.json",
	"FactKindChangeRecord":                            "change.record.v1.schema.json",
	"FactKindIncidentRoutingAppliedPagerDutyResource": "incident_routing.applied_pagerduty_resource.v1.schema.json",
	"FactKindIncidentRoutingObservedPagerDutyService": "incident_routing.observed_pagerduty_service.v1.schema.json",
	"FactKindIncidentRoutingCoverageWarning":          "incident_routing.coverage_warning.v1.schema.json",
	// GCP family: the two cloud kinds the reducer decodes, plus
	// gcp_tag_observation, whose only production payload consumer is the
	// shared cloud-tag-evidence storage loader
	// (go/internal/storage/postgres/cloud_tag_evidence.go), decoded through
	// decodeGCPTagObservation in factschema_decode_cloud_tag_evidence.go
	// (#4686).
	"FactKindGCPCloudResource":     "gcp_cloud_resource.v1.schema.json",
	"FactKindGCPCloudRelationship": "gcp_cloud_relationship.v1.schema.json",
	"FactKindGCPTagObservation":    "gcp_tag_observation.v1.schema.json",
	// Azure family: the two cloud kinds the reducer decodes, plus
	// azure_tag_observation, decoded the same way as gcp_tag_observation above
	// (#4686). azure_image_reference is already mapped above under the
	// cross-provider image_reference family (#4685). azure_identity_observation
	// and azure_resource_change remain deferred: their consumers are an
	// unconverted Azure-specific storage loader, so mapping them here would
	// assert a gate contract no decode seam backs.
	"FactKindAzureCloudResource":     "azure_cloud_resource.v1.schema.json",
	"FactKindAzureCloudRelationship": "azure_cloud_relationship.v1.schema.json",
	"FactKindAzureTagObservation":    "azure_tag_observation.v1.schema.json",
	// Kubernetes live family: all three kinds are wired through the reducer's
	// typed decode seam (factschema_decode_kuberneteslive.go).
	"FactKindKubernetesLivePodTemplate":  "kubernetes_live.pod_template.v1.schema.json",
	"FactKindKubernetesLiveRelationship": "kubernetes_live.relationship.v1.schema.json",
	"FactKindKubernetesLiveWarning":      "kubernetes_live.warning.v1.schema.json",
	"FactKindKubernetesLiveNamespace":    "kubernetes_live.namespace.v1.schema.json",
	// OCI registry family: the six consumed kinds a decode seam references
	// (the projector canonical extractor decodes all six; the reducer registry
	// index decodes manifest/index/tag). oci_registry.warning has no decode
	// seam (typed-but-deferred), so it is intentionally absent here.
	"FactKindOCIRegistryRepository":  "oci_registry.repository.v1.schema.json",
	"FactKindOCIImageManifest":       "oci_registry.image_manifest.v1.schema.json",
	"FactKindOCIImageIndex":          "oci_registry.image_index.v1.schema.json",
	"FactKindOCIImageDescriptor":     "oci_registry.image_descriptor.v1.schema.json",
	"FactKindOCIImageTagObservation": "oci_registry.image_tag_observation.v1.schema.json",
	"FactKindOCIImageReferrer":       "oci_registry.image_referrer.v1.schema.json",
	// terraform_state family: the six kinds the projector's canonical
	// extractor decodes (factschema_decode_terraformstate.go), including
	// provider_binding (#5446, terraformStateProviderBindingsByResource's
	// decodeTerraformStateProviderBinding wrapper). The two remaining
	// typed-but-not-yet-consumed kinds (candidate, warning) carry a schema
	// but no projector decode call, so they are intentionally absent here —
	// mapping them would assert a gate contract for a kind no extractor
	// reads.
	"FactKindTerraformStateSnapshot":        "terraform_state_snapshot.v1.schema.json",
	"FactKindTerraformStateResource":        "terraform_state_resource.v1.schema.json",
	"FactKindTerraformStateModule":          "terraform_state_module.v1.schema.json",
	"FactKindTerraformStateOutput":          "terraform_state_output.v1.schema.json",
	"FactKindTerraformStateTagObservation":  "terraform_state_tag_observation.v1.schema.json",
	"FactKindTerraformStateProviderBinding": "terraform_state_provider_binding.v1.schema.json",
	// package_registry family: only the three kinds the projector's canonical
	// extractor decodes (factschema_decode_packageregistry.go). The six
	// typed-but-not-yet-consumed kinds (source_hint, package_artifact,
	// vulnerability_hint, registry_event, repository_hosting, warning) carry a
	// schema but no decode-seam call from the reducer or projector, so they are
	// intentionally absent here — mapping them would assert a gate contract for
	// a kind no decode site reads.
	"FactKindPackageRegistryPackage":           "package_registry.package.v1.schema.json",
	"FactKindPackageRegistryPackageVersion":    "package_registry.package_version.v1.schema.json",
	"FactKindPackageRegistryPackageDependency": "package_registry.package_dependency.v1.schema.json",
	// sbom_attestation family: only the kinds a reducer decode seam wrapper
	// actually decodes (factschema_decode_sbom.go) are mapped, so the gate
	// covers exactly what the reducer reads through the typed seam. Every
	// kind in this family now has a wired decode call: sbom.dependency_relationship
	// and sbom.external_reference feed buildSBOMAttachmentIndex's dependency
	// and external-reference evidence rows (#5370), and
	// attestation.slsa_provenance feeds the SLSA provenance evidence
	// buildSBOMAttachmentIndex joins onto a statement's attachment decision
	// (#5371).
	"FactKindSBOMDocument":                     "sbom.document.v1.schema.json",
	"FactKindSBOMComponent":                    "sbom.component.v1.schema.json",
	"FactKindSBOMDependencyRelationship":       "sbom.dependency_relationship.v1.schema.json",
	"FactKindSBOMExternalReference":            "sbom.external_reference.v1.schema.json",
	"FactKindSBOMWarning":                      "sbom.warning.v1.schema.json",
	"FactKindAttestationStatement":             "attestation.statement.v1.schema.json",
	"FactKindAttestationSignatureVerification": "attestation.signature_verification.v1.schema.json",
	"FactKindAttestationSLSAProvenance":        "attestation.slsa_provenance.v1.schema.json",
	// vulnerability_intelligence family: the eight kinds a reducer decode seam
	// wrapper actually decodes (factschema_decode_vulnerability.go). The three
	// unwired kinds (reference, source_snapshot, warning) carry a schema but no
	// reducer decode call, so they are intentionally absent here — mapping them
	// would assert a gate contract for a kind no handler reads.
	// vulnerability.suppression belongs to the separate
	// vulnerability_suppression registry family and is untyped; it is not
	// listed here either.
	"FactKindVulnerabilityCVE":                "vulnerability.cve.v1.schema.json",
	"FactKindVulnerabilityAffectedPackage":    "vulnerability.affected_package.v1.schema.json",
	"FactKindVulnerabilityAffectedProduct":    "vulnerability.affected_product.v1.schema.json",
	"FactKindVulnerabilityOSPackage":          "vulnerability.os_package.v1.schema.json",
	"FactKindVulnerabilityEPSSScore":          "vulnerability.epss_score.v1.schema.json",
	"FactKindVulnerabilityKnownExploited":     "vulnerability.known_exploited.v1.schema.json",
	"FactKindVulnerabilityGoModuleEvidence":   "vulnerability.go_module_evidence.v1.schema.json",
	"FactKindVulnerabilityGoCallReachability": "vulnerability.go_call_reachability.v1.schema.json",
	// code family: the two git-collector kinds whose outer envelope the
	// code-graph-core reducer decode seam decodes
	// (factschema_decode_codegraph.go). Bare (non-namespaced) wire kinds.
	"FactKindCodegraphFile":       "file.v1.schema.json",
	"FactKindCodegraphRepository": "repository.v1.schema.json",
	// codedataflow family (Contract System v1 Wave 4f S2, issue #4754): all
	// five kinds a reducer decode seam wrapper actually decodes
	// (factschema_decode_codedataflow.go), including code_dataflow_function
	// even though its only decode call site is a wrapper with no
	// materialization handler yet (the wrapper still satisfies the gate's
	// "decode seam exists" definition). Bare (non-namespaced) wire kinds,
	// same as the codegraph family.
	"FactKindCodeDataflowScanned":   "code_dataflow_scanned.v1.schema.json",
	"FactKindCodeDataflowFunction":  "code_dataflow_function.v1.schema.json",
	"FactKindCodeFunctionSummary":   "code_function_summary.v1.schema.json",
	"FactKindCodeFunctionSource":    "code_function_source.v1.schema.json",
	"FactKindCodeTaintEvidence":     "code_taint_evidence.v1.schema.json",
	"FactKindCodeInterprocEvidence": "code_interproc_evidence.v1.schema.json",
	// service_catalog family: the three kinds a reducer decode seam wrapper
	// actually decodes (factschema_decode_servicecatalog.go), read by the
	// correlation index, PLUS service_catalog.operational_link, which is
	// decoded by the query-layer incident-context read model
	// (go/internal/query/factschema_decode_incident.go, #4794 W2a) — it was
	// previously read only by a raw-SQL JSONB loader this reducer-decode-
	// call-only gate could not see (servicecatalog/v1 README.md), but that
	// loader is now a typed decode site too. The other four service_catalog
	// kinds (dependency, api_link, scorecard_definition, scorecard_result,
	// warning) carry no typed struct at all.
	"FactKindServiceCatalogEntity":          "service_catalog.entity.v1.schema.json",
	"FactKindServiceCatalogOwnership":       "service_catalog.ownership.v1.schema.json",
	"FactKindServiceCatalogRepositoryLink":  "service_catalog.repository_link.v1.schema.json",
	"FactKindServiceCatalogOperationalLink": "service_catalog.operational_link.v1.schema.json",
	// ci_cd_run family: all six kinds a reducer decode seam wrapper actually
	// decodes (factschema_decode_cicdrun.go). ci.job, ci.pipeline_definition,
	// and ci.warning carry no typed struct at all (cicdrun/v1 AGENTS.md), so
	// they have no row here either.
	"FactKindCICDRun":                    "ci.run.v1.schema.json",
	"FactKindCICDArtifact":               "ci.artifact.v1.schema.json",
	"FactKindCICDEnvironmentObservation": "ci.environment_observation.v1.schema.json",
	"FactKindCICDTriggerEdge":            "ci.trigger_edge.v1.schema.json",
	"FactKindCICDStep":                   "ci.step.v1.schema.json",
	"FactKindCICDWorkflowImageEvidence":  "ci.workflow_image_evidence.v1.schema.json",
	// secrets_iam family: every kind a
	// reducer decode seam wrapper actually decodes
	// (factschema_decode_secretsiam.go) is mapped. The #4789 W1c source
	// contracts add schemas and direct-map encoders for the full family, but
	// W2c (#4796) owns the loader-side second-decode path; mapping those
	// source-only kinds here before a reducer/query decode seam exists would
	// assert a hollow usage contract.
	"FactKindVaultAuthRole":                        "vault_auth_role.v1.schema.json",
	"FactKindVaultACLPolicy":                       "vault_acl_policy.v1.schema.json",
	"FactKindVaultKVMetadata":                      "vault_kv_metadata.v1.schema.json",
	"FactKindKubernetesServiceAccount":             "k8s_service_account.v1.schema.json",
	"FactKindKubernetesWorkloadIdentityUse":        "k8s_workload_identity_use.v1.schema.json",
	"FactKindEKSIRSAAnnotation":                    "eks_irsa_annotation.v1.schema.json",
	"FactKindEKSPodIdentityAssociation":            "eks_pod_identity_association.v1.schema.json",
	"FactKindKubernetesGCPWorkloadIdentityBinding": "k8s_gcp_workload_identity_binding.v1.schema.json",
	// work_item family (Wave 4d): the eight kinds a QUERY-side decode seam
	// wrapper actually decodes (go/internal/query/factschema_decode_workitem.go).
	// work_item is read straight from Postgres by the query evidence read model —
	// no reducer or projector domain consumes it — so its decode site is the
	// query layer, gated via QueryDir. work_item.issue_type_metadata is typed in
	// the contracts module but the read model does not consume it, so it has no
	// query wrapper and no mapping here (mapping it would assert a gate contract
	// for a kind no read path decodes).
	"FactKindWorkItemRecord":            "work_item.record.v1.schema.json",
	"FactKindWorkItemTransition":        "work_item.transition.v1.schema.json",
	"FactKindWorkItemExternalLink":      "work_item.external_link.v1.schema.json",
	"FactKindWorkItemProjectMetadata":   "work_item.project_metadata.v1.schema.json",
	"FactKindWorkItemIssueTypeMetadata": "work_item.issue_type_metadata.v1.schema.json",
	"FactKindWorkItemStatusMetadata":    "work_item.status_metadata.v1.schema.json",
	"FactKindWorkItemWorkflowMetadata":  "work_item.workflow_metadata.v1.schema.json",
	"FactKindWorkItemFieldMetadata":     "work_item.field_metadata.v1.schema.json",
	"FactKindWorkItemMetadataWarning":   "work_item.metadata_warning.v1.schema.json",
	// security_alert family (Wave 4e): the single repository_alert kind a
	// reducer decode seam wrapper actually decodes
	// (go/internal/reducer/factschema_decode_securityalert.go). Its one decode
	// site feeds both the reconciliation read surface and the
	// supply-chain-impact seeder, but both read the same decoded struct, so one
	// mapping covers the gate for every field either consumer reads.
	"FactKindSecurityAlertRepositoryAlert": "security_alert.repository_alert.v1.schema.json",
	// reducer_derived package-correlation family: all three kinds are emitted
	// through typed reducer-derived encoders and read back through reducer decode
	// seams (package_correlation_writer.go, factschema_decode_reducerderived.go).
	"FactKindReducerPackageOwnershipCorrelation":   "reducer_package_ownership_correlation.v1.schema.json",
	"FactKindReducerPackageConsumptionCorrelation": "reducer_package_consumption_correlation.v1.schema.json",
	"FactKindReducerPackagePublicationCorrelation": "reducer_package_publication_correlation.v1.schema.json",
	// observability family (Wave 4e): the seventeen kinds the coverage-metadata
	// classifier decodes through the typed view seam
	// (factschema_decode_observability.go). observability.source_instance carries
	// no coverage object and is skipped by the classifier, so it has no reducer
	// decode seam and no row here — mapping it would assert a gate contract for a
	// kind no reducer read path decodes (the same reason sbom omits its unconsumed
	// kinds).
	"FactKindObservabilityDeclaredFolder":       "observability.declared_folder.v1.schema.json",
	"FactKindObservabilityDeclaredDashboard":    "observability.declared_dashboard.v1.schema.json",
	"FactKindObservabilityDeclaredDatasource":   "observability.declared_datasource.v1.schema.json",
	"FactKindObservabilityDeclaredAlertRule":    "observability.declared_alert_rule.v1.schema.json",
	"FactKindObservabilityDeclaredScrapeConfig": "observability.declared_scrape_config.v1.schema.json",
	"FactKindObservabilityDeclaredMetricRule":   "observability.declared_metric_rule.v1.schema.json",
	"FactKindObservabilityDeclaredMetricRoute":  "observability.declared_metric_route.v1.schema.json",
	"FactKindObservabilityDeclaredLogRoute":     "observability.declared_log_route.v1.schema.json",
	"FactKindObservabilityDeclaredTraceRoute":   "observability.declared_trace_route.v1.schema.json",
	"FactKindObservabilityAppliedResource":      "observability.applied_resource.v1.schema.json",
	"FactKindObservabilityAppliedSyncState":     "observability.applied_sync_state.v1.schema.json",
	"FactKindObservabilityObservedDashboard":    "observability.observed_dashboard.v1.schema.json",
	"FactKindObservabilityObservedTarget":       "observability.observed_target.v1.schema.json",
	"FactKindObservabilityObservedRule":         "observability.observed_rule.v1.schema.json",
	"FactKindObservabilityObservedLogSignal":    "observability.observed_log_signal.v1.schema.json",
	"FactKindObservabilityObservedTraceSignal":  "observability.observed_trace_signal.v1.schema.json",
	"FactKindObservabilityCoverageWarning":      "observability.coverage_warning.v1.schema.json",
	// documentation family (Wave 4e): only the two kinds a reducer decode seam
	// wrapper actually decodes (factschema_decode_documentation.go) are mapped,
	// so the gate covers exactly what the reducer reads through the typed
	// seam. The other six documentation kinds (source, section, link,
	// claim_candidate, finding, evidence_packet) have no reducer or projector
	// decode call — finding/evidence_packet are read only by the query
	// layer's raw SQL, out of scope for this gate — so they are intentionally
	// absent here (documentation/v1/README.md).
	"FactKindDocumentationDocument":      "documentation_document.v1.schema.json",
	"FactKindDocumentationEntityMention": "documentation_entity_mention.v1.schema.json",
	// codeowners family (issue #5419 Phase 3): codeowners.ownership is a
	// directly-emitted fact decoded by the reducer through the typed seam
	// (factschema_decode_codeowners.go).
	"FactKindCodeownersOwnership": "codeowners.ownership.v1.schema.json",
	// submodule family (issue #5420 Phase 3): submodule.pin is a
	// directly-emitted fact decoded by the reducer through the typed seam
	// (factschema_decode_submodule.go).
	"FactKindSubmodulePin": "submodule.pin.v1.schema.json",
}

// jsonSchemaDocument is the subset of a checked-in factschema JSON Schema
// this gate reads: the declared property names. Property type/required
// details are schema-diff's concern (issue #4569, the forward direction);
// this gate only needs to know which top-level keys are declared at all, to
// check the reverse direction (a handler reading a field no schema declares).
type jsonSchemaDocument struct {
	Properties map[string]json.RawMessage `json:"properties"`
}

// LoadDeclaredFieldsFromSchemas reads every JSON Schema file
// factKindSchemaFile names under schemaDir and returns the declared property
// name set per FactKind constant, in the shape CheckManifest's
// declaredOverride parameter expects.
//
// A mapped schema file that is MISSING is a fail-closed ERROR, not a skip.
// Every fact kind in factKindSchemaFile is a kind Load already requires a
// decode seam for (via UnmappedSeamFactKinds), so its schema file must exist:
// if a schema were deleted or moved, silently skipping it would make
// CheckManifest fall back to the manifest's OWN DeclaredFields for that kind,
// which can never report a violation — a false-green that would disable the
// gate for that kind precisely when its declared contract vanished. Failing
// closed here means a removed schema fails the gate loudly instead.
func LoadDeclaredFieldsFromSchemas(schemaDir string) (map[string]map[string]struct{}, error) {
	declared := map[string]map[string]struct{}{}
	for factKindConst, fileName := range factKindSchemaFile {
		path := filepath.Join(schemaDir, fileName)
		// #nosec G304 -- path is schemaDir (a CLI/gate-configured directory)
		// joined with a fixed name from this file's own factKindSchemaFile
		// map, not untrusted input.
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf(
				"payload-usage-manifest: read declared schema %s for fact kind %s: %w (a mapped schema file must exist; a missing one would silently disable the gate for this kind)",
				path, factKindConst, err,
			)
		}
		var doc jsonSchemaDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("payload-usage-manifest: parse schema %s: %w", path, err)
		}
		fields := make(map[string]struct{}, len(doc.Properties))
		for name := range doc.Properties {
			fields[name] = struct{}{}
		}
		declared[factKindConst] = fields
	}
	return declared, nil
}

// MergeRegistryPayloadSchemaFields is an ADDITIVE input hook for issue
// #4570's registry v2 payload_schema refs. Per issue #4573's "Out of scope"
// section, registry v2 field validation is issue #4570's own concern and may
// not have landed payload_schema refs yet; this gate's source of truth is
// sdk/go/factschema/schema/*.json (LoadDeclaredFieldsFromSchemas). When a
// registry payload_schema ref is present for a kind, callers MAY widen
// (never narrow) declared with it — narrowing here would let a registry
// authoring bug fail this gate for a field the real schema already declares.
// registryFields is nil-safe: an empty or nil map is a no-op.
func MergeRegistryPayloadSchemaFields(declared map[string]map[string]struct{}, registryFields map[string]map[string]struct{}) map[string]map[string]struct{} {
	if len(registryFields) == 0 {
		return declared
	}
	merged := make(map[string]map[string]struct{}, len(declared))
	maps.Copy(merged, declared)
	for factKind, fields := range registryFields {
		existing := merged[factKind]
		if existing == nil {
			existing = map[string]struct{}{}
		}
		widened := make(map[string]struct{}, len(existing)+len(fields))
		for f := range existing {
			widened[f] = struct{}{}
		}
		for f := range fields {
			widened[f] = struct{}{}
		}
		merged[factKind] = widened
	}
	return merged
}

// isKnownFactKindConstant reports whether factKindConst has a schema file
// mapping registered.
func isKnownFactKindConstant(factKindConst string) bool {
	_, ok := factKindSchemaFile[factKindConst]
	return ok
}

// UnmappedSeamFactKinds returns the FactKindConst of every seam with no
// schema-file mapping in factKindSchemaFile, sorted, so a caller can fail
// loudly on a newly migrated kind whose schema mapping was forgotten rather
// than silently skipping its gate coverage.
func UnmappedSeamFactKinds(seams []DecodeSeam) []string {
	var missing []string
	for _, s := range seams {
		if !isKnownFactKindConstant(s.FactKindConst) {
			missing = append(missing, s.FactKindConst)
		}
	}
	sort.Strings(missing)
	return missing
}

// JoinSorted is a small formatting helper for error messages listing several
// fact kind identifiers.
func JoinSorted(names []string) string {
	return strings.Join(names, ", ")
}
