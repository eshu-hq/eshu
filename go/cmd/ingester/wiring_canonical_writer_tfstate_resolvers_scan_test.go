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

// TestOpenIngesterCanonicalWriterSourceWiresTerraformStateResolvers is the
// hermetic, unconditional sibling of
// TestOpenIngesterCanonicalWriterWiresTerraformStateResolversLive (this
// package, wiring_canonical_writer_tfstate_resolvers_live_test.go). That live
// test is the strongest possible check -- it drives the real construction
// path and asserts the writer it returns actually carries both resolvers --
// but it only runs when ESHU_CYPHER_BOLT_DSN is set. No CI workflow or
// specs/ci-gates.v1.yaml lane sets that variable: the race-graph-writes gate
// executes this test file, but every case in it unconditionally t.Skip()s
// without it. The one CI gate that does run the real eshu-ingester against a
// live NornicDB, golden-corpus-gate, makes zero assertions about
// MATCHES_STATE or config_repo_id in testdata/golden/e2e-20repo-snapshot.json
// either -- the golden corpus's terraformstate cassette
// (testdata/cassettes/terraformstate/supply-chain-demo.json) only carries
// module-qualified state addresses (module.ecs.aws_ecs_cluster.*,
// module.ecs.aws_ecs_service.*, module.ecs.aws_instance.*), and
// tfstate_state_match_edge.go's exact-address-equality design documents that
// a module-qualified state address never matches a bare config address by
// design; no fixture in the corpus declares a Terraform backend matching the
// cassette's locator either, so MATCHES_STATE ownership never resolves in
// that live gate today. Concretely: deleting the two
// .WithTerraformState*Resolver(...) calls from openIngesterCanonicalWriter
// (wiring_canonical_writer_open.go) leaves go build, go test
// ./cmd/ingester/..., golangci-lint, race-graph-writes, and
// golden-corpus-gate all green -- this test is the guard that actually
// catches that regression, unconditionally, without a live backend.
//
// The same class of gap applies to .WithKustomizeOverlayResolver(...) (issue
// #5445 slice 3): deleting that call leaves the resolver nil in production,
// so kustomizeExtendsBaseEdgeStatements fails closed on every write -- the
// EXTENDS_BASE edge is never wrong, but it is never MATERIALIZED either,
// which is the exact #5443-class dead-feature regression (a feature built
// and unit-tested but wired into only one binary, or none). This test checks
// the same source-string presence for it, unconditionally.
func TestOpenIngesterCanonicalWriterSourceWiresTerraformStateResolvers(t *testing.T) {
	t.Parallel()

	source := readOwnPackageSource(t, "wiring_canonical_writer_open.go")
	for _, call := range []string{
		".WithTerraformStateOwnershipResolver(",
		".WithTerraformStateConfigMatchResolver(",
		".WithKustomizeOverlayResolver(",
	} {
		if !strings.Contains(source, call) {
			t.Errorf("openIngesterCanonicalWriter source missing %s wiring call", call)
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
