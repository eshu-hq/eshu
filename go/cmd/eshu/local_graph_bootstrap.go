package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

var localGraphApplySchema = applyLocalGraphSchema

func applyLocalGraphBootstrap(
	ctx context.Context,
	runtimeConfig localHostRuntimeConfig,
	managedGraph *managedLocalGraph,
) error {
	if runtimeConfig.Profile != query.ProfileLocalAuthoritative || managedGraph == nil {
		return nil
	}
	backend, err := localGraphSchemaBackend(runtimeConfig, managedGraph)
	if err != nil {
		return err
	}
	return localGraphApplySchema(ctx, localGraphSchemaEnv(runtimeConfig, managedGraph), backend)
}

func localGraphSchemaBackend(
	runtimeConfig localHostRuntimeConfig,
	managedGraph *managedLocalGraph,
) (graph.SchemaBackend, error) {
	if runtimeConfig.GraphBackend != managedGraph.Backend {
		return "", fmt.Errorf(
			"local graph backend mismatch: runtime=%q managed=%q",
			runtimeConfig.GraphBackend,
			managedGraph.Backend,
		)
	}
	switch managedGraph.Backend {
	case query.GraphBackendNornicDB:
		return graph.SchemaBackendNornicDB, nil
	default:
		return "", fmt.Errorf("local graph backend %q does not support schema bootstrap", managedGraph.Backend)
	}
}

func localGraphSchemaEnv(runtimeConfig localHostRuntimeConfig, managedGraph *managedLocalGraph) func(string) string {
	overrides := graphEnvOverrides(managedGraph)
	if overrides == nil {
		overrides = map[string]string{}
	}
	if runtimeConfig.GraphBackend != "" {
		overrides["ESHU_GRAPH_BACKEND"] = string(runtimeConfig.GraphBackend)
	}
	return func(key string) string {
		if value, ok := overrides[key]; ok {
			return value
		}
		return os.Getenv(key)
	}
}

func applyLocalGraphSchema(
	ctx context.Context,
	getenv func(string) string,
	backend graph.SchemaBackend,
) (err error) {
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, getenv)
	if err != nil {
		return fmt.Errorf("open local graph schema connection: %w", err)
	}
	defer func() {
		if closeErr := driver.Close(context.Background()); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	executor := localGraphSchemaExecutor{
		driver:       driver,
		databaseName: cfg.DatabaseName,
	}
	if err = graph.EnsureSchemaWithBackend(ctx, executor, slog.Default(), backend); err != nil {
		return fmt.Errorf("apply local graph schema: %w", err)
	}
	return nil
}

type localGraphSchemaExecutor struct {
	driver       neo4jdriver.DriverWithContext
	databaseName string
}

func (e localGraphSchemaExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.databaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}
