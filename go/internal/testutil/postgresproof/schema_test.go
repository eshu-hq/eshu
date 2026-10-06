// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"net/url"
	"strings"
	"testing"
)

func TestDeferredPartitionProofDSNPrefersTheDeferredPartitionVariable(t *testing.T) {
	t.Setenv("ESHU_DEFERRED_PARTITION_PROOF_DSN", "postgres://deferred/postgres")
	t.Setenv("ESHU_LATEST_GENERATION_PROOF_DSN", "postgres://latest/postgres")
	if got := DeferredPartitionProofDSN(t); got != "postgres://deferred/postgres" {
		t.Fatalf("DeferredPartitionProofDSN = %q, want the deferred-partition DSN", got)
	}
}

func TestDeferredPartitionProofDSNFallsBackToTheLatestGenerationVariable(t *testing.T) {
	t.Setenv("ESHU_DEFERRED_PARTITION_PROOF_DSN", "")
	t.Setenv("ESHU_LATEST_GENERATION_PROOF_DSN", "postgres://latest/postgres")
	if got := DeferredPartitionProofDSN(t); got != "postgres://latest/postgres" {
		t.Fatalf("DeferredPartitionProofDSN = %q, want the latest-generation DSN", got)
	}
}

func TestIsolatedSchemaNameAndSearchPath(t *testing.T) {
	name := isolatedSchemaName("activation_proof")
	if !strings.HasPrefix(name, "activation_proof_") || len(name) <= len("activation_proof_") {
		t.Fatalf("isolatedSchemaName = %q, want activation_proof_<nanos>", name)
	}
	dsn, err := isolatedSchemaDSN("postgres://u@127.0.0.1:5432/postgres?sslmode=disable", name)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("search_path"); got != name+",public" {
		t.Fatalf("search_path = %q, want the isolated schema first, then public", got)
	}
	if got := parsed.Query().Get("sslmode"); got != "disable" {
		t.Fatalf("sslmode = %q, want the original query kept", got)
	}
}
