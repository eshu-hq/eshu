// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestLiveBackendAggregateWiring pins the live-backend aggregate wiring:
// run-scoped download so a failed-job rerun reunites every attempt's
// reports, then newest-valid select before the exact-four verify (#8724).
func TestLiveBackendAggregateWiring(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "live-backend-tests.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed readinessWorkflow
	if err := yaml.Unmarshal(workflow, &parsed); err != nil {
		t.Fatal(err)
	}
	aggregate, ok := parsed.Jobs[backendAggregateJob]
	if !ok {
		t.Fatalf("aggregate job %q missing", backendAggregateJob)
	}
	if problem := checkBackendAggregate(aggregate); problem != "" {
		t.Fatal(problem)
	}
}
