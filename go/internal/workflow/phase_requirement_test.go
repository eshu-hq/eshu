// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestPhaseRequirementValidate guards the #4459 DeadLetterDomain validation
// (Copilot review finding on PR #4518): a blank DeadLetterDomain is the valid
// "not bridged yet" sentinel, a known reducer.Domain is valid, but an
// unknown/misspelled domain string must be rejected — it now participates in
// completeness blocking (ReconcileRunProgress), so an invalid value would
// otherwise silently pass through the readiness/completeness pipeline and
// never match a real fact_work_items.domain value.
func TestPhaseRequirementValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		requirement PhaseRequirement
		wantErr     bool
	}{
		{
			name: "blank DeadLetterDomain is valid (not bridged)",
			requirement: PhaseRequirement{
				Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
				PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				Required:  true,
			},
			wantErr: false,
		},
		{
			name: "known DeadLetterDomain is valid",
			requirement: PhaseRequirement{
				Keyspace:         reducer.GraphProjectionKeyspaceServiceUID,
				PhaseName:        reducer.GraphProjectionPhaseDeploymentMapping,
				Required:         true,
				DeadLetterDomain: reducer.DomainDeploymentMapping,
			},
			wantErr: false,
		},
		{
			name: "unknown DeadLetterDomain is rejected",
			requirement: PhaseRequirement{
				Keyspace:         reducer.GraphProjectionKeyspaceServiceUID,
				PhaseName:        reducer.GraphProjectionPhaseWorkloadMaterialization,
				Required:         true,
				DeadLetterDomain: reducer.Domain("not_a_real_reducer_domain"),
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.requirement.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want non-nil for an invalid DeadLetterDomain")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}
