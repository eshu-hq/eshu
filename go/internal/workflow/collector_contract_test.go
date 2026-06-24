// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/tfstate"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestCollectorContractForGitIncludesAcceptedCanonicalKeyspaces(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorGit)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorGit)
	}
	want := []reducer.GraphProjectionKeyspace{
		reducer.GraphProjectionKeyspaceCodeEntitiesUID,
		reducer.GraphProjectionKeyspaceDeployableUnitUID,
		reducer.GraphProjectionKeyspaceServiceUID,
	}
	if !reflect.DeepEqual(contract.CanonicalKeyspaces, want) {
		t.Fatalf("CanonicalKeyspaces = %#v, want %#v", contract.CanonicalKeyspaces, want)
	}
}

func TestRequiredPhasesForCollectorIncludesGitDeployableUnitGate(t *testing.T) {
	t.Parallel()

	requirements := RequiredPhasesForCollector(scope.CollectorGit)
	want := []PhaseRequirement{
		{
			Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
			PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
			Required:  true,
		},
		{
			Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
			PhaseName: reducer.GraphProjectionPhaseSemanticNodesCommitted,
			Required:  true,
		},
		{
			Keyspace:  reducer.GraphProjectionKeyspaceDeployableUnitUID,
			PhaseName: reducer.GraphProjectionPhaseDeployableUnitCorrelation,
			Required:  true,
		},
		{
			Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
			PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
			Required:  true,
		},
		{
			Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
			PhaseName: reducer.GraphProjectionPhaseDeploymentMapping,
			Required:  true,
		},
		{
			Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
			PhaseName: reducer.GraphProjectionPhaseWorkloadMaterialization,
			Required:  true,
		},
	}
	if !reflect.DeepEqual(requirements, want) {
		t.Fatalf("RequiredPhasesForCollector(git) = %#v, want %#v", requirements, want)
	}
}

func TestRequiredPhasesForCollectorMatchesTerraformStateReducerContract(t *testing.T) {
	t.Parallel()

	requirements := RequiredPhasesForCollector(scope.CollectorTerraformState)
	contract := tfstate.DefaultRuntimeContract()
	want := make([]PhaseRequirement, 0, len(contract.Checkpoints))
	for _, checkpoint := range contract.Checkpoints {
		want = append(want, PhaseRequirement{
			Keyspace:  checkpoint.Keyspace,
			PhaseName: checkpoint.Phase,
			Required:  true,
		})
	}
	if !reflect.DeepEqual(requirements, want) {
		t.Fatalf("RequiredPhasesForCollector(terraform_state) = %#v, want %#v", requirements, want)
	}
}

func TestCollectorContractForAWSHasNoOperationalGraphReadinessUntilProjectionLands(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorAWS)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorAWS)
	}
	if got := len(contract.CanonicalKeyspaces); got != 0 {
		t.Fatalf("AWS CanonicalKeyspaces = %#v, want empty until cloud-resource graph projection is implemented", contract.CanonicalKeyspaces)
	}
	if got := len(contract.RequiredPhases); got != 0 {
		t.Fatalf("AWS RequiredPhases = %#v, want empty until cloud-resource graph projection is implemented", contract.RequiredPhases)
	}
}

func TestCollectorContractForGCPHasNoOperationalGraphReadinessUntilProjectionLands(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorGCP)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorGCP)
	}
	if contract.CollectorKind != scope.CollectorGCP {
		t.Fatalf("CollectorKind = %q, want %q", contract.CollectorKind, scope.CollectorGCP)
	}
	if got := len(contract.CanonicalKeyspaces); got != 0 {
		t.Fatalf("GCP CanonicalKeyspaces = %#v, want empty until GCP graph projection is implemented", contract.CanonicalKeyspaces)
	}
	if got := len(contract.RequiredPhases); got != 0 {
		t.Fatalf("GCP RequiredPhases = %#v, want empty until GCP graph projection is implemented", contract.RequiredPhases)
	}
}

func TestCollectorContractForAzureHasNoOperationalGraphReadinessUntilProjectionLands(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorAzure)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorAzure)
	}
	if contract.CollectorKind != scope.CollectorAzure {
		t.Fatalf("CollectorKind = %q, want %q", contract.CollectorKind, scope.CollectorAzure)
	}
	if got := len(contract.CanonicalKeyspaces); got != 0 {
		t.Fatalf("Azure CanonicalKeyspaces = %#v, want empty until Azure graph projection is implemented", contract.CanonicalKeyspaces)
	}
	if got := len(contract.RequiredPhases); got != 0 {
		t.Fatalf("Azure RequiredPhases = %#v, want empty until Azure graph projection is implemented", contract.RequiredPhases)
	}
}

func TestCollectorContractForCICDRunHasNoOperationalGraphReadinessUntilProjectionLands(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorCICDRun)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorCICDRun)
	}
	if got := len(contract.CanonicalKeyspaces); got != 0 {
		t.Fatalf("CICDRun CanonicalKeyspaces = %#v, want empty until reducer projection is implemented", contract.CanonicalKeyspaces)
	}
	if got := len(contract.RequiredPhases); got != 0 {
		t.Fatalf("CICDRun RequiredPhases = %#v, want empty until reducer projection is implemented", contract.RequiredPhases)
	}
}

func TestRequiredPhasesForCollectorIncludesWebhookAnchorGate(t *testing.T) {
	t.Parallel()

	requirements := RequiredPhasesForCollector(scope.CollectorWebhook)
	want := []PhaseRequirement{
		{
			Keyspace:  reducer.GraphProjectionKeyspaceWebhookEventUID,
			PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
			Required:  true,
		},
		{
			Keyspace:  reducer.GraphProjectionKeyspaceWebhookEventUID,
			PhaseName: reducer.GraphProjectionPhaseCrossSourceAnchorReady,
			Required:  true,
		},
	}
	if !reflect.DeepEqual(requirements, want) {
		t.Fatalf("RequiredPhasesForCollector(webhook) = %#v, want %#v", requirements, want)
	}
}

func TestCollectorContractForWebhookIncludesAcceptedCanonicalKeyspaces(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorWebhook)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorWebhook)
	}
	want := []reducer.GraphProjectionKeyspace{
		reducer.GraphProjectionKeyspaceWebhookEventUID,
	}
	if !reflect.DeepEqual(contract.CanonicalKeyspaces, want) {
		t.Fatalf("CanonicalKeyspaces = %#v, want %#v", contract.CanonicalKeyspaces, want)
	}
}

func TestCollectorContractForDocumentationHasNoOperationalKeyspaces(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorDocumentation)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorDocumentation)
	}
	if contract.CollectorKind != scope.CollectorDocumentation {
		t.Fatalf("CollectorKind = %q, want %q", contract.CollectorKind, scope.CollectorDocumentation)
	}
	if len(contract.CanonicalKeyspaces) != 0 {
		t.Fatalf("CanonicalKeyspaces = %#v, want empty for schema-only documentation slice", contract.CanonicalKeyspaces)
	}
	if len(contract.RequiredPhases) != 0 {
		t.Fatalf("RequiredPhases = %#v, want empty for schema-only documentation slice", contract.RequiredPhases)
	}
}

func TestCollectorContractForOCIRegistryHasNoOperationalKeyspaces(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorOCIRegistry)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorOCIRegistry)
	}
	if contract.CollectorKind != scope.CollectorOCIRegistry {
		t.Fatalf("CollectorKind = %q, want %q", contract.CollectorKind, scope.CollectorOCIRegistry)
	}
	if len(contract.CanonicalKeyspaces) != 0 {
		t.Fatalf("CanonicalKeyspaces = %#v, want empty for fact-only OCI registry slice", contract.CanonicalKeyspaces)
	}
	if len(contract.RequiredPhases) != 0 {
		t.Fatalf("RequiredPhases = %#v, want empty for fact-only OCI registry slice", contract.RequiredPhases)
	}
}

func TestCollectorContractForPackageRegistryHasNoOperationalKeyspaces(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorPackageRegistry)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorPackageRegistry)
	}
	if contract.CollectorKind != scope.CollectorPackageRegistry {
		t.Fatalf("CollectorKind = %q, want %q", contract.CollectorKind, scope.CollectorPackageRegistry)
	}
	if len(contract.CanonicalKeyspaces) != 0 {
		t.Fatalf("CanonicalKeyspaces = %#v, want empty for fact-only package registry slice", contract.CanonicalKeyspaces)
	}
	if len(contract.RequiredPhases) != 0 {
		t.Fatalf("RequiredPhases = %#v, want empty for fact-only package registry slice", contract.RequiredPhases)
	}
}

func TestCollectorContractForVulnerabilityIntelligenceHasNoOperationalKeyspaces(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorVulnerabilityIntelligence)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorVulnerabilityIntelligence)
	}
	if contract.CollectorKind != scope.CollectorVulnerabilityIntelligence {
		t.Fatalf("CollectorKind = %q, want %q", contract.CollectorKind, scope.CollectorVulnerabilityIntelligence)
	}
	if len(contract.CanonicalKeyspaces) != 0 {
		t.Fatalf("CanonicalKeyspaces = %#v, want empty for fact-only vulnerability intelligence slice", contract.CanonicalKeyspaces)
	}
	if len(contract.RequiredPhases) != 0 {
		t.Fatalf("RequiredPhases = %#v, want empty for fact-only vulnerability intelligence slice", contract.RequiredPhases)
	}
}

func TestCollectorContractForScannerWorkerHasNoOperationalKeyspaces(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorScannerWorker)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorScannerWorker)
	}
	if contract.CollectorKind != scope.CollectorScannerWorker {
		t.Fatalf("CollectorKind = %q, want %q", contract.CollectorKind, scope.CollectorScannerWorker)
	}
	if len(contract.CanonicalKeyspaces) != 0 {
		t.Fatalf("CanonicalKeyspaces = %#v, want empty because scanner workers emit source facts only", contract.CanonicalKeyspaces)
	}
	if len(contract.RequiredPhases) != 0 {
		t.Fatalf("RequiredPhases = %#v, want empty because reducers own scanner-derived truth", contract.RequiredPhases)
	}
}

func TestCollectorContractForVaultLiveHasNoOperationalKeyspaces(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorVaultLive)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorVaultLive)
	}
	if contract.CollectorKind != scope.CollectorVaultLive {
		t.Fatalf("CollectorKind = %q, want %q", contract.CollectorKind, scope.CollectorVaultLive)
	}
	if len(contract.CanonicalKeyspaces) != 0 {
		t.Fatalf("CanonicalKeyspaces = %#v, want empty because Vault live emits source facts only", contract.CanonicalKeyspaces)
	}
	if len(contract.RequiredPhases) != 0 {
		t.Fatalf("RequiredPhases = %#v, want empty because reducers own secrets/IAM truth", contract.RequiredPhases)
	}
}

func TestCollectorContractForSecurityAlertHasNoOperationalKeyspaces(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorSecurityAlert)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorSecurityAlert)
	}
	if contract.CollectorKind != scope.CollectorSecurityAlert {
		t.Fatalf("CollectorKind = %q, want %q", contract.CollectorKind, scope.CollectorSecurityAlert)
	}
	if len(contract.CanonicalKeyspaces) != 0 {
		t.Fatalf("CanonicalKeyspaces = %#v, want empty because provider security alerts emit source facts only", contract.CanonicalKeyspaces)
	}
	if len(contract.RequiredPhases) != 0 {
		t.Fatalf("RequiredPhases = %#v, want empty because reducers own alert impact truth", contract.RequiredPhases)
	}
}

func TestCollectorContractForPagerDutyHasNoOperationalKeyspaces(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorPagerDuty)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorPagerDuty)
	}
	if contract.CollectorKind != scope.CollectorPagerDuty {
		t.Fatalf("CollectorKind = %q, want %q", contract.CollectorKind, scope.CollectorPagerDuty)
	}
	if len(contract.CanonicalKeyspaces) != 0 {
		t.Fatalf("CanonicalKeyspaces = %#v, want empty because PagerDuty emits incident evidence only", contract.CanonicalKeyspaces)
	}
	if len(contract.RequiredPhases) != 0 {
		t.Fatalf("RequiredPhases = %#v, want empty because reducers own incident context truth", contract.RequiredPhases)
	}
}

func TestCollectorContractForJiraHasNoOperationalKeyspaces(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorJira)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorJira)
	}
	if contract.CollectorKind != scope.CollectorJira {
		t.Fatalf("CollectorKind = %q, want %q", contract.CollectorKind, scope.CollectorJira)
	}
	if len(contract.CanonicalKeyspaces) != 0 {
		t.Fatalf("CanonicalKeyspaces = %#v, want empty because Jira emits source facts only", contract.CanonicalKeyspaces)
	}
	if len(contract.RequiredPhases) != 0 {
		t.Fatalf("RequiredPhases = %#v, want empty because reducers own work-item truth", contract.RequiredPhases)
	}
}

func TestCollectorContractForReturnsClonedSlices(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorGit)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorGit)
	}
	contract.CanonicalKeyspaces[0] = reducer.GraphProjectionKeyspaceCloudResourceUID
	contract.RequiredPhases[0] = PhaseRequirement{
		Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
		PhaseName: reducer.GraphProjectionPhaseSemanticNodesCommitted,
		Required:  true,
	}

	fresh, ok := CollectorContractFor(scope.CollectorGit)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) fresh found = false, want true", scope.CollectorGit)
	}
	if got, want := fresh.CanonicalKeyspaces[0], reducer.GraphProjectionKeyspaceCodeEntitiesUID; got != want {
		t.Fatalf("fresh CanonicalKeyspaces[0] = %q, want %q", got, want)
	}
	if got, want := fresh.RequiredPhases[0].PhaseName, reducer.GraphProjectionPhaseCanonicalNodesCommitted; got != want {
		t.Fatalf("fresh RequiredPhases[0].PhaseName = %q, want %q", got, want)
	}
}
