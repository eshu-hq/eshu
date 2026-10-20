// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// scriptedReducerGenerationCheck answers factory-writer freshness probes
// from a script so wiring tests can flip the answer between groups. When
// the script runs out, the last answer repeats.
type scriptedReducerGenerationCheck struct {
	fresh []bool
	calls int
}

func (s *scriptedReducerGenerationCheck) check(
	_ context.Context,
	_, _ string,
) (bool, error) {
	index := s.calls
	if index >= len(s.fresh) {
		index = len(s.fresh) - 1
	}
	s.calls++
	return s.fresh[index], nil
}

func stampedReducerSemanticWrite() semantic.EntityWrite {
	return semantic.EntityWrite{
		RepoIDs:      []string{"repo-1"},
		Rows:         []semantic.EntityRow{semanticFunctionRow("function-go-1")},
		ScopeID:      "scope-1",
		GenerationID: "generation-2",
	}
}

// The factory threads the freshness check into both backend branches: a
// stale generation aborts before any statement dispatches on either route,
// with the sentinel.
func TestSemanticEntityWriterForGraphBackendFencesStaleWrite(t *testing.T) {
	t.Parallel()

	for _, backend := range []runtimecfg.GraphBackend{
		runtimecfg.GraphBackendNeo4j,
		runtimecfg.GraphBackendNornicDB,
	} {
		backend := backend
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()

			exec := &routeRecordingReducerExecutor{}
			check := &scriptedReducerGenerationCheck{fresh: []bool{false}}
			writer, err := semanticEntityWriterForGraphBackend(
				exec, 100, backend, func(string) string { return "" }, check.check,
			)
			if err != nil {
				t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
			}
			_, err = writer.WriteSemanticEntities(context.Background(), stampedReducerSemanticWrite())
			if !errors.Is(err, semantic.ErrGenerationSuperseded) {
				t.Fatalf("WriteSemanticEntities() error = %v, want ErrGenerationSuperseded", err)
			}
			if len(exec.executeCyphers) != 0 || len(exec.groupCyphers) != 0 {
				t.Fatalf(
					"dispatched %d autocommit + %d grouped statements, want 0 for a fenced write",
					len(exec.executeCyphers), len(exec.groupCyphers),
				)
			}
		})
	}
}

// A current generation still writes through the factory fence on both
// branches: the Neo4j branch and the NornicDB canonical-node branch both
// dispatch the retract+upsert groups.
func TestSemanticEntityWriterForGraphBackendFreshWriteProceeds(t *testing.T) {
	t.Parallel()

	for _, backend := range []runtimecfg.GraphBackend{
		runtimecfg.GraphBackendNeo4j,
		runtimecfg.GraphBackendNornicDB,
	} {
		backend := backend
		t.Run(string(backend), func(t *testing.T) {
			t.Parallel()

			exec := &routeRecordingReducerExecutor{}
			check := &scriptedReducerGenerationCheck{fresh: []bool{true}}
			writer, err := semanticEntityWriterForGraphBackend(
				exec, 100, backend, func(string) string { return "" }, check.check,
			)
			if err != nil {
				t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
			}
			result, err := writer.WriteSemanticEntities(context.Background(), stampedReducerSemanticWrite())
			if err != nil {
				t.Fatalf("WriteSemanticEntities() error = %v", err)
			}
			if result.CanonicalWrites != 1 {
				t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
			}
			if len(exec.groupCyphers) == 0 {
				t.Fatalf("no grouped statements dispatched for a fresh write")
			}
			if check.calls == 0 {
				t.Fatalf("freshness check never probed for a fenced write")
			}
		})
	}
}
