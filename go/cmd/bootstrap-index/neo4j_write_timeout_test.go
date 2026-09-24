// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// TestBootstrapCanonicalTransactionTimeoutAppliesConfiguredTimeoutToBothBackends pins the server-side
// transaction timeout contract for ESHU_CANONICAL_WRITE_TIMEOUT: NornicDB keeps
// its 30s default when the variable is unset, while Neo4j applies the timeout
// only when the operator configures a valid positive duration, so an unset
// Neo4j deployment keeps its unbounded transactions.
func TestBootstrapCanonicalTransactionTimeoutAppliesConfiguredTimeoutToBothBackends(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend runtimecfg.GraphBackend
		raw     string
		want    time.Duration
	}{
		{name: "neo4j configured", backend: runtimecfg.GraphBackendNeo4j, raw: "300s", want: 300 * time.Second},
		{name: "neo4j unset", backend: runtimecfg.GraphBackendNeo4j, raw: "", want: 0},
		{name: "neo4j invalid", backend: runtimecfg.GraphBackendNeo4j, raw: "soon", want: 0},
		{name: "neo4j non-positive", backend: runtimecfg.GraphBackendNeo4j, raw: "-1s", want: 0},
		{name: "nornicdb configured", backend: runtimecfg.GraphBackendNornicDB, raw: "3s", want: 3 * time.Second},
		{name: "nornicdb unset keeps default", backend: runtimecfg.GraphBackendNornicDB, raw: "", want: 30 * time.Second},
		{name: "nornicdb invalid keeps default", backend: runtimecfg.GraphBackendNornicDB, raw: "soon", want: 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(key string) string {
				if key == "ESHU_CANONICAL_WRITE_TIMEOUT" {
					return tt.raw
				}
				return ""
			}
			if got := bootstrapCanonicalTransactionTimeout(tt.backend, getenv); got != tt.want {
				t.Fatalf("bootstrapCanonicalTransactionTimeout(%s, %q) = %s, want %s", tt.backend, tt.raw, got, tt.want)
			}
		})
	}
}
