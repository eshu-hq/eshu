// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestProjectorTerraformStateConfigMatchResolverLive is the real-backend
// regression test the #5443 P1 review finding required: two TerraformResource
// nodes sharing the same (repo_id, name) pair (no uniqueness constraint backs
// that pair -- tf_resource_unique is (name, path, line_number)), proving
// projectorTerraformStateConfigMatchResolver reports the ambiguity (count=2)
// instead of silently resolving to a single candidate, alongside a genuinely
// unique pair resolving to count=1. Opt-in, matching every other bolt-backed
// live test in this repository: set ESHU_CYPHER_BOLT_DSN (and optionally
// ESHU_CYPHER_BOLT_DATABASE) to run it against a running graph backend.
// Skipped otherwise.
func TestProjectorTerraformStateConfigMatchResolverLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if dsn == "" {
		t.Skip("ESHU_CYPHER_BOLT_DSN not set; skipping bolt integration test")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE"))
	if database == "" {
		database = "nornic"
	}

	driver, err := neo4jdriver.NewDriverWithContext(dsn, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open bolt driver %q: %v", dsn, err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })

	connCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(connCtx); err != nil {
		t.Fatalf("verify bolt connectivity %q: %v", dsn, err)
	}

	const (
		ambiguousRepoID = "repo-5443-p1-live-ambiguous"
		uniqueRepoID    = "repo-5443-p1-live-unique"
		address         = "aws_instance.web"
	)
	ctx := context.Background()
	writeSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: database,
	})
	t.Cleanup(func() { _ = writeSession.Close(context.Background()) })

	cleanup := func() {
		result, err := writeSession.Run(context.Background(),
			`MATCH (c:TerraformResource) WHERE c.repo_id IN $repo_ids DETACH DELETE c`,
			map[string]any{"repo_ids": []string{ambiguousRepoID, uniqueRepoID}},
		)
		if err != nil {
			t.Errorf("cleanup: %v", err)
			return
		}
		if _, err := result.Consume(context.Background()); err != nil {
			t.Errorf("consume cleanup: %v", err)
		}
	}
	t.Cleanup(cleanup)
	cleanup()

	seed := func(repoID, path string, line int) {
		t.Helper()
		result, err := writeSession.Run(ctx,
			`CREATE (c:TerraformResource {repo_id: $repo_id, name: $name, path: $path, line_number: $line})`,
			map[string]any{"repo_id": repoID, "name": address, "path": path, "line": line},
		)
		if err != nil {
			t.Fatalf("seed TerraformResource repo_id=%s path=%s: %v", repoID, path, err)
		}
		if _, err := result.Consume(ctx); err != nil {
			t.Fatalf("consume seed repo_id=%s path=%s: %v", repoID, path, err)
		}
	}
	// Two Terraform roots in the same repo both declaring "aws_instance.web":
	// the exact ambiguity shape the review finding described.
	seed(ambiguousRepoID, "envs/a/main.tf", 1)
	seed(ambiguousRepoID, "envs/b/main.tf", 1)
	// One Terraform root in a different repo: the unambiguous control case.
	seed(uniqueRepoID, "envs/a/main.tf", 1)

	resolver := projectorTerraformStateConfigMatchResolver{driver: driver, databaseName: database}
	counts, err := resolver.CountConfigMatchCandidates(ctx, []sourcecypher.TerraformStateConfigMatchQuery{
		{UID: "state-ambiguous", OwningRepoID: ambiguousRepoID, Address: address},
		{UID: "state-unique", OwningRepoID: uniqueRepoID, Address: address},
	})
	if err != nil {
		t.Fatalf("CountConfigMatchCandidates: %v", err)
	}

	if got, want := counts["state-ambiguous"], 2; got != want {
		t.Fatalf("ambiguous pair candidate count = %d, want %d (two TerraformResource nodes share this (repo_id, name) pair)", got, want)
	}
	if got, want := counts["state-unique"], 1; got != want {
		t.Fatalf("unique pair candidate count = %d, want %d", got, want)
	}
}
