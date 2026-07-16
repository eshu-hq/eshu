// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_graph_schema_delta_proof

package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/graphschemacompat"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

const functionLegacyIDIndexName = "nornicdb_function_legacy_id_lookup"

type countingSchemaExecutor struct {
	executor graph.CypherExecutor
	cyphers  []string
}

func (e *countingSchemaExecutor) ExecuteCypher(
	ctx context.Context,
	statement graph.CypherStatement,
) error {
	e.cyphers = append(e.cyphers, statement.Cypher)
	return e.executor.ExecuteCypher(ctx, statement)
}

func TestLiveNornicDBAppliesOnlyMissingSchemaDelta(t *testing.T) {
	entityID := os.Getenv("ESHU_PROOF_ENTITY_ID")
	if entityID == "" {
		t.Fatal("ESHU_PROOF_ENTITY_ID is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	deps, err := openNeo4j(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	defer func() {
		if err := deps.close(); err != nil {
			t.Errorf("close graph: %v", err)
		}
	}()

	existing, err := deps.inspector.GraphSchemaObjectNames(ctx)
	if err != nil {
		t.Fatalf("inspect graph schema before delta: %v", err)
	}
	_, existedBefore := existing[functionLegacyIDIndexName]
	counting := &countingSchemaExecutor{executor: deps.executor}
	delta := &missingGraphSchemaExecutor{executor: counting, existingNames: existing}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	started := time.Now()
	if err := graph.EnsureSchemaWithBackendStrict(
		ctx,
		delta,
		logger,
		graph.SchemaBackendNornicDB,
	); err != nil {
		t.Fatalf("apply missing graph schema delta: %v", err)
	}
	applyDuration := time.Since(started)
	wantCalls := 1
	if existedBefore {
		wantCalls = 0
	}
	if len(counting.cyphers) != wantCalls {
		t.Fatalf("executed graph DDL statements = %d, want %d", len(counting.cyphers), wantCalls)
	}
	for _, cypher := range counting.cyphers {
		name, err := graphSchemaObjectName(cypher)
		if err != nil || name != functionLegacyIDIndexName {
			t.Fatalf("executed unexpected graph DDL %q (name=%q err=%v)", cypher, name, err)
		}
	}

	after, err := deps.inspector.GraphSchemaObjectNames(ctx)
	if err != nil {
		t.Fatalf("inspect graph schema after delta: %v", err)
	}
	if _, ok := after[functionLegacyIDIndexName]; !ok {
		t.Fatalf("graph schema missing %s after delta", functionLegacyIDIndexName)
	}
	repeat := &countingSchemaExecutor{executor: deps.executor}
	if err := graph.EnsureSchemaWithBackendStrict(
		ctx,
		&missingGraphSchemaExecutor{executor: repeat, existingNames: after},
		logger,
		graph.SchemaBackendNornicDB,
	); err != nil {
		t.Fatalf("repeat missing-only graph schema delta: %v", err)
	}
	if len(repeat.cyphers) != 0 {
		t.Fatalf("repeat graph DDL statements = %d, want 0", len(repeat.cyphers))
	}

	schemaExecutor, ok := deps.executor.(*neo4jSchemaExecutor)
	if !ok {
		t.Fatalf("graph executor = %T, want *neo4jSchemaExecutor", deps.executor)
	}
	collisionDuration := runFunctionLegacyIDCollisionProof(t, ctx, schemaExecutor, entityID)

	db, err := runtimecfg.OpenPostgres(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open postgres marker store: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close postgres marker store: %v", err)
		}
	}()
	app, err := graph.SchemaApplicationForBackend(graph.SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("load graph schema application: %v", err)
	}
	if err := graphschemacompat.MarkApplied(ctx, db, app); err != nil {
		t.Fatalf("mark graph schema applied: %v", err)
	}

	t.Logf(
		"existed_before=%t executed_ddl=%d apply_seconds=%.6f repeat_ddl=0 collision_seconds=%.6f fingerprint=%s statements=%d",
		existedBefore,
		len(counting.cyphers),
		applyDuration.Seconds(),
		collisionDuration.Seconds(),
		app.Fingerprint,
		app.StatementCount,
	)
}

func runFunctionLegacyIDCollisionProof(
	t *testing.T,
	ctx context.Context,
	executor *neo4jSchemaExecutor,
	entityID string,
) time.Duration {
	t.Helper()
	session := executor.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: executor.databaseName,
	})
	defer func() {
		if err := session.Close(ctx); err != nil {
			t.Errorf("close collision proof session: %v", err)
		}
	}()
	started := time.Now()
	result, err := session.Run(ctx,
		"MATCH (idAnchor:Function {id: $entity_id}) "+
			"WHERE coalesce(idAnchor.uid, '') <> $entity_id "+
			"RETURN true AS collision LIMIT 1",
		map[string]any{"entity_id": entityID},
	)
	if err != nil {
		t.Fatalf("run Function legacy-ID collision proof: %v", err)
	}
	if _, err := result.Collect(ctx); err != nil {
		t.Fatalf("collect Function legacy-ID collision proof: %v", err)
	}
	return time.Since(started)
}
