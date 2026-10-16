// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

// infraSearchKeyRecorder records the statements a canonical write executes.
type infraSearchKeyRecorder struct{ calls []sourcecypher.Statement }

func (r *infraSearchKeyRecorder) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	r.calls = append(r.calls, stmt)
	return nil
}

// infraSearchKeyMainHashes are the SHA-256 of the TerraformVariable entity
// upsert in each canonical entity mode on origin/main before #7245 (default,
// inline containment, batched inline containment), the same goldens
// internal/storage/cypher pins.
var infraSearchKeyMainHashes = map[string]bool{
	"2bdec555e4dbaefa13d56bdd61cb684fa3647034207325c9e5bb583c5ac31d1d": true,
	"2351870d9149971c85d75181e09f8143c5d58c8fa56a1cab17890f448deb6b72": true,
	"ac6413409f387b180872858f56b9bdc5d7ed842f28794c27548d85df7bf42958": true,
}

// TestIngesterWiringResolvesInfraSearchKeyFromGraphBackend proves this binary's
// canonical writer composition (the dialect-resolving builder plus its
// backend configuration) writes the #7245 key on Neo4j and emits main's
// statement byte for byte on NornicDB.
func TestIngesterWiringResolvesInfraSearchKeyFromGraphBackend(t *testing.T) {
	t.Parallel()

	mat := canonical.CanonicalMaterialization{
		ScopeID: "scope-7245", GenerationID: "gen-7245", RepoID: "repo-7245",
		Repository: &canonical.RepositoryRow{RepoID: "repo-7245", Name: "r", Path: "/repos/r"},
		Files:      []canonical.FileRow{{Path: "/repos/r/main.tf", RelativePath: "main.tf", Name: "main.tf", Language: "hcl", RepoID: "repo-7245", DirPath: "/repos/r"}},
		Entities:   []canonical.EntityRow{{EntityID: "tfvar-1", Label: "TerraformVariable", EntityName: "region", FilePath: "/repos/r/main.tf", RelativePath: "main.tf", StartLine: 1, EndLine: 3, Language: "hcl", RepoID: "repo-7245"}},
	}
	for _, tc := range []struct {
		backend runtimecfg.GraphBackend
		keyed   bool
	}{{runtimecfg.GraphBackendNeo4j, true}, {runtimecfg.GraphBackendNornicDB, false}} {
		rec := &infraSearchKeyRecorder{}
		writer, err := newIngesterCanonicalNodeWriter(rec, tc.backend, 500, nil)
		if err != nil {
			t.Fatalf("%s builder error = %v", tc.backend, err)
		}
		writer = configureIngesterCanonicalWriter(writer, ingesterCanonicalWriterConfig{GraphBackend: tc.backend})
		if err := writer.Write(context.Background(), mat); err != nil {
			t.Fatalf("%s Write() error = %v", tc.backend, err)
		}
		found := false
		for _, call := range rec.calls {
			if !strings.Contains(call.Cypher, "MERGE (n:TerraformVariable {uid") {
				continue
			}
			found = true
			if got := strings.HasSuffix(call.Cypher, "\n"+search.Clause("n")); got != tc.keyed {
				t.Errorf("%s entity upsert keyed = %v, want %v", tc.backend, got, tc.keyed)
			}
			if !tc.keyed {
				sum := sha256.Sum256([]byte(call.Cypher))
				if !infraSearchKeyMainHashes[hex.EncodeToString(sum[:])] || strings.Contains(call.Cypher, graph.InfraSearchKeyProperty) {
					t.Errorf("NornicDB entity upsert differs from main:\n%s", call.Cypher)
				}
			}
		}
		if !found {
			t.Fatalf("%s wrote no TerraformVariable upsert", tc.backend)
		}
	}
	if _, err := newIngesterCanonicalNodeWriter(&infraSearchKeyRecorder{}, "memgraph", 500, nil); err == nil {
		t.Fatal("builder accepted an unknown graph backend")
	}
}
