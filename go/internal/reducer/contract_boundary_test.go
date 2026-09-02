// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"sort"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/eshusearch"
)

func TestReducerContractAliasesPreserveTypeIdentity(t *testing.T) {
	t.Parallel()

	aliases := []struct {
		name     string
		root     any
		contract any
	}{
		{name: "Domain", root: Domain(""), contract: reducercontract.Domain("")},
		{name: "IntentStatus", root: IntentStatus(""), contract: reducercontract.IntentStatus("")},
		{name: "ResultStatus", root: ResultStatus(""), contract: reducercontract.ResultStatus("")},
		{name: "FailureRecord", root: FailureRecord{}, contract: reducercontract.FailureRecord{}},
		{name: "RetryableError", root: (*RetryableError)(nil), contract: (*reducercontract.RetryableError)(nil)},
		{name: "Intent", root: Intent{}, contract: reducercontract.Intent{}},
		{name: "Result", root: Result{}, contract: reducercontract.Result{}},
		{name: "OwnershipShape", root: OwnershipShape{}, contract: reducercontract.OwnershipShape{}},
		{name: "CrossScopeDependency", root: CrossScopeDependency{}, contract: reducercontract.CrossScopeDependency{}},
		{name: "DomainDefinition", root: DomainDefinition{}, contract: reducercontract.DomainDefinition{}},
		{name: "Handler", root: (*Handler)(nil), contract: (*reducercontract.Handler)(nil)},
		{name: "HandlerFunc", root: HandlerFunc(nil), contract: reducercontract.HandlerFunc(nil)},
		{name: "ContainerImageIdentityOutcome", root: ContainerImageIdentityOutcome(""), contract: reducercontract.ContainerImageIdentityOutcome("")},
	}

	// The fact-kind name is a durable wire value: it is written into stored
	// facts, so changing the string silently orphans every fact already
	// persisted under the old name. Type identity cannot catch that -- both
	// sides stay the same type while the value changes -- so pin the value
	// itself, on both the root alias and the contract constant it points at.
	t.Run("ContainerImageIdentityFactKind", func(t *testing.T) {
		t.Parallel()
		const want = "reducer_container_image_identity"
		if containerImageIdentityFactKind != want {
			t.Errorf("root containerImageIdentityFactKind = %q, want %q", containerImageIdentityFactKind, want)
		}
		if reducercontract.ContainerImageIdentityFactKind != want {
			t.Errorf("contract ContainerImageIdentityFactKind = %q, want %q", reducercontract.ContainerImageIdentityFactKind, want)
		}
		if containerImageIdentityFactKind != reducercontract.ContainerImageIdentityFactKind {
			t.Errorf("root alias %q and contract constant %q have diverged",
				containerImageIdentityFactKind, reducercontract.ContainerImageIdentityFactKind)
		}
	})

	for _, alias := range aliases {
		alias := alias
		t.Run(alias.name, func(t *testing.T) {
			t.Parallel()
			if got, want := reflect.TypeOf(alias.root), reflect.TypeOf(alias.contract); got != want {
				t.Fatalf("root type = %v, contract type = %v", got, want)
			}
		})
	}
}

func TestRegistrableReducerDomainsCharacterization(t *testing.T) {
	t.Parallel()

	want := []Domain{
		DomainAWSCloudImageMaterialization,
		DomainAWSCloudRuntimeDrift,
		DomainAWSRelationshipMaterialization,
		DomainAWSResourceMaterialization,
		DomainAzureRelationshipMaterialization,
		DomainAzureResourceMaterialization,
		DomainCICDRunCorrelation,
		DomainCloudAssetResolution,
		DomainCloudInventoryAdmission,
		DomainCodeCallMaterialization,
		DomainCodeFunctionSummary,
		DomainCodeImportRepoEdge,
		DomainCodeInterprocEvidence,
		DomainCodeTaintEvidence,
		DomainCodeownersOwnership,
		DomainConfigStateDrift,
		DomainContainerImageIdentity,
		DomainCrossplaneSatisfiedByMaterialization,
		DomainDeployableUnitCorrelation,
		DomainDeploymentMapping,
		DomainDocumentationMaterialization,
		DomainEC2BlockDeviceKMSPostureMaterialization,
		DomainEC2InstanceIdentityMaterialization,
		DomainEC2InstanceNodeMaterialization,
		DomainEC2InternetExposureMaterialization,
		DomainEC2UsesProfileMaterialization,
		eshusearch.DomainEshuSearchDocument,
		DomainGCPRelationshipMaterialization,
		DomainGCPResourceMaterialization,
		DomainIAMCanAssumeMaterialization,
		DomainIAMCanPerformMaterialization,
		DomainIAMEscalationMaterialization,
		DomainIAMInstanceProfileRoleMaterialization,
		DomainIncidentRepositoryCorrelation,
		DomainIncidentRoutingMaterialization,
		DomainInheritanceMaterialization,
		DomainKubernetesCorrelation,
		DomainKubernetesCorrelationMaterialization,
		DomainKubernetesNamespaceMaterialization,
		DomainKubernetesWorkloadMaterialization,
		DomainMultiCloudRuntimeDrift,
		DomainObservabilityCoverageCorrelation,
		DomainObservabilityCoverageMaterialization,
		DomainPackageSourceCorrelation,
		DomainPlatformInfraMaterialization,
		DomainRationaleMaterialization,
		DomainRDSPostureMaterialization,
		DomainS3ExternalPrincipalGrantMaterialization,
		DomainS3InternetExposureMaterialization,
		DomainS3LogsToMaterialization,
		DomainSBOMAttestationAttachment,
		DomainSecretsIAMGraphProjection,
		DomainSecretsIAMTrustChain,
		DomainSecurityAlertReconciliation,
		DomainSecurityGroupCidrMaterialization,
		DomainSecurityGroupReachabilityMaterialization,
		DomainSecurityGroupRuleMaterialization,
		DomainSemanticEntityMaterialization,
		DomainServiceCatalogCorrelation,
		DomainShellExecMaterialization,
		DomainSQLRelationshipMaterialization,
		DomainSubmodulePin,
		DomainSupplyChainImpact,
		DomainWorkloadCloudRelationshipMaterialization,
		DomainWorkloadIdentity,
		DomainWorkloadMaterialization,
	}

	if got := registrableReducerDomains(); !reflect.DeepEqual(got, want) {
		t.Fatalf("registrableReducerDomains() = %v, want %v", got, want)
	}

	wantKnown := append([]Domain(nil), want...)
	wantKnown = append(wantKnown, DomainDataLineage, DomainOwnership, DomainGovernance)
	sort.Slice(wantKnown, func(i, j int) bool { return wantKnown[i] < wantKnown[j] })
	if got := reducercontract.KnownDomains(); !reflect.DeepEqual(got, wantKnown) {
		t.Fatalf("contract.KnownDomains() = %v, want 66 registrable plus reserved data_lineage, ownership, governance: %v", got, wantKnown)
	}
}

func TestDefaultDomainDefinitionOrderCharacterization(t *testing.T) {
	t.Parallel()

	want := []Domain{
		DomainWorkloadIdentity,
		DomainCloudAssetResolution,
		DomainDeploymentMapping,
		DomainCodeCallMaterialization,
		DomainPlatformInfraMaterialization,
		DomainWorkloadMaterialization,
		DomainSemanticEntityMaterialization,
		DomainSQLRelationshipMaterialization,
		DomainShellExecMaterialization,
		DomainInheritanceMaterialization,
		DomainDocumentationMaterialization,
		DomainRationaleMaterialization,
		DomainCodeownersOwnership,
		DomainSubmodulePin,
	}

	definitions := DefaultDomainDefinitions()
	got := make([]Domain, 0, len(definitions))
	for _, definition := range definitions {
		got = append(got, definition.Domain)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultDomainDefinitions() order = %v, want %v", got, want)
	}
}

func TestAllDomainsUnionCharacterization(t *testing.T) {
	t.Parallel()

	want := []Domain{
		DomainAWSCloudImageMaterialization,
		DomainAWSCloudRuntimeDrift,
		DomainAWSRelationshipMaterialization,
		DomainAWSResourceMaterialization,
		DomainAzureRelationshipMaterialization,
		DomainAzureResourceMaterialization,
		DomainCICDRunCorrelation,
		DomainCloudAssetResolution,
		DomainCloudInventoryAdmission,
		DomainCodeCallMaterialization,
		DomainCodeCalls,
		DomainCodeFunctionSummary,
		DomainCodeImportRepoEdge,
		DomainCodeInterprocEvidence,
		DomainCodeTaintEvidence,
		DomainCodeownersOwnership,
		DomainCodeownersOwnershipEdges,
		DomainConfigStateDrift,
		DomainContainerImageIdentity,
		DomainCrossplaneSatisfiedByMaterialization,
		DomainDataLineage,
		DomainDeployableUnitCorrelation,
		DomainDeployableUnitEdges,
		DomainDeploymentMapping,
		DomainDocumentationEdges,
		DomainDocumentationMaterialization,
		DomainEC2BlockDeviceKMSPostureMaterialization,
		DomainEC2InstanceIdentityMaterialization,
		DomainEC2InstanceNodeMaterialization,
		DomainEC2InternetExposureMaterialization,
		DomainEC2UsesProfileMaterialization,
		eshusearch.DomainEshuSearchDocument,
		DomainGCPRelationshipMaterialization,
		DomainGCPResourceMaterialization,
		DomainGovernance,
		DomainHandlesRoute,
		DomainIAMCanAssumeMaterialization,
		DomainIAMCanPerformMaterialization,
		DomainIAMEscalationMaterialization,
		DomainIAMInstanceProfileRoleMaterialization,
		DomainIncidentRepositoryCorrelation,
		DomainIncidentRoutingMaterialization,
		DomainInheritanceEdges,
		DomainInheritanceMaterialization,
		DomainInvokesCloudAction,
		DomainKubernetesCorrelation,
		DomainKubernetesCorrelationMaterialization,
		DomainKubernetesNamespaceMaterialization,
		DomainKubernetesWorkloadMaterialization,
		DomainMultiCloudRuntimeDrift,
		DomainObservabilityCoverageCorrelation,
		DomainObservabilityCoverageMaterialization,
		DomainOwnership,
		DomainPackageSourceCorrelation,
		DomainPlatformInfraMaterialization,
		DomainRationaleEdges,
		DomainRationaleMaterialization,
		DomainRDSPostureMaterialization,
		DomainRepoDependency,
		DomainRunsIn,
		DomainS3ExternalPrincipalGrantMaterialization,
		DomainS3InternetExposureMaterialization,
		DomainS3LogsToMaterialization,
		DomainSBOMAttestationAttachment,
		DomainSecretsIAMGraphProjection,
		DomainSecretsIAMTrustChain,
		DomainSecurityAlertReconciliation,
		DomainSecurityGroupCidrMaterialization,
		DomainSecurityGroupReachabilityMaterialization,
		DomainSecurityGroupRuleMaterialization,
		DomainSemanticEntityMaterialization,
		DomainServiceCatalogCorrelation,
		DomainShellExec,
		DomainShellExecMaterialization,
		DomainSQLRelationshipMaterialization,
		DomainSQLRelationships,
		DomainSubmodulePin,
		DomainSubmodulePinEdges,
		DomainSupplyChainImpact,
		DomainWorkloadCloudRelationshipMaterialization,
		DomainWorkloadDependency,
		DomainWorkloadIdentity,
		DomainWorkloadMaterialization,
	}

	if got := AllDomains(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AllDomains() = %v, want %v", got, want)
	}
}

func TestIntentClonePayloadIsTopLevelCopyWithSharedNestedValues(t *testing.T) {
	t.Parallel()

	nested := map[string]string{"state": "original"}
	source := Intent{Payload: map[string]any{
		"top_level": "original",
		"nested":    nested,
	}}
	cloned := source.Clone()

	cloned.Payload["top_level"] = "changed"
	if got := source.Payload["top_level"]; got != "original" {
		t.Fatalf("source top-level payload = %v, want original", got)
	}

	clonedNested := cloned.Payload["nested"].(map[string]string)
	clonedNested["state"] = "changed"
	if got := nested["state"]; got != "changed" {
		t.Fatalf("nested payload = %q, want shared nested value", got)
	}
}
