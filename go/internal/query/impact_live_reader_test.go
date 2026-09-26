// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// impactLiveReader is the #5167 impact live tests' GraphQuery and schema
// executor over ESHU_NEO4J_URI. It is untagged and env-gated (the callers skip
// without ESHU_OCI_PROVE_LIVE), so it compiles on every go test run.
type impactLiveReader struct {
	driver neo4jdriver.DriverWithContext
}

// openImpactLiveReader connects to ESHU_NEO4J_URI and applies Eshu's schema
// for ESHU_LIVE_GRAPH_BACKEND (default nornicdb), as production bootstrap does.
func openImpactLiveReader(t *testing.T) impactLiveReader {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required (e.g. bolt://localhost:17998)")
	}
	backend := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND"))
	if backend == "" {
		backend = "nornicdb"
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}
	reader := impactLiveReader{driver: driver}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if err := graph.EnsureSchemaWithBackend(ctx, reader, logger, graph.SchemaBackend(backend)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return reader
}

// Run reads in an auto-commit read session, as the production Neo4jReader does.
func (r impactLiveReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return r.run(ctx, neo4jdriver.AccessModeRead, cypher, params)
}

// RunWrite runs one auto-commit write statement and returns its rows.
func (r impactLiveReader) RunWrite(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	return r.run(ctx, neo4jdriver.AccessModeWrite, cypher, params)
}

func (r impactLiveReader) run(ctx context.Context, mode neo4jdriver.AccessMode, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: mode})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		rows = append(rows, record.AsMap())
	}
	return rows, nil
}

// RunSingle returns the first row of Run, or nil.
func (r impactLiveReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// ExecuteCypher runs one write statement.
func (r impactLiveReader) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	_, err := neo4jdriver.ExecuteQuery(ctx, r.driver, stmt.Cypher, stmt.Parameters, neo4jdriver.EagerResultTransformer)
	return err
}
