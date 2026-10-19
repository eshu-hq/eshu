// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !ifafaultinjection

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type ifaLeaseOffGraphExecutor struct{}

func (ifaLeaseOffGraphExecutor) Execute(context.Context, sourcecypher.Statement) error { return nil }

type ifaLeaseOffReducerExecutor struct{}

func (ifaLeaseOffReducerExecutor) Execute(context.Context, reducercontract.Intent) (reducer.Result, error) {
	return reducer.Result{}, nil
}

// TestIfaLeaseExpiryBarrierOffNeverReadsEnv proves the default binary does not
// inspect the fault-only barrier setting or change either executor identity.
func TestIfaLeaseExpiryBarrierOffNeverReadsEnv(t *testing.T) {
	barrier, err := newIfaLeaseExpiryBarrier(func(key string) string {
		t.Fatalf("untagged barrier read environment key %q", key)
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	graph := ifaLeaseOffGraphExecutor{}
	if got := barrier.wrapGraph(graph); got != sourcecypher.Executor(graph) {
		t.Fatalf("graph changed in untagged build: %T", got)
	}
	runtime := ifaLeaseOffReducerExecutor{}
	if got := barrier.wrapReducer(runtime); got != reducer.Executor(runtime) {
		t.Fatalf("reducer changed in untagged build: %T", got)
	}
}
