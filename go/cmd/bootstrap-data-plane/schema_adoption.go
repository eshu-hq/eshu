// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const graphSchemaAdoptExistingEnv = "ESHU_GRAPH_SCHEMA_ADOPT_EXISTING"

type graphSchemaAdoptionMode int

const (
	graphSchemaAdoptionDisabled graphSchemaAdoptionMode = iota
	graphSchemaAdoptionOpportunistic
	graphSchemaAdoptionRequired
)

type graphSchemaInspector interface {
	GraphSchemaObjectNames(context.Context) (map[string]struct{}, error)
}

// graphSchemaAdoptionModeFromEnv returns the schema adoption policy for a
// marker-missing run. NornicDB defaults to opportunistic adoption because
// re-running CREATE CONSTRAINT IF NOT EXISTS against a large retained graph can
// spend minutes per constraint refreshing unique values.
func graphSchemaAdoptionModeFromEnv(getenv func(string) string, backend graph.SchemaBackend) graphSchemaAdoptionMode {
	raw := strings.ToLower(strings.TrimSpace(getenv(graphSchemaAdoptExistingEnv)))
	switch raw {
	case "1", "true", "t", "yes", "y", "on":
		return graphSchemaAdoptionRequired
	case "0", "false", "f", "no", "n", "off":
		return graphSchemaAdoptionDisabled
	case "":
		if backend == graph.SchemaBackendNornicDB {
			return graphSchemaAdoptionOpportunistic
		}
		return graphSchemaAdoptionDisabled
	default:
		return graphSchemaAdoptionDisabled
	}
}

func adoptExistingGraphSchema(
	ctx context.Context,
	db bootstrapExecutor,
	inspector graphSchemaInspector,
	logger *slog.Logger,
	app graph.SchemaApplication,
) (bool, map[string]struct{}, error) {
	if inspector == nil {
		return false, nil, fmt.Errorf("%s requires graph schema inspection support", graphSchemaAdoptExistingEnv)
	}
	expectedNames, err := expectedGraphSchemaObjectNames(app.Backend)
	if err != nil {
		return false, nil, err
	}
	actualNames, err := inspector.GraphSchemaObjectNames(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("inspect graph schema for adoption: %w", err)
	}
	missing := missingGraphSchemaObjectNames(expectedNames, actualNames)
	if len(missing) > 0 {
		if logger != nil {
			logger.Info(
				"graph schema adoption incomplete",
				telemetry.EventAttr("bootstrap.graph.adoption_incomplete"),
				"graph_backend", app.Backend,
				"schema_fingerprint", app.Fingerprint,
				"expected_schema_objects", len(expectedNames),
				"actual_schema_objects", len(actualNames),
				"missing_schema_objects", len(missing),
				"first_missing_schema_objects", firstStrings(missing, 10),
			)
		}
		return false, actualNames, nil
	}
	if err := markGraphSchemaApplied(ctx, db, app); err != nil {
		return false, actualNames, err
	}
	if logger != nil {
		logger.Info(
			"graph schema adopted",
			telemetry.EventAttr("bootstrap.graph.adopted"),
			"graph_backend", app.Backend,
			"schema_fingerprint", app.Fingerprint,
			"statement_count", app.StatementCount,
			"schema_object_count", len(actualNames),
		)
	}
	return true, actualNames, nil
}

type missingGraphSchemaExecutor struct {
	executor      graph.CypherExecutor
	existingNames map[string]struct{}
}

func (e *missingGraphSchemaExecutor) ExecuteCypher(
	ctx context.Context,
	statement graph.CypherStatement,
) error {
	name, err := graphSchemaObjectName(statement.Cypher)
	if err == nil {
		if _, exists := e.existingNames[name]; exists {
			return nil
		}
	}
	return e.executor.ExecuteCypher(ctx, statement)
}

func expectedGraphSchemaObjectNames(backend graph.SchemaBackend) (map[string]struct{}, error) {
	statements, err := graph.SchemaStatementsForBackend(backend)
	if err != nil {
		return nil, err
	}
	names := make(map[string]struct{}, len(statements))
	for _, statement := range statements {
		name, err := graphSchemaObjectName(statement)
		if err != nil {
			return nil, err
		}
		names[name] = struct{}{}
	}
	return names, nil
}

func graphSchemaObjectName(statement string) (string, error) {
	fields := strings.Fields(statement)
	if len(fields) > 0 && strings.EqualFold(fields[0], "CALL") {
		if name, ok := graphSchemaProcedureIndexName(statement); ok {
			return name, nil
		}
	}
	if len(fields) < 4 || !strings.EqualFold(fields[0], "CREATE") {
		return "", fmt.Errorf("cannot adopt unsupported graph schema statement %q", graphSchemaAdoptionStatementSummary(statement))
	}
	switch strings.ToUpper(fields[1]) {
	case "CONSTRAINT", "INDEX":
		if strings.EqualFold(fields[2], "IF") {
			return "", fmt.Errorf("cannot adopt unnamed graph schema statement %q", graphSchemaAdoptionStatementSummary(statement))
		}
		return fields[2], nil
	default:
		return "", fmt.Errorf("cannot adopt unsupported graph schema statement %q", graphSchemaAdoptionStatementSummary(statement))
	}
}

func graphSchemaProcedureIndexName(statement string) (string, bool) {
	const prefix = "db.index.fulltext.createNodeIndex("
	callIndex := strings.Index(statement, prefix)
	if callIndex < 0 {
		return "", false
	}
	rest := strings.TrimSpace(statement[callIndex+len(prefix):])
	if !strings.HasPrefix(rest, "'") {
		return "", false
	}
	rest = strings.TrimPrefix(rest, "'")
	name, _, ok := strings.Cut(rest, "'")
	name = strings.TrimSpace(name)
	return name, ok && name != ""
}

func graphSchemaAdoptionStatementSummary(statement string) string {
	return strings.Join(strings.Fields(statement), " ")
}

func missingGraphSchemaObjectNames(expected, actual map[string]struct{}) []string {
	missing := make([]string, 0)
	for name := range expected {
		if _, ok := actual[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func firstStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func (e *neo4jSchemaExecutor) GraphSchemaObjectNames(ctx context.Context) (map[string]struct{}, error) {
	names := make(map[string]struct{})
	for _, statement := range []string{"SHOW CONSTRAINTS", "SHOW INDEXES"} {
		if err := e.collectGraphSchemaNames(ctx, statement, names); err != nil {
			return nil, err
		}
	}
	return names, nil
}

func (e *neo4jSchemaExecutor) collectGraphSchemaNames(
	ctx context.Context,
	statement string,
	names map[string]struct{},
) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: e.databaseName,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, statement, nil)
	if err != nil {
		return fmt.Errorf("run %s: %w", statement, err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return fmt.Errorf("collect %s: %w", statement, err)
	}
	for _, record := range records {
		rawName, ok := record.Get("name")
		if !ok {
			continue
		}
		name, ok := rawName.(string)
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name != "" {
			names[name] = struct{}{}
		}
	}
	return nil
}
