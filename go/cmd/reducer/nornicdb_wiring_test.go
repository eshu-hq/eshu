// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type drainAwareReducerDB struct {
	fakeReducerDB
	reducerGraphWork bool
}

func (f *drainAwareReducerDB) QueryContext(ctx context.Context, query string, args ...any) (postgres.Rows, error) {
	if strings.Contains(query, "active_fact_work_items AS (") {
		return &fakeExistsRows{value: f.reducerGraphWork}, nil
	}
	return f.fakeReducerDB.QueryContext(ctx, query, args...)
}

func TestBuildReducerServiceWiresNornicDBProjectorDrainGate(t *testing.T) {
	t.Parallel()

	db := &drainAwareReducerDB{reducerGraphWork: true}
	service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(name string) string {
		switch name {
		case "ESHU_GRAPH_BACKEND":
			return string(runtimecfg.GraphBackendNornicDB)
		case queryProfileEnv:
			return string(query.ProfileLocalAuthoritative)
		default:
			return ""
		}
	}, nil, nil, slog.Default(), nil)
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
	active, err := service.CodeCallProjectionRunner.ReducerGraphDrain.HasActiveReducerGraphWork(context.Background())
	if err != nil {
		t.Fatalf("ReducerGraphDrain.HasActiveReducerGraphWork() error = %v, want nil", err)
	}
	if !active {
		t.Fatal("ReducerGraphDrain.HasActiveReducerGraphWork() = false, want injected live query result")
	}
}

func TestBuildReducerServiceLeavesProjectorDrainDisabledByDefault(t *testing.T) {
	t.Parallel()

	db := &drainAwareReducerDB{reducerGraphWork: true}
	service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(name string) string {
		if name == "ESHU_GRAPH_BACKEND" {
			return string(runtimecfg.GraphBackendNornicDB)
		}
		return ""
	}, nil, nil, slog.Default(), nil)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}
	if service.CodeCallProjectionRunner.ReducerGraphDrain != nil {
		t.Fatal("CodeCallProjectionRunner.ReducerGraphDrain != nil with default profile, want disabled drain")
	}
}

func TestBuildReducerServiceWiresExpectedSourceLocalProjectors(t *testing.T) {
	t.Parallel()

	db := &fakeReducerDB{}
	service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(name string) string {
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
	}, nil, nil, slog.Default(), nil)
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
	service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{}, postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{}, func(name string) string {
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
	}, nil, nil, slog.Default(), nil)
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
