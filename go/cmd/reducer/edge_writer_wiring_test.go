// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildReducerServiceWiresCodeownersAndSubmodulePinEdgeWriters guards
// against codex P1 finding on #5420/#5419: the production
// reducer.DefaultHandlers{...} literal in buildReducerService omitted
// CodeownersOwnershipEdgeWriter and SubmodulePinEdgeWriter, so both domain
// handlers' EdgeWriter nil guard tripped on every generation and dead-lettered
// every codeowners/submodule intent. That guard runs before the handler ever
// touches FactLoader, so exercising it through the actual production
// buildReducerService wiring (not a hand-populated DefaultHandlers literal)
// and asserting the returned error is not the nil-EdgeWriter sentinel proves
// the field is wired, without requiring the fake DB below to stub the
// fact_records query shape the handler would reach next.
func TestBuildReducerServiceWiresCodeownersAndSubmodulePinEdgeWriters(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(string) string { return "" }, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	for _, tc := range []struct {
		domain          reducer.Domain
		nilWriterErrMsg string
	}{
		{domain: reducer.DomainCodeownersOwnership, nilWriterErrMsg: "codeowners ownership materialization edge writer is required"},
		{domain: reducer.DomainSubmodulePin, nilWriterErrMsg: "submodule pin materialization edge writer is required"},
	} {
		t.Run(string(tc.domain), func(t *testing.T) {
			intent := reducer.Intent{
				IntentID:        "intent-" + string(tc.domain),
				ScopeID:         "scope-123",
				GenerationID:    "generation-456",
				SourceSystem:    "git",
				Domain:          tc.domain,
				Cause:           "shared follow-up",
				EntityKeys:      []string{"repo-a"},
				RelatedScopeIDs: []string{"scope-123"},
				EnqueuedAt:      time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
				AvailableAt:     time.Date(2026, time.April, 12, 12, 0, 0, 0, time.UTC),
				Status:          reducer.IntentStatusPending,
			}

			_, execErr := service.Executor.Execute(context.Background(), intent)
			if execErr != nil && strings.Contains(execErr.Error(), tc.nilWriterErrMsg) {
				t.Fatalf("Executor.Execute() error = %v, want an error other than the nil-EdgeWriter sentinel %q", execErr, tc.nilWriterErrMsg)
			}
		})
	}
}
