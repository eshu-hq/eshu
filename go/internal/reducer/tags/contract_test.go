package tags

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
	if got, want := contract.CanonicalKeyspaces[0], reducer.GraphProjectionKeyspaceCloudResourceUID; got != want {
		t.Fatalf("CanonicalKeyspaces[0] = %q, want %q", got, want)
	}
}

func TestRuntimeContractTemplateReturnsClonedSlices(t *testing.T) {
	t.Parallel()

	contract := RuntimeContractTemplate()
	contract.Components[0] = "mutated"
	contract.CanonicalKeyspaces[0] = reducer.GraphProjectionKeyspaceServiceUID

	fresh := RuntimeContractTemplate()
	if got, want := fresh.Components[0], "normalizer"; got != want {
		t.Fatalf("fresh Components[0] = %q, want %q", got, want)
	}
	if got, want := fresh.CanonicalKeyspaces[0], reducer.GraphProjectionKeyspaceCloudResourceUID; got != want {
		t.Fatalf("fresh CanonicalKeyspaces[0] = %q, want %q", got, want)
	}
}
