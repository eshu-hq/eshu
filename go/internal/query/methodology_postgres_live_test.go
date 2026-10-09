// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestQueryMethodologyPostgresLive checks the pilot through the production
// read port, including independent outcomes and physically absent indexes.
func TestQueryMethodologyPostgresLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run disposable query methodology proof")
	}
	handle, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	handle.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = handle.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	seedCloudResourceListLiveCorpus(t, ctx, handle)
	applyMethodologyPostgresMigrations(t, ctx, handle)
	proof := runMethodologyPostgresProof(t, ctx, handle)
	if proof.Variants != 64 || proof.Cases < 96 {
		t.Fatalf("production coverage = %d variants/%d cases, want 64 variants and >=96 cases", proof.Variants, proof.Cases)
	}
	writeMethodologyPostgresArtifact(t, ctx, handle, proof)
	if path := os.Getenv("ESHU_QUERY_METHODOLOGY_POSTGRES_REPORT"); path != "" {
		encoded, err := json.MarshalIndent(proof, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
