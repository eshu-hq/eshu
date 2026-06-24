// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestLoadProjectorRetryInjectorBuildsInjectorFromEnv(t *testing.T) {
	t.Parallel()

	injector, err := loadProjectorRetryInjector(func(name string) string {
		if name == "ESHU_PROJECTOR_RETRY_ONCE_SCOPE_GENERATION" {
			return "scope-123:generation-456"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("loadProjectorRetryInjector() error = %v, want nil", err)
	}
	if injector == nil {
		t.Fatal("loadProjectorRetryInjector() = nil, want injector")
	}
}

func TestLoadProjectorRetryInjectorReturnsNilWhenUnset(t *testing.T) {
	t.Parallel()

	injector, err := loadProjectorRetryInjector(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadProjectorRetryInjector() error = %v, want nil", err)
	}
	if injector != nil {
		t.Fatalf("loadProjectorRetryInjector() = %T, want nil", injector)
	}
}

func TestLoadProjectorRetryPolicyReadsSharedRetryConfig(t *testing.T) {
	t.Parallel()

	cfg, err := loadProjectorRetryPolicy(func(name string) string {
		switch name {
		case "ESHU_PROJECTOR_MAX_ATTEMPTS":
			return "4"
		case "ESHU_PROJECTOR_RETRY_DELAY":
			return "42s"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("loadProjectorRetryPolicy() error = %v, want nil", err)
	}
	if got, want := cfg.MaxAttempts, 4; got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
	if got, want := cfg.RetryDelay.Seconds(), 42.0; got != want {
		t.Fatalf("RetryDelay = %v, want 42s", cfg.RetryDelay)
	}
}

func TestProjectorCanonicalExecutorRetriesNornicDBMergeUniqueConflict(t *testing.T) {
	t.Parallel()

	raw := &projectorRetryRecordingExecutor{
		failures: 1,
		err: errors.New(
			"Neo4jError: Neo.ClientError.Transaction.TransactionCommitFailed " +
				"(commit failed: constraint violation: Constraint violation " +
				"(UNIQUE on Package.[uid]): Node with uid=npm://registry.npmjs.org/@angular/core already exists)",
		),
	}
	executor := projectorCanonicalExecutorForGraphBackend(
		raw,
		runtimecfg.GraphBackendNornicDB,
		func(string) string { return "" },
		nil,
		nil,
	)
	group, ok := executor.(sourcecypher.GroupExecutor)
	if !ok {
		t.Fatal("projector canonical executor does not expose grouped writes")
	}

	err := group.ExecuteGroup(context.Background(), []sourcecypher.Statement{{
		Operation: sourcecypher.OperationCanonicalUpsert,
		Cypher:    "UNWIND $rows AS row MERGE (p:Package:PackageRegistryPackage {uid: row.uid})",
		Parameters: map[string]any{
			"rows": []map[string]any{{"uid": "npm://registry.npmjs.org/@angular/core"}},
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteGroup() error = %v, want nil after retry", err)
	}
	if got, want := raw.groupCalls, 2; got != want {
		t.Fatalf("raw ExecuteGroup calls = %d, want %d", got, want)
	}
}

func TestProjectorCanonicalExecutorWrapsNornicDBWithTimeoutHint(t *testing.T) {
	t.Parallel()

	executor := projectorCanonicalExecutorForGraphBackend(
		&projectorRetryRecordingExecutor{},
		runtimecfg.GraphBackendNornicDB,
		func(name string) string {
			if name == canonicalWriteTimeoutEnv {
				return "3s"
			}
			return ""
		},
		nil,
		nil,
	)
	timeout, ok := executor.(sourcecypher.TimeoutExecutor)
	if !ok {
		t.Fatalf("projector canonical executor type = %T, want TimeoutExecutor", executor)
	}
	if got, want := timeout.Timeout.String(), "3s"; got != want {
		t.Fatalf("timeout = %s, want %s", got, want)
	}
	if got, want := timeout.TimeoutHint, canonicalWriteTimeoutEnv; got != want {
		t.Fatalf("timeout hint = %q, want %q", got, want)
	}
}

func TestProjectorCanonicalExecutorKeepsNeo4jGroupedWithoutNornicDBTimeout(t *testing.T) {
	t.Parallel()

	executor := projectorCanonicalExecutorForGraphBackend(
		&projectorRetryRecordingExecutor{},
		runtimecfg.GraphBackendNeo4j,
		func(string) string { return "" },
		nil,
		nil,
	)
	if _, ok := executor.(sourcecypher.TimeoutExecutor); ok {
		t.Fatal("Neo4j projector executor unexpectedly uses NornicDB timeout wrapper")
	}
	if _, ok := executor.(sourcecypher.GroupExecutor); !ok {
		t.Fatal("Neo4j projector executor does not expose grouped writes")
	}
}

type projectorRetryRecordingExecutor struct {
	failures   int
	err        error
	groupCalls int
}

func (e *projectorRetryRecordingExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (e *projectorRetryRecordingExecutor) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	e.groupCalls++
	if e.failures <= 0 {
		return nil
	}
	e.failures--
	return e.err
}
