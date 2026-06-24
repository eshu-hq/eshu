// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuildReducerServiceWiresNornicDBProjectorDrainGate(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(name string) string {
		switch name {
		case "ESHU_GRAPH_BACKEND":
			return string(runtimecfg.GraphBackendNornicDB)
		case queryProfileEnv:
			return string(query.ProfileLocalAuthoritative)
		default:
			return ""
		}
	}, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	queue, ok := service.WorkSource.(postgres.ReducerQueue)
	if !ok {
		t.Fatalf("WorkSource type = %T, want postgres.ReducerQueue", service.WorkSource)
	}
	if !queue.RequireProjectorDrainBeforeClaim {
		t.Fatal("RequireProjectorDrainBeforeClaim = false, want true")
	}
	if service.CodeCallProjectionRunner.ReducerGraphDrain == nil {
		t.Fatal("CodeCallProjectionRunner.ReducerGraphDrain = nil, want local-authoritative drain")
	}
}

func TestBuildReducerServiceWiresExpectedSourceLocalProjectors(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(name string) string {
		switch name {
		case "ESHU_GRAPH_BACKEND":
			return string(runtimecfg.GraphBackendNornicDB)
		case queryProfileEnv:
			return string(query.ProfileLocalAuthoritative)
		case reducerExpectedSourceLocalProjectorsEnv:
			return "878"
		default:
			return ""
		}
	}, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	queue, ok := service.WorkSource.(postgres.ReducerQueue)
	if !ok {
		t.Fatalf("WorkSource type = %T, want postgres.ReducerQueue", service.WorkSource)
	}
	if got, want := queue.ExpectedSourceLocalProjectors, 878; got != want {
		t.Fatalf("ExpectedSourceLocalProjectors = %d, want %d", got, want)
	}
}

func TestBuildReducerServiceWiresSemanticEntityClaimLimit(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(name string) string {
		switch name {
		case "ESHU_GRAPH_BACKEND":
			return string(runtimecfg.GraphBackendNornicDB)
		case queryProfileEnv:
			return string(query.ProfileLocalAuthoritative)
		case reducerSemanticEntityClaimLimitEnv:
			return "4"
		default:
			return ""
		}
	}, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	queue, ok := service.WorkSource.(postgres.ReducerQueue)
	if !ok {
		t.Fatalf("WorkSource type = %T, want postgres.ReducerQueue", service.WorkSource)
	}
	if got, want := queue.SemanticEntityClaimLimit, 4; got != want {
		t.Fatalf("SemanticEntityClaimLimit = %d, want %d", got, want)
	}
}
