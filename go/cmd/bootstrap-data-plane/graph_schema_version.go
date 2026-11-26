// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// QueryBackendVersion implements graph.BackendVersionQuerier by delegating to
// the wrapped executor. A wrapped executor that cannot report a version fails
// the #11535 schema gate closed.
func (e *statementTimeoutExecutor) QueryBackendVersion(ctx context.Context) (string, error) {
	querier, ok := e.executor.(graph.BackendVersionQuerier)
	if !ok {
		return "", fmt.Errorf("schema executor %T cannot report the backend version", e.executor)
	}
	return querier.QueryBackendVersion(ctx)
}

// QueryBackendVersion implements graph.BackendVersionQuerier against the live
// backend: the #11535 schema gate admits the 1.5.x-gated statement list only
// on NornicDB >= 1.5 and fails closed on any other answer.
func (e *neo4jSchemaExecutor) QueryBackendVersion(ctx context.Context) (string, error) {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: e.databaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, graph.NornicDBBackendVersionCypher, nil)
	if err != nil {
		return "", fmt.Errorf("query nornicdb backend version: %w", err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return "", fmt.Errorf("collect nornicdb backend version: %w", err)
	}
	if len(records) == 0 || len(records[0].Values) == 0 {
		return "", fmt.Errorf("nornicdb backend version: dbms.components() returned no rows")
	}
	return graph.NornicDBComponentVersionFromCell(records[0].Values[0])
}
