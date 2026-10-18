// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// edgeAbsentGraphExecutor reports every Repository node present (so the
// pre-write target guard passes and the write runs) but no
// CORRELATES_DEPLOYABLE_UNIT edge: the write "succeeded" and wrote nothing.
type edgeAbsentGraphExecutor struct{}

func (edgeAbsentGraphExecutor) Execute(context.Context, sourcecypher.Statement) error { return nil }

func (edgeAbsentGraphExecutor) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	return nil
}

func (edgeAbsentGraphExecutor) ExecuteProbe(_ context.Context, stmt sourcecypher.Statement) (bool, error) {
	return !strings.Contains(stmt.Cypher, "CORRELATES_DEPLOYABLE_UNIT"), nil
}

// TestBuildReducerServiceWiresDeployableUnitEdgeVerifier guards the #8283
// verified-success wiring at the production composition root: the
// deployable-unit handler must verify its admitted edges through the shared
// edge writer, so a write that left no edge fails with the counting
// unverified class instead of succeeding (and recording a fingerprint that
// keeps the item closed). Focused handler tests inject their own verifier, so
// only this test detects the wiring being dropped.
func TestBuildReducerServiceWiresDeployableUnitEdgeVerifier(t *testing.T) {
	t.Parallel()

	database := &deployableUnitTargetBoundDB{}
	service, err := buildReducerService(
		context.Background(), database, edgeAbsentGraphExecutor{}, stubCypherExecutor{},
		postgres.NewSharedIntentStore(database), stubCypherReader{}, stubCypherReader{},
		obligationConsumerDisabledGetenv(func(string) string { return "" }), nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	now := time.Now().UTC()
	_, execErr := service.Executor.Execute(context.Background(), reducer.Intent{
		IntentID:        "deployable-unit-edge-verify-wiring",
		Domain:          reducer.DomainDeployableUnitCorrelation,
		ScopeID:         "repository:test-scope",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		EntityKeys:      []string{"repo-edge-api"},
		RelatedScopeIDs: []string{"repository:deploy-repo"},
		CycleStartedAt:  now,
		EnqueuedAt:      now,
	})
	if !database.readDeployableUnitInputs {
		t.Fatalf("handler never read its resolved relationships (execErr=%v): the fixture did not reach the edge write", execErr)
	}
	var classed interface{ FailureClass() string }
	if !errors.As(execErr, &classed) || classed.FailureClass() != reducer.DeployableUnitEdgeWriteUnverifiedFailureClass {
		t.Fatalf("execution error = %v, want failure class %q from the wired edge verifier", execErr, reducer.DeployableUnitEdgeWriteUnverifiedFailureClass)
	}
}
