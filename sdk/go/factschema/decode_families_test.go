// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema //nolint:filelength // per-family decode-dispatch test registry; one decodeByKind case + one allDecodedKinds row + one unsupported-major case per migrated fact kind, reviewed as a single source-of-truth table (mirrors decode_test.go's payloadContracts exemption). Splitting per-family would fragment the drift guard the per-kind tests below depend on.

import (
	"errors"
	"reflect"
	"testing"
)

// requiredFieldsForKind returns the reflectively derived required-field set
// for one fact kind, looked up via the payloadContracts registry
// (decode_test.go) so this file has no key list of its own to drift out of
// sync with the structs — it always asks the same single source of truth
// decodeAndValidate itself reads.
func requiredFieldsForKind(t *testing.T, factKind string) []string {
	t.Helper()
	for _, contract := range payloadContracts {
		if contract.factKind == factKind {
			return payloadKeySetOf(contract.typ).Required
		}
	}
	t.Fatalf("requiredFieldsForKind: no payloadContracts row for fact kind %q", factKind)
	return nil
}

// structTypeForKind returns the payload struct reflect.Type for one fact kind
// from the payloadContracts registry, so a required-field value can be built to
// match the field's real Go type rather than assuming every required field is a
// string.
func structTypeForKind(t *testing.T, factKind string) reflect.Type {
	t.Helper()
	for _, contract := range payloadContracts {
		if contract.factKind == factKind {
			return contract.typ
		}
	}
	t.Fatalf("structTypeForKind: no payloadContracts row for fact kind %q", factKind)
	return nil
}

// requiredFieldValue returns a payload value for a required field that decodes
// cleanly into its Go type. nonEmpty selects a populated value (for the
// full-payload positive test) versus a type-appropriate empty-but-present value
// (for the present-but-empty test): "" or [] or {} rather than always "".
// Deriving the value from the field's kind keeps the generic tests correct for
// any required non-string field (for example gcp_iam_policy_observation.members,
// a required []map[string]string), not just string identity fields.
func requiredFieldValue(t *testing.T, typ reflect.Type, jsonName string, nonEmpty bool) any {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, _, skip := parseJSONTag(field.Tag.Get("json"), field.Name)
		if skip || name != jsonName {
			continue
		}
		switch field.Type.Kind() {
		case reflect.Slice:
			if nonEmpty {
				// One element whose own shape is valid: a []map[string]string
				// (members) needs a map element; any other slice gets a string
				// element. Either way this is a present, non-empty collection.
				if field.Type.Elem().Kind() == reflect.Map {
					return []any{map[string]any{"k": "v"}}
				}
				return []any{"x"}
			}
			return []any{} // present-but-empty collection
		case reflect.Map:
			if nonEmpty {
				return map[string]any{"k": "v"}
			}
			return map[string]any{}
		case reflect.Struct:
			return requiredPayloadForType(t, field.Type, nonEmpty)
		case reflect.Bool:
			// A required bool has no "empty" value distinct from its zero value
			// (false is both a valid observed value and the Go zero value), so
			// both the nonEmpty and present-but-empty cases return a concrete
			// bool rather than a string: assignField rejects a string payload
			// value for a bool field.
			if nonEmpty {
				return true
			}
			return false
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if nonEmpty {
				return 1
			}
			return 0
		case reflect.Float32, reflect.Float64:
			if nonEmpty {
				return 1.0
			}
			return 0.0
		default:
			if nonEmpty {
				return "x"
			}
			return "" // present-but-empty scalar (a valid observed value)
		}
	}
	t.Fatalf("requiredFieldValue: fact struct %s has no required field with json name %q", typ.Name(), jsonName)
	return nil
}

func requiredPayloadForType(t *testing.T, typ reflect.Type, nonEmpty bool) map[string]any {
	t.Helper()
	out := map[string]any{}
	for _, key := range payloadKeySetOf(typ).Required {
		out[key] = requiredFieldValue(t, typ, key, nonEmpty)
	}
	return out
}

// fullPayloadForKind returns a minimal valid payload map (every required key
// present, non-empty) for one fact kind, so a per-kind test can delete a single
// required key and prove decode dead-letters on exactly that field. Each value
// matches the required field's Go type (a required slice gets a non-empty
// slice, not the string "x").
func fullPayloadForKind(t *testing.T, factKind string) map[string]any {
	t.Helper()
	typ := structTypeForKind(t, factKind)
	return requiredPayloadForType(t, typ, true)
}

// decodeByKind dispatches to the kind's public Decode function so the test
// exercises the real production seam, not a re-implementation. It returns the
// error only, which is all the required-field tests assert on.
func decodeByKind(t *testing.T, factKind string, payload map[string]any) error {
	t.Helper()
	env := Envelope{FactKind: factKind, SchemaVersion: "1.0.0", Payload: payload}
	switch factKind {
	case FactKindAWSResource:
		_, err := DecodeAWSResource(env)
		return err
	case FactKindAWSRelationship:
		_, err := DecodeAWSRelationship(env)
		return err
	case FactKindAWSDNSRecord:
		_, err := DecodeAWSDNSRecord(env)
		return err
	case FactKindAWSImageReference:
		_, err := DecodeAWSImageReference(env)
		return err
	case FactKindAWSSecurityGroupRule:
		_, err := DecodeAWSSecurityGroupRule(env)
		return err
	case FactKindAWSWarning:
		_, err := DecodeAWSWarning(env)
		return err
	case FactKindEC2InstancePosture:
		_, err := DecodeEC2InstancePosture(env)
		return err
	case FactKindRDSInstancePosture:
		_, err := DecodeRDSInstancePosture(env)
		return err
	case FactKindS3BucketPosture:
		_, err := DecodeS3BucketPosture(env)
		return err
	case FactKindS3ExternalPrincipalGrant:
		_, err := DecodeS3ExternalPrincipalGrant(env)
		return err
	case FactKindAWSIAMPermission:
		_, err := DecodeAWSIAMPermission(env)
		return err
	case FactKindAWSResourcePolicyPermission:
		_, err := DecodeAWSResourcePolicyPermission(env)
		return err
	case FactKindAWSIAMPrincipal:
		_, err := DecodeAWSIAMPrincipal(env)
		return err
	case FactKindAWSIAMTrustPolicy:
		_, err := DecodeAWSIAMTrustPolicy(env)
		return err
	case FactKindAWSIAMPermissionPolicy:
		_, err := DecodeAWSIAMPermissionPolicy(env)
		return err
	case FactKindAWSIAMPolicyAttachment:
		_, err := DecodeAWSIAMPolicyAttachment(env)
		return err
	case FactKindAWSIAMPermissionBoundary:
		_, err := DecodeAWSIAMPermissionBoundary(env)
		return err
	case FactKindAWSIAMInstanceProfile:
		_, err := DecodeAWSIAMInstanceProfile(env)
		return err
	case FactKindAWSIAMAccessAnalyzerFinding:
		_, err := DecodeAWSIAMAccessAnalyzerFinding(env)
		return err
	case FactKindIncidentRecord:
		_, err := DecodeIncidentRecord(env)
		return err
	case FactKindIncidentLifecycleEvent:
		_, err := DecodeIncidentLifecycleEvent(env)
		return err
	case FactKindChangeRecord:
		_, err := DecodeChangeRecord(env)
		return err
	case FactKindIncidentRoutingAppliedPagerDutyResource:
		_, err := DecodeIncidentRoutingAppliedPagerDutyResource(env)
		return err
	case FactKindIncidentRoutingAppliedAlertRoute:
		_, err := DecodeIncidentRoutingAppliedAlertRoute(env)
		return err
	case FactKindIncidentRoutingObservedPagerDutyService:
		_, err := DecodeIncidentRoutingObservedPagerDutyService(env)
		return err
	case FactKindIncidentRoutingObservedPagerDutyIntegration:
		_, err := DecodeIncidentRoutingObservedPagerDutyIntegration(env)
		return err
	case FactKindIncidentRoutingCoverageWarning:
		_, err := DecodeIncidentRoutingCoverageWarning(env)
		return err
	case FactKindGCPCloudResource:
		_, err := DecodeGCPCloudResource(env)
		return err
	case FactKindGCPCloudRelationship:
		_, err := DecodeGCPCloudRelationship(env)
		return err
	case FactKindGCPCollectionWarning:
		_, err := DecodeGCPCollectionWarning(env)
		return err
	case FactKindGCPDNSRecord:
		_, err := DecodeGCPDNSRecord(env)
		return err
	case FactKindGCPIAMPolicyObservation:
		_, err := DecodeGCPIAMPolicyObservation(env)
		return err
	case FactKindGCPTagObservation:
		_, err := DecodeGCPTagObservation(env)
		return err
	case FactKindGCPImageReference:
		_, err := DecodeGCPImageReference(env)
		return err
	case FactKindGCPIAMPrincipal:
		_, err := DecodeGCPIAMPrincipal(env)
		return err
	case FactKindGCPIAMTrustPolicy:
		_, err := DecodeGCPIAMTrustPolicy(env)
		return err
	case FactKindGCPIAMPermissionPolicy:
		_, err := DecodeGCPIAMPermissionPolicy(env)
		return err
	case FactKindAzureCloudResource:
		_, err := DecodeAzureCloudResource(env)
		return err
	case FactKindAzureCloudRelationship:
		_, err := DecodeAzureCloudRelationship(env)
		return err
	case FactKindAzureDNSRecord:
		_, err := DecodeAzureDNSRecord(env)
		return err
	case FactKindAzureCollectionWarning:
		_, err := DecodeAzureCollectionWarning(env)
		return err
	case FactKindAzureTagObservation:
		_, err := DecodeAzureTagObservation(env)
		return err
	case FactKindAzureIdentityObservation:
		_, err := DecodeAzureIdentityObservation(env)
		return err
	case FactKindAzureResourceChange:
		_, err := DecodeAzureResourceChange(env)
		return err
	case FactKindAzureImageReference:
		_, err := DecodeAzureImageReference(env)
		return err
	case FactKindKubernetesLivePodTemplate:
		_, err := DecodeKubernetesLivePodTemplate(env)
		return err
	case FactKindKubernetesLiveRelationship:
		_, err := DecodeKubernetesLiveRelationship(env)
		return err
	case FactKindKubernetesLiveWarning:
		_, err := DecodeKubernetesLiveWarning(env)
		return err
	case FactKindKubernetesLiveNamespace:
		_, err := DecodeKubernetesLiveNamespace(env)
		return err
	case FactKindOCIRegistryRepository:
		_, err := DecodeOCIRegistryRepository(env)
		return err
	case FactKindOCIImageManifest:
		_, err := DecodeOCIImageManifest(env)
		return err
	case FactKindOCIImageIndex:
		_, err := DecodeOCIImageIndex(env)
		return err
	case FactKindOCIImageDescriptor:
		_, err := DecodeOCIImageDescriptor(env)
		return err
	case FactKindOCIImageTagObservation:
		_, err := DecodeOCIImageTagObservation(env)
		return err
	case FactKindOCIImageReferrer:
		_, err := DecodeOCIImageReferrer(env)
		return err
	case FactKindOCIRegistryWarning:
		_, err := DecodeOCIRegistryWarning(env)
		return err
	case FactKindTerraformStateSnapshot:
		_, err := DecodeTerraformStateSnapshot(env)
		return err
	case FactKindTerraformStateResource:
		_, err := DecodeTerraformStateResource(env)
		return err
	case FactKindTerraformStateModule:
		_, err := DecodeTerraformStateModule(env)
		return err
	case FactKindTerraformStateOutput:
		_, err := DecodeTerraformStateOutput(env)
		return err
	case FactKindTerraformStateTagObservation:
		_, err := DecodeTerraformStateTagObservation(env)
		return err
	case FactKindTerraformStateCandidate:
		_, err := DecodeTerraformStateCandidate(env)
		return err
	case FactKindTerraformStateProviderBinding:
		_, err := DecodeTerraformStateProviderBinding(env)
		return err
	case FactKindTerraformStateWarning:
		_, err := DecodeTerraformStateWarning(env)
		return err
	case FactKindPackageRegistryPackage:
		_, err := DecodePackageRegistryPackage(env)
		return err
	case FactKindPackageRegistryPackageVersion:
		_, err := DecodePackageRegistryPackageVersion(env)
		return err
	case FactKindPackageRegistryPackageDependency:
		_, err := DecodePackageRegistryPackageDependency(env)
		return err
	case FactKindPackageRegistrySourceHint:
		_, err := DecodePackageRegistrySourceHint(env)
		return err
	case FactKindPackageRegistryPackageArtifact:
		_, err := DecodePackageRegistryPackageArtifact(env)
		return err
	case FactKindPackageRegistryVulnerabilityHint:
		_, err := DecodePackageRegistryVulnerabilityHint(env)
		return err
	case FactKindPackageRegistryRegistryEvent:
		_, err := DecodePackageRegistryRegistryEvent(env)
		return err
	case FactKindPackageRegistryRepositoryHosting:
		_, err := DecodePackageRegistryRepositoryHosting(env)
		return err
	case FactKindPackageRegistryWarning:
		_, err := DecodePackageRegistryWarning(env)
		return err
	case FactKindSBOMDocument:
		_, err := DecodeSBOMDocument(env)
		return err
	case FactKindSBOMComponent:
		_, err := DecodeSBOMComponent(env)
		return err
	case FactKindSBOMDependencyRelationship:
		_, err := DecodeSBOMDependencyRelationship(env)
		return err
	case FactKindSBOMExternalReference:
		_, err := DecodeSBOMExternalReference(env)
		return err
	case FactKindSBOMWarning:
		_, err := DecodeSBOMWarning(env)
		return err
	case FactKindAttestationStatement:
		_, err := DecodeAttestationStatement(env)
		return err
	case FactKindAttestationSignatureVerification:
		_, err := DecodeAttestationSignatureVerification(env)
		return err
	case FactKindAttestationSLSAProvenance:
		_, err := DecodeAttestationSLSAProvenance(env)
		return err
	case FactKindScannerWorkerAnalysis:
		_, err := DecodeScannerWorkerAnalysis(env)
		return err
	case FactKindScannerWorkerWarning:
		_, err := DecodeScannerWorkerWarning(env)
		return err
	case FactKindVulnerabilityCVE:
		_, err := DecodeVulnerabilityCVE(env)
		return err
	case FactKindVulnerabilityAffectedPackage:
		_, err := DecodeVulnerabilityAffectedPackage(env)
		return err
	case FactKindVulnerabilityAffectedProduct:
		_, err := DecodeVulnerabilityAffectedProduct(env)
		return err
	case FactKindVulnerabilityOSPackage:
		_, err := DecodeVulnerabilityOSPackage(env)
		return err
	case FactKindVulnerabilityEPSSScore:
		_, err := DecodeVulnerabilityEPSSScore(env)
		return err
	case FactKindVulnerabilityKnownExploited:
		_, err := DecodeVulnerabilityKnownExploited(env)
		return err
	case FactKindVulnerabilityGoModuleEvidence:
		_, err := DecodeVulnerabilityGoModuleEvidence(env)
		return err
	case FactKindVulnerabilityGoCallReachability:
		_, err := DecodeVulnerabilityGoCallReachability(env)
		return err
	case FactKindVulnerabilityReference:
		_, err := DecodeVulnerabilityReference(env)
		return err
	case FactKindVulnerabilitySourceSnapshot:
		_, err := DecodeVulnerabilitySourceSnapshot(env)
		return err
	case FactKindCodegraphFile:
		_, err := DecodeCodegraphFile(env)
		return err
	case FactKindCodegraphRepository:
		_, err := DecodeCodegraphRepository(env)
		return err
	case FactKindCodeDataflowScanned:
		_, err := DecodeCodeDataflowScanned(env)
		return err
	case FactKindCodeDataflowFunction:
		_, err := DecodeCodeDataflowFunction(env)
		return err
	case FactKindCodeFunctionSummary:
		_, err := DecodeCodeFunctionSummary(env)
		return err
	case FactKindCodeFunctionSource:
		_, err := DecodeCodeFunctionSource(env)
		return err
	case FactKindCodeTaintEvidence:
		_, err := DecodeCodeTaintEvidence(env)
		return err
	case FactKindCodeInterprocEvidence:
		_, err := DecodeCodeInterprocEvidence(env)
		return err
	case FactKindCICDRun:
		_, err := DecodeCICDRun(env)
		return err
	case FactKindCICDArtifact:
		_, err := DecodeCICDArtifact(env)
		return err
	case FactKindCICDEnvironmentObservation:
		_, err := DecodeCICDEnvironmentObservation(env)
		return err
	case FactKindCICDTriggerEdge:
		_, err := DecodeCICDTriggerEdge(env)
		return err
	case FactKindCICDStep:
		_, err := DecodeCICDStep(env)
		return err
	case FactKindCICDWorkflowImageEvidence:
		_, err := DecodeCICDWorkflowImageEvidence(env)
		return err
	case FactKindVaultAuthRole:
		_, err := DecodeVaultAuthRole(env)
		return err
	case FactKindVaultACLPolicy:
		_, err := DecodeVaultACLPolicy(env)
		return err
	case FactKindVaultKVMetadata:
		_, err := DecodeVaultKVMetadata(env)
		return err
	case FactKindKubernetesServiceAccount:
		_, err := DecodeKubernetesServiceAccount(env)
		return err
	case FactKindKubernetesWorkloadIdentityUse:
		_, err := DecodeKubernetesWorkloadIdentityUse(env)
		return err
	case FactKindEKSIRSAAnnotation:
		_, err := DecodeEKSIRSAAnnotation(env)
		return err
	case FactKindEKSPodIdentityAssociation:
		_, err := DecodeEKSPodIdentityAssociation(env)
		return err
	case FactKindKubernetesGCPWorkloadIdentityBinding:
		_, err := DecodeKubernetesGCPWorkloadIdentityBinding(env)
		return err
	case FactKindKubernetesRBACRole:
		_, err := DecodeKubernetesRBACRole(env)
		return err
	case FactKindKubernetesRBACBinding:
		_, err := DecodeKubernetesRBACBinding(env)
		return err
	case FactKindKubernetesServiceAccountTokenPosture:
		_, err := DecodeKubernetesServiceAccountTokenPosture(env)
		return err
	case FactKindVaultAuthMount:
		_, err := DecodeVaultAuthMount(env)
		return err
	case FactKindVaultIdentityEntity:
		_, err := DecodeVaultIdentityEntity(env)
		return err
	case FactKindVaultIdentityAlias:
		_, err := DecodeVaultIdentityAlias(env)
		return err
	case FactKindVaultSecretEngineMount:
		_, err := DecodeVaultSecretEngineMount(env)
		return err
	case FactKindSecretsIAMCoverageWarning:
		_, err := DecodeSecretsIAMCoverageWarning(env)
		return err
	case FactKindWorkItemRecord:
		_, err := DecodeWorkItemRecord(env)
		return err
	case FactKindWorkItemTransition:
		_, err := DecodeWorkItemTransition(env)
		return err
	case FactKindWorkItemExternalLink:
		_, err := DecodeWorkItemExternalLink(env)
		return err
	case FactKindWorkItemProjectMetadata:
		_, err := DecodeWorkItemProjectMetadata(env)
		return err
	case FactKindWorkItemIssueTypeMetadata:
		_, err := DecodeWorkItemIssueTypeMetadata(env)
		return err
	case FactKindWorkItemStatusMetadata:
		_, err := DecodeWorkItemStatusMetadata(env)
		return err
	case FactKindWorkItemWorkflowMetadata:
		_, err := DecodeWorkItemWorkflowMetadata(env)
		return err
	case FactKindWorkItemFieldMetadata:
		_, err := DecodeWorkItemFieldMetadata(env)
		return err
	case FactKindWorkItemMetadataWarning:
		_, err := DecodeWorkItemMetadataWarning(env)
		return err
	case FactKindSecurityAlertRepositoryAlert:
		_, err := DecodeSecurityAlertRepositoryAlert(env)
		return err
	case FactKindReducerSupplyChainImpactFinding:
		_, err := DecodeReducerSupplyChainImpactFinding(env)
		return err
	case FactKindReducerAWSCloudRuntimeDriftFinding:
		_, err := DecodeReducerAWSCloudRuntimeDriftFinding(env)
		return err
	case FactKindReducerMultiCloudRuntimeDriftFinding:
		_, err := DecodeReducerMultiCloudRuntimeDriftFinding(env)
		return err
	case FactKindReducerPackageOwnershipCorrelation:
		_, err := DecodeReducerPackageOwnershipCorrelation(env)
		return err
	case FactKindReducerPackageConsumptionCorrelation:
		_, err := DecodeReducerPackageConsumptionCorrelation(env)
		return err
	case FactKindReducerPackagePublicationCorrelation:
		_, err := DecodeReducerPackagePublicationCorrelation(env)
		return err
	case FactKindDocumentationSource:
		_, err := DecodeDocumentationSource(env)
		return err
	case FactKindDocumentationDocument:
		_, err := DecodeDocumentationDocument(env)
		return err
	case FactKindDocumentationSection:
		_, err := DecodeDocumentationSection(env)
		return err
	case FactKindDocumentationLink:
		_, err := DecodeDocumentationLink(env)
		return err
	case FactKindDocumentationEntityMention:
		_, err := DecodeDocumentationEntityMention(env)
		return err
	case FactKindDocumentationClaimCandidate:
		_, err := DecodeDocumentationClaimCandidate(env)
		return err
	case FactKindDocumentationFinding:
		_, err := DecodeDocumentationFinding(env)
		return err
	case FactKindDocumentationEvidencePacket:
		_, err := DecodeDocumentationEvidencePacket(env)
		return err
	case FactKindSemanticDocumentationObservation:
		_, err := DecodeSemanticDocumentationObservation(env)
		return err
	case FactKindSemanticCodeHint:
		_, err := DecodeSemanticCodeHint(env)
		return err
	case FactKindServiceCatalogEntity:
		_, err := DecodeServiceCatalogEntity(env)
		return err
	case FactKindServiceCatalogOwnership:
		_, err := DecodeServiceCatalogOwnership(env)
		return err
	case FactKindServiceCatalogRepositoryLink:
		_, err := DecodeServiceCatalogRepositoryLink(env)
		return err
	case FactKindServiceCatalogOperationalLink:
		_, err := DecodeServiceCatalogOperationalLink(env)
		return err
	case FactKindCodeownersOwnership:
		_, err := DecodeCodeownersOwnership(env)
		return err
	case FactKindObservabilityDeclaredFolder:
		_, err := DecodeObservabilityDeclaredFolder(env)
		return err
	case FactKindObservabilityDeclaredDashboard:
		_, err := DecodeObservabilityDeclaredDashboard(env)
		return err
	case FactKindObservabilityDeclaredDatasource:
		_, err := DecodeObservabilityDeclaredDatasource(env)
		return err
	case FactKindObservabilityDeclaredAlertRule:
		_, err := DecodeObservabilityDeclaredAlertRule(env)
		return err
	case FactKindObservabilityDeclaredScrapeConfig:
		_, err := DecodeObservabilityDeclaredScrapeConfig(env)
		return err
	case FactKindObservabilityDeclaredMetricRule:
		_, err := DecodeObservabilityDeclaredMetricRule(env)
		return err
	case FactKindObservabilityDeclaredMetricRoute:
		_, err := DecodeObservabilityDeclaredMetricRoute(env)
		return err
	case FactKindObservabilityDeclaredLogRoute:
		_, err := DecodeObservabilityDeclaredLogRoute(env)
		return err
	case FactKindObservabilityDeclaredTraceRoute:
		_, err := DecodeObservabilityDeclaredTraceRoute(env)
		return err
	case FactKindObservabilityAppliedResource:
		_, err := DecodeObservabilityAppliedResource(env)
		return err
	case FactKindObservabilityAppliedSyncState:
		_, err := DecodeObservabilityAppliedSyncState(env)
		return err
	case FactKindObservabilityObservedDashboard:
		_, err := DecodeObservabilityObservedDashboard(env)
		return err
	case FactKindObservabilityObservedTarget:
		_, err := DecodeObservabilityObservedTarget(env)
		return err
	case FactKindObservabilityObservedRule:
		_, err := DecodeObservabilityObservedRule(env)
		return err
	case FactKindObservabilityObservedLogSignal:
		_, err := DecodeObservabilityObservedLogSignal(env)
		return err
	case FactKindObservabilityObservedTraceSignal:
		_, err := DecodeObservabilityObservedTraceSignal(env)
		return err
	case FactKindObservabilityCoverageWarning:
		_, err := DecodeObservabilityCoverageWarning(env)
		return err
	case FactKindObservabilitySourceInstance:
		_, err := DecodeObservabilitySourceInstance(env)
		return err
	default:
		t.Fatalf("decodeByKind: unhandled fact kind %q — add it to the switch", factKind)
		return nil
	}
}

// allDecodedKinds is every fact kind this module decodes, so the per-kind tests
// below fail if a new kind is added to payloadContracts without wiring its
// Decode dispatch and coverage here.
var allDecodedKinds = []string{
	FactKindAWSResource,
	FactKindAWSRelationship,
	FactKindAWSDNSRecord,
	FactKindAWSImageReference,
	FactKindAWSSecurityGroupRule,
	FactKindAWSWarning,
	FactKindEC2InstancePosture,
	FactKindRDSInstancePosture,
	FactKindS3BucketPosture,
	FactKindS3ExternalPrincipalGrant,
	FactKindAWSIAMPermission,
	FactKindAWSResourcePolicyPermission,
	FactKindAWSIAMPrincipal,
	FactKindAWSIAMTrustPolicy,
	FactKindAWSIAMPermissionPolicy,
	FactKindAWSIAMPolicyAttachment,
	FactKindAWSIAMPermissionBoundary,
	FactKindAWSIAMInstanceProfile,
	FactKindAWSIAMAccessAnalyzerFinding,
	FactKindIncidentRecord,
	FactKindIncidentLifecycleEvent,
	FactKindChangeRecord,
	FactKindIncidentRoutingAppliedPagerDutyResource,
	FactKindIncidentRoutingAppliedAlertRoute,
	FactKindIncidentRoutingObservedPagerDutyService,
	FactKindIncidentRoutingObservedPagerDutyIntegration,
	FactKindIncidentRoutingCoverageWarning,
	FactKindGCPCloudResource,
	FactKindGCPCloudRelationship,
	FactKindGCPCollectionWarning,
	FactKindGCPDNSRecord,
	FactKindGCPIAMPolicyObservation,
	FactKindGCPTagObservation,
	FactKindGCPImageReference,
	FactKindGCPIAMPrincipal,
	FactKindGCPIAMTrustPolicy,
	FactKindGCPIAMPermissionPolicy,
	FactKindAzureCloudResource,
	FactKindAzureCloudRelationship,
	FactKindAzureDNSRecord,
	FactKindAzureCollectionWarning,
	FactKindAzureTagObservation,
	FactKindAzureIdentityObservation,
	FactKindAzureResourceChange,
	FactKindAzureImageReference,
	FactKindKubernetesLivePodTemplate,
	FactKindKubernetesLiveRelationship,
	FactKindKubernetesLiveWarning,
	FactKindKubernetesLiveNamespace,
	FactKindOCIRegistryRepository,
	FactKindOCIImageManifest,
	FactKindOCIImageIndex,
	FactKindOCIImageDescriptor,
	FactKindOCIImageTagObservation,
	FactKindOCIImageReferrer,
	FactKindOCIRegistryWarning,
	FactKindTerraformStateSnapshot,
	FactKindTerraformStateResource,
	FactKindTerraformStateModule,
	FactKindTerraformStateOutput,
	FactKindTerraformStateTagObservation,
	FactKindTerraformStateCandidate,
	FactKindTerraformStateProviderBinding,
	FactKindTerraformStateWarning,
	FactKindPackageRegistryPackage,
	FactKindPackageRegistryPackageVersion,
	FactKindPackageRegistryPackageDependency,
	FactKindPackageRegistrySourceHint,
	FactKindPackageRegistryPackageArtifact,
	FactKindPackageRegistryVulnerabilityHint,
	FactKindPackageRegistryRegistryEvent,
	FactKindPackageRegistryRepositoryHosting,
	FactKindPackageRegistryWarning,
	FactKindSBOMDocument,
	FactKindSBOMComponent,
	FactKindSBOMDependencyRelationship,
	FactKindSBOMExternalReference,
	FactKindSBOMWarning,
	FactKindAttestationStatement,
	FactKindAttestationSignatureVerification,
	FactKindAttestationSLSAProvenance,
	FactKindScannerWorkerAnalysis,
	FactKindScannerWorkerWarning,
	FactKindVulnerabilityCVE,
	FactKindVulnerabilityAffectedPackage,
	FactKindVulnerabilityAffectedProduct,
	FactKindVulnerabilityOSPackage,
	FactKindVulnerabilityEPSSScore,
	FactKindVulnerabilityKnownExploited,
	FactKindVulnerabilityGoModuleEvidence,
	FactKindVulnerabilityGoCallReachability,
	FactKindVulnerabilityReference,
	FactKindVulnerabilitySourceSnapshot,
	FactKindCodegraphFile,
	FactKindCodegraphRepository,
	FactKindCodeDataflowScanned,
	FactKindCodeDataflowFunction,
	FactKindCodeFunctionSummary,
	FactKindCodeFunctionSource,
	FactKindCodeTaintEvidence,
	FactKindCodeInterprocEvidence,
	FactKindCICDRun,
	FactKindCICDArtifact,
	FactKindCICDEnvironmentObservation,
	FactKindCICDTriggerEdge,
	FactKindCICDStep,
	FactKindCICDWorkflowImageEvidence,
	FactKindVaultAuthRole,
	FactKindVaultACLPolicy,
	FactKindVaultKVMetadata,
	FactKindKubernetesServiceAccount,
	FactKindKubernetesWorkloadIdentityUse,
	FactKindEKSIRSAAnnotation,
	FactKindEKSPodIdentityAssociation,
	FactKindKubernetesGCPWorkloadIdentityBinding,
	FactKindKubernetesRBACRole,
	FactKindKubernetesRBACBinding,
	FactKindKubernetesServiceAccountTokenPosture,
	FactKindVaultAuthMount,
	FactKindVaultIdentityEntity,
	FactKindVaultIdentityAlias,
	FactKindVaultSecretEngineMount,
	FactKindSecretsIAMCoverageWarning,
	FactKindWorkItemRecord,
	FactKindWorkItemTransition,
	FactKindWorkItemExternalLink,
	FactKindWorkItemProjectMetadata,
	FactKindWorkItemIssueTypeMetadata,
	FactKindWorkItemStatusMetadata,
	FactKindWorkItemWorkflowMetadata,
	FactKindWorkItemFieldMetadata,
	FactKindWorkItemMetadataWarning,
	FactKindSecurityAlertRepositoryAlert,
	FactKindReducerSupplyChainImpactFinding,
	FactKindReducerAWSCloudRuntimeDriftFinding,
	FactKindReducerMultiCloudRuntimeDriftFinding,
	FactKindReducerPackageOwnershipCorrelation,
	FactKindReducerPackageConsumptionCorrelation,
	FactKindReducerPackagePublicationCorrelation,
	FactKindObservabilityDeclaredFolder,
	FactKindObservabilityDeclaredDashboard,
	FactKindObservabilityDeclaredDatasource,
	FactKindObservabilityDeclaredAlertRule,
	FactKindObservabilityDeclaredScrapeConfig,
	FactKindObservabilityDeclaredMetricRule,
	FactKindObservabilityDeclaredMetricRoute,
	FactKindObservabilityDeclaredLogRoute,
	FactKindObservabilityDeclaredTraceRoute,
	FactKindObservabilityAppliedResource,
	FactKindObservabilityAppliedSyncState,
	FactKindObservabilityObservedDashboard,
	FactKindObservabilityObservedTarget,
	FactKindObservabilityObservedRule,
	FactKindObservabilityObservedLogSignal,
	FactKindObservabilityObservedTraceSignal,
	FactKindObservabilityCoverageWarning,
	FactKindObservabilitySourceInstance,
	FactKindDocumentationSource,
	FactKindDocumentationDocument,
	FactKindDocumentationSection,
	FactKindDocumentationLink,
	FactKindDocumentationEntityMention,
	FactKindDocumentationClaimCandidate,
	FactKindDocumentationFinding,
	FactKindDocumentationEvidencePacket,
	FactKindSemanticDocumentationObservation,
	FactKindSemanticCodeHint,
	FactKindServiceCatalogEntity,
	FactKindServiceCatalogOwnership,
	FactKindServiceCatalogRepositoryLink,
	FactKindServiceCatalogOperationalLink,
	FactKindCodeownersOwnership,
}

// TestDecodeEachKind_MissingEachRequiredFieldDeadLetters proves, for every
// decoded fact kind and every one of its required fields, that removing that one
// field from an otherwise-valid payload yields a classified *DecodeError naming
// exactly that field with ClassificationInputInvalid. This is the accuracy
// backstop generalized across the whole migrated domain: no required field can
// go silently unvalidated.
func TestDecodeEachKind_MissingEachRequiredFieldDeadLetters(t *testing.T) {
	t.Parallel()

	for _, factKind := range allDecodedKinds {
		t.Run(factKind, func(t *testing.T) {
			t.Parallel()

			for _, field := range requiredFieldsForKind(t, factKind) {
				t.Run(field, func(t *testing.T) {
					t.Parallel()

					payload := fullPayloadForKind(t, factKind)
					delete(payload, field)

					err := decodeByKind(t, factKind, payload)
					if err == nil {
						t.Fatalf("decode %s missing %q: error = nil, want *DecodeError", factKind, field)
					}
					var decodeErr *DecodeError
					if !errors.As(err, &decodeErr) {
						t.Fatalf("decode %s missing %q: error = %T, want *DecodeError", factKind, field, err)
					}
					if decodeErr.Classification != ClassificationInputInvalid {
						t.Fatalf("decode %s missing %q: classification = %q, want %q", factKind, field, decodeErr.Classification, ClassificationInputInvalid)
					}
					if decodeErr.Field != field {
						t.Fatalf("decode %s missing %q: field = %q, want %q", factKind, field, decodeErr.Field, field)
					}
				})
			}
		})
	}
}

// TestDecodeEachKind_FullRequiredPayloadDecodes proves that an envelope carrying
// every required key (each present and non-empty) decodes without error for
// every kind — the positive counterpart to the missing-field test, so the
// dead-letter assertion cannot pass merely because decode always errors.
func TestDecodeEachKind_FullRequiredPayloadDecodes(t *testing.T) {
	t.Parallel()

	for _, factKind := range allDecodedKinds {
		t.Run(factKind, func(t *testing.T) {
			t.Parallel()

			if err := decodeByKind(t, factKind, fullPayloadForKind(t, factKind)); err != nil {
				t.Fatalf("decode %s full required payload: error = %v, want nil", factKind, err)
			}
		})
	}
}

// TestDecodeEachKind_PresentButEmptyRequiredFieldDecodes proves the
// absent-vs-empty distinction holds for every kind: a required key present with
// an empty string is a valid observed value and decodes, while only an absent or
// null key dead-letters (covered above). This guards the byte-identical contract
// — an incomplete-but-present fact must decode exactly as it did before typing.
func TestDecodeEachKind_PresentButEmptyRequiredFieldDecodes(t *testing.T) {
	t.Parallel()

	for _, factKind := range allDecodedKinds {
		t.Run(factKind, func(t *testing.T) {
			t.Parallel()

			typ := structTypeForKind(t, factKind)
			payload := fullPayloadForKind(t, factKind)
			for _, field := range requiredFieldsForKind(t, factKind) {
				payload[field] = requiredFieldValue(t, typ, field, false)
			}
			if err := decodeByKind(t, factKind, payload); err != nil {
				t.Fatalf("decode %s all-empty required payload: error = %v, want nil (present-but-empty is valid)", factKind, err)
			}
		})
	}
}

// TestDecodeEachKind_UnsupportedMajorDeadLetters proves every kind's Decode
// function rejects an unsupported schema-version major as a classified error
// wrapping ErrUnsupportedSchemaMajor, not a best-effort decode.
func TestDecodeEachKind_UnsupportedMajorDeadLetters(t *testing.T) {
	t.Parallel()

	for _, factKind := range allDecodedKinds {
		t.Run(factKind, func(t *testing.T) {
			t.Parallel()

			env := Envelope{FactKind: factKind, SchemaVersion: "2.0.0", Payload: fullPayloadForKind(t, factKind)}
			var err error
			switch factKind {
			case FactKindAWSResource:
				_, err = DecodeAWSResource(env)
			case FactKindAWSRelationship:
				_, err = DecodeAWSRelationship(env)
			case FactKindAWSDNSRecord:
				_, err = DecodeAWSDNSRecord(env)
			case FactKindAWSImageReference:
				_, err = DecodeAWSImageReference(env)
			case FactKindAWSSecurityGroupRule:
				_, err = DecodeAWSSecurityGroupRule(env)
			case FactKindAWSWarning:
				_, err = DecodeAWSWarning(env)
			case FactKindEC2InstancePosture:
				_, err = DecodeEC2InstancePosture(env)
			case FactKindRDSInstancePosture:
				_, err = DecodeRDSInstancePosture(env)
			case FactKindS3BucketPosture:
				_, err = DecodeS3BucketPosture(env)
			case FactKindS3ExternalPrincipalGrant:
				_, err = DecodeS3ExternalPrincipalGrant(env)
			case FactKindAWSIAMPermission:
				_, err = DecodeAWSIAMPermission(env)
			case FactKindAWSResourcePolicyPermission:
				_, err = DecodeAWSResourcePolicyPermission(env)
			case FactKindAWSIAMPrincipal:
				_, err = DecodeAWSIAMPrincipal(env)
			case FactKindAWSIAMTrustPolicy:
				_, err = DecodeAWSIAMTrustPolicy(env)
			case FactKindAWSIAMPermissionPolicy:
				_, err = DecodeAWSIAMPermissionPolicy(env)
			case FactKindAWSIAMPolicyAttachment:
				_, err = DecodeAWSIAMPolicyAttachment(env)
			case FactKindAWSIAMPermissionBoundary:
				_, err = DecodeAWSIAMPermissionBoundary(env)
			case FactKindAWSIAMInstanceProfile:
				_, err = DecodeAWSIAMInstanceProfile(env)
			case FactKindAWSIAMAccessAnalyzerFinding:
				_, err = DecodeAWSIAMAccessAnalyzerFinding(env)
			case FactKindIncidentRecord:
				_, err = DecodeIncidentRecord(env)
			case FactKindIncidentLifecycleEvent:
				_, err = DecodeIncidentLifecycleEvent(env)
			case FactKindChangeRecord:
				_, err = DecodeChangeRecord(env)
			case FactKindIncidentRoutingAppliedPagerDutyResource:
				_, err = DecodeIncidentRoutingAppliedPagerDutyResource(env)
			case FactKindIncidentRoutingAppliedAlertRoute:
				_, err = DecodeIncidentRoutingAppliedAlertRoute(env)
			case FactKindIncidentRoutingObservedPagerDutyService:
				_, err = DecodeIncidentRoutingObservedPagerDutyService(env)
			case FactKindIncidentRoutingObservedPagerDutyIntegration:
				_, err = DecodeIncidentRoutingObservedPagerDutyIntegration(env)
			case FactKindIncidentRoutingCoverageWarning:
				_, err = DecodeIncidentRoutingCoverageWarning(env)
			case FactKindGCPCloudResource:
				_, err = DecodeGCPCloudResource(env)
			case FactKindGCPCloudRelationship:
				_, err = DecodeGCPCloudRelationship(env)
			case FactKindGCPCollectionWarning:
				_, err = DecodeGCPCollectionWarning(env)
			case FactKindGCPDNSRecord:
				_, err = DecodeGCPDNSRecord(env)
			case FactKindGCPIAMPolicyObservation:
				_, err = DecodeGCPIAMPolicyObservation(env)
			case FactKindGCPTagObservation:
				_, err = DecodeGCPTagObservation(env)
			case FactKindGCPImageReference:
				_, err = DecodeGCPImageReference(env)
			case FactKindGCPIAMPrincipal:
				_, err = DecodeGCPIAMPrincipal(env)
			case FactKindGCPIAMTrustPolicy:
				_, err = DecodeGCPIAMTrustPolicy(env)
			case FactKindGCPIAMPermissionPolicy:
				_, err = DecodeGCPIAMPermissionPolicy(env)
			case FactKindAzureCloudResource:
				_, err = DecodeAzureCloudResource(env)
			case FactKindAzureCloudRelationship:
				_, err = DecodeAzureCloudRelationship(env)
			case FactKindAzureDNSRecord:
				_, err = DecodeAzureDNSRecord(env)
			case FactKindAzureCollectionWarning:
				_, err = DecodeAzureCollectionWarning(env)
			case FactKindAzureTagObservation:
				_, err = DecodeAzureTagObservation(env)
			case FactKindAzureIdentityObservation:
				_, err = DecodeAzureIdentityObservation(env)
			case FactKindAzureResourceChange:
				_, err = DecodeAzureResourceChange(env)
			case FactKindAzureImageReference:
				_, err = DecodeAzureImageReference(env)
			case FactKindKubernetesLivePodTemplate:
				_, err = DecodeKubernetesLivePodTemplate(env)
			case FactKindKubernetesLiveRelationship:
				_, err = DecodeKubernetesLiveRelationship(env)
			case FactKindKubernetesLiveWarning:
				_, err = DecodeKubernetesLiveWarning(env)
			case FactKindKubernetesLiveNamespace:
				_, err = DecodeKubernetesLiveNamespace(env)
			case FactKindOCIRegistryRepository:
				_, err = DecodeOCIRegistryRepository(env)
			case FactKindOCIImageManifest:
				_, err = DecodeOCIImageManifest(env)
			case FactKindOCIImageIndex:
				_, err = DecodeOCIImageIndex(env)
			case FactKindOCIImageDescriptor:
				_, err = DecodeOCIImageDescriptor(env)
			case FactKindOCIImageTagObservation:
				_, err = DecodeOCIImageTagObservation(env)
			case FactKindOCIImageReferrer:
				_, err = DecodeOCIImageReferrer(env)
			case FactKindOCIRegistryWarning:
				_, err = DecodeOCIRegistryWarning(env)
			case FactKindTerraformStateSnapshot:
				_, err = DecodeTerraformStateSnapshot(env)
			case FactKindTerraformStateResource:
				_, err = DecodeTerraformStateResource(env)
			case FactKindTerraformStateModule:
				_, err = DecodeTerraformStateModule(env)
			case FactKindTerraformStateOutput:
				_, err = DecodeTerraformStateOutput(env)
			case FactKindTerraformStateTagObservation:
				_, err = DecodeTerraformStateTagObservation(env)
			case FactKindTerraformStateCandidate:
				_, err = DecodeTerraformStateCandidate(env)
			case FactKindTerraformStateProviderBinding:
				_, err = DecodeTerraformStateProviderBinding(env)
			case FactKindTerraformStateWarning:
				_, err = DecodeTerraformStateWarning(env)
			case FactKindPackageRegistryPackage:
				_, err = DecodePackageRegistryPackage(env)
			case FactKindPackageRegistryPackageVersion:
				_, err = DecodePackageRegistryPackageVersion(env)
			case FactKindPackageRegistryPackageDependency:
				_, err = DecodePackageRegistryPackageDependency(env)
			case FactKindPackageRegistrySourceHint:
				_, err = DecodePackageRegistrySourceHint(env)
			case FactKindPackageRegistryPackageArtifact:
				_, err = DecodePackageRegistryPackageArtifact(env)
			case FactKindPackageRegistryVulnerabilityHint:
				_, err = DecodePackageRegistryVulnerabilityHint(env)
			case FactKindPackageRegistryRegistryEvent:
				_, err = DecodePackageRegistryRegistryEvent(env)
			case FactKindPackageRegistryRepositoryHosting:
				_, err = DecodePackageRegistryRepositoryHosting(env)
			case FactKindPackageRegistryWarning:
				_, err = DecodePackageRegistryWarning(env)
			case FactKindSBOMDocument:
				_, err = DecodeSBOMDocument(env)
			case FactKindSBOMComponent:
				_, err = DecodeSBOMComponent(env)
			case FactKindSBOMDependencyRelationship:
				_, err = DecodeSBOMDependencyRelationship(env)
			case FactKindSBOMExternalReference:
				_, err = DecodeSBOMExternalReference(env)
			case FactKindSBOMWarning:
				_, err = DecodeSBOMWarning(env)
			case FactKindAttestationStatement:
				_, err = DecodeAttestationStatement(env)
			case FactKindAttestationSignatureVerification:
				_, err = DecodeAttestationSignatureVerification(env)
			case FactKindAttestationSLSAProvenance:
				_, err = DecodeAttestationSLSAProvenance(env)
			case FactKindScannerWorkerAnalysis:
				_, err = DecodeScannerWorkerAnalysis(env)
			case FactKindScannerWorkerWarning:
				_, err = DecodeScannerWorkerWarning(env)
			case FactKindVulnerabilityCVE:
				_, err = DecodeVulnerabilityCVE(env)
			case FactKindVulnerabilityAffectedPackage:
				_, err = DecodeVulnerabilityAffectedPackage(env)
			case FactKindVulnerabilityAffectedProduct:
				_, err = DecodeVulnerabilityAffectedProduct(env)
			case FactKindVulnerabilityOSPackage:
				_, err = DecodeVulnerabilityOSPackage(env)
			case FactKindVulnerabilityEPSSScore:
				_, err = DecodeVulnerabilityEPSSScore(env)
			case FactKindVulnerabilityKnownExploited:
				_, err = DecodeVulnerabilityKnownExploited(env)
			case FactKindVulnerabilityGoModuleEvidence:
				_, err = DecodeVulnerabilityGoModuleEvidence(env)
			case FactKindVulnerabilityGoCallReachability:
				_, err = DecodeVulnerabilityGoCallReachability(env)
			case FactKindVulnerabilityReference:
				_, err = DecodeVulnerabilityReference(env)
			case FactKindVulnerabilitySourceSnapshot:
				_, err = DecodeVulnerabilitySourceSnapshot(env)
			case FactKindCodegraphFile:
				_, err = DecodeCodegraphFile(env)
			case FactKindCodegraphRepository:
				_, err = DecodeCodegraphRepository(env)
			case FactKindCodeDataflowScanned:
				_, err = DecodeCodeDataflowScanned(env)
			case FactKindCodeDataflowFunction:
				_, err = DecodeCodeDataflowFunction(env)
			case FactKindCodeFunctionSummary:
				_, err = DecodeCodeFunctionSummary(env)
			case FactKindCodeFunctionSource:
				_, err = DecodeCodeFunctionSource(env)
			case FactKindCodeTaintEvidence:
				_, err = DecodeCodeTaintEvidence(env)
			case FactKindCodeInterprocEvidence:
				_, err = DecodeCodeInterprocEvidence(env)
			case FactKindCICDRun:
				_, err = DecodeCICDRun(env)
			case FactKindCICDArtifact:
				_, err = DecodeCICDArtifact(env)
			case FactKindCICDEnvironmentObservation:
				_, err = DecodeCICDEnvironmentObservation(env)
			case FactKindCICDTriggerEdge:
				_, err = DecodeCICDTriggerEdge(env)
			case FactKindCICDStep:
				_, err = DecodeCICDStep(env)
			case FactKindCICDWorkflowImageEvidence:
				_, err = DecodeCICDWorkflowImageEvidence(env)
			case FactKindVaultAuthRole:
				_, err = DecodeVaultAuthRole(env)
			case FactKindVaultACLPolicy:
				_, err = DecodeVaultACLPolicy(env)
			case FactKindVaultKVMetadata:
				_, err = DecodeVaultKVMetadata(env)
			case FactKindKubernetesServiceAccount:
				_, err = DecodeKubernetesServiceAccount(env)
			case FactKindKubernetesWorkloadIdentityUse:
				_, err = DecodeKubernetesWorkloadIdentityUse(env)
			case FactKindEKSIRSAAnnotation:
				_, err = DecodeEKSIRSAAnnotation(env)
			case FactKindEKSPodIdentityAssociation:
				_, err = DecodeEKSPodIdentityAssociation(env)
			case FactKindKubernetesGCPWorkloadIdentityBinding:
				_, err = DecodeKubernetesGCPWorkloadIdentityBinding(env)
			case FactKindKubernetesRBACRole:
				_, err = DecodeKubernetesRBACRole(env)
			case FactKindKubernetesRBACBinding:
				_, err = DecodeKubernetesRBACBinding(env)
			case FactKindKubernetesServiceAccountTokenPosture:
				_, err = DecodeKubernetesServiceAccountTokenPosture(env)
			case FactKindVaultAuthMount:
				_, err = DecodeVaultAuthMount(env)
			case FactKindVaultIdentityEntity:
				_, err = DecodeVaultIdentityEntity(env)
			case FactKindVaultIdentityAlias:
				_, err = DecodeVaultIdentityAlias(env)
			case FactKindVaultSecretEngineMount:
				_, err = DecodeVaultSecretEngineMount(env)
			case FactKindSecretsIAMCoverageWarning:
				_, err = DecodeSecretsIAMCoverageWarning(env)
			case FactKindWorkItemRecord:
				_, err = DecodeWorkItemRecord(env)
			case FactKindWorkItemTransition:
				_, err = DecodeWorkItemTransition(env)
			case FactKindWorkItemExternalLink:
				_, err = DecodeWorkItemExternalLink(env)
			case FactKindWorkItemProjectMetadata:
				_, err = DecodeWorkItemProjectMetadata(env)
			case FactKindWorkItemIssueTypeMetadata:
				_, err = DecodeWorkItemIssueTypeMetadata(env)
			case FactKindWorkItemStatusMetadata:
				_, err = DecodeWorkItemStatusMetadata(env)
			case FactKindWorkItemWorkflowMetadata:
				_, err = DecodeWorkItemWorkflowMetadata(env)
			case FactKindWorkItemFieldMetadata:
				_, err = DecodeWorkItemFieldMetadata(env)
			case FactKindWorkItemMetadataWarning:
				_, err = DecodeWorkItemMetadataWarning(env)
			case FactKindSecurityAlertRepositoryAlert:
				_, err = DecodeSecurityAlertRepositoryAlert(env)
			case FactKindReducerSupplyChainImpactFinding:
				_, err = DecodeReducerSupplyChainImpactFinding(env)
			case FactKindReducerAWSCloudRuntimeDriftFinding:
				_, err = DecodeReducerAWSCloudRuntimeDriftFinding(env)
			case FactKindReducerMultiCloudRuntimeDriftFinding:
				_, err = DecodeReducerMultiCloudRuntimeDriftFinding(env)
			case FactKindReducerPackageOwnershipCorrelation:
				_, err = DecodeReducerPackageOwnershipCorrelation(env)
			case FactKindReducerPackageConsumptionCorrelation:
				_, err = DecodeReducerPackageConsumptionCorrelation(env)
			case FactKindReducerPackagePublicationCorrelation:
				_, err = DecodeReducerPackagePublicationCorrelation(env)
			case FactKindObservabilityDeclaredFolder:
				_, err = DecodeObservabilityDeclaredFolder(env)
			case FactKindObservabilityDeclaredDashboard:
				_, err = DecodeObservabilityDeclaredDashboard(env)
			case FactKindObservabilityDeclaredDatasource:
				_, err = DecodeObservabilityDeclaredDatasource(env)
			case FactKindObservabilityDeclaredAlertRule:
				_, err = DecodeObservabilityDeclaredAlertRule(env)
			case FactKindObservabilityDeclaredScrapeConfig:
				_, err = DecodeObservabilityDeclaredScrapeConfig(env)
			case FactKindObservabilityDeclaredMetricRule:
				_, err = DecodeObservabilityDeclaredMetricRule(env)
			case FactKindObservabilityDeclaredMetricRoute:
				_, err = DecodeObservabilityDeclaredMetricRoute(env)
			case FactKindObservabilityDeclaredLogRoute:
				_, err = DecodeObservabilityDeclaredLogRoute(env)
			case FactKindObservabilityDeclaredTraceRoute:
				_, err = DecodeObservabilityDeclaredTraceRoute(env)
			case FactKindObservabilityAppliedResource:
				_, err = DecodeObservabilityAppliedResource(env)
			case FactKindObservabilityAppliedSyncState:
				_, err = DecodeObservabilityAppliedSyncState(env)
			case FactKindObservabilityObservedDashboard:
				_, err = DecodeObservabilityObservedDashboard(env)
			case FactKindObservabilityObservedTarget:
				_, err = DecodeObservabilityObservedTarget(env)
			case FactKindObservabilityObservedRule:
				_, err = DecodeObservabilityObservedRule(env)
			case FactKindObservabilityObservedLogSignal:
				_, err = DecodeObservabilityObservedLogSignal(env)
			case FactKindObservabilityObservedTraceSignal:
				_, err = DecodeObservabilityObservedTraceSignal(env)
			case FactKindObservabilityCoverageWarning:
				_, err = DecodeObservabilityCoverageWarning(env)
			case FactKindObservabilitySourceInstance:
				_, err = DecodeObservabilitySourceInstance(env)
			case FactKindDocumentationSource:
				_, err = DecodeDocumentationSource(env)
			case FactKindDocumentationDocument:
				_, err = DecodeDocumentationDocument(env)
			case FactKindDocumentationSection:
				_, err = DecodeDocumentationSection(env)
			case FactKindDocumentationLink:
				_, err = DecodeDocumentationLink(env)
			case FactKindDocumentationEntityMention:
				_, err = DecodeDocumentationEntityMention(env)
			case FactKindDocumentationClaimCandidate:
				_, err = DecodeDocumentationClaimCandidate(env)
			case FactKindDocumentationFinding:
				_, err = DecodeDocumentationFinding(env)
			case FactKindDocumentationEvidencePacket:
				_, err = DecodeDocumentationEvidencePacket(env)
			case FactKindSemanticDocumentationObservation:
				_, err = DecodeSemanticDocumentationObservation(env)
			case FactKindSemanticCodeHint:
				_, err = DecodeSemanticCodeHint(env)
			case FactKindServiceCatalogEntity:
				_, err = DecodeServiceCatalogEntity(env)
			case FactKindServiceCatalogOwnership:
				_, err = DecodeServiceCatalogOwnership(env)
			case FactKindServiceCatalogRepositoryLink:
				_, err = DecodeServiceCatalogRepositoryLink(env)
			case FactKindServiceCatalogOperationalLink:
				_, err = DecodeServiceCatalogOperationalLink(env)
			case FactKindCodeownersOwnership:
				_, err = DecodeCodeownersOwnership(env)
			}
			if !errors.Is(err, ErrUnsupportedSchemaMajor) {
				t.Fatalf("decode %s unsupported major: error = %v, want errors.Is ErrUnsupportedSchemaMajor", factKind, err)
			}
		})
	}
}
