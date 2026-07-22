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

// TestOpenBootstrapCanonicalWriterSourceWiresTerraformStateResolvers is the
// hermetic, unconditional sibling of
// TestOpenBootstrapCanonicalWriterWiresTerraformStateResolversLive (this
// package, wiring_tfstate_resolvers_live_test.go). That live test drives the
// real construction path and asserts the returned writer actually carries
// both resolvers, but it only runs when ESHU_CYPHER_BOLT_DSN is set -- and no
// CI workflow or specs/ci-gates.v1.yaml lane sets it, so every case in the
// live test file unconditionally t.Skip()s in CI today. The one CI gate that
// runs the real eshu-bootstrap-index against a live NornicDB,
// golden-corpus-gate, makes zero assertions about MATCHES_STATE or
// config_repo_id either -- see the equivalent comment on
// TestOpenIngesterCanonicalWriterSourceWiresTerraformStateResolvers
// (go/cmd/ingester) for why the golden corpus's terraformstate cassette
// cannot exercise MATCHES_STATE truth as it stands (module-qualified state
// addresses that the exact-address-equality design never matches against a
// bare config address, and no fixture backend matching the cassette's
// locator). Concretely: deleting the two .WithTerraformState*Resolver(...)
// calls from openBootstrapCanonicalWriter (wiring.go) leaves go build, go
// test ./cmd/bootstrap-index/..., golangci-lint, race-graph-writes, and
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
func TestOpenBootstrapCanonicalWriterSourceWiresTerraformStateResolvers(t *testing.T) {
	t.Parallel()

	source := readOwnPackageSource(t, "wiring.go")
	for _, call := range []string{
		".WithTerraformStateOwnershipResolver(",
		".WithTerraformStateConfigMatchResolver(",
		".WithKustomizeOverlayResolver(",
	} {
		if !strings.Contains(source, call) {
			t.Errorf("openBootstrapCanonicalWriter source missing %s wiring call", call)
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
