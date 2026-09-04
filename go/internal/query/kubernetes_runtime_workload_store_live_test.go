// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestKubernetesRuntimeWorkloadGateIndependentTruthSeamsLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live Kubernetes runtime gate proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	seedKubernetesRuntimeWorkloadGateLive(t, ctx, db)

	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	candidates := []KubernetesRuntimeCandidate{
		{WorkloadUID: "accepted", Digest: digest, EdgeScopeID: "edge-allowed", EdgeGenerationID: "edge-allowed-gen"},
		{WorkloadUID: "owner-denied", Digest: digest, EdgeScopeID: "edge-allowed", EdgeGenerationID: "edge-allowed-gen"},
		{WorkloadUID: "edge-denied", Digest: digest, EdgeScopeID: "edge-denied", EdgeGenerationID: "edge-denied-gen"},
		{WorkloadUID: "owner-stale", Digest: digest, EdgeScopeID: "edge-allowed", EdgeGenerationID: "edge-allowed-gen"},
		{WorkloadUID: "owner-tombstoned", Digest: digest, EdgeScopeID: "edge-allowed", EdgeGenerationID: "edge-allowed-gen"},
		{WorkloadUID: "edge-superseded", Digest: digest, EdgeScopeID: "edge-superseded", EdgeGenerationID: "edge-old-gen"},
	}
	store := NewPostgresKubernetesRuntimeWorkloadStore(db)
	got, err := store.CurrentAuthorizedKubernetesRuntimeWorkloads(
		ctx, candidates, false, nil, []string{"owner-allowed", "owner-stale", "edge-allowed", "edge-superseded"},
	)
	if err != nil {
		t.Fatalf("current authorized Kubernetes runtime workloads: %v", err)
	}
	want := []KubernetesRuntimeWorkloadMatch{{
		Digest: digest,
		WorkloadRef: impact.KubernetesRuntimeWorkloadRef{
			UID: "accepted", ClusterID: "cluster-from-owner", Namespace: "payments", Name: "api",
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gate rows = %#v, want only independent-current-scope acceptance %#v", got, want)
	}

	query, args := buildKubernetesRuntimeWorkloadQuery(candidates, false, nil, []string{
		"owner-allowed", "owner-stale", "edge-allowed", "edge-superseded",
	})
	var plan string
	if err := db.QueryRowContext(ctx, "EXPLAIN (FORMAT TEXT) "+query, args...).Scan(&plan); err != nil {
		t.Fatalf("explain gate query: %v", err)
	}
	t.Logf("independent owner/edge gate rows=%d; first plan row=%s", len(got), plan)
}

func seedKubernetesRuntimeWorkloadGateLive(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`CREATE TEMP TABLE ingestion_scopes (
			scope_id text PRIMARY KEY, scope_kind text NOT NULL, source_key text NOT NULL,
			active_generation_id text NULL
		)`,
		`CREATE TEMP TABLE scope_generations (
			generation_id text PRIMARY KEY, scope_id text NOT NULL, status text NOT NULL
		)`,
		`CREATE TEMP TABLE fact_records (
			fact_id text PRIMARY KEY, scope_id text NOT NULL, generation_id text NOT NULL,
			is_tombstone boolean NOT NULL
		)`,
		`CREATE TEMP TABLE graph_node_owner (
			uid text PRIMARY KEY, winning_row jsonb NOT NULL
		)`,
		`INSERT INTO ingestion_scopes(scope_id, scope_kind, source_key, active_generation_id) VALUES
			('owner-allowed','cluster','cluster-a','owner-allowed-gen'),
			('owner-denied','cluster','cluster-b','owner-denied-gen'),
			('owner-stale','cluster','cluster-c','owner-stale-new-gen'),
			('edge-allowed','repository','repository:edge-allowed','edge-allowed-gen'),
			('edge-denied','repository','repository:edge-denied','edge-denied-gen'),
			('edge-superseded','repository','repository:edge-superseded','edge-new-gen')`,
		`INSERT INTO scope_generations(generation_id, scope_id, status) VALUES
			('owner-allowed-gen','owner-allowed','active'),
			('owner-denied-gen','owner-denied','active'),
			('owner-stale-old-gen','owner-stale','superseded'),
			('owner-stale-new-gen','owner-stale','active'),
			('edge-allowed-gen','edge-allowed','active'),
			('edge-denied-gen','edge-denied','active'),
			('edge-old-gen','edge-superseded','superseded'),
			('edge-new-gen','edge-superseded','active')`,
		`INSERT INTO fact_records(fact_id, scope_id, generation_id, is_tombstone) VALUES
			('fact-accepted','owner-allowed','owner-allowed-gen',false),
			('fact-owner-denied','owner-denied','owner-denied-gen',false),
			('fact-edge-denied','owner-allowed','owner-allowed-gen',false),
			('fact-owner-stale','owner-stale','owner-stale-old-gen',false),
			('fact-owner-tombstoned','owner-allowed','owner-allowed-gen',true),
			('fact-edge-superseded','owner-allowed','owner-allowed-gen',false)`,
		`INSERT INTO graph_node_owner(uid, winning_row) VALUES
			('accepted', '{"source_fact_id":"fact-accepted","cluster_id":"cluster-from-owner","namespace":"payments","name":"api"}'),
			('owner-denied', '{"source_fact_id":"fact-owner-denied","cluster_id":"denied","namespace":"denied","name":"denied"}'),
			('edge-denied', '{"source_fact_id":"fact-edge-denied","cluster_id":"denied","namespace":"denied","name":"denied"}'),
			('owner-stale', '{"source_fact_id":"fact-owner-stale","cluster_id":"stale","namespace":"stale","name":"stale"}'),
			('owner-tombstoned', '{"source_fact_id":"fact-owner-tombstoned","cluster_id":"tomb","namespace":"tomb","name":"tomb"}'),
			('edge-superseded', '{"source_fact_id":"fact-edge-superseded","cluster_id":"old","namespace":"old","name":"old"}')`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed live Kubernetes runtime gate: %v\n%s", err, statement)
		}
	}
}
