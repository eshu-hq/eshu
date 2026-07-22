// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"strings"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestOpenIngesterCanonicalWriterWiresTerraformStateResolversLive drives the
// real, deployed cmd/ingester canonical-writer construction path against a
// live Bolt-speaking graph backend -- not a hand-built writer, not the
// isolated adapter types in isolation -- and proves both #5443 MATCHES_STATE
// resolvers are non-nil on the writer it returns. Opt-in, matching every
// other bolt-backed live test in this repository: set ESHU_CYPHER_BOLT_DSN
// to run it against a running graph backend. Skipped otherwise.
//
// This exists because the P1 re-review finding on this branch was that the
// ambiguity fix was correct but wired only into cmd/projector, a binary no
// Helm template deploys; cmd/ingester is the actual StatefulSet. A test that
// only asserted the writer *option* method sets the field, or that only
// exercised the adapter types directly (as
// TestOpenIngesterCanonicalWriterAcceptsNornicDBOnSharedBoltPath and the
// projector-side adapter unit/live tests already did), would have passed on
// the unwired binary too -- that is the exact false-green shape this finding
// flagged. Deleting the two .WithTerraformState*Resolver(...) calls in
// openIngesterCanonicalWriter (wiring_canonical_writer_open.go) makes this
// test fail: verified by temporarily reverting that chain locally, see the
// #5443 P1 fix commit message for the before/after run.
//
// runtimecfg.OpenNeo4jDriver (which this wiring calls) verifies connectivity
// eagerly (VerifyConnectivity), so this cannot run as a pure unit test with a
// fake URI -- it needs a reachable Bolt endpoint, exactly like
// TestProjectorTerraformStateConfigMatchResolverLive.
func TestOpenIngesterCanonicalWriterWiresTerraformStateResolversLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if dsn == "" {
		t.Skip("ESHU_CYPHER_BOLT_DSN not set; skipping bolt integration test")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE"))

	getenv := func(key string) string {
		switch key {
		case "ESHU_NEO4J_URI":
			return dsn
		case "ESHU_NEO4J_USERNAME":
			return "neo4j"
		case "ESHU_NEO4J_PASSWORD":
			return "test-password"
		case "ESHU_NEO4J_DATABASE":
			return database
		default:
			return ""
		}
	}

	writer, closer, err := openIngesterCanonicalWriter(context.Background(), postgres.SQLDB{}, getenv, nil, nil)
	if err != nil {
		t.Fatalf("openIngesterCanonicalWriter() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if closer != nil {
			_ = closer.Close()
		}
	})

	canonicalWriter, ok := writer.(*sourcecypher.CanonicalNodeWriter)
	if !ok {
		t.Fatalf("openIngesterCanonicalWriter() writer type = %T, want *sourcecypher.CanonicalNodeWriter", writer)
	}

	ownershipWired, configMatchWired := canonicalWriter.TerraformStateResolversConfigured()
	if !ownershipWired {
		t.Error("openIngesterCanonicalWriter() left the #5443 TerraformStateOwnershipResolver nil; " +
			"MATCHES_STATE edges will never be written by the deployed ingester")
	}
	if !configMatchWired {
		t.Error("openIngesterCanonicalWriter() left the #5443 TerraformStateConfigMatchResolver nil; " +
			"ambiguous MATCHES_STATE candidates will not fail closed on the deployed ingester")
	}
}
