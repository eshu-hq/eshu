package workflow

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
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

func TestRequiredPhasesForCollectorIncludesTerraformStateAnchorGate(t *testing.T) {
	t.Parallel()

	requirements := RequiredPhasesForCollector(scope.CollectorTerraformState)
	want := []PhaseRequirement{
		{
			Keyspace:  reducer.GraphProjectionKeyspaceTerraformResourceUID,
			PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
			Required:  true,
		},
		{
			Keyspace:  reducer.GraphProjectionKeyspaceTerraformModuleUID,
			PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
			Required:  true,
		},
		{
			Keyspace:  reducer.GraphProjectionKeyspaceTerraformResourceUID,
			PhaseName: reducer.GraphProjectionPhaseCrossSourceAnchorReady,
			Required:  true,
		},
	}
	if !reflect.DeepEqual(requirements, want) {
		t.Fatalf("RequiredPhasesForCollector(terraform_state) = %#v, want %#v", requirements, want)
	}
}

func TestRequiredPhasesForCollectorIncludesAWSAnchorGate(t *testing.T) {
	t.Parallel()

	requirements := RequiredPhasesForCollector(scope.CollectorAWS)
	want := []PhaseRequirement{
		{
			Keyspace:  reducer.GraphProjectionKeyspaceCloudResourceUID,
			PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
			Required:  true,
		},
		{
			Keyspace:  reducer.GraphProjectionKeyspaceCloudResourceUID,
			PhaseName: reducer.GraphProjectionPhaseCrossSourceAnchorReady,
			Required:  true,
		},
	}
	if !reflect.DeepEqual(requirements, want) {
		t.Fatalf("RequiredPhasesForCollector(aws) = %#v, want %#v", requirements, want)
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

func TestCollectorContractForReturnsClonedSlices(t *testing.T) {
	t.Parallel()

	contract, ok := CollectorContractFor(scope.CollectorAWS)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) found = false, want true", scope.CollectorAWS)
	}
	contract.CanonicalKeyspaces[0] = reducer.GraphProjectionKeyspaceCodeEntitiesUID
	contract.RequiredPhases[0] = PhaseRequirement{
		Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
		PhaseName: reducer.GraphProjectionPhaseSemanticNodesCommitted,
		Required:  true,
	}

	fresh, ok := CollectorContractFor(scope.CollectorAWS)
	if !ok {
		t.Fatalf("CollectorContractFor(%q) fresh found = false, want true", scope.CollectorAWS)
	}
	if got, want := fresh.CanonicalKeyspaces[0], reducer.GraphProjectionKeyspaceCloudResourceUID; got != want {
		t.Fatalf("fresh CanonicalKeyspaces[0] = %q, want %q", got, want)
	}
	if got, want := fresh.RequiredPhases[0].PhaseName, reducer.GraphProjectionPhaseCanonicalNodesCommitted; got != want {
		t.Fatalf("fresh RequiredPhases[0].PhaseName = %q, want %q", got, want)
	}
}
