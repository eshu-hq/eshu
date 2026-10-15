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
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

type infraSearchKeyRecorder struct{ calls []sourcecypher.Statement }

func (r *infraSearchKeyRecorder) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	r.calls = append(r.calls, stmt)
	return nil
}

// TestReducerWiringResolvesInfraSearchKeyFromGraphBackend proves the reducer
// resolves the #7245 dialect from the configured graph backend (the same
// runtimecfg.LoadGraphBackend value main.go passes) and that the two writers
// it builds for searched CloudResource fields key on Neo4j only, emitting
// main's statement on NornicDB.
func TestReducerWiringResolvesInfraSearchKeyFromGraphBackend(t *testing.T) {
	t.Parallel()

	const cloudMainHash = "733703b08b79e1e1006830393bb69912d70c635cb19ab555d06fd1603d0e6a63"
	const ec2MainHash = "6cf550675ec03363a2ec37fdc83c13a5d33cd6d294a0e3bd909596101ebce8c7"
	rows := []map[string]any{{"uid": "u1", "name": "n"}}
	for _, tc := range []struct {
		env   string
		keyed bool
	}{{"neo4j", true}, {"nornicdb", false}} {
		backend, err := runtimecfg.LoadGraphBackend(func(key string) string {
			if key == "ESHU_GRAPH_BACKEND" {
				return tc.env
			}
			return ""
		})
		if err != nil {
			t.Fatalf("LoadGraphBackend(%s) error = %v", tc.env, err)
		}
		searchKey, err := search.DialectForBackend(graph.SchemaBackend(backend))
		if err != nil {
			t.Fatalf("DialectForBackend(%s) error = %v", backend, err)
		}
		rec := &infraSearchKeyRecorder{}
		cloud, ec2 := newSearchKeyedCloudNodeWriters(rec, 0, searchKey)
		if err := cloud.WriteCloudResourceNodes(context.Background(), rows, "x"); err != nil {
			t.Fatal(err)
		}
		if err := ec2.WriteEC2InstanceNodes(context.Background(), rows, "x"); err != nil {
			t.Fatal(err)
		}
		if len(rec.calls) != 2 {
			t.Fatalf("%s executed %d statements, want 2", tc.env, len(rec.calls))
		}
		for i, wantMain := range []string{cloudMainHash, ec2MainHash} {
			cypher := rec.calls[i].Cypher
			if got := strings.HasSuffix(cypher, "\n"+search.Clause("r")); got != tc.keyed {
				t.Errorf("%s statement %d keyed = %v, want %v", tc.env, i, got, tc.keyed)
			}
			if sum := sha256.Sum256([]byte(cypher)); !tc.keyed && hex.EncodeToString(sum[:]) != wantMain {
				t.Errorf("%s statement %d differs from main:\n%s", tc.env, i, cypher)
			}
		}
	}
}
