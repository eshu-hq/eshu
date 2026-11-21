// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildReducerServiceWiresTheWinnersContract pins the #11117 wiring: the
// keyed winners maintainer requires the contract store (Run refuses to start
// without it), so a reducer built without one would fail at startup.
func TestBuildReducerServiceWiresTheWinnersContract(t *testing.T) {
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
	m := service.SupplyChainImpactWinnersMaintainer
	if m == nil || m.KeyedRebuilder == nil {
		t.Fatal("buildReducerService() winners maintainer or its keyed rebuilder = nil, want both wired")
	}
	if m.Contract == nil {
		t.Fatal("buildReducerService() winners maintainer Contract = nil, want the winners contract store")
	}
}
