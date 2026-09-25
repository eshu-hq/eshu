// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildReducerServiceWiresSupersededGenerationDrain guards #7121: the
// shared projection runner only drains superseded-generation intents when its
// IntentReader implements the optional SupersededGenerationReader port. The
// runner discovers the port by type assertion, so a wrapper or a different
// reader silently disables the drain; this asserts the production wiring keeps
// it.
func TestBuildReducerServiceWiresSupersededGenerationDrain(t *testing.T) {
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
		func(string) string { return "" },
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	if service.SharedProjectionRunner == nil {
		t.Fatal("SharedProjectionRunner = nil, want non-nil")
	}
	if _, ok := service.SharedProjectionRunner.IntentReader.(worker.SupersededGenerationReader); !ok {
		t.Fatalf("SharedProjectionRunner.IntentReader = %T, want a worker.SupersededGenerationReader", service.SharedProjectionRunner.IntentReader)
	}
}
