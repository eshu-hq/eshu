// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

type infraSearchKeyRecorder struct{ calls []sourcecypher.Statement }

func (r *infraSearchKeyRecorder) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	r.calls = append(r.calls, stmt)
	return nil
}

// TestReducerWiringResolvesInfraSearchKeyFromGraphBackend proves the reducer
// resolves the #7245 dialect from the configured graph backend through
// loadReducerGraphBackend, the helper buildReducerService calls, and that the
// two writers
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
		backend, searchKey, err := loadReducerGraphBackend(func(key string) string {
			if key == "ESHU_GRAPH_BACKEND" {
				return tc.env
			}
			return ""
		})
		if err != nil {
			t.Fatalf("loadReducerGraphBackend(%s) error = %v", tc.env, err)
		}
		if string(backend) != tc.env {
			t.Fatalf("loadReducerGraphBackend(%s) backend = %q", tc.env, backend)
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

// TestReducerCanonicalGraphWritersCarryInfraSearchKeyDialect proves the
// dialect buildReducerService resolves reaches the gated cloudResourceNode and
// ec2InstanceNode writers through newReducerCanonicalGraphWriters: both key on
// Neo4j and neither keys on NornicDB.
func TestReducerCanonicalGraphWritersCarryInfraSearchKeyDialect(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{"uid": "u1", "name": "n"}}
	for _, tc := range []struct {
		env   string
		keyed int
	}{{"neo4j", 2}, {"nornicdb", 0}} {
		_, searchKey, err := loadReducerGraphBackend(func(key string) string {
			if key == "ESHU_GRAPH_BACKEND" {
				return tc.env
			}
			return ""
		})
		if err != nil {
			t.Fatalf("loadReducerGraphBackend(%s) error = %v", tc.env, err)
		}
		rec := &infraSearchKeyRecorder{}
		writers := newReducerCanonicalGraphWriters(rec, nil, 0, nil, nil, searchKey)
		if err := writers.cloudResourceNode.WriteCloudResourceNodes(context.Background(), rows, "x"); err != nil {
			t.Fatal(err)
		}
		if err := writers.ec2InstanceNode.WriteEC2InstanceNodes(context.Background(), rows, "x"); err != nil {
			t.Fatal(err)
		}
		if len(rec.calls) != 2 {
			t.Fatalf("%s executed %d statements, want 2", tc.env, len(rec.calls))
		}
		keyed := 0
		for _, call := range rec.calls {
			if strings.Contains(call.Cypher, search.Clause("r")) {
				keyed++
			}
		}
		if keyed != tc.keyed {
			t.Errorf("%s keyed statements = %d, want %d", tc.env, keyed, tc.keyed)
		}
	}
}
