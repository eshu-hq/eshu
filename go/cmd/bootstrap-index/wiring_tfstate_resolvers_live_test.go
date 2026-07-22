// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"strings"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestOpenBootstrapCanonicalWriterWiresTerraformStateResolversLive drives the
// real, deployed cmd/bootstrap-index canonical-writer construction path
// against a live Bolt-speaking graph backend and proves both #5443
// MATCHES_STATE resolvers are non-nil on the writer it returns. Opt-in,
// matching every other bolt-backed live test in this repository: set
// ESHU_CYPHER_BOLT_DSN to run it against a running graph backend. Skipped
// otherwise.
//
// bootstrap-index is the one-shot initial index; a materialization it
// produces from a full (non-delta) sync is not skipped by the P0-1
// DeltaProjection guard, so an unwired resolver here would leave the very
// first index with zero MATCHES_STATE coverage until a later ingester cycle
// -- which is not guaranteed for a local/Compose deployment that only ever
// runs bootstrap-index. See the #5443 P1 re-review fix commit for the
// equivalent cmd/ingester regression this class of bug caused.
//
// runtimecfg.OpenNeo4jDriver (which this wiring calls) verifies connectivity
// eagerly, so this cannot run as a pure unit test with a fake URI -- it needs
// a reachable Bolt endpoint, exactly like
// TestProjectorTerraformStateConfigMatchResolverLive and
// TestOpenIngesterCanonicalWriterWiresTerraformStateResolversLive.
func TestOpenBootstrapCanonicalWriterWiresTerraformStateResolversLive(t *testing.T) {
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

	writer, closer, err := openBootstrapCanonicalWriter(context.Background(), &fakeBootstrapDB{}, getenv, nil, nil)
	if err != nil {
		t.Fatalf("openBootstrapCanonicalWriter() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		if closer != nil {
			_ = closer.Close()
		}
	})

	canonicalWriter, ok := writer.(*sourcecypher.CanonicalNodeWriter)
	if !ok {
		t.Fatalf("openBootstrapCanonicalWriter() writer type = %T, want *sourcecypher.CanonicalNodeWriter", writer)
	}

	ownershipWired, configMatchWired := canonicalWriter.TerraformStateResolversConfigured()
	if !ownershipWired {
		t.Error("openBootstrapCanonicalWriter() left the #5443 TerraformStateOwnershipResolver nil; " +
			"MATCHES_STATE edges will never be written by the initial bootstrap index")
	}
	if !configMatchWired {
		t.Error("openBootstrapCanonicalWriter() left the #5443 TerraformStateConfigMatchResolver nil; " +
			"ambiguous MATCHES_STATE candidates will not fail closed on the initial bootstrap index")
	}
}
