// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

// Shared live-backend plumbing for #6786's scoped-grant proof
// (scoped_grant_live_test.go): applying Eshu's real schema before seeding,
// and resolving which backend/database a run targets from environment
// variables. NornicDB's read-predicate behavior differs materially with the
// schema applied versus a bare container -- see
// docs/internal/evidence/nornicdb-scoped-grant-predicates.md -- so every live
// test in this file's sibling MUST apply schema first.
package entity

import (
	"context"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/graph"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// liveGraphBackend resolves which schema dialect and default database a live
// scoped-grant test run targets, from ESHU_LIVE_GRAPH_BACKEND
// (nornicdb|neo4j, default nornicdb -- matching this epic's primary subject)
// and ESHU_NEO4J_DATABASE (default "nornic" for nornicdb, "neo4j" for neo4j).
func liveGraphBackend() (graph.SchemaBackend, string) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")))
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if backend == "neo4j" {
		if database == "" {
			database = "neo4j"
		}
		return graph.SchemaBackendNeo4j, database
	}
	if database == "" {
		database = "nornic"
	}
	return graph.SchemaBackendNornicDB, database
}

// liveSchemaExecutor adapts a Bolt driver session to graph.CypherExecutor so
// EnsureSchemaWithBackendStrict can apply the real schema DDL through the
// same driver and database a live test reads and writes through.
type liveSchemaExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

// ExecuteCypher satisfies graph.CypherExecutor.
func (e liveSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}
