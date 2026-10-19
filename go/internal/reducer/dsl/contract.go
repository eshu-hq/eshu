// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dsl

import (
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// PublishedCheckpoint identifies one reducer readiness row the DSL-owned
// correlation layer is expected to publish once implemented.
type PublishedCheckpoint struct {
	Keyspace gpphase.Keyspace
	Phase    gpphase.Phase
}

// RuntimeContract captures the accepted DSL reducer scaffold.
type RuntimeContract struct {
	Components  []string
	Checkpoints []PublishedCheckpoint
}

var defaultRuntimeContract = RuntimeContract{
	Components: []string{
		"evaluator",
		"drift_evaluator",
		"deployment_mapping",
		"workload_materialization",
	},
	Checkpoints: []PublishedCheckpoint{
		{
			Keyspace: gpphase.KeyspaceTerraformResourceUID,
			Phase:    gpphase.PhaseCrossSourceAnchorReady,
		},
		{
			Keyspace: gpphase.KeyspaceCloudResourceUID,
			Phase:    gpphase.PhaseCrossSourceAnchorReady,
		},
		{
			Keyspace: gpphase.KeyspaceWebhookEventUID,
			Phase:    gpphase.PhaseCrossSourceAnchorReady,
		},
		{
			Keyspace: gpphase.KeyspaceServiceUID,
			Phase:    gpphase.PhaseDeploymentMapping,
		},
		{
			Keyspace: gpphase.KeyspaceServiceUID,
			Phase:    gpphase.PhaseWorkloadMaterialization,
		},
	},
}

// DefaultRuntimeContract returns the accepted DSL reducer scaffold.
func DefaultRuntimeContract() RuntimeContract {
	contract := defaultRuntimeContract
	contract.Components = slices.Clone(defaultRuntimeContract.Components)
	contract.Checkpoints = slices.Clone(defaultRuntimeContract.Checkpoints)
	return contract
}

// RuntimeContractTemplate returns a defensive copy of the accepted DSL reducer
// scaffold.
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
