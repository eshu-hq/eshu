// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestOpenProjectorCanonicalWriterSourceWiresTerraformStateResolvers is the
// hermetic, unconditional counterpart of
// TestOpenIngesterCanonicalWriterSourceWiresTerraformStateResolvers
// (go/cmd/ingester) and
// TestOpenBootstrapCanonicalWriterSourceWiresTerraformStateResolvers
// (go/cmd/bootstrap-index) for the third #5443 canonical-writer call site.
// cmd/projector's openProjectorCanonicalWriter has no bolt-backed live test
// of its own asserting the writer it returns carries both resolvers, and
// like the other two binaries it unconditionally dials a live driver
// (runtimecfg.OpenNeo4jDriver), so it cannot be exercised as a pure unit
// test either. The golden-corpus-gate live run makes zero assertions about
// MATCHES_STATE or config_repo_id in
// testdata/golden/e2e-20repo-snapshot.json (see the equivalent comment on
// the ingester sibling test for why the corpus cannot exercise MATCHES_STATE
// truth as it stands today). Concretely: deleting the two
// .WithTerraformState*Resolver(...) calls from openProjectorCanonicalWriter
// (runtime_wiring.go) leaves go build, go test ./cmd/projector/...,
// golangci-lint, race-graph-writes, and golden-corpus-gate all green -- this
// test is the guard that actually catches that regression, unconditionally,
// without a live backend.
func TestOpenProjectorCanonicalWriterSourceWiresTerraformStateResolvers(t *testing.T) {
	t.Parallel()

	source := readOwnPackageSource(t, "runtime_wiring.go")
	for _, call := range []string{
		".WithTerraformStateOwnershipResolver(",
		".WithTerraformStateConfigMatchResolver(",
	} {
		if !strings.Contains(source, call) {
			t.Errorf("openProjectorCanonicalWriter source missing %s wiring call", call)
		}
	}
}

// readOwnPackageSource reads a file from the same directory as this test
// file, addressed relative to runtime.Caller(0) so it resolves regardless of
// the working directory `go test` is invoked from.
func readOwnPackageSource(t *testing.T, name string) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Join(filepath.Dir(filename), name)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(contents)
}
