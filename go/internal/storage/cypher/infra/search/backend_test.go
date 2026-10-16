// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

// TestRuntimeGraphBackendsMatchSchemaBackends pins the conversion every
// writing binary performs, graph.SchemaBackend(runtimecfg.LoadGraphBackend(...)):
// each runtime backend value is a schema backend value, and it resolves to the
// dialect its backend needs. An external test, so the package itself never
// imports internal/runtime.
func TestRuntimeGraphBackendsMatchSchemaBackends(t *testing.T) {
	t.Parallel()

	for backend, want := range map[runtimecfg.GraphBackend]search.Dialect{
		runtimecfg.GraphBackendNeo4j:    search.On,
		runtimecfg.GraphBackendNornicDB: search.Off,
	} {
		got, err := search.DialectForBackend(graph.SchemaBackend(backend))
		if err != nil || got != want {
			t.Fatalf("DialectForBackend(%q) = %v, %v; want %v", backend, got, err, want)
		}
	}
}
