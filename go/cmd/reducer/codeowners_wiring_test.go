// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuildReducerServiceWiresCodeownersOwnershipEdgeWriter(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(string) string { return "" }, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	result, err := service.Executor.Execute(context.Background(), reducer.Intent{
		IntentID:     "codeowners-wiring",
		Domain:       reducer.DomainCodeownersOwnership,
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
	})
	if err != nil {
		t.Fatalf("codeowners ownership execution error = %v, want nil", err)
	}
	if result.Status != reducer.ResultStatusSucceeded {
		t.Fatalf("codeowners ownership execution status = %q, want %q", result.Status, reducer.ResultStatusSucceeded)
	}
}
