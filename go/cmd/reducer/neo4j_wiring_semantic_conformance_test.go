// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestSemanticModuleConformanceCasesUseTheReducerWiring pins the writer
// construction backendconformance mirrors to the reducer's own
// semanticEntityWriterForGraphBackend. The live conformance cases for the
// semantic :Module write (#6965 Phase 4) are only meaningful if they run the
// statements the reducer really sends each backend; if the wiring changes
// (a new write mode, a different retract mode) this fails until the mirror
// follows.
func TestSemanticModuleConformanceCasesUseTheReducerWiring(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		backend      runtimecfg.GraphBackend
		conformance  backendconformance.BackendID
		neo4jBatchSz int
	}{
		{runtimecfg.GraphBackendNornicDB, backendconformance.BackendNornicDB, 100},
		{runtimecfg.GraphBackendNeo4j, backendconformance.BackendNeo4j, 100},
	} {
		t.Run(string(tc.backend), func(t *testing.T) {
			t.Parallel()

			production := func(executor sourcecypher.Executor) *sourcecypher.SemanticEntityWriter {
				writer, err := semanticEntityWriterForGraphBackend(executor, tc.neo4jBatchSz, tc.backend, func(string) string { return "" })
				if err != nil {
					t.Fatalf("semanticEntityWriterForGraphBackend() error = %v", err)
				}
				return writer
			}
			mirror, err := backendconformance.SemanticEntityWriterFor(tc.conformance)
			if err != nil {
				t.Fatalf("SemanticEntityWriterFor() error = %v", err)
			}

			want, err := backendconformance.SemanticModuleWriteCases(production)
			if err != nil {
				t.Fatalf("SemanticModuleWriteCases(production) error = %v", err)
			}
			got, err := backendconformance.SemanticModuleWriteCases(mirror)
			if err != nil {
				t.Fatalf("SemanticModuleWriteCases(mirror) error = %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("conformance semantic Module cases differ from the reducer wiring\n got: %#v\nwant: %#v", got, want)
			}
		})
	}
}
