// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

func TestServiceRunStartsRepoDependencyProjectionRunner(t *testing.T) {
	t.Parallel()

	store := &fakeRepoDependencyIntentStore{leaseGranted: false}

	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   &stubReducerWorkSource{},
		Executor:     &stubReducerExecutor{},
		WorkSink:     &stubReducerWorkSink{},
		RepoDependencyProjectionRunner: &RepoDependencyProjectionRunner{
			IntentReader:                    store,
			LeaseManager:                    store,
			AcceptanceUnitGate:              store,
			EdgeWriter:                      &recordingCodeCallProjectionEdgeWriter{},
			WorkloadMaterializationReplayer: &recordingWorkloadMaterializationReplayer{},
			WorkloadReadinessPrefetch:       readyRepoDependencyWorkloadPrefetch,
			AcceptedGen:                     acceptedGenerationFixed("", false),
			Config: RepoDependencyProjectionRunnerConfig{
				PollInterval: 10 * time.Millisecond,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	store.mu.Lock()
	claims := store.leaseClaims
	store.mu.Unlock()

	if claims == 0 {
		t.Fatal("expected repo dependency projection runner to attempt at least one lease claim")
	}
}
