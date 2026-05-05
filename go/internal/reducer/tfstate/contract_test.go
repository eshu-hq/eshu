package tfstate

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestDefaultRuntimeContract(t *testing.T) {
	t.Parallel()

	contract := DefaultRuntimeContract()
	if err := contract.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	if got, want := len(contract.Checkpoints), 2; got != want {
		t.Fatalf("len(Checkpoints) = %d, want %d", got, want)
	}
	if got, want := contract.Checkpoints[0].Keyspace, reducer.GraphProjectionKeyspaceTerraformResourceUID; got != want {
		t.Fatalf("Checkpoints[0].Keyspace = %q, want %q", got, want)
	}
	if got, want := contract.Checkpoints[1].Keyspace, reducer.GraphProjectionKeyspaceTerraformModuleUID; got != want {
		t.Fatalf("Checkpoints[1].Keyspace = %q, want %q", got, want)
	}
}

func TestRuntimeContractTemplateReturnsClonedSlices(t *testing.T) {
	t.Parallel()

	contract := RuntimeContractTemplate()
	contract.Components[0] = "mutated"
	contract.Checkpoints[0] = PublishedCheckpoint{
		Keyspace: reducer.GraphProjectionKeyspaceServiceUID,
		Phase:    reducer.GraphProjectionPhaseDeploymentMapping,
	}

	fresh := RuntimeContractTemplate()
	if got, want := fresh.Components[0], "resource_projector"; got != want {
		t.Fatalf("fresh Components[0] = %q, want %q", got, want)
	}
	if got, want := fresh.Checkpoints[0].Keyspace, reducer.GraphProjectionKeyspaceTerraformResourceUID; got != want {
		t.Fatalf("fresh Checkpoints[0].Keyspace = %q, want %q", got, want)
	}
}
