package tfstate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// PublishedCheckpoint identifies one reducer readiness row this package will
// own once the Terraform state projector family is implemented.
type PublishedCheckpoint struct {
	Keyspace reducer.GraphProjectionKeyspace
	Phase    reducer.GraphProjectionPhase
}

// RuntimeContract captures the accepted Terraform state reducer scaffold.
type RuntimeContract struct {
	Components  []string
	Checkpoints []PublishedCheckpoint
}

var defaultRuntimeContract = RuntimeContract{
	Components: []string{
		"resource_projector",
		"module_projector",
		"output_projector",
	},
	Checkpoints: []PublishedCheckpoint{
		{
			Keyspace: reducer.GraphProjectionKeyspaceTerraformResourceUID,
			Phase:    reducer.GraphProjectionPhaseCanonicalNodesCommitted,
		},
		{
			Keyspace: reducer.GraphProjectionKeyspaceTerraformModuleUID,
			Phase:    reducer.GraphProjectionPhaseCanonicalNodesCommitted,
		},
	},
}

// DefaultRuntimeContract returns the accepted Terraform state reducer scaffold.
func DefaultRuntimeContract() RuntimeContract {
	contract := defaultRuntimeContract
	contract.Components = slices.Clone(defaultRuntimeContract.Components)
	contract.Checkpoints = slices.Clone(defaultRuntimeContract.Checkpoints)
	return contract
}

// RuntimeContractTemplate returns a defensive copy of the accepted Terraform
// state reducer scaffold.
func RuntimeContractTemplate() RuntimeContract {
	return DefaultRuntimeContract()
}

// Validate checks that the scaffold does not contain blank ownership metadata.
func (c RuntimeContract) Validate() error {
	if len(c.Components) == 0 {
		return fmt.Errorf("components must not be empty")
	}
	if len(c.Checkpoints) == 0 {
		return fmt.Errorf("checkpoints must not be empty")
	}
	for _, component := range c.Components {
		if strings.TrimSpace(component) == "" {
			return fmt.Errorf("components must not contain blank values")
		}
	}
	for _, checkpoint := range c.Checkpoints {
		if strings.TrimSpace(string(checkpoint.Keyspace)) == "" {
			return fmt.Errorf("keyspace must not be blank")
		}
		if strings.TrimSpace(string(checkpoint.Phase)) == "" {
			return fmt.Errorf("phase must not be blank")
		}
	}
	return nil
}
