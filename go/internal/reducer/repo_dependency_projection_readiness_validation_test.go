// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

func TestRepoDependencyProjectionRunnerValidationRequiresWorkloadReadinessDependencies(t *testing.T) {
	t.Parallel()

	store := &fakeRepoDependencyIntentStore{leaseGranted: true}
	valid := RepoDependencyProjectionRunner{
		IntentReader:                    store,
		LeaseManager:                    store,
		AcceptanceUnitGate:              store,
		EdgeWriter:                      &recordingCodeCallProjectionEdgeWriter{},
		WorkloadMaterializationReplayer: &recordingWorkloadMaterializationReplayer{},
		WorkloadReadinessPrefetch:       readyRepoDependencyWorkloadPrefetch,
		AcceptedGen:                     acceptedGenerationFixed("", false),
	}

	tests := []struct {
		name string
		edit func(*RepoDependencyProjectionRunner)
		want string
	}{
		{
			name: "missing workload materialization replayer",
			edit: func(runner *RepoDependencyProjectionRunner) {
				runner.WorkloadMaterializationReplayer = nil
			},
			want: "workload materialization replayer is required",
		},
		{
			name: "workload materialization replayer lacks readiness fence support",
			edit: func(runner *RepoDependencyProjectionRunner) {
				runner.WorkloadMaterializationReplayer = legacyWorkloadMaterializationReplayer{}
			},
			want: "must support readiness fences",
		},
		{
			name: "missing workload readiness prefetch",
			edit: func(runner *RepoDependencyProjectionRunner) {
				runner.WorkloadReadinessPrefetch = nil
			},
			want: "workload readiness prefetch is required",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runner := valid
			test.edit(&runner)
			if err := runner.validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

type legacyWorkloadMaterializationReplayer struct{}

func (legacyWorkloadMaterializationReplayer) ReplayWorkloadMaterialization(
	context.Context,
	string,
	string,
	string,
) (bool, error) {
	return true, nil
}

func readyRepoDependencyWorkloadPrefetch(
	_ context.Context,
	_ []gpphase.PhaseKey,
	_ gpphase.Phase,
) (gpphase.ReadinessLookup, error) {
	return func(gpphase.PhaseKey, gpphase.Phase) (bool, bool) {
		return true, true
	}, nil
}
