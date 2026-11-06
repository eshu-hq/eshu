// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildReducerServiceWiresActiveGenerationRebind guards #10106: the
// runtime rebinds a superseded Crossplane SATISFIED_BY intent onto its
// scope's active generation only when ActiveGeneration is wired. A nil
// lookup silently restores the dropped-replay behavior, so the production
// executor chain must reach a Runtime carrying both generation checks.
func TestBuildReducerServiceWiresActiveGenerationRebind(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(
		context.Background(),
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		obligationConsumerDisabledGetenv(func(string) string { return "" }),
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	executor := service.Executor
	for {
		switch wrapped := executor.(type) {
		case *reducer.Runtime:
			if wrapped.GenerationCheck == nil {
				t.Fatal("Runtime.GenerationCheck = nil, want the Postgres freshness check")
			}
			if wrapped.ActiveGeneration == nil {
				t.Fatal("Runtime.ActiveGeneration = nil, want the Postgres active-generation lookup (#10106)")
			}
			return
		case *activeWorkerExecutor:
			executor = wrapped.inner
		default:
			t.Fatalf("service.Executor chain reaches %T, want *reducer.Runtime", executor)
		}
	}
}
