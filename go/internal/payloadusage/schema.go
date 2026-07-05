// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"encoding/json"
	"fmt"
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
	"FactKindAWSIAMPermission":            "aws_iam_permission.v1.schema.json",
	"FactKindAWSResourcePolicyPermission": "aws_resource_policy_permission.v1.schema.json",
	"FactKindAWSIAMPrincipal":             "aws_iam_principal.v1.schema.json",
	// Incident family: ONLY the kinds a reducer decode seam wrapper actually
	// decodes (factschema_decode_incident.go) are mapped, so the gate covers
	// exactly what the reducer reads through the typed seam. The unwired
	// incident kinds (lifecycle_event, change.record, applied_alert_route,
	// observed_pagerduty_integration) carry a schema but no reducer decode
	// call, so they are intentionally absent here — mapping them would assert a
	// gate contract for a kind no handler reads.
	"FactKindIncidentRecord":                          "incident.record.v1.schema.json",
	"FactKindIncidentRoutingAppliedPagerDutyResource": "incident_routing.applied_pagerduty_resource.v1.schema.json",
	"FactKindIncidentRoutingObservedPagerDutyService": "incident_routing.observed_pagerduty_service.v1.schema.json",
	"FactKindIncidentRoutingCoverageWarning":          "incident_routing.coverage_warning.v1.schema.json",
	// GCP family: only the two wired cloud kinds the reducer decodes.
	"FactKindGCPCloudResource":     "gcp_cloud_resource.v1.schema.json",
	"FactKindGCPCloudRelationship": "gcp_cloud_relationship.v1.schema.json",
	// Azure family: only the two wired cloud kinds the reducer decodes.
	"FactKindAzureCloudResource":     "azure_cloud_resource.v1.schema.json",
	"FactKindAzureCloudRelationship": "azure_cloud_relationship.v1.schema.json",
	// Kubernetes live family: all three kinds are wired through the reducer's
	// typed decode seam (factschema_decode_kuberneteslive.go).
	"FactKindKubernetesLivePodTemplate":  "kubernetes_live.pod_template.v1.schema.json",
	"FactKindKubernetesLiveRelationship": "kubernetes_live.relationship.v1.schema.json",
	"FactKindKubernetesLiveWarning":      "kubernetes_live.warning.v1.schema.json",
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
	// terraform_state family: only the five kinds the projector's canonical
	// extractor decodes (factschema_decode_terraformstate.go). The three
	// typed-but-not-yet-consumed kinds (candidate, provider_binding, warning)
	// carry a schema but no projector decode call, so they are intentionally
	// absent here — mapping them would assert a gate contract for a kind no
	// extractor reads.
	"FactKindTerraformStateSnapshot":       "terraform_state_snapshot.v1.schema.json",
	"FactKindTerraformStateResource":       "terraform_state_resource.v1.schema.json",
	"FactKindTerraformStateModule":         "terraform_state_module.v1.schema.json",
	"FactKindTerraformStateOutput":         "terraform_state_output.v1.schema.json",
	"FactKindTerraformStateTagObservation": "terraform_state_tag_observation.v1.schema.json",
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
	// covers exactly what the reducer reads through the typed seam.
	// sbom.dependency_relationship, sbom.external_reference, and
	// attestation.slsa_provenance carry a schema but no reducer decode call
	// (typed-but-not-yet-consumed, sbom/v1/doc.go), so they are intentionally
	// absent here — mapping them would assert a gate contract for a kind no
	// handler reads.
	"FactKindSBOMDocument":                     "sbom.document.v1.schema.json",
	"FactKindSBOMComponent":                    "sbom.component.v1.schema.json",
	"FactKindSBOMWarning":                      "sbom.warning.v1.schema.json",
	"FactKindAttestationStatement":             "attestation.statement.v1.schema.json",
	"FactKindAttestationSignatureVerification": "attestation.signature_verification.v1.schema.json",
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
	// secrets_iam family (Wave 4d, VAULT + K8S lanes only): every kind a
	// reducer decode seam wrapper actually decodes
	// (factschema_decode_secretsiam.go) is mapped. The AWS IAM lane's kinds
	// (aws_iam_principal and siblings) are already mapped above under their
	// own FactKind constants. The GCP IAM lane (gcp_iam_principal,
	// gcp_iam_trust_policy, gcp_iam_permission_policy) has no reducer decode
	// call this wave, so it is intentionally absent here.
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
	"FactKindWorkItemRecord":           "work_item.record.v1.schema.json",
	"FactKindWorkItemTransition":       "work_item.transition.v1.schema.json",
	"FactKindWorkItemExternalLink":     "work_item.external_link.v1.schema.json",
	"FactKindWorkItemProjectMetadata":  "work_item.project_metadata.v1.schema.json",
	"FactKindWorkItemStatusMetadata":   "work_item.status_metadata.v1.schema.json",
	"FactKindWorkItemWorkflowMetadata": "work_item.workflow_metadata.v1.schema.json",
	"FactKindWorkItemFieldMetadata":    "work_item.field_metadata.v1.schema.json",
	"FactKindWorkItemMetadataWarning":  "work_item.metadata_warning.v1.schema.json",
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
	for k, v := range declared {
		merged[k] = v
	}
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
